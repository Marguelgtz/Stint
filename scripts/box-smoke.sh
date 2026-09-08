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

if [ "$STINT_PHASED" = 1 ]; then
  config_contains() {
    local key="$1" expected="$2" actual
    actual="$(hermes config get "$key" 2>/dev/null)" \
      || fail "config get $key"
    printf '%s' "$actual" | grep -Fq "$expected" \
      || fail "config $key does not contain '$expected' (got: $actual)"
  }
  echo "=== SMOKE compression config is pinned and routed to medium ==="
  config_contains model.context_length 262144
  config_contains auxiliary.compression.provider custom
  config_contains auxiliary.compression.model "$HERMES_MODEL"
  config_contains auxiliary.compression.base_url http://127.0.0.1:18092/v1
  config_contains auxiliary.compression.extra_body reasoning_effort
  config_contains custom_providers context_length
  config_contains custom_providers 262144
  config_contains custom_providers models:
  config_contains custom_providers "$HERMES_MODEL:"
  config_contains compression.enabled true
  config_contains compression.threshold_tokens 180000
  config_contains compression.proactive_prune_tokens 48000
  config_contains compression.tail_mode lean
  echo "SMOKE compression config: enabled, 180K trigger, 48K prune, medium summary route"

  # The historical failure was NInfer's implicit 8K completion cap truncating
  # every thinking-model summary. Verify the running server carries the launch
  # flag from PR #78 before any Deep Work process can encounter that path.
  ninfer_pid="$(pgrep -xo ninfer-serve 2>/dev/null || true)"
  [ -n "$ninfer_pid" ] || fail "NInfer process is not running"
  ninfer_cmd="$(tr '\0' ' ' <"/proc/$ninfer_pid/cmdline" 2>/dev/null || true)"
  printf '%s' "$ninfer_cmd" | grep -Fq -- "--max-context 262144" \
    || fail "NInfer is not serving --max-context 262144"
  printf '%s' "$ninfer_cmd" | grep -Fq -- "--kv-capacity 262144" \
    || fail "NInfer is not serving --kv-capacity 262144"
  printf '%s' "$ninfer_cmd" | grep -Fq -- "--default-max-tokens 262144" \
    || fail "NInfer is still using the historical completion cap"
  echo "SMOKE NInfer launch: native context/KV and uncapped summary budget observed"
  OBSERVER="${PHASING_DIR:-/root/stint-phasing}/deep-observe"
  [ -x "$OBSERVER" ] || fail "Deep Work worker observer is missing"
  "$OBSERVER" >/tmp/stint-deep-observe.json || fail "Deep Work worker observer failed"
  python3 - /tmp/stint-deep-observe.json <<'PY' || fail "Deep Work worker observer emitted invalid JSON"
import json, sys
with open(sys.argv[1], encoding="utf-8") as stream:
    data = json.load(stream)
assert data.get("ninfer", {}).get("running") is True
assert "compression" in data and "phaseRoutes" in data
PY
  echo "SMOKE worker observer: sanitized NInfer, phase, and compression telemetry available"
fi

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
