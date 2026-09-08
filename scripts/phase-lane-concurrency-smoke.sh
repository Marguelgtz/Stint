#!/usr/bin/env bash
# Prove that a two-client NInfer box can carry xhigh and medium requests at
# the same time. Output is intentionally reduced to exit codes and counters.
set -u

MODEL="${HERMES_MODEL:-qwen3.8-27b}"
PHASING_DIR="${PHASING_DIR:-/root/stint-phasing}"
fail() { echo "LANE_SMOKE_FAIL $*"; exit 1; }

command -v hermes >/dev/null 2>&1 || fail "hermes missing"
pgrep -x ninfer-serve >/dev/null 2>&1 || fail "ninfer-serve missing"
cmdline="$(tr '\0' ' ' </proc/$(pgrep -xo ninfer-serve)/cmdline 2>/dev/null || true)"
printf '%s' "$cmdline" | grep -Fq -- '--max-concurrency 2' || fail "NInfer is not serving two lanes"

: >"$PHASING_DIR/wire-xhigh.jsonl"
: >"$PHASING_DIR/wire-medium.jsonl"
set +e
timeout 150 hermes chat -q 'Reply with exactly: LANE_XHIGH_OK' --oneshot -Q \
  --provider custom:qwen-stint-xhigh -m "$MODEL" >/tmp/lane-xhigh.out 2>&1 &
xhigh_pid=$!
timeout 150 hermes chat -q 'Reply with exactly: LANE_MEDIUM_OK' --oneshot -Q \
  --provider custom:qwen-stint-medium -m "$MODEL" >/tmp/lane-medium.out 2>&1 &
medium_pid=$!
wait "$xhigh_pid"; xhigh_status=$?
wait "$medium_pid"; medium_status=$?
set -e
[ "$xhigh_status" -eq 0 ] || fail "xhigh request exited $xhigh_status"
[ "$medium_status" -eq 0 ] || fail "medium request exited $medium_status"
grep -q 'LANE_XHIGH_OK' /tmp/lane-xhigh.out || fail "xhigh response marker missing"
grep -q 'LANE_MEDIUM_OK' /tmp/lane-medium.out || fail "medium response marker missing"
grep -q '"reasoning_effort": "xhigh"' "$PHASING_DIR/wire-xhigh.jsonl" || fail "xhigh wire marker missing"
grep -q '"reasoning_effort": "medium"' "$PHASING_DIR/wire-medium.jsonl" || fail "medium wire marker missing"

curl -fsS -m 5 http://127.0.0.1:8080/slots >/tmp/lane-slots.json || fail "NInfer slots unavailable"
python3 - /tmp/lane-slots.json <<'PY' || exit 1
import json, sys
data = json.load(open(sys.argv[1], encoding="utf-8"))
slots = data if isinstance(data, list) else data.get("slots", data.get("data", []))
if not isinstance(slots, list) or len(slots) < 2:
    raise SystemExit(f"expected two NInfer lanes, got {slots!r}")
PY
echo "LANE_SMOKE_PASS concurrent=xhigh+medium lanes=2"
