#!/usr/bin/env bash
# Launch a detached on-box Deep Work supervisor from an operator machine.
#
# Required environment: STINT_BOX_HOST, STINT_BOX_PORT, STINT_BOX_KEY,
# STINT_MISSION, STINT_REPO, STINT_GITHUB_TOKEN_FILE,
# STINT_GITHUB_REPOSITORY, and STINT_GITHUB_BASE. The script exits after the
# remote supervisor reports RUNNING; it never starts a local Deep Work
# coordinator, uploader, git publisher, or model tunnel.
set -Eeuo pipefail

ROOT="${STINT_REMOTE_ROOT:-/var/lib/stint-onbox}"
BIN="${STINT_BIN:-./bin/stint}"
SUPERVISOR_LOCAL="${STINT_SUPERVISOR_LOCAL:-scripts/onbox-deep-supervisor.sh}"
R2_SYNC_LOCAL="${STINT_R2_SYNC_LOCAL:-scripts/onbox-r2-sync.py}"
R2_ARCHIVE_LOCAL="${STINT_R2_ARCHIVE_LOCAL:-scripts/onbox-r2-archive.py}"
GITHUB_PUBLISH_LOCAL="${STINT_GITHUB_PUBLISH_LOCAL:-scripts/onbox-github-publish.py}"
MISSION_LOCAL="${STINT_MISSION:-}"
REPO_LOCAL="${STINT_REPO:-}"
HOST="${STINT_BOX_HOST:-}"
PORT="${STINT_BOX_PORT:-22}"
KEY="${STINT_BOX_KEY:-}"
CLIENTS="${STINT_ONBOX_CLIENTS:-1}"
SKIP_GITHUB="${STINT_ONBOX_SKIP_GITHUB:-0}"
GITHUB_TOKEN_LOCAL="${STINT_GITHUB_TOKEN_FILE:-}"
GITHUB_REPOSITORY="${STINT_GITHUB_REPOSITORY:-}"
GITHUB_BASE="${STINT_GITHUB_BASE:-}"
ACTION_PLAN_LOCAL="${STINT_ONBOX_ACTION_PLAN:-}"
ACTION_PLAN_TARGET="${STINT_ONBOX_ACTION_PLAN_PATH:-}"
REMOTE_BIN="$ROOT/bin/stint"
REMOTE_SUPERVISOR="$ROOT/onbox-deep-supervisor.sh"
REMOTE_R2_SYNC="$ROOT/onbox-r2-sync.py"
REMOTE_R2_ARCHIVE="$ROOT/onbox-r2-archive.py"
REMOTE_GITHUB_PUBLISH="$ROOT/onbox-github-publish.py"
REMOTE_GITHUB_TOKEN="$ROOT/config/github.token"
REMOTE_MISSION="$ROOT/mission.md"
REMOTE_ACTION_PLAN_SEED="$ROOT/action-plan.seed.md"
REMOTE_REPO="$ROOT/repo"
REMOTE_READY="$ROOT/runtime/RUNNING.json"
TOKEN_TMP=""
REPO_STAGE=""

die() { echo "ONBOX_LAUNCH_FAIL $*" >&2; exit 1; }
cleanup_local() {
  [ -z "$TOKEN_TMP" ] || rm -f "$TOKEN_TMP"
  [ -z "$REPO_STAGE" ] || rm -rf "$REPO_STAGE"
}
trap cleanup_local EXIT

[ -n "$HOST" ] || die "STINT_BOX_HOST is required"
[ -n "$KEY" ] || die "STINT_BOX_KEY is required"
[ -n "$MISSION_LOCAL" ] && [ -r "$MISSION_LOCAL" ] || die "STINT_MISSION must name a readable mission"
[ -n "$REPO_LOCAL" ] && git -C "$REPO_LOCAL" rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "STINT_REPO must name a git repository or linked worktree"
SOURCE_HEAD="$(git -C "$REPO_LOCAL" rev-parse HEAD)"
SOURCE_ORIGIN="$(git -C "$REPO_LOCAL" remote get-url origin 2>/dev/null || true)"
[ -n "$SOURCE_ORIGIN" ] || die "STINT_REPO must have an origin remote"
if [ -n "$ACTION_PLAN_LOCAL" ]; then
  case "$ACTION_PLAN_LOCAL" in
    /*) ACTION_PLAN_SOURCE="$ACTION_PLAN_LOCAL" ;;
    *) ACTION_PLAN_SOURCE="$REPO_LOCAL/$ACTION_PLAN_LOCAL" ;;
  esac
  [ -r "$ACTION_PLAN_SOURCE" ] || die "STINT_ONBOX_ACTION_PLAN must name a readable file"
  if [ -z "$ACTION_PLAN_TARGET" ]; then
    case "$ACTION_PLAN_LOCAL" in
      /*) ACTION_PLAN_TARGET="deep-work/action-plan.md" ;;
      *) ACTION_PLAN_TARGET="$ACTION_PLAN_LOCAL" ;;
    esac
  fi
  case "/$ACTION_PLAN_TARGET/" in
    */../*|*/./*|//*|/*$'\n'*|/*$'\r'*) die "STINT_ONBOX_ACTION_PLAN_PATH must stay inside the worktree" ;;
  esac
  case "$ACTION_PLAN_TARGET" in
    ''|/*|.) die "STINT_ONBOX_ACTION_PLAN_PATH must be a non-empty relative path" ;;
  esac
fi
[ -x "$BIN" ] || die "stint binary is missing or not executable: $BIN"
[ -x "$SUPERVISOR_LOCAL" ] || die "supervisor script is missing or not executable: $SUPERVISOR_LOCAL"

if [ "$SKIP_GITHUB" = 1 ]; then
  echo "WARNING: STINT_ONBOX_SKIP_GITHUB=1 disables checkpoint/PR publishing; fixture use only" >&2
else
  [ -x "$GITHUB_PUBLISH_LOCAL" ] || die "GitHub publisher is missing or not executable: $GITHUB_PUBLISH_LOCAL"
  [ -n "$GITHUB_TOKEN_LOCAL" ] && [ -r "$GITHUB_TOKEN_LOCAL" ] || die "STINT_GITHUB_TOKEN_FILE must name a readable least-privilege token file"
  case "$GITHUB_REPOSITORY" in
    ?*/?*) ;;
    *) die "STINT_GITHUB_REPOSITORY must be owner/name" ;;
  esac
  [ -n "$GITHUB_BASE" ] || die "STINT_GITHUB_BASE is required for the first stack layer"

  # Normalize a token/env file to a one-line secret before transfer. The GPU
  # receives only the credential it needs, never an operator config file.
  TOKEN_TMP="$(mktemp)"
  python3 - "$GITHUB_TOKEN_LOCAL" "$TOKEN_TMP" <<'PY'
import os, sys
src, dst = sys.argv[1:]
text = open(src, encoding="utf-8").read().strip()
token = ""
for raw in text.splitlines():
    line = raw.strip()
    if not line or line.startswith("#"):
        continue
    if line.startswith("export "):
        line = line[7:].strip()
    if "=" in line:
        key, value = line.split("=", 1)
        if key.strip() in {"GITHUB_TOKEN", "GH_TOKEN", "STINT_GITHUB_TOKEN"}:
            token = value.strip().strip('"').strip("'")
            break
    elif len(text.splitlines()) == 1:
        token = line
        break
if not token or "\n" in token or "\r" in token:
    raise SystemExit("invalid GitHub token file")
with open(dst, "w", encoding="utf-8") as stream:
    stream.write(token + "\n")
os.chmod(dst, 0o600)
PY
fi

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

# Materialize a standalone repository at the exact committed source HEAD. This
# works for both normal checkouts and linked worktrees, and deliberately excludes
# dirty/untracked operator files from the GPU baseline.
REPO_STAGE="$(mktemp -d)"
git clone --quiet --no-hardlinks --no-checkout "$REPO_LOCAL" "$REPO_STAGE" || die "failed to stage repository baseline"
git -C "$REPO_STAGE" checkout --quiet --detach "$SOURCE_HEAD" || die "failed to checkout staged repository HEAD"
git -C "$REPO_STAGE" remote set-url origin "$SOURCE_ORIGIN"

echo "transferring pinned Stint runtime and mission repository"
"${SSH[@]}" "mkdir -p '$ROOT/bin' '$ROOT/runtime' '$ROOT/config' '$ROOT/state' /root/.config/stint && chmod 700 '$ROOT' '$ROOT/config' '$ROOT/state' '$ROOT/runtime' /root/.config/stint"
scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$BIN" "root@$HOST:$REMOTE_BIN"
scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$SUPERVISOR_LOCAL" "root@$HOST:$REMOTE_SUPERVISOR"
scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$MISSION_LOCAL" "root@$HOST:$REMOTE_MISSION"
"${SSH[@]}" "chmod 0755 '$REMOTE_BIN' '$REMOTE_SUPERVISOR'"
"${SSH[@]}" "rm -rf '$REMOTE_REPO' && mkdir -p '$REMOTE_REPO'"
rsync -a --delete -e "$RSYNC_SSH" "$REPO_STAGE/" "root@$HOST:$REMOTE_REPO/"
if [ -n "$ACTION_PLAN_LOCAL" ]; then
  scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$ACTION_PLAN_SOURCE" "root@$HOST:$REMOTE_ACTION_PLAN_SEED"
fi

if [ "$SKIP_GITHUB" != 1 ]; then
  scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$GITHUB_PUBLISH_LOCAL" "root@$HOST:$REMOTE_GITHUB_PUBLISH"
  scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$TOKEN_TMP" "root@$HOST:$REMOTE_GITHUB_TOKEN"
  "${SSH[@]}" "chmod 0700 '$REMOTE_GITHUB_PUBLISH' && chmod 0600 '$REMOTE_GITHUB_TOKEN'"
  github_preflight=(env \
    "STINT_GITHUB_TOKEN_FILE=$REMOTE_GITHUB_TOKEN" \
    "STINT_GITHUB_REPOSITORY=$GITHUB_REPOSITORY" \
    "STINT_GITHUB_BASE=$GITHUB_BASE" \
    "STINT_ONBOX_ORIGIN=gpu-instance" \
    "STINT_ONBOX_INSTANCE_ID=$STINT_INSTANCE_ID" \
    "$REMOTE_GITHUB_PUBLISH" preflight "$REMOTE_REPO")
  github_preflight_cmd="$(printf '%q ' "${github_preflight[@]}")"
  echo "verifying GPU-side GitHub origin and publish credential"
  "${SSH[@]}" "$github_preflight_cmd" || die "GPU-side GitHub publish preflight failed"
fi

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
[ -n "$ACTION_PLAN_LOCAL" ] && args+=(--action-plan "$ACTION_PLAN_TARGET" --action-plan-seed "$REMOTE_ACTION_PLAN_SEED")

echo "starting detached on-box supervisor"
remote_env=("STINT_ONBOX_BIN=$REMOTE_BIN" "STINT_ONBOX_ROOT=$ROOT" \
  "STINT_ONBOX_READY_FILE=$REMOTE_READY" "STINT_ONBOX_INSTANCE_ID=$STINT_INSTANCE_ID" \
  "STINT_ONBOX_DEADLINE=$STINT_DEADLINE" "STINT_ONBOX_CLIENTS=$CLIENTS" \
  "STINT_ONBOX_ORIGIN=gpu-instance")
if [ "$SKIP_GITHUB" = 1 ]; then
  remote_env+=("STINT_ONBOX_SKIP_GITHUB=1")
else
  remote_env+=("STINT_ONBOX_GITHUB_PUBLISH=$REMOTE_GITHUB_PUBLISH" \
    "STINT_GITHUB_TOKEN_FILE=$REMOTE_GITHUB_TOKEN" \
    "STINT_GITHUB_REPOSITORY=$GITHUB_REPOSITORY" \
    "STINT_GITHUB_BASE=$GITHUB_BASE" \
    "STINT_GITHUB_PR_DRAFT=${STINT_GITHUB_PR_DRAFT:-1}")
fi
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
    [ "$SKIP_GITHUB" = 1 ] || echo "GitHub publishing is owned by the GPU: $GITHUB_REPOSITORY (stack base $GITHUB_BASE)"
    exit 0
  fi
  sleep 5
done
die "on-box supervisor did not report RUNNING; inspect $ROOT/supervisor.log over SSH"
