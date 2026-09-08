#!/usr/bin/env bash
# Live P6 smoke: boot a GPU box, start the on-box supervisor, terminate the
# operator-side tunnel/watchdog, and observe the remote run only.
set -Eeuo pipefail

REPO_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
RUN_ID="${STINT_ONBOX_RUN_ID:-$(date -u +%Y%m%d-%H%M%S)}"
STINT_BIN="${STINT_BIN:-$REPO_ROOT/bin/stint}"
RUN_ROOT="${STINT_ONBOX_RUN_ROOT:-$HOME/.local/state/stint-onbox-smoke-$RUN_ID}"
CONFIG_ROOT="${STINT_ONBOX_CONFIG_ROOT:-$HOME/.config/stint-onbox-smoke-$RUN_ID}"
ARTIFACT_DIR="${STINT_ONBOX_ARTIFACT_DIR:-$REPO_ROOT/onbox-deep-smoke-$RUN_ID}"
MISSION_PATH="${STINT_ONBOX_MISSION:-$REPO_ROOT/deep-work/PHASE_LANE_E2E_MISSION.md}"
REMOTE_ROOT="${STINT_REMOTE_ROOT:-/var/lib/stint-onbox}"
LOG="$ARTIFACT_DIR/launcher.log"

export XDG_STATE_HOME="$RUN_ROOT"
export XDG_CONFIG_HOME="$CONFIG_ROOT"
mkdir -p "$RUN_ROOT/stint" "$CONFIG_ROOT/stint" "$ARTIFACT_DIR"

say() { printf '[%s] %s\n' "$(date -u +%FT%TZ)" "$*" | tee -a "$LOG"; }
die() { say "FAIL $*"; exit 1; }

SESSION_JSON="$RUN_ROOT/stint/session.json"
LOCAL_READY=0
REMOTE_STARTED=0
SSH=()

cleanup() {
  rc=$?
  if [ "$REMOTE_STARTED" = 1 ] && [ "${#SSH[@]}" -gt 0 ]; then
    "${SSH[@]}" "STINT_ONBOX_ROOT='$REMOTE_ROOT' '$REMOTE_ROOT/onbox-deep-supervisor.sh' status" >"$ARTIFACT_DIR/final-remote-status.txt" 2>>"$LOG" || true
  fi
  if [ "$LOCAL_READY" = 1 ]; then
    say "tearing down local lifecycle record"
    timeout 2m "$STINT_BIN" down >>"$LOG" 2>&1 || say "WARN local teardown failed"
  fi
  exit "$rc"
}
trap cleanup EXIT

[ -x "$STINT_BIN" ] || die "missing Stint binary: $STINT_BIN"
[ -r "$HOME/.config/stint-dryrun/stint/credentials.json" ] || die "missing Vast credentials"
[ -r "$HOME/.config/vanta-r2.env" ] || die "missing R2 environment file"

cp "$HOME/.config/stint-dryrun/stint/credentials.json" "$CONFIG_ROOT/stint/credentials.json"
chmod 600 "$CONFIG_ROOT/stint/credentials.json"

say "renting isolated RTX 4090 session for on-box supervisor smoke"
setsid "$STINT_BIN" start interactive --hours 1.5 --tunnel-port 8413 \
  --runtime ninfer --ninfer-config native --clients 2 \
  --min-measured-download-mbps 30 --min-network-mbps 300 \
  --network-candidate-attempts 5 --max-cost-usd 2 --yes >>"$LOG" 2>&1 < /dev/null &
START_PID=$!
LOCAL_READY=1
for _ in $(seq 1 540); do
  if [ -r "$SESSION_JSON" ] && grep -Eq '"status"[[:space:]]*:[[:space:]]*"READY"' "$SESSION_JSON"; then
    break
  fi
  if ! kill -0 "$START_PID" 2>/dev/null; then
    wait "$START_PID" || true
    break
  fi
  sleep 5
done
grep -Eq '"status"[[:space:]]*:[[:space:]]*"READY"' "$SESSION_JSON" || die "compute did not reach READY"

mapfile -t box < <(python3 - "$SESSION_JSON" <<'PY'
import json, sys
state = json.load(open(sys.argv[1], encoding="utf-8"))
for key in ("sshHost", "sshPort", "instanceId", "deadline"):
    print(state.get(key, ""))
PY
)
HOST="${box[0]:-}"; PORT="${box[1]:-}"; INSTANCE="${box[2]:-}"; DEADLINE="${box[3]:-}"
[ -n "$HOST" ] && [ -n "$PORT" ] && [ -n "$INSTANCE" ] && [ -n "$DEADLINE" ] || die "READY session lacks remote identity"
KEY="$CONFIG_ROOT/stint/ssh/id_ed25519"
# `stint start` generates (and attaches) the keypair selected by its own
# XDG_CONFIG_HOME. Keep that private key intact: replacing it with a key
# from another config root after the rental is created makes SSH fail with
# `Permission denied (publickey)`.
[ -r "$KEY" ] || die "lifecycle READY without its generated SSH private key"
chmod 600 "$KEY"
SSH=(ssh -i "$KEY" -p "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "root@$HOST")

say "provisioning Hermes and phase routes on $HOST:$PORT"
timeout 25m "${SSH[@]}" 'bash -s' < "$REPO_ROOT/scripts/provision-box.sh" >>"$LOG" 2>&1
rsync -a -e "ssh -i $KEY -p $PORT -o BatchMode=yes -o StrictHostKeyChecking=accept-new" \
  "$REPO_ROOT/scripts/phaseproxy.py" "$REPO_ROOT/scripts/box-phase-setup.sh" \
  "$REPO_ROOT/scripts/deep-observe.sh" "$REPO_ROOT/scripts/phase-lane-concurrency-smoke.sh" \
  "root@$HOST:/root/" >>"$LOG" 2>&1
"${SSH[@]}" 'chmod +x /root/phaseproxy.py /root/box-phase-setup.sh /root/deep-observe.sh /root/phase-lane-concurrency-smoke.sh && PHASE_PROXY=/root/phaseproxy.py /root/box-phase-setup.sh' >>"$LOG" 2>&1
timeout 12m "${SSH[@]}" 'STINT_PHASED=1 PHASING_DIR=/root/stint-phasing bash -s' < "$REPO_ROOT/scripts/box-smoke.sh" >>"$LOG" 2>&1
timeout 6m "${SSH[@]}" 'PHASING_DIR=/root/stint-phasing /root/phase-lane-concurrency-smoke.sh' >>"$LOG" 2>&1

say "starting detached on-box supervisor"
STINT_BOX_HOST="$HOST" STINT_BOX_PORT="$PORT" STINT_BOX_KEY="$KEY" \
  STINT_MISSION="$MISSION_PATH" STINT_REPO="$REPO_ROOT" \
  STINT_BIN="$STINT_BIN" STINT_REMOTE_ROOT="$REMOTE_ROOT" \
  STINT_VAST_CREDENTIALS="$CONFIG_ROOT/stint/credentials.json" \
  STINT_R2_ENV_FILE="$HOME/.config/vanta-r2.env" STINT_ONBOX_CLIENTS=2 \
  STINT_ONBOX_ACTION_PLAN=deep-work/phase-plan.md \
  STINT_ONBOX_TASK_TIMEOUT=15m STINT_ONBOX_MAX_ATTEMPTS=1 \
  "$REPO_ROOT/scripts/launch-onbox-deep.sh" >>"$LOG" 2>&1
REMOTE_STARTED=1
say "on-box supervisor reported RUNNING; simulating operator shutdown"

# No local coordinator or model tunnel is needed after the handshake. The
# remote supervisor and its on-box watchdog remain alive.
python3 - "$SESSION_JSON" <<'PY'
import json, os, signal, sys
state = json.load(open(sys.argv[1], encoding="utf-8"))
for key in ("tunnelPid", "watchdogPid"):
    pid = int(state.get(key, 0) or 0)
    if pid > 0:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
PY

for _ in $(seq 1 90); do
  status="$(${SSH[@]} "STINT_ONBOX_ROOT='$REMOTE_ROOT' '$REMOTE_ROOT/onbox-deep-supervisor.sh' status" 2>>"$LOG" || true)"
  printf '%s\n' "$status" >"$ARTIFACT_DIR/remote-status.txt"
  printf '%s\n' "$status" | sed -n '1,3p'
  if printf '%s\n' "$status" | grep -Eq '"phase":"(landed|stopped)"'; then
    break
  fi
  sleep 10
done

say "capturing sanitized observer and on-box state"
: >"$ARTIFACT_DIR/deep-observe.json"
: >"$ARTIFACT_DIR/remote-state.json"
: >"$ARTIFACT_DIR/remote-handoff.md"
"${SSH[@]}" '/root/deep-observe.sh' >"$ARTIFACT_DIR/deep-observe.json" 2>>"$LOG" || true
session_id="$(${SSH[@]} "cat '$REMOTE_ROOT/state/stint/deep/latest'" 2>/dev/null | tr -d '[:space:]' || true)"
case "$session_id" in
  ''|*[!A-Za-z0-9_-]*) die "no safe remote deep session id" ;;
esac
"${SSH[@]}" "cat '$REMOTE_ROOT/state/stint/deep/$session_id/deep.json'" >"$ARTIFACT_DIR/remote-state.json"
worktree="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("worktreePath", ""))' "$ARTIFACT_DIR/remote-state.json" 2>/dev/null || true)"
if [ -n "$worktree" ]; then
  "${SSH[@]}" "cat '$worktree/DEEP_WORK_HANDOFF.md'" >"$ARTIFACT_DIR/remote-handoff.md" 2>>"$LOG" || true
fi
"${SSH[@]}" "cat '$REMOTE_ROOT/state/stint/deep/$session_id/incidents.jsonl'" >"$ARTIFACT_DIR/incidents.jsonl" 2>>"$LOG" || true

python3 - "$ARTIFACT_DIR/remote-state.json" "$ARTIFACT_DIR/remote-handoff.md" <<'PY'
import json, sys
state = json.load(open(sys.argv[1], encoding="utf-8"))
tasks = {task.get("id"): task.get("status") for task in state.get("tasks", [])}
if state.get("phase") != "landed":
    raise SystemExit(f"on-box session did not land: {state.get('phase')}")
if tasks.get("PLAN-001") != "verified" or tasks.get("PHASE-001") != "verified" or tasks.get("PHASE-002") != "verified":
    raise SystemExit(f"on-box tasks not verified: {tasks}")
if not open(sys.argv[2], encoding="utf-8", errors="replace").read().strip():
    raise SystemExit("on-box handoff missing")
print("ONBOX_DEEP_SMOKE_PASS", state.get("sessionId"), tasks)
PY
say "PASS on-box supervisor, disconnect survival, two-lane phases, verification, and handoff"
