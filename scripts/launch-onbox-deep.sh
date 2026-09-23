#!/usr/bin/env bash
# Launch a detached on-box Deep Work supervisor from an operator machine.
#
# Required environment: STINT_BOX_HOST, STINT_BOX_PORT, STINT_BOX_KEY,
# STINT_MISSION, STINT_REPO, STINT_GITHUB_TOKEN_FILE,
# STINT_GITHUB_REPOSITORY, STINT_GITHUB_BASE, and STINT_VAST_CREDENTIALS.
# The script exits after the remote supervisor reports RUNNING. It never starts a local Deep Work
# coordinator, uploader, git publisher, or model tunnel.
set -Eeuo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO_ROOT="$(dirname -- "$SCRIPT_DIR")"
ROOT="${STINT_REMOTE_ROOT:-/var/lib/stint-onbox}"
BIN="${STINT_BIN:-$REPO_ROOT/bin/stint}"
SUPERVISOR_LOCAL="${STINT_SUPERVISOR_LOCAL:-$SCRIPT_DIR/onbox-deep-supervisor.sh}"
PROVISION_LOCAL="${STINT_PROVISION_LOCAL:-$SCRIPT_DIR/provision-box.sh}"
PHASE_PROXY_LOCAL="${STINT_PHASE_PROXY_LOCAL:-$SCRIPT_DIR/phaseproxy.py}"
PHASE_SETUP_LOCAL="${STINT_PHASE_SETUP_LOCAL:-$SCRIPT_DIR/box-phase-setup.sh}"
DEEP_OBSERVE_LOCAL="${STINT_DEEP_OBSERVE_LOCAL:-$SCRIPT_DIR/deep-observe.sh}"
BOX_SMOKE_LOCAL="${STINT_BOX_SMOKE_LOCAL:-$SCRIPT_DIR/box-smoke.sh}"
LANE_SMOKE_LOCAL="${STINT_LANE_SMOKE_LOCAL:-$SCRIPT_DIR/phase-lane-concurrency-smoke.sh}"
R2_SYNC_LOCAL="${STINT_R2_SYNC_LOCAL:-$SCRIPT_DIR/onbox-r2-sync.py}"
R2_ARCHIVE_LOCAL="${STINT_R2_ARCHIVE_LOCAL:-$SCRIPT_DIR/onbox-r2-archive.py}"
GITHUB_PUBLISH_LOCAL="${STINT_GITHUB_PUBLISH_LOCAL:-$SCRIPT_DIR/onbox-github-publish.py}"
MISSION_LOCAL="${STINT_MISSION:-}"
REPO_LOCAL="${STINT_REPO:-}"
HOST="${STINT_BOX_HOST:-}"
PORT="${STINT_BOX_PORT:-22}"
KEY="${STINT_BOX_KEY:-}"
CLIENTS="${STINT_ONBOX_CLIENTS:-1}"
RESUME="${STINT_ONBOX_RESUME:-0}"
REBIND_COMPUTE="${STINT_ONBOX_REBIND_COMPUTE:-0}"
REBIND_REASON="${STINT_ONBOX_REBIND_REASON:-}"
ONBOX_MODEL="${STINT_ONBOX_MODEL:-}"
ONBOX_MODEL_SET="${STINT_ONBOX_MODEL+x}"
if [ "$RESUME" != 1 ] && [ -z "$ONBOX_MODEL" ]; then ONBOX_MODEL="qwen3.8-27b"; fi
PHASING_DIR="${STINT_PHASING_DIR:-/root/stint-phasing}"
SKIP_GITHUB="${STINT_ONBOX_SKIP_GITHUB:-0}"
SKIP_WATCHDOG="${STINT_ONBOX_SKIP_WATCHDOG:-0}"
GITHUB_TOKEN_LOCAL="${STINT_GITHUB_TOKEN_FILE:-}"
GITHUB_REPOSITORY="${STINT_GITHUB_REPOSITORY:-}"
GITHUB_BASE="${STINT_GITHUB_BASE:-}"
GITHUB_MODE="${STINT_GITHUB_MODE:-engineering}"
GITHUB_APPROVAL="${STINT_GITHUB_APPROVAL:-internal}"
GITHUB_ALLOWED_AUTHORS="${STINT_GITHUB_ALLOWED_AUTHORS:-}"
ACTION_PLAN_LOCAL="${STINT_ONBOX_ACTION_PLAN:-}"
ACTION_PLAN_TARGET="${STINT_ONBOX_ACTION_PLAN_PATH:-}"
REMOTE_BIN="$ROOT/bin/stint"
REMOTE_SUPERVISOR="$ROOT/onbox-deep-supervisor.sh"
REMOTE_BOOTSTRAP="$ROOT/bootstrap"
REMOTE_PROVISION="$REMOTE_BOOTSTRAP/provision-box.sh"
REMOTE_PHASE_PROXY="$REMOTE_BOOTSTRAP/phaseproxy.py"
REMOTE_PHASE_SETUP="$REMOTE_BOOTSTRAP/box-phase-setup.sh"
REMOTE_DEEP_OBSERVE="$REMOTE_BOOTSTRAP/deep-observe.sh"
REMOTE_BOX_SMOKE="$REMOTE_BOOTSTRAP/box-smoke.sh"
REMOTE_LANE_SMOKE="$REMOTE_BOOTSTRAP/phase-lane-concurrency-smoke.sh"
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
}
trap cleanup_local EXIT

[ -n "$HOST" ] || die "STINT_BOX_HOST is required"
[ -n "$KEY" ] || die "STINT_BOX_KEY is required"
case "$RESUME" in 0|1) ;; *) die "STINT_ONBOX_RESUME must be 0 or 1" ;; esac
case "$REBIND_COMPUTE" in 0|1) ;; *) die "STINT_ONBOX_REBIND_COMPUTE must be 0 or 1" ;; esac
if [ "$REBIND_COMPUTE" = 1 ]; then
  [ "$RESUME" = 1 ] || die "STINT_ONBOX_REBIND_COMPUTE=1 requires STINT_ONBOX_RESUME=1"
  [ -n "${REBIND_REASON//[[:space:]]/}" ] || die "STINT_ONBOX_REBIND_REASON is required for an audited compute rebind"
elif [ -n "${REBIND_REASON//[[:space:]]/}" ]; then
  die "STINT_ONBOX_REBIND_REASON requires STINT_ONBOX_REBIND_COMPUTE=1"
fi
if [ "$RESUME" = 1 ]; then
  [ -z "$MISSION_LOCAL" ] || die "STINT_MISSION must be omitted when resuming durable state"
  [ -z "$REPO_LOCAL" ] || die "STINT_REPO must be omitted when resuming durable state"
  [ -z "$ACTION_PLAN_LOCAL" ] || die "STINT_ONBOX_ACTION_PLAN is a seed for new sessions; use STINT_ONBOX_ACTION_PLAN_PATH to override a resumed session"
  [ -n "$ACTION_PLAN_TARGET" ] || [ -z "${STINT_ONBOX_ACTION_PLAN_PATH+x}" ] || die "STINT_ONBOX_ACTION_PLAN_PATH cannot be empty"
else
  [ -n "$MISSION_LOCAL" ] && [ -r "$MISSION_LOCAL" ] || die "STINT_MISSION must name a readable mission"
  [ -n "$REPO_LOCAL" ] && git -C "$REPO_LOCAL" rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "STINT_REPO must name a git repository or linked worktree"
fi
[[ "$CLIENTS" =~ ^[1-9][0-9]*$ ]] || die "STINT_ONBOX_CLIENTS must be a positive integer"
SOURCE_HEAD=""
SOURCE_ORIGIN=""
if [ "$RESUME" = 0 ]; then
  SOURCE_HEAD="$(git -C "$REPO_LOCAL" rev-parse HEAD)"
  SOURCE_ORIGIN="$(git -C "$REPO_LOCAL" remote get-url origin 2>/dev/null || true)"
  [ -n "$SOURCE_ORIGIN" ] || die "STINT_REPO must have an origin remote"
fi
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
fi
if [ -n "$ACTION_PLAN_TARGET" ]; then
  case "/$ACTION_PLAN_TARGET/" in
    */../*|*/./*|//*|/*$'\n'*|/*$'\r'*) die "STINT_ONBOX_ACTION_PLAN_PATH must stay inside the worktree" ;;
  esac
  case "$ACTION_PLAN_TARGET" in
    ''|/*|.) die "STINT_ONBOX_ACTION_PLAN_PATH must be a non-empty relative path" ;;
  esac
fi
[ -x "$BIN" ] || die "stint binary is missing or not executable: $BIN"
[ -x "$SUPERVISOR_LOCAL" ] || die "supervisor script is missing or not executable: $SUPERVISOR_LOCAL"
for script in "$PROVISION_LOCAL" "$PHASE_PROXY_LOCAL" "$PHASE_SETUP_LOCAL" \
  "$DEEP_OBSERVE_LOCAL" "$BOX_SMOKE_LOCAL" "$LANE_SMOKE_LOCAL"; do
  [ -r "$script" ] || die "fresh-box bootstrap component is missing: $script"
done

if [ "$SKIP_GITHUB" = 1 ]; then
  echo "WARNING: STINT_ONBOX_SKIP_GITHUB=1 disables checkpoint/PR publishing; fixture use only" >&2
else
  [ "$GITHUB_MODE" = engineering ] || die "the on-box checkpoint publisher currently supports STINT_GITHUB_MODE=engineering only"
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

if [ "$SKIP_WATCHDOG" = 1 ]; then
  echo "WARNING: STINT_ONBOX_SKIP_WATCHDOG=1 disables automatic Vast deadline teardown; fixture use only" >&2
else
  [ -n "${STINT_VAST_CREDENTIALS:-}" ] && [ -r "$STINT_VAST_CREDENTIALS" ] || \
    die "STINT_VAST_CREDENTIALS must name the Vast credentials file used by the on-box deadline watchdog"
fi

SSH=(ssh -i "$KEY" -p "$PORT" -o BatchMode=yes -o ConnectTimeout=15 \
  -o ServerAliveInterval=10 -o ServerAliveCountMax=3 \
  -o StrictHostKeyChecking=accept-new "root@$HOST")
SCP=(scp -q -i "$KEY" -P "$PORT" -o BatchMode=yes -o ConnectTimeout=15 \
  -o ServerAliveInterval=10 -o ServerAliveCountMax=3 \
  -o StrictHostKeyChecking=accept-new)
RSYNC_SSH="ssh -i $KEY -p $PORT -o BatchMode=yes -o ConnectTimeout=15 -o ServerAliveInterval=10 -o ServerAliveCountMax=3 -o StrictHostKeyChecking=accept-new"

session_json="${STINT_SESSION_JSON:-${XDG_STATE_HOME:-$HOME/.local/state}/stint/session.json}"
if [ -r "$session_json" ]; then
  SESSION_DATA="$(python3 - "$session_json" <<'PY'
import datetime, json, sys
state = json.load(open(sys.argv[1], encoding="utf-8"))
if state.get("status") != "READY":
    raise SystemExit(f"compute session status must be READY, got {state.get('status', 'missing')}")
try:
    instance = int(state.get("instanceId", 0))
except (TypeError, ValueError):
    raise SystemExit("compute session has an invalid instance id")
if instance <= 0:
    raise SystemExit("compute session has no positive instance id")
deadline = state.get("deadline", "")
try:
    parsed = datetime.datetime.fromisoformat(deadline.replace("Z", "+00:00"))
except (TypeError, ValueError):
    raise SystemExit("compute session has an invalid deadline")
if parsed.tzinfo is None or parsed <= datetime.datetime.now(datetime.timezone.utc):
    raise SystemExit("compute session deadline is not in the future")
print(instance)
print(deadline)
print(state.get("startedAt", ""))
PY
  )" || die "could not read a READY compute identity from $session_json"
  mapfile -t session_values <<<"$SESSION_DATA"
  state_instance="${session_values[0]:-}"
  state_deadline="${session_values[1]:-}"
  if [ -n "${STINT_INSTANCE_ID:-}" ] && [ "$STINT_INSTANCE_ID" != "$state_instance" ]; then
    die "STINT_INSTANCE_ID does not match the READY local compute session"
  fi
  if [ -n "${STINT_DEADLINE:-}" ] && [ "$STINT_DEADLINE" != "$state_deadline" ]; then
    die "STINT_DEADLINE does not match the READY local compute session"
  fi
  STINT_INSTANCE_ID="$state_instance"
  STINT_DEADLINE="$state_deadline"
  STINT_STARTED_AT="${session_values[2]:-}"
fi
[ -n "${STINT_INSTANCE_ID:-}" ] && [ -n "${STINT_DEADLINE:-}" ] || \
  die "set STINT_INSTANCE_ID and STINT_DEADLINE or provide a READY session file at $session_json"

RESUME_MODEL=""
if [ "$RESUME" = 1 ]; then
  echo "checking durable on-box session, repository branch, and compute binding before qualification"
  resume_preflight_cmd="$(printf '%q ' python3 - "$ROOT" "$STINT_INSTANCE_ID" "$REBIND_COMPUTE" \
    "$GITHUB_MODE" "$GITHUB_REPOSITORY" "$GITHUB_BASE" "$GITHUB_ALLOWED_AUTHORS" "$GITHUB_APPROVAL" "$SKIP_GITHUB")"
  RESUME_DATA="$("${SSH[@]}" "$resume_preflight_cmd" <<'PY'
import json, os, re, subprocess, sys
root, current_raw, rebind_raw, mode, repository, base, authors_raw, approval, skip_github = sys.argv[1:]
repo = os.path.join(root, "repo")
latest = os.path.join(root, "state", "stint", "deep", "latest")
if not os.path.isfile(latest):
    raise SystemExit("durable Deep Work state is missing; resume requires a restored state volume")
session = open(latest, encoding="utf-8").read().strip()
if not re.fullmatch(r"[A-Za-z0-9_-]+", session):
    raise SystemExit("durable Deep Work latest pointer is invalid")
state_path = os.path.join(root, "state", "stint", "deep", session, "deep.json")
try:
    state = json.load(open(state_path, encoding="utf-8"))
except (OSError, ValueError) as exc:
    raise SystemExit(f"durable Deep Work state is unreadable: {exc}")
if state.get("sessionId") != session:
    raise SystemExit("durable Deep Work session id does not match latest pointer")
execution = state.get("exec") or {}
if execution.get("worker") != "hermes-onbox":
    raise SystemExit("latest durable session is not an on-box Hermes session")
if os.path.realpath(state.get("repoPath", "")) != os.path.realpath(repo):
    raise SystemExit("saved Deep Work repository path differs from this launch root")
branch = state.get("branch", "")
if branch != f"stint/deep-{session}":
    raise SystemExit("saved Deep Work branch is invalid for its session")
expected_worktree = os.path.join(repo, ".stint-deep", session)
if os.path.abspath(state.get("worktreePath", "")) != os.path.abspath(expected_worktree):
    raise SystemExit("saved Deep Work worktree path differs from its session repository")
if not os.path.isdir(repo):
    raise SystemExit("saved Deep Work repository is missing; restore the state and repository volume")
check = subprocess.run(["git", "-C", repo, "show-ref", "--verify", "--quiet", f"refs/heads/{branch}"])
if check.returncode:
    raise SystemExit(f"saved Deep Work branch {branch} is missing from the restored repository")
binding = state.get("computeBinding") or {}
try:
    bound_id = int(binding.get("instanceId", 0))
    current_id = int(current_raw)
except (TypeError, ValueError):
    raise SystemExit("saved or current compute identity is invalid")
needs_rebind = binding.get("provider") != "vast" or bound_id != current_id
if needs_rebind and rebind_raw != "1":
    raise SystemExit(f"saved session is bound to Vast instance {bound_id}, current instance is {current_id}; set STINT_ONBOX_REBIND_COMPUTE=1 and STINT_ONBOX_REBIND_REASON after verifying restored state")
if not needs_rebind and rebind_raw == "1":
    raise SystemExit("compute already matches saved session; do not request a rebind")
policy = state.get("github") or {}
if skip_github == "1":
    if str(policy.get("mode", "")).strip().lower() != "none":
        raise SystemExit("GitHub publishing cannot be disabled for a session whose persisted policy enables it")
else:
    mode = mode.strip().lower()
    repository = repository.strip()
    base = base.strip()
    approval = approval.strip().lower()
    authors = sorted(author.strip().lower() for author in authors_raw.split(",") if author.strip())
    saved_authors = sorted(str(author).strip().lower() for author in policy.get("allowedAuthors", []))
    expected = (str(policy.get("mode", "")), str(policy.get("repository", "")),
                str(policy.get("base", "")), saved_authors, str(policy.get("approval", "")))
    actual = (mode, repository, base, authors, approval)
    if expected != actual:
        raise SystemExit("publisher GitHub configuration differs from persisted mission policy (mode, repository, base, allowed authors, approval)")
    if mode != "engineering":
        raise SystemExit(f"on-box checkpoint publishing does not support GitHub mode {mode!r}")
model = str(execution.get("model", "")).strip()
print(model)
print(session)
print(branch)
print(bound_id)
PY
  )" || die "durable on-box resume preflight failed"
  mapfile -t resume_values <<<"$RESUME_DATA"
  RESUME_MODEL="${resume_values[0]:-}"
  if [ -n "$ONBOX_MODEL_SET" ] && [ -z "$ONBOX_MODEL" ]; then
    die "STINT_ONBOX_MODEL cannot be empty when resuming"
  fi
  [ -n "$RESUME_MODEL" ] || [ -n "$ONBOX_MODEL" ] || die "saved session has no persisted model; set STINT_ONBOX_MODEL explicitly"
  if [ -z "$ONBOX_MODEL" ]; then ONBOX_MODEL="$RESUME_MODEL"; fi
fi

# New sessions transfer a pinned repository image. Resume uses the already
# restored state and worktree in place and must never clear the remote repo.
if [ "$RESUME" = 0 ]; then
  REPO_STAGE="$(mktemp -d)"
  git clone --quiet --no-hardlinks --no-checkout "$REPO_LOCAL" "$REPO_STAGE" || die "failed to stage repository baseline"
  git -C "$REPO_STAGE" checkout --quiet --detach "$SOURCE_HEAD" || die "failed to checkout staged repository HEAD"
  git -C "$REPO_STAGE" remote set-url origin "$SOURCE_ORIGIN"
fi

if [ "$RESUME" = 1 ]; then
  echo "transferring pinned Stint runtime while preserving restored Deep Work state and repository"
else
  echo "transferring pinned Stint runtime and mission repository"
fi
retry_step "prepare remote directories" "${SSH[@]}" "mkdir -p '$ROOT/bin' '$ROOT/runtime' '$ROOT/config' '$ROOT/state' '$REMOTE_BOOTSTRAP' /root/.config/stint && chmod 700 '$ROOT' '$ROOT/config' '$ROOT/state' '$ROOT/runtime' '$REMOTE_BOOTSTRAP' /root/.config/stint"
retry_step "transfer Stint binary" "${SCP[@]}" "$BIN" "root@$HOST:$REMOTE_BIN"
retry_step "transfer supervisor" "${SCP[@]}" "$SUPERVISOR_LOCAL" "root@$HOST:$REMOTE_SUPERVISOR"
retry_step "install remote executables" "${SSH[@]}" "chmod 0755 '$REMOTE_BIN' '$REMOTE_SUPERVISOR'"
if [ "$RESUME" = 0 ]; then
  retry_step "transfer mission" "${SCP[@]}" "$MISSION_LOCAL" "root@$HOST:$REMOTE_MISSION"
fi
# Install the deadline credential before any potentially slow qualification.
if [ "$SKIP_WATCHDOG" != 1 ]; then
  retry_step "transfer Vast credentials" "${SCP[@]}" "$STINT_VAST_CREDENTIALS" "root@$HOST:/root/.config/stint/credentials.json"
  retry_step "protect Vast credentials" "${SSH[@]}" "chmod 0600 /root/.config/stint/credentials.json"
fi
if [ "$RESUME" = 0 ]; then
  retry_step "prepare remote repository" "${SSH[@]}" "rm -rf '$REMOTE_REPO' && mkdir -p '$REMOTE_REPO'"
  retry_step "transfer repository" rsync -a --delete -e "$RSYNC_SSH" "$REPO_STAGE/" "root@$HOST:$REMOTE_REPO/"
fi
# Register only the exact validated path for root-side Git commands.
retry_step "register remote repository" "${SSH[@]}" "git config --global --add safe.directory '$REMOTE_REPO'"

if [ "$SKIP_GITHUB" != 1 ]; then
  retry_step "transfer GitHub publisher" "${SCP[@]}" "$GITHUB_PUBLISH_LOCAL" "root@$HOST:$REMOTE_GITHUB_PUBLISH"
  retry_step "transfer GitHub token" "${SCP[@]}" "$TOKEN_TMP" "root@$HOST:$REMOTE_GITHUB_TOKEN"
  retry_step "protect GitHub publisher config" "${SSH[@]}" "chmod 0700 '$REMOTE_GITHUB_PUBLISH' && chmod 0600 '$REMOTE_GITHUB_TOKEN'"
  github_preflight=(env \
    "STINT_GITHUB_TOKEN_FILE=$REMOTE_GITHUB_TOKEN" \
    "STINT_GITHUB_REPOSITORY=$GITHUB_REPOSITORY" \
    "STINT_GITHUB_BASE=$GITHUB_BASE" \
    "STINT_GITHUB_MODE=$GITHUB_MODE" \
    "STINT_GITHUB_ALLOWED_AUTHORS=$GITHUB_ALLOWED_AUTHORS" \
    "STINT_GITHUB_APPROVAL=$GITHUB_APPROVAL" \
    "STINT_ONBOX_ORIGIN=gpu-instance" \
    "STINT_ONBOX_INSTANCE_ID=$STINT_INSTANCE_ID" \
    "$REMOTE_GITHUB_PUBLISH" preflight "$REMOTE_REPO")
  github_preflight_cmd="$(printf '%q ' "${github_preflight[@]}")"
  echo "verifying GPU-side GitHub origin and publish credential"
  retry_step "GPU-side GitHub publish preflight" "${SSH[@]}" "$github_preflight_cmd" || die "GPU-side GitHub publish preflight failed"
fi
retry_step "transfer Deep Work bootstrap" rsync -a -e "$RSYNC_SSH" \
  "$PROVISION_LOCAL" "$PHASE_PROXY_LOCAL" "$PHASE_SETUP_LOCAL" \
  "$DEEP_OBSERVE_LOCAL" "$BOX_SMOKE_LOCAL" "$LANE_SMOKE_LOCAL" \
  "root@$HOST:$REMOTE_BOOTSTRAP/"
retry_step "protect Deep Work bootstrap" "${SSH[@]}" \
  "chmod 0755 '$REMOTE_PROVISION' '$REMOTE_PHASE_PROXY' '$REMOTE_PHASE_SETUP' '$REMOTE_DEEP_OBSERVE' '$REMOTE_BOX_SMOKE' '$REMOTE_LANE_SMOKE'"

# Production and the live smoke use this same fresh-box sequence. Do not start
# the detached supervisor until runtime, model, phase providers, compression
# configuration, verifier toolchain, and real Hermes route calls pass.
remote_provision=(env "STINT_TARGET_REPO=$REMOTE_REPO" "STINT_MODEL_ID=$ONBOX_MODEL" \
  timeout "${STINT_BOOTSTRAP_TIMEOUT:-25m}" "$REMOTE_PROVISION")
remote_provision_cmd="$(printf '%q ' "${remote_provision[@]}")"
echo "qualifying runtime and the target repository verification environment"
retry_step "provision and verify box runtime" "${SSH[@]}" "$remote_provision_cmd" || \
  die "fresh-box runtime or repository verification preflight failed"

remote_phase_setup=(env "PHASE_PROXY=$REMOTE_PHASE_PROXY" "DEEP_OBSERVE=$REMOTE_DEEP_OBSERVE" \
  "HERMES_MODEL=$ONBOX_MODEL" "PHASING_DIR=$PHASING_DIR" "$REMOTE_PHASE_SETUP")
remote_phase_setup_cmd="$(printf '%q ' "${remote_phase_setup[@]}")"
retry_step "install phase routes and compression" "${SSH[@]}" "$remote_phase_setup_cmd" || \
  die "Hermes phase-provider setup failed"

remote_box_smoke=(env "STINT_PHASED=1" "HERMES_MODEL=$ONBOX_MODEL" \
  "PHASING_DIR=$PHASING_DIR" timeout "${STINT_BOX_SMOKE_TIMEOUT:-12m}" "$REMOTE_BOX_SMOKE")
remote_box_smoke_cmd="$(printf '%q ' "${remote_box_smoke[@]}")"
"${SSH[@]}" "$remote_box_smoke_cmd" || \
  die "Hermes phase routes or compression configuration failed qualification"
if [ "$CLIENTS" -eq 2 ]; then
  remote_lane_smoke=(env "HERMES_MODEL=$ONBOX_MODEL" "PHASING_DIR=$PHASING_DIR" \
    timeout "${STINT_LANE_SMOKE_TIMEOUT:-8m}" "$REMOTE_LANE_SMOKE")
  remote_lane_smoke_cmd="$(printf '%q ' "${remote_lane_smoke[@]}")"
  "${SSH[@]}" "$remote_lane_smoke_cmd" || \
    die "two-lane xhigh/medium concurrency qualification failed"
fi
if [ "$RESUME" = 0 ] && [ -n "$ACTION_PLAN_LOCAL" ]; then
  retry_step "transfer action-plan seed" "${SCP[@]}" "$ACTION_PLAN_SOURCE" "root@$HOST:$REMOTE_ACTION_PLAN_SEED"
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

if [ "$RESUME" = 1 ]; then
  args=(--resume --ready-file "$REMOTE_READY")
  [ -z "$ONBOX_MODEL_SET" ] || args+=(--model "$ONBOX_MODEL")
  [ -z "${STINT_ONBOX_PROVIDER+x}" ] || args+=(--provider "$STINT_ONBOX_PROVIDER")
  [ -z "${STINT_ONBOX_REASONING+x}" ] || args+=(--reasoning "$STINT_ONBOX_REASONING")
  [ -z "${STINT_ONBOX_TASK_TIMEOUT+x}" ] || args+=(--task-timeout "$STINT_ONBOX_TASK_TIMEOUT")
  [ -z "${STINT_ONBOX_MAX_ATTEMPTS+x}" ] || args+=(--max-attempts "$STINT_ONBOX_MAX_ATTEMPTS")
  [ -z "${STINT_ONBOX_ACTION_PLAN_PATH+x}" ] || args+=(--action-plan "$ACTION_PLAN_TARGET")
  if [ -n "${STINT_ONBOX_ALLOW_COMMANDS+x}" ]; then
    if [ -z "$STINT_ONBOX_ALLOW_COMMANDS" ]; then
      args+=(--clear-allow-commands)
    else
      while IFS= read -r command_prefix; do
        [ -n "$command_prefix" ] || continue
        args+=(--allow-command "$command_prefix")
      done <<<"$STINT_ONBOX_ALLOW_COMMANDS"
    fi
  fi
  if [ "$REBIND_COMPUTE" = 1 ]; then
    args+=(--rebind-compute --rebind-reason "$REBIND_REASON")
  fi
else
  args=(--mission "$REMOTE_MISSION" --repo "$REMOTE_REPO" --deadline "$STINT_DEADLINE" \
    --provider "${STINT_ONBOX_PROVIDER:-custom:qwen-stint-{reasoning}}" \
    --model "$ONBOX_MODEL" \
    --reasoning "${STINT_ONBOX_REASONING:-medium}" \
    --task-timeout "${STINT_ONBOX_TASK_TIMEOUT:-15m}" \
    --max-attempts "${STINT_ONBOX_MAX_ATTEMPTS:-2}" \
    --ready-file "$REMOTE_READY")
  [ -z "$ACTION_PLAN_TARGET" ] || args+=(--action-plan "$ACTION_PLAN_TARGET")
  [ -z "$ACTION_PLAN_LOCAL" ] || args+=(--action-plan-seed "$REMOTE_ACTION_PLAN_SEED")
  if [ -n "${STINT_ONBOX_ALLOW_COMMANDS:-}" ]; then
    while IFS= read -r command_prefix; do
      [ -n "$command_prefix" ] || continue
      args+=(--allow-command "$command_prefix")
    done <<<"$STINT_ONBOX_ALLOW_COMMANDS"
  fi
fi

echo "starting detached on-box supervisor"
remote_env=("STINT_ONBOX_BIN=$REMOTE_BIN" "STINT_ONBOX_ROOT=$ROOT" \
  "STINT_ONBOX_READY_FILE=$REMOTE_READY" "STINT_ONBOX_INSTANCE_ID=$STINT_INSTANCE_ID" \
  "STINT_ONBOX_DEADLINE=$STINT_DEADLINE" "STINT_ONBOX_STARTED_AT=${STINT_STARTED_AT:-}" \
  "STINT_ONBOX_CLIENTS=$CLIENTS" \
  "STINT_ONBOX_ORIGIN=gpu-instance")
if [ "$SKIP_GITHUB" = 1 ]; then
  remote_env+=("STINT_ONBOX_SKIP_GITHUB=1")
else
  remote_env+=("STINT_ONBOX_GITHUB_PUBLISH=$REMOTE_GITHUB_PUBLISH" \
    "STINT_GITHUB_TOKEN_FILE=$REMOTE_GITHUB_TOKEN" \
    "STINT_GITHUB_REPOSITORY=$GITHUB_REPOSITORY" \
    "STINT_GITHUB_BASE=$GITHUB_BASE" \
    "STINT_GITHUB_MODE=$GITHUB_MODE" \
    "STINT_GITHUB_ALLOWED_AUTHORS=$GITHUB_ALLOWED_AUTHORS" \
    "STINT_GITHUB_APPROVAL=$GITHUB_APPROVAL" \
    "STINT_GITHUB_PR_DRAFT=${STINT_GITHUB_PR_DRAFT:-1}")
fi
[ -n "${STINT_R2_ENV_FILE:-}" ] && remote_env+=("STINT_ONBOX_R2_SYNC=$REMOTE_R2_SYNC" "STINT_ONBOX_R2_ARCHIVE=$REMOTE_R2_ARCHIVE" "STINT_R2_ENV_FILE=$ROOT/config/r2.env")
[ "$SKIP_WATCHDOG" = 1 ] && remote_env+=("STINT_ONBOX_SKIP_WATCHDOG=1")
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
