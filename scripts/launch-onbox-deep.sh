#!/usr/bin/env bash
# Launch a detached on-box Deep Work supervisor from an operator machine.
#
# Required environment: STINT_BOX_HOST, STINT_BOX_PORT, STINT_BOX_KEY,
# STINT_MISSION, STINT_REPO, STINT_GITHUB_TOKEN_FILE,
# STINT_GITHUB_REPOSITORY, and STINT_GITHUB_BASE. Set STINT_BASELINE_REF to
# pin a clean candidate branch (for example stint/provenance-easy-wins).
# The script exits after the
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
GITHUB_MODE="${STINT_GITHUB_MODE:-}"
GITHUB_ALLOWED_AUTHORS="${STINT_GITHUB_ALLOWED_AUTHORS:-}"
GITHUB_APPROVAL="${STINT_GITHUB_APPROVAL:-}"
COMPLETION_POLICY="${STINT_ONBOX_COMPLETION_POLICY:-}"
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
REMOTE_BOUNDARY="$ROOT/operator-boundary.json"
REMOTE_REPO="$ROOT/repo"
REMOTE_READY="$ROOT/runtime/RUNNING.json"
TOKEN_TMP=""
REPO_STAGE=""
BOUNDARY_TMP=""
TRANSFER_ATTEMPTS="${STINT_ONBOX_TRANSFER_ATTEMPTS:-5}"
TRANSFER_RETRY_SECONDS="${STINT_ONBOX_TRANSFER_RETRY_SECONDS:-3}"

die() { echo "ONBOX_LAUNCH_FAIL $*" >&2; exit 1; }
retry_step() {
  local label="$1"
  shift
  local attempt rc=1
  for attempt in $(seq 1 "$TRANSFER_ATTEMPTS"); do
    if "$@"; then
      return 0
    else
      rc=$?
    fi
    echo "ONBOX_LAUNCH_RETRY $label attempt=$attempt/$TRANSFER_ATTEMPTS rc=$rc" >&2
    [ "$attempt" -eq "$TRANSFER_ATTEMPTS" ] || sleep "$TRANSFER_RETRY_SECONDS"
  done
  return "$rc"
}
cleanup_local() {
  [ -z "$TOKEN_TMP" ] || rm -f "$TOKEN_TMP"
  [ -z "$REPO_STAGE" ] || rm -rf "$REPO_STAGE"
  [ -z "$BOUNDARY_TMP" ] || rm -f "$BOUNDARY_TMP"
}
trap cleanup_local EXIT

[ -n "$HOST" ] || die "STINT_BOX_HOST is required"
[ -n "$KEY" ] || die "STINT_BOX_KEY is required"
[ -n "$MISSION_LOCAL" ] && [ -r "$MISSION_LOCAL" ] || die "STINT_MISSION must name a readable mission"
[ -n "$REPO_LOCAL" ] && git -C "$REPO_LOCAL" rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "STINT_REPO must name a git repository or linked worktree"
BASELINE_REF="${STINT_BASELINE_REF:-HEAD}"
SOURCE_HEAD="$(git -C "$REPO_LOCAL" rev-parse "$BASELINE_REF^{commit}" 2>/dev/null || true)"
[ -n "$SOURCE_HEAD" ] || die "STINT_BASELINE_REF does not resolve to a commit: $BASELINE_REF"
SOURCE_ORIGIN="$(git -C "$REPO_LOCAL" remote get-url origin 2>/dev/null || true)"
[ -n "$SOURCE_ORIGIN" ] || die "STINT_REPO must have an origin remote"
BOUNDARY_TMP="$(mktemp)"
python3 - "$REPO_LOCAL" "$BASELINE_REF" "$SOURCE_HEAD" "$BOUNDARY_TMP" <<'PY'
import json, subprocess, sys
repo, ref, head, path = sys.argv[1:]
status = subprocess.check_output(["git", "-C", repo, "status", "--short"], text=True)
branch = subprocess.check_output(["git", "-C", repo, "branch", "--show-current"], text=True).strip()
payload = {"repository": repo, "operatorBranch": branch, "baselineRef": ref, "baselineHead": head, "status": status.splitlines()}
with open(path, "w", encoding="utf-8") as stream:
    json.dump(payload, stream, indent=2)
    stream.write("\n")
PY

# Read policy metadata once on the operator side so the GPU supervisor gets
# the same explicit capability boundary without transferring a token through
# command arguments or prompts. The mission remains the source of truth.
mapfile -t mission_policy < <(python3 - "$MISSION_LOCAL" <<'PY'
import sys
section = ""
values = {"mode":"", "repository":"", "base":"", "allowed-authors":"", "approval":"", "completion":""}
for raw in open(sys.argv[1], encoding="utf-8"):
    line = raw.strip()
    if line.startswith("## "):
        section = line[3:].strip().lower()
        continue
    if ":" not in line:
        continue
    key, value = line.split(":", 1)
    key, value = key.strip().lower(), value.strip()
    if section == "github" and key in values:
        values[key] = value
    if section == "completion" and key == "policy":
        values["completion"] = value
for key in ("mode", "repository", "base", "allowed-authors", "approval", "completion"):
    print(values[key])
PY
)
GITHUB_MODE="${GITHUB_MODE:-${mission_policy[0]:-}}"
GITHUB_REPOSITORY="${GITHUB_REPOSITORY:-${mission_policy[1]:-}}"
GITHUB_BASE="${GITHUB_BASE:-${mission_policy[2]:-}}"
GITHUB_ALLOWED_AUTHORS="${GITHUB_ALLOWED_AUTHORS:-${mission_policy[3]:-}}"
GITHUB_APPROVAL="${GITHUB_APPROVAL:-${mission_policy[4]:-}}"
COMPLETION_POLICY="${COMPLETION_POLICY:-${mission_policy[5]:-report-and-destroy}}"
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

SSH=(ssh -i "$KEY" -p "$PORT" -o BatchMode=yes -o ConnectTimeout=15 \
  -o ServerAliveInterval=10 -o ServerAliveCountMax=3 \
  -o StrictHostKeyChecking=accept-new "root@$HOST")
SCP=(scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o ConnectTimeout=15 \
  -o ServerAliveInterval=10 -o ServerAliveCountMax=3 \
  -o StrictHostKeyChecking=accept-new)
RSYNC_SSH="ssh -i $KEY -p $PORT -o BatchMode=yes -o ConnectTimeout=15 -o ServerAliveInterval=10 -o ServerAliveCountMax=3 -o StrictHostKeyChecking=accept-new"

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

echo "transferring pinned Stint runtime and mission repository baseline=$BASELINE_REF head=$SOURCE_HEAD"
retry_step "prepare remote directories" "${SSH[@]}" "mkdir -p '$ROOT/bin' '$ROOT/runtime' '$ROOT/config' '$ROOT/state' /root/.config/stint && chmod 700 '$ROOT' '$ROOT/config' '$ROOT/state' '$ROOT/runtime' /root/.config/stint"
retry_step "transfer Stint binary" "${SCP[@]}" "$BIN" "root@$HOST:$REMOTE_BIN"
retry_step "transfer supervisor" "${SCP[@]}" "$SUPERVISOR_LOCAL" "root@$HOST:$REMOTE_SUPERVISOR"
retry_step "transfer mission" "${SCP[@]}" "$MISSION_LOCAL" "root@$HOST:$REMOTE_MISSION"
retry_step "transfer operator boundary" "${SCP[@]}" "$BOUNDARY_TMP" "root@$HOST:$REMOTE_BOUNDARY"
retry_step "install remote executables" "${SSH[@]}" "chmod 0755 '$REMOTE_BIN' '$REMOTE_SUPERVISOR'"
retry_step "protect operator boundary" "${SSH[@]}" "chmod 0600 '$REMOTE_BOUNDARY'"
retry_step "prepare remote repository" "${SSH[@]}" "rm -rf '$REMOTE_REPO' && mkdir -p '$REMOTE_REPO'"
retry_step "transfer repository" rsync -a --delete -e "$RSYNC_SSH" "$REPO_STAGE/" "root@$HOST:$REMOTE_REPO/"
# The staged tree preserves the operator UID during rsync. Register the exact
# validated path for root-side Git commands instead of weakening ownership
# checks globally or changing the copied repository contents.
retry_step "register remote repository" "${SSH[@]}" "git config --global --add safe.directory '$REMOTE_REPO'"
if [ -n "$ACTION_PLAN_LOCAL" ]; then
  retry_step "transfer action-plan seed" "${SCP[@]}" "$ACTION_PLAN_SOURCE" "root@$HOST:$REMOTE_ACTION_PLAN_SEED"
fi

if [ "$SKIP_GITHUB" != 1 ]; then
  retry_step "transfer GitHub publisher" "${SCP[@]}" "$GITHUB_PUBLISH_LOCAL" "root@$HOST:$REMOTE_GITHUB_PUBLISH"
  retry_step "transfer GitHub token" "${SCP[@]}" "$TOKEN_TMP" "root@$HOST:$REMOTE_GITHUB_TOKEN"
  retry_step "protect GitHub publisher config" "${SSH[@]}" "chmod 0700 '$REMOTE_GITHUB_PUBLISH' && chmod 0600 '$REMOTE_GITHUB_TOKEN'"
  github_preflight=(env \
    "STINT_GITHUB_TOKEN_FILE=$REMOTE_GITHUB_TOKEN" \
    "STINT_GITHUB_REPOSITORY=$GITHUB_REPOSITORY" \
    "STINT_GITHUB_BASE=$GITHUB_BASE" \
    "STINT_ONBOX_ORIGIN=gpu-instance" \
    "STINT_ONBOX_INSTANCE_ID=$STINT_INSTANCE_ID" \
    "$REMOTE_GITHUB_PUBLISH" preflight "$REMOTE_REPO")
  github_preflight_cmd="$(printf '%q ' "${github_preflight[@]}")"
  echo "verifying GPU-side GitHub origin and publish credential"
  retry_step "GPU-side GitHub publish preflight" "${SSH[@]}" "$github_preflight_cmd" || die "GPU-side GitHub publish preflight failed"
fi

# Copying this file is explicit because it enables remote deadline destroy.
if [ -n "${STINT_VAST_CREDENTIALS:-}" ]; then
  [ -r "$STINT_VAST_CREDENTIALS" ] || die "STINT_VAST_CREDENTIALS is not readable"
  retry_step "transfer Vast credentials" "${SCP[@]}" "$STINT_VAST_CREDENTIALS" "root@$HOST:/root/.config/stint/credentials.json"
  retry_step "protect Vast credentials" "${SSH[@]}" "chmod 0600 /root/.config/stint/credentials.json"
fi

if [ -n "${STINT_R2_ENV_FILE:-}" ]; then
  [ -r "$STINT_R2_ENV_FILE" ] || die "STINT_R2_ENV_FILE is not readable"
  [ -x "$R2_SYNC_LOCAL" ] || die "R2 sync helper is missing or not executable: $R2_SYNC_LOCAL"
  [ -x "$R2_ARCHIVE_LOCAL" ] || die "R2 archive helper is missing or not executable: $R2_ARCHIVE_LOCAL"
  retry_step "transfer R2 sync helper" "${SCP[@]}" "$R2_SYNC_LOCAL" "root@$HOST:$REMOTE_R2_SYNC"
  retry_step "transfer R2 archive helper" "${SCP[@]}" "$R2_ARCHIVE_LOCAL" "root@$HOST:$REMOTE_R2_ARCHIVE"
  retry_step "transfer R2 config" "${SCP[@]}" "$STINT_R2_ENV_FILE" "root@$HOST:$ROOT/config/r2.env"
  retry_step "prepare R2 uploader" "${SSH[@]}" "chmod 0700 '$REMOTE_R2_SYNC' '$REMOTE_R2_ARCHIVE' '$ROOT/config/r2.env' && python3 -c 'import boto3' 2>/dev/null || python3 -m pip install --quiet --user boto3"
fi

DEFAULT_PROVIDER='custom:qwen-stint-{reasoning}'
PROVIDER="${STINT_ONBOX_PROVIDER:-$DEFAULT_PROVIDER}"
args=(--mission "$REMOTE_MISSION" --repo "$REMOTE_REPO" --deadline "$STINT_DEADLINE" \
  --provider "$PROVIDER" \
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
  "STINT_ONBOX_ORIGIN=gpu-instance" "STINT_ONBOX_COMPLETION_POLICY=$COMPLETION_POLICY" \
  "STINT_ONBOX_UNATTENDED=1" \
  "STINT_OPERATOR_BOUNDARY_FILE=$REMOTE_BOUNDARY" "STINT_BASELINE_REF=$BASELINE_REF")
if [ "$SKIP_GITHUB" = 1 ]; then
  remote_env+=("STINT_ONBOX_SKIP_GITHUB=1")
else
  remote_env+=("STINT_ONBOX_GITHUB_PUBLISH=$REMOTE_GITHUB_PUBLISH" \
    "STINT_GITHUB_TOKEN_FILE=$REMOTE_GITHUB_TOKEN" \
    "STINT_GITHUB_REPOSITORY=$GITHUB_REPOSITORY" \
    "STINT_GITHUB_BASE=$GITHUB_BASE" \
    "STINT_GITHUB_MODE=${GITHUB_MODE:-engineering}" \
    "STINT_GITHUB_ALLOWED_AUTHORS=$GITHUB_ALLOWED_AUTHORS" \
    "STINT_GITHUB_APPROVAL=${GITHUB_APPROVAL:-internal}" \
    "STINT_GITHUB_PR_DRAFT=${STINT_GITHUB_PR_DRAFT:-1}")
fi
[ -n "${STINT_R2_ENV_FILE:-}" ] && remote_env+=("STINT_ONBOX_R2_SYNC=$REMOTE_R2_SYNC" "STINT_ONBOX_R2_ARCHIVE=$REMOTE_R2_ARCHIVE" "STINT_R2_ENV_FILE=$ROOT/config/r2.env")
[ -n "${STINT_R2_PREFIX:-}" ] && remote_env+=("STINT_R2_PREFIX=$STINT_R2_PREFIX")
[ -n "${STINT_ONBOX_SKIP_WATCHDOG:-}" ] && remote_env+=("STINT_ONBOX_SKIP_WATCHDOG=$STINT_ONBOX_SKIP_WATCHDOG")
remote_start=(env "${remote_env[@]}" "$REMOTE_SUPERVISOR" start -- "${args[@]}")
remote_start_cmd="$(printf '%q ' "${remote_start[@]}")"
remote_status=(env "STINT_ONBOX_ROOT=$ROOT" "STINT_ONBOX_BIN=$REMOTE_BIN" "$REMOTE_SUPERVISOR" status)
remote_status_cmd="$(printf '%q ' "${remote_status[@]}")"
# A transport can drop after delivering `start`. Retrying an unconditional
# start would report "already running" and make a healthy launch look failed.
# This remote transaction starts only when the prior attempt did not take.
remote_start_if_needed_cmd="if $remote_status_cmd 2>/dev/null | grep -q '^ONBOX_SUPERVISOR_RUNNING'; then $remote_status_cmd; else $remote_start_cmd; fi"
retry_step "start remote supervisor" "${SSH[@]}" "$remote_start_if_needed_cmd" || \
  die "could not start or recover the on-box supervisor handshake"

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
