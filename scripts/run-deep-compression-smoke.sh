#!/usr/bin/env bash
# Run the bounded, isolated GPU acceptance smoke for `stint deep dash`.
#
# This deliberately does not use the CP1 launcher or its state directory. It
# rents a short-lived box, creates one throwaway remote repository, records
# dashboard snapshots, captures the sanitized observer result, and tears the
# box down when the one-task coordinator exits.
set -Eeuo pipefail

REPO_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
RUN_ID="${STINT_SMOKE_RUN_ID:-$(date -u +%Y%m%d-%H%M%S)}"
STINT_BIN="${STINT_BIN:-$REPO_ROOT/bin/stint-deep-dashboard-smoke}"
RUN_ROOT="${STINT_SMOKE_RUN_ROOT:-$HOME/.local/state/stint-deep-dashboard-smoke-$RUN_ID}"
CONFIG_ROOT="${STINT_SMOKE_CONFIG_ROOT:-$HOME/.config/stint-deep-dashboard-smoke-$RUN_ID}"
ARTIFACT_DIR="${STINT_SMOKE_ARTIFACT_DIR:-$HOME/Documents/projects/Stint/deep-compression-smoke-$RUN_ID}"
NINFER_CLIENTS="${STINT_SMOKE_CLIENTS:-1}"
MISSION_PATH="${STINT_SMOKE_MISSION:-$REPO_ROOT/deep-work/COMPRESSION_SMOKE_MISSION.md}"
ACTION_PLAN_PATH="${STINT_SMOKE_ACTION_PLAN:-}"
LANE_SMOKE="${STINT_LANE_SMOKE:-0}"
REQUIRE_COMPRESSION="${STINT_REQUIRE_COMPRESSION:-1}"
EXPECTED_REMOTE_FILE="${STINT_EXPECTED_REMOTE_FILE:-compression-smoke.ok}"
EXPECTED_REMOTE_TEXT="${STINT_EXPECTED_REMOTE_TEXT:-twelve chunks read after compression smoke}"

export XDG_STATE_HOME="$RUN_ROOT"
export XDG_CONFIG_HOME="$CONFIG_ROOT"

STATE_DIR="$XDG_STATE_HOME/stint"
SESSION_JSON="$STATE_DIR/session.json"
LOG="$ARTIFACT_DIR/launcher.log"
DASHBOARD_LOG="$ARTIFACT_DIR/dashboard-transcript.log"
COORDINATOR_LOG="$ARTIFACT_DIR/coordinator.log"
mkdir -p "$STATE_DIR" "$XDG_CONFIG_HOME/stint" "$ARTIFACT_DIR"

say() { printf '[%s] %s\n' "$(date -u +%FT%TZ)" "$*" | tee -a "$LOG"; }
require() { [ -x "$STINT_BIN" ] || { say "FAIL missing executable: $STINT_BIN"; exit 1; }; }

down() {
  if [ -f "$SESSION_JSON" ]; then
    say "tearing down dedicated smoke compute"
    timeout 5m "$STINT_BIN" down >>"$LOG" 2>&1 || say "WARN teardown command failed; inspect $SESSION_JSON"
  fi
}

REMOTE_READY=0
capture_remote_evidence() {
  [ "$REMOTE_READY" = 1 ] || return 0
  say "capturing sanitized GPU observer and smoke artifact"
  timeout 30s "${SSH[@]}" '/root/stint-phasing/deep-observe' >"$ARTIFACT_DIR/deep-observe.json" 2>>"$LOG" || true
  deep_id=""
  if [ -r "$STATE_DIR/deep/latest" ]; then
    deep_id="$(tr -d '[:space:]' < "$STATE_DIR/deep/latest")"
  fi
  case "$deep_id" in
    ''|*[!A-Za-z0-9_-]*)
      say "WARN no safe Deep Work session id for remote artifact capture"
      return 0
      ;;
  esac
  remote_worktree="/root/stint-deep-dashboard-smoke/.stint-deep/$deep_id"
  case "$EXPECTED_REMOTE_FILE" in
    /*|*..*|*[!A-Za-z0-9_./-]*) say "WARN unsafe expected artifact path"; return 0 ;;
  esac
  timeout 30s "${SSH[@]}" "cd '$remote_worktree' && test -f '$EXPECTED_REMOTE_FILE' && cat '$EXPECTED_REMOTE_FILE' && git status --short" \
    >"$ARTIFACT_DIR/remote-artifact.txt" 2>>"$LOG" || true
}

DASHBOARD_PID=""
stop_dashboard_recorder() {
  if [ -n "$DASHBOARD_PID" ] && kill -0 "$DASHBOARD_PID" 2>/dev/null; then
    kill "$DASHBOARD_PID" 2>/dev/null || true
    wait "$DASHBOARD_PID" 2>/dev/null || true
  fi
}

cleanup() {
  status=$?
  "$STINT_BIN" deep dash --no-color --refresh >>"$DASHBOARD_LOG" 2>&1 || true
  stop_dashboard_recorder
  capture_remote_evidence
  down
  exit "$status"
}
trap cleanup EXIT

require
cp "$HOME/.config/stint-dryrun/stint/credentials.json" "$XDG_CONFIG_HOME/stint/credentials.json"
mkdir -p "$XDG_CONFIG_HOME/stint/ssh"
cp "$HOME/.config/stint-dryrun/stint/ssh/id_ed25519" "$XDG_CONFIG_HOME/stint/ssh/id_ed25519"
cp "$HOME/.config/stint-dryrun/stint/ssh/id_ed25519.pub" "$XDG_CONFIG_HOME/stint/ssh/id_ed25519.pub"
chmod 600 "$XDG_CONFIG_HOME/stint/ssh/id_ed25519"

say "starting isolated 90-minute native-NInfer smoke session"
setsid "$STINT_BIN" start interactive --hours 1.5 --tunnel-port 8413 \
  --runtime ninfer --ninfer-config native --clients "$NINFER_CLIENTS" \
  --min-measured-download-mbps 30 --min-network-mbps 300 \
  --network-candidate-attempts 5 --max-cost-usd 2 --yes >>"$LOG" 2>&1 < /dev/null &
START_PID=$!

ready=0
for _ in $(seq 1 540); do
  if [ -f "$SESSION_JSON" ] && grep -Eq '"status"[[:space:]]*:[[:space:]]*"READY"' "$SESSION_JSON"; then
    ready=1
    break
  fi
  if ! kill -0 "$START_PID" 2>/dev/null; then
    wait "$START_PID" || true
    break
  fi
  sleep 5
done
[ "$ready" = 1 ] || { say "FAIL compute did not reach READY; see $LOG"; exit 1; }

mapfile -t BOX < <(python3 - "$SESSION_JSON" <<'PY'
import json
import os
import sys
state = json.load(open(sys.argv[1], encoding="utf-8"))
print(state.get("sshHost", ""))
print(state.get("sshPort", ""))
PY
)
B_HOST="${BOX[0]:-}"
B_PORT="${BOX[1]:-}"
[ -n "$B_HOST" ] && [ -n "$B_PORT" ] || { say "FAIL READY session lacks SSH details"; exit 1; }
KEY="$XDG_CONFIG_HOME/stint/ssh/id_ed25519"
SSH=(ssh -i "$KEY" -p "$B_PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o "UserKnownHostsFile=$XDG_STATE_HOME/known_hosts" "root@$B_HOST")
SSH_RSYNC="ssh -i $KEY -p $B_PORT -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=$XDG_STATE_HOME/known_hosts"
REMOTE_READY=1
say "box READY: $B_HOST:$B_PORT"

say "provisioning Hermes and phase routes"
timeout 25m "${SSH[@]}" 'bash -s' < "$REPO_ROOT/scripts/provision-box.sh" >>"$LOG" 2>&1
rsync -a -e "$SSH_RSYNC" \
  "$REPO_ROOT/scripts/phaseproxy.py" \
  "$REPO_ROOT/scripts/box-phase-setup.sh" \
  "$REPO_ROOT/scripts/deep-observe.sh" \
  "$REPO_ROOT/scripts/deep-compression-smoke-box-setup.sh" \
  "$REPO_ROOT/scripts/phase-lane-concurrency-smoke.sh" \
  "root@$B_HOST:/root/" >>"$LOG" 2>&1
"${SSH[@]}" 'chmod +x /root/phaseproxy.py /root/box-phase-setup.sh /root/deep-observe.sh /root/deep-compression-smoke-box-setup.sh /root/phase-lane-concurrency-smoke.sh && PHASE_PROXY=/root/phaseproxy.py /root/box-phase-setup.sh' >>"$LOG" 2>&1

say "running normal phased box smoke"
timeout 12m "${SSH[@]}" 'STINT_PHASED=1 PHASING_DIR=/root/stint-phasing bash -s' < "$REPO_ROOT/scripts/box-smoke.sh" >>"$LOG" 2>&1
if [ "$LANE_SMOKE" = 1 ]; then
  say "running concurrent xhigh/medium two-lane smoke"
  timeout 6m "${SSH[@]}" 'PHASING_DIR=/root/stint-phasing /root/phase-lane-concurrency-smoke.sh' >>"$LOG" 2>&1
fi
"${SSH[@]}" 'hermes config set compression.threshold_tokens 20000 && hermes config get compression.threshold_tokens' >>"$LOG" 2>&1
"${SSH[@]}" '/root/deep-compression-smoke-box-setup.sh' >>"$LOG" 2>&1
"${SSH[@]}" 'git config --global --add safe.directory /root/stint-deep-dashboard-smoke' >>"$LOG" 2>&1

dashboard_recorder() {
  while :; do
    "$STINT_BIN" deep dash --no-color --refresh >>"$DASHBOARD_LOG" 2>&1 || true
    sleep 10
  done
}
dashboard_recorder &
DASHBOARD_PID=$!

say "running bounded Deep Work task"
DEEP_ACTION_ARGS=()
if [ -n "$ACTION_PLAN_PATH" ]; then
  DEEP_ACTION_ARGS=(--action-plan "$ACTION_PLAN_PATH")
fi
set +e
"$STINT_BIN" deep start \
  --mission "$MISSION_PATH" \
  --repo /root/stint-deep-dashboard-smoke \
  --worker hermes --provider custom:qwen-stint-medium --model qwen3.8-27b \
  --reasoning medium --task-timeout 15m --max-attempts 1 --hours 0.5 \
  "${DEEP_ACTION_ARGS[@]}" \
  >>"$COORDINATOR_LOG" 2>&1
COORDINATOR_STATUS=$?
set -e
say "coordinator exited status=$COORDINATOR_STATUS"
[ "$COORDINATOR_STATUS" = 0 ]

"$STINT_BIN" deep dash --no-color --refresh >>"$DASHBOARD_LOG" 2>&1 || true
stop_dashboard_recorder
capture_remote_evidence
python3 - "$ARTIFACT_DIR/deep-observe.json" "$ARTIFACT_DIR/remote-artifact.txt" "$COORDINATOR_LOG" <<'PY'
import json
import sys

observer_path, artifact_path, coordinator_path = sys.argv[1:]
observer = json.load(open(observer_path, encoding="utf-8"))
compression = observer.get("compression", {})
routes = observer.get("phaseRoutes", {})
if os.environ.get("STINT_REQUIRE_COMPRESSION", "1") == "1" and (compression.get("state") != "completed" or compression.get("truncated", 0) != 0):
    raise SystemExit(f"compression acceptance failed: {compression}")
if routes.get("mediumRequests", 0) < 1:
    raise SystemExit(f"medium route acceptance failed: {routes}")
expected = os.environ.get("STINT_EXPECTED_REMOTE_TEXT", "twelve chunks read after compression smoke")
if expected not in open(artifact_path, encoding="utf-8").read():
    raise SystemExit("remote verified artifact was not captured")
coordinator = open(coordinator_path, encoding="utf-8", errors="replace").read().lower()
for marker in ("context compression summary was truncated", "context window overflow"):
    if marker in coordinator:
        raise SystemExit(f"unexpected compression failure marker: {marker}")
PY
say "PASS compression, medium route, verified artifact, and dashboard transcript captured in $ARTIFACT_DIR"
