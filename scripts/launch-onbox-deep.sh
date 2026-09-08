#!/usr/bin/env bash
# Launch a detached on-box Deep Work supervisor from an operator machine.
#
# Required environment: STINT_BOX_HOST, STINT_BOX_PORT, STINT_BOX_KEY,
# STINT_MISSION, and STINT_REPO. The script exits after the remote supervisor
# reports RUNNING; it never starts a local Deep Work coordinator or model tunnel.
set -Eeuo pipefail

ROOT="${STINT_REMOTE_ROOT:-/var/lib/stint-onbox}"
BIN="${STINT_BIN:-./bin/stint}"
SUPERVISOR_LOCAL="${STINT_SUPERVISOR_LOCAL:-scripts/onbox-deep-supervisor.sh}"
R2_SYNC_LOCAL="${STINT_R2_SYNC_LOCAL:-scripts/onbox-r2-sync.py}"
R2_ARCHIVE_LOCAL="${STINT_R2_ARCHIVE_LOCAL:-scripts/onbox-r2-archive.py}"
MISSION_LOCAL="${STINT_MISSION:-}"
REPO_LOCAL="${STINT_REPO:-}"
HOST="${STINT_BOX_HOST:-}"
PORT="${STINT_BOX_PORT:-22}"
KEY="${STINT_BOX_KEY:-}"
CLIENTS="${STINT_ONBOX_CLIENTS:-1}"
REMOTE_BIN="$ROOT/bin/stint"
REMOTE_SUPERVISOR="$ROOT/onbox-deep-supervisor.sh"
REMOTE_R2_SYNC="$ROOT/onbox-r2-sync.py"
REMOTE_R2_ARCHIVE="$ROOT/onbox-r2-archive.py"
REMOTE_MISSION="$ROOT/mission.md"
REMOTE_REPO="$ROOT/repo"
REMOTE_READY="$ROOT/runtime/RUNNING.json"

die() { echo "ONBOX_LAUNCH_FAIL $*" >&2; exit 1; }
[ -n "$HOST" ] || die "STINT_BOX_HOST is required"
[ -n "$KEY" ] || die "STINT_BOX_KEY is required"
[ -n "$MISSION_LOCAL" ] && [ -r "$MISSION_LOCAL" ] || die "STINT_MISSION must name a readable mission"
[ -n "$REPO_LOCAL" ] && [ -d "$REPO_LOCAL/.git" ] || die "STINT_REPO must name a git repository"
[ -x "$BIN" ] || die "stint binary is missing or not executable: $BIN"
[ -x "$SUPERVISOR_LOCAL" ] || die "supervisor script is missing or not executable: $SUPERVISOR_LOCAL"

SSH=(ssh -i "$KEY" -p "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "root@$HOST")
RSYNC_SSH="ssh -i $KEY -p $PORT -o BatchMode=yes -o StrictHostKeyChecking=accept-new"

if [ -z "${STINT_INSTANCE_ID:-}" ] || [ -z "${STINT_DEADLINE:-}" ]; then
  session_json="${STINT_SESSION_JSON:-$HOME/.local/state/stint/session.json}"
  [ -r "$session_json" ] || die "set STINT_INSTANCE_ID and STINT_DEADLINE or provide $session_json"
  mapfile -t session_values < <(python3 - "$session_json" <<'PY'
import json, sys
state = json.load(open(sys.argv[1], encoding="utf-8"))
print(state.get("instanceId", ""))
print(state.get("deadline", ""))
PY
)
  STINT_INSTANCE_ID="${STINT_INSTANCE_ID:-${session_values[0]:-}}"
  STINT_DEADLINE="${STINT_DEADLINE:-${session_values[1]:-}}"
fi
[ -n "${STINT_INSTANCE_ID:-}" ] || die "STINT_INSTANCE_ID is required for the on-box watchdog"
[ -n "${STINT_DEADLINE:-}" ] || die "STINT_DEADLINE is required for the on-box watchdog"

echo "transferring pinned Stint runtime and mission repository"
"${SSH[@]}" "mkdir -p '$ROOT/bin' '$ROOT/runtime' '$ROOT/config' '$ROOT/state' /root/.config/stint && chmod 700 '$ROOT' '$ROOT/config' '$ROOT/state' '$ROOT/runtime' /root/.config/stint"
scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$BIN" "root@$HOST:$REMOTE_BIN"
scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$SUPERVISOR_LOCAL" "root@$HOST:$REMOTE_SUPERVISOR"
scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$MISSION_LOCAL" "root@$HOST:$REMOTE_MISSION"
"${SSH[@]}" "chmod 0755 '$REMOTE_BIN' '$REMOTE_SUPERVISOR'"
"${SSH[@]}" "rm -rf '$REMOTE_REPO' && mkdir -p '$REMOTE_REPO'"
rsync -a --delete -e "$RSYNC_SSH" "$REPO_LOCAL/" "root@$HOST:$REMOTE_REPO/"

# Copying this file is explicit because it enables remote deadline destroy.
if [ -n "${STINT_VAST_CREDENTIALS:-}" ]; then
  [ -r "$STINT_VAST_CREDENTIALS" ] || die "STINT_VAST_CREDENTIALS is not readable"
  scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$STINT_VAST_CREDENTIALS" "root@$HOST:/root/.config/stint/credentials.json"
  "${SSH[@]}" "chmod 0600 /root/.config/stint/credentials.json"
fi

if [ -n "${STINT_R2_ENV_FILE:-}" ]; then
  [ -r "$STINT_R2_ENV_FILE" ] || die "STINT_R2_ENV_FILE is not readable"
  [ -x "$R2_SYNC_LOCAL" ] || die "R2 sync helper is missing or not executable: $R2_SYNC_LOCAL"
  [ -x "$R2_ARCHIVE_LOCAL" ] || die "R2 archive helper is missing or not executable: $R2_ARCHIVE_LOCAL"
  scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$R2_SYNC_LOCAL" "root@$HOST:$REMOTE_R2_SYNC"
  scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$R2_ARCHIVE_LOCAL" "root@$HOST:$REMOTE_R2_ARCHIVE"
  scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$STINT_R2_ENV_FILE" "root@$HOST:$ROOT/config/r2.env"
  "${SSH[@]}" "chmod 0700 '$REMOTE_R2_SYNC' '$REMOTE_R2_ARCHIVE' '$ROOT/config/r2.env' && python3 -c 'import boto3' 2>/dev/null || python3 -m pip install --quiet --user boto3"
fi

args=(--mission "$REMOTE_MISSION" --repo "$REMOTE_REPO" --deadline "$STINT_DEADLINE" \
  --provider "${STINT_ONBOX_PROVIDER:-custom:qwen-stint-{reasoning}}" \
  --model "${STINT_ONBOX_MODEL:-qwen3.8-27b}" \
  --reasoning "${STINT_ONBOX_REASONING:-medium}" \
  --task-timeout "${STINT_ONBOX_TASK_TIMEOUT:-15m}" \
  --max-attempts "${STINT_ONBOX_MAX_ATTEMPTS:-2}" \
  --ready-file "$REMOTE_READY")
[ -n "${STINT_ONBOX_ACTION_PLAN:-}" ] && args+=(--action-plan "$STINT_ONBOX_ACTION_PLAN")

echo "starting detached on-box supervisor"
remote_env=("STINT_ONBOX_BIN=$REMOTE_BIN" "STINT_ONBOX_ROOT=$ROOT" \
  "STINT_ONBOX_READY_FILE=$REMOTE_READY" "STINT_ONBOX_INSTANCE_ID=$STINT_INSTANCE_ID" \
  "STINT_ONBOX_DEADLINE=$STINT_DEADLINE" "STINT_ONBOX_CLIENTS=$CLIENTS")
[ -n "${STINT_R2_ENV_FILE:-}" ] && remote_env+=("STINT_ONBOX_R2_SYNC=$REMOTE_R2_SYNC" "STINT_ONBOX_R2_ARCHIVE=$REMOTE_R2_ARCHIVE" "STINT_R2_ENV_FILE=$ROOT/config/r2.env")
[ -n "${STINT_ONBOX_SKIP_WATCHDOG:-}" ] && remote_env+=("STINT_ONBOX_SKIP_WATCHDOG=$STINT_ONBOX_SKIP_WATCHDOG")
remote_start=(env "${remote_env[@]}" "$REMOTE_SUPERVISOR" start -- "${args[@]}")
remote_start_cmd="$(printf '%q ' "${remote_start[@]}")"
"${SSH[@]}" "$remote_start_cmd"

for _ in $(seq 1 90); do
  status="$(${SSH[@]} "env STINT_ONBOX_ROOT='$ROOT' STINT_ONBOX_BIN='$REMOTE_BIN' '$REMOTE_SUPERVISOR' status" 2>/dev/null || true)"
  if printf '%s\n' "$status" | grep -q '^ONBOX_SUPERVISOR_RUNNING' && \
     printf '%s\n' "$status" | grep -q '"status":"RUNNING"'; then
    printf '%s\n' "$status" | sed -n '1,3p'
    echo "on-box supervisor is RUNNING; the operator machine may disconnect"
    exit 0
  fi
  sleep 5
done
die "on-box supervisor did not report RUNNING; inspect $ROOT/supervisor.log over SSH"
