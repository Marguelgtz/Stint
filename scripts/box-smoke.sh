#!/usr/bin/env bash
# P2 gate: configure the box's Hermes for Deep Work --worker hermes and
# empirically verify the exact headless invocation the Stint executor uses
# reaches the box's local model endpoint. Run OVER SSH, after
# provision-box.sh:
#
#   ssh -i <stint-key> -p <port> root@<host> 'bash -s' < scripts/box-smoke.sh
#
# Idempotent. Set STINT_PHASED=1 after box-phase-setup.sh to smoke both
# distinct reasoning routes as well as the ordinary Hermes safety gates.
# Exits non-zero if any SMOKE probe fails.
set -u
export PATH="$PATH:/usr/local/bin:$HOME/.local/bin"
HERMES_MODEL="${HERMES_MODEL:-qwen3.8-27b}"
STINT_PHASED="${STINT_PHASED:-0}"

if [ "$STINT_PHASED" = 1 ]; then
  SMOKE_PROVIDER="custom:qwen-stint-xhigh"
  SMOKE_BASE_URL="http://127.0.0.1:18091/v1"
else
  SMOKE_PROVIDER="custom"
  SMOKE_BASE_URL="http://127.0.0.1:8080/v1"
fi

fail() { echo "SMOKE_FAIL $*"; exit 1; }
command -v hermes >/dev/null 2>&1 || fail "hermes not on PATH (run provision-box.sh first)"

echo "=== SMOKE configure model (custom provider -> local NInfer) ==="
hermes config set model.provider "$SMOKE_PROVIDER"      || fail "config model.provider"
hermes config set model.base_url "$SMOKE_BASE_URL" || fail "config model.base_url"
hermes config set model.api_key dummy        || fail "config model.api_key"
hermes config set model.default "$HERMES_MODEL" || fail "config model.default"

# A setup rerun may leave the forwarders alive. Clear their append-only wire
# logs here so the assertions below prove this smoke invocation reached each
# route, rather than matching an older request.
if [ "$STINT_PHASED" = 1 ]; then
  : >"${PHASING_DIR:-/root/stint-phasing}/wire-xhigh.jsonl"
  : >"${PHASING_DIR:-/root/stint-phasing}/wire-medium.jsonl"
fi

echo "=== SMOKE configure approvals: manual (interactive) + oneshot deny ==="
hermes config set approvals.mode manual      || fail "config approvals.mode"
hermes config set approvals.single_query_mode deny || fail "config approvals.single_query_mode"
echo "SMOKE approvals.mode now: $(hermes config get approvals.mode 2>/dev/null | head -1)"
echo "SMOKE approvals.single_query_mode now: $(hermes config get approvals.single_query_mode 2>/dev/null | head -1)"

# A: the exact executor shape (minus the prompt staging) reaches the model.
echo "=== SMOKE A: provider=custom reaches local model? (bounded 150s) ==="
timeout 150 hermes chat -q "Reply with exactly: OK" --oneshot -Q \
  --provider "$SMOKE_PROVIDER" -m "$HERMES_MODEL" >/tmp/smoke_a.out 2>&1
ec=$?
echo "  $(tail -3 /tmp/smoke_a.out | tr '\n' ' ')"
echo "SMOKE A exit=$ec"
[ $ec -eq 0 ] || fail "A: hermes chat did not reach the model (exit $ec)"

if [ "$STINT_PHASED" = 1 ]; then
  echo "=== SMOKE A2: medium phase route reaches local model? (bounded 150s) ==="
  timeout 150 hermes chat -q "Reply with exactly: OK" --oneshot -Q \
    --provider custom:qwen-stint-medium -m "$HERMES_MODEL" >/tmp/smoke_a2.out 2>&1
  ec=$?
  echo "  $(tail -3 /tmp/smoke_a2.out | tr '\n' ' ')"
  echo "SMOKE A2 exit=$ec"
  [ $ec -eq 0 ] || fail "A2: medium phase did not reach the model (exit $ec)"
  XHIGH_WIRE="${PHASING_DIR:-/root/stint-phasing}/wire-xhigh.jsonl"
  MEDIUM_WIRE="${PHASING_DIR:-/root/stint-phasing}/wire-medium.jsonl"
  grep -q '"reasoning_effort": "xhigh"' "$XHIGH_WIRE" \
    || fail "A: xhigh reasoning_effort was not observed on the outbound wire"
  grep -q '"reasoning_effort": "medium"' "$MEDIUM_WIRE" \
    || fail "A2: medium reasoning_effort was not observed on the outbound wire"
  echo "SMOKE phase wire: xhigh and medium reasoning_effort observed"
fi

# B: an ordinary command runs headless inside the agent run.
echo "=== SMOKE B: ordinary command runs headless? (bounded 150s) ==="
timeout 150 hermes chat -q "Run this exact shell command and quote its output verbatim: echo CP1SMOKE_OK" \
  --oneshot -Q --provider "$SMOKE_PROVIDER" -m "$HERMES_MODEL" >/tmp/smoke_b.out 2>&1
ec=$?
echo "  $(tail -3 /tmp/smoke_b.out | tr '\n' ' ')"
echo "SMOKE B exit=$ec"
grep -q "CP1SMOKE_OK" /tmp/smoke_b.out || fail "B: expected command output missing"

# C: a genuinely destructive command must be denied without hanging in the
# headless oneshot context. Use a disposable sentinel so an approval bypass is
# detected safely: the smoke fails if the model manages to delete it.
echo "=== SMOKE C: dangerous-class command denied, not hung? (bounded 90s) ==="
mkdir -p /tmp/stint-smoke-deny-sentinel
timeout 90 hermes chat -q "Run this exact shell command: rm -rf /tmp/stint-smoke-deny-sentinel" \
  --oneshot -Q --provider "$SMOKE_PROVIDER" -m "$HERMES_MODEL" >/tmp/smoke_c.out 2>&1
ec=$?
echo "  $(tail -3 /tmp/smoke_c.out | tr '\n' ' ')"
echo "SMOKE C exit=$ec  (124 = timed out/hung; else = responded)"
[ $ec -eq 0 ] || fail "C: oneshot approval gate hung or errored (exit $ec)"
[ -d /tmp/stint-smoke-deny-sentinel ] || fail "C: dangerous command executed despite single_query_mode=deny"
rm -rf /tmp/stint-smoke-deny-sentinel

echo "SMOKE done"
