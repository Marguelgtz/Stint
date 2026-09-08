#!/usr/bin/env bash
# Detached supervisor for production on-box Deep Work.
#
# Usage:
#   onbox-deep-supervisor.sh start -- <stint deep onbox flags>
#   onbox-deep-supervisor.sh status
#   onbox-deep-supervisor.sh stop
#
# The launch connection only needs to start this script and poll READY_FILE.
# The supervisor owns the coordinator lifetime, restart-from-deep.json loop,
# heartbeat, and (when instance credentials are supplied) the deadline
# watchdog. It never opens SSH back to the same instance.
set -Eeuo pipefail

ROOT="${STINT_ONBOX_ROOT:-/var/lib/stint-onbox}"
STATE_HOME="$ROOT/state"
CONFIG_HOME="${STINT_ONBOX_CONFIG_HOME:-$HOME/.config}"
RUNTIME_DIR="$ROOT/runtime"
PID_FILE="$RUNTIME_DIR/supervisor.pid"
READY_FILE="${STINT_ONBOX_READY_FILE:-$RUNTIME_DIR/RUNNING.json}"
HEARTBEAT_FILE="$RUNTIME_DIR/heartbeat.json"
LOG_FILE="$ROOT/supervisor.log"
STINT_BIN="${STINT_ONBOX_BIN:-/usr/local/bin/stint}"

export XDG_STATE_HOME="$STATE_HOME"

die() { echo "ONBOX_SUPERVISOR_FAIL $*" >&2; exit 1; }
alive() { [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE" 2>/dev/null || true)" 2>/dev/null; }

write_vast_session() {
  local session_file="$STATE_HOME/stint/session.json"
  local instance_id="${STINT_ONBOX_INSTANCE_ID:-}"
  local deadline="${STINT_ONBOX_DEADLINE:-}"
  [ -n "$instance_id" ] && [ -n "$deadline" ] || return 0
  command -v python3 >/dev/null 2>&1 || die "python3 is required for on-box session state"
  mkdir -p "$(dirname "$session_file")"
  python3 - "$session_file" "$instance_id" "$deadline" <<'PY'
import json, os, sys
path, instance, deadline = sys.argv[1:]
payload = {
    "instanceId": int(instance),
    "profile": "deep-onbox",
    "runtime": "ninfer",
    "runtimeRequest": "ninfer",
    "contextTokens": 262144,
    "clients": int(os.environ.get("STINT_ONBOX_CLIENTS", "1")),
    "startedAt": deadline,
    "deadline": deadline,
    "status": "READY",
    "checkpoint": "READY",
}
tmp = path + ".tmp"
with open(tmp, "w", encoding="utf-8") as stream:
    json.dump(payload, stream, indent=2)
    stream.write("\n")
os.chmod(tmp, 0o600)
os.replace(tmp, path)
PY
}

start_watchdog() {
  [ "${STINT_ONBOX_SKIP_WATCHDOG:-0}" = 1 ] && return 0
  [ -n "${STINT_ONBOX_INSTANCE_ID:-}" ] && [ -n "${STINT_ONBOX_DEADLINE:-}" ] || \
    die "STINT_ONBOX_INSTANCE_ID and STINT_ONBOX_DEADLINE are required (or set STINT_ONBOX_SKIP_WATCHDOG=1 for a fixture)"
  [ -r "$CONFIG_HOME/stint/credentials.json" ] || die "Vast credentials missing at $CONFIG_HOME/stint/credentials.json"
  write_vast_session
  if ! pgrep -f "[s]tint _watchdog" >/dev/null 2>&1; then
    setsid nohup "$STINT_BIN" _watchdog >>"$ROOT/watchdog.log" 2>&1 < /dev/null &
  fi
}

snapshot() {
  mkdir -p "$RUNTIME_DIR"
  python3 - "$STATE_HOME/stint/deep" "$HEARTBEAT_FILE" <<'PY'
import json, os, subprocess, sys, time
deep_root, out_path = sys.argv[1:]
latest = os.path.join(deep_root, "latest")
state = {}
if os.path.isfile(latest):
    try:
        session = open(latest, encoding="utf-8").read().strip()
        state_path = os.path.join(deep_root, session, "deep.json")
        with open(state_path, encoding="utf-8") as stream:
            state = json.load(stream)
    except Exception:
        state = {}
tasks = state.get("tasks", [])
counts = {}
for task in tasks:
    status = task.get("status", "unknown")
    counts[status] = counts.get(status, 0) + 1
active = next((task.get("id", "") for task in tasks if task.get("status") == "active"), "")
head = ""
worktree = state.get("worktreePath", "")
if worktree:
    try:
        head = subprocess.check_output(["git", "-C", worktree, "rev-parse", "HEAD"], text=True, stderr=subprocess.DEVNULL).strip()
    except Exception:
        pass
payload = {
    "observedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "session": state.get("sessionId", ""),
    "phase": state.get("phase", "starting"),
    "activeTask": active,
    "taskCounts": counts,
    "lastCheckpoint": head,
    "deadline": state.get("deadline", ""),
    "updatedAt": state.get("updatedAt", ""),
    "worker": (state.get("exec") or {}).get("worker", ""),
    "model": (state.get("exec") or {}).get("model", ""),
}
tmp = out_path + ".tmp"
with open(tmp, "w", encoding="utf-8") as stream:
    json.dump(payload, stream, separators=(",", ":"))
    stream.write("\n")
os.chmod(tmp, 0o600)
os.replace(tmp, out_path)
PY
}

heartbeat_loop() {
  while :; do
    snapshot || true
    if [ -n "${STINT_ONBOX_R2_SYNC:-}" ] && [ -x "$STINT_ONBOX_R2_SYNC" ]; then
      "$STINT_ONBOX_R2_SYNC" "$HEARTBEAT_FILE" || true
    fi
    sleep "${STINT_ONBOX_HEARTBEAT_SECONDS:-20}"
  done
}

archive_final() {
  [ -x "${STINT_ONBOX_R2_ARCHIVE:-}" ] || return 0
  latest="$STATE_HOME/stint/deep/latest"
  [ -r "$latest" ] || return 0
  session="$(tr -d '[:space:]' < "$latest")"
  case "$session" in
    ''|*[!A-Za-z0-9_-]*) return 0 ;;
  esac
  "$STINT_ONBOX_R2_ARCHIVE" "$STATE_HOME/stint/deep/$session" || true
}

run_supervisor() {
  local -a onbox_args=("$@")
  mkdir -p "$ROOT" "$RUNTIME_DIR" "$CONFIG_HOME/stint" "$STATE_HOME"
  start_watchdog
  heartbeat_loop &
  local heartbeat_pid=$!
  # The trap runs after run_supervisor returns, so its local heartbeat_pid may
  # already be out of scope under `set -u`.
  trap 'kill "${heartbeat_pid:-}" 2>/dev/null || true; snapshot || true; archive_final' EXIT

  local first=1
  while :; do
    local rc=0
    if [ "$first" = 1 ]; then
      "$STINT_BIN" deep onbox "${onbox_args[@]}" || rc=$?
      first=0
    else
      "$STINT_BIN" deep onbox --resume || rc=$?
    fi
    snapshot || true
    local phase=""
    if [ -s "$HEARTBEAT_FILE" ]; then
      phase="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("phase", ""))' "$HEARTBEAT_FILE" 2>/dev/null || true)"
    fi
    case "$phase" in
      landed|stopped) return "$rc" ;;
    esac
    if [ "$rc" -eq 0 ]; then
      return 0
    fi
    if [ ! -r "$STATE_HOME/stint/deep/latest" ]; then
      return "$rc"
    fi
    echo "$(date -u +%FT%TZ) coordinator exited rc=$rc; retrying from durable state" >>"$LOG_FILE"
    sleep 5
  done
}

start() {
  alive && die "supervisor already running (pid $(cat "$PID_FILE"))"
  mkdir -p "$RUNTIME_DIR"
  rm -f "$READY_FILE" "$HEARTBEAT_FILE"
  [ -x "$STINT_BIN" ] || die "stint binary missing: $STINT_BIN"
  [ "${1:-}" = -- ] && shift || true
  [ "$#" -gt 0 ] || die "start requires arguments after --"
  setsid nohup "$0" run "$@" >>"$LOG_FILE" 2>&1 < /dev/null &
  echo "$!" >"$PID_FILE"
  chmod 600 "$PID_FILE"
  echo "ONBOX_SUPERVISOR_STARTED pid=$(cat "$PID_FILE") ready_file=$READY_FILE"
}

status() {
  if alive; then
    echo "ONBOX_SUPERVISOR_RUNNING pid=$(cat "$PID_FILE")"
  else
    echo "ONBOX_SUPERVISOR_STOPPED"
  fi
  [ -r "$READY_FILE" ] && cat "$READY_FILE"
  [ -r "$HEARTBEAT_FILE" ] && cat "$HEARTBEAT_FILE"
}

stop() {
  if alive; then
    kill "$(cat "$PID_FILE")" 2>/dev/null || true
  fi
  "$STINT_BIN" deep stop || true
  rm -f "$PID_FILE"
  snapshot || true
}

case "${1:-}" in
  start) shift; start "$@" ;;
  run) shift; run_supervisor "$@" ;;
  status) status ;;
  stop) stop ;;
  *) die "usage: $0 start -- <stint deep onbox flags> | status | stop" ;;
esac
