#!/usr/bin/env bash
# P2 gate: configure the box's Hermes for Deep Work --worker hermes and
# empirically verify the exact headless invocation the Stint executor uses
# reaches the box's local model endpoint. Run OVER SSH, after
# provision-box.sh:
#
#   ssh -i <stint-key> -p <port> root@<host> 'bash -s' < scripts/box-smoke.sh
#
# Idempotent. Exits non-zero if any SMOKE probe fails.
set -u
export PATH="$PATH:/usr/local/bin:$HOME/.local/bin"
HERMES_MODEL="${HERMES_MODEL:-qwen3.8-27b}"

fail() { echo "SMOKE_FAIL $*"; exit 1; }
command -v hermes >/dev/null 2>&1 || fail "hermes not on PATH (run provision-box.sh first)"

echo "=== SMOKE configure model (custom provider -> local NInfer) ==="
hermes config set model.provider custom      || fail "config model.provider"
hermes config set model.base_url "http://127.0.0.1:8080/v1" || fail "config model.base_url"
hermes config set model.api_key dummy        || fail "config model.api_key"
hermes config set model.default "$HERMES_MODEL" || fail "config model.default"

echo "=== SMOKE configure approvals: manual (interactive) + oneshot deny ==="
hermes config set approvals.mode manual      || fail "config approvals.mode"
hermes config set approvals.single_query_mode deny || fail "config approvals.single_query_mode"
echo "SMOKE approvals.mode now: $(hermes approval-mode 2>/dev/null | head -1)"

# A: the exact executor shape (minus the prompt staging) reaches the model.
echo "=== SMOKE A: provider=custom reaches local model? (bounded 150s) ==="
timeout 150 hermes chat -q "Reply with exactly: OK" --oneshot -Q \
  --provider custom -m "$HERMES_MODEL" >/tmp/smoke_a.out 2>&1
ec=$?
echo "  $(tail -3 /tmp/smoke_a.out | tr '\n' ' ')"
echo "SMOKE A exit=$ec"
[ $ec -eq 0 ] || fail "A: hermes chat did not reach the model (exit $ec)"

# B: an ordinary command runs headless inside the agent run.
echo "=== SMOKE B: ordinary command runs headless? (bounded 150s) ==="
timeout 150 hermes chat -q "Run this exact shell command and quote its output verbatim: echo CP1SMOKE_OK" \
  --oneshot -Q --provider custom -m "$HERMES_MODEL" >/tmp/smoke_b.out 2>&1
ec=$?
echo "  $(tail -3 /tmp/smoke_b.out | tr '\n' ' ')"
echo "SMOKE B exit=$ec"
grep -q "CP1SMOKE_OK" /tmp/smoke_b.out || fail "B: expected command output missing"

# C: a non-ordinary (potentially dangerous) command must not hang in the
# headless oneshot context — the gate resolves (deny), exit 0 from hermes.
echo "=== SMOKE C: dangerous-class command denied, not hung? (bounded 90s) ==="
timeout 90 hermes chat -q "Run this exact shell command: id" \
  --oneshot -Q --provider custom -m "$HERMES_MODEL" >/tmp/smoke_c.out 2>&1
ec=$?
echo "  $(tail -3 /tmp/smoke_c.out | tr '\n' ' ')"
echo "SMOKE C exit=$ec  (124 = timed out/hung; else = responded)"
[ $ec -eq 0 ] || fail "C: oneshot approval gate hung or errored (exit $ec)"

echo "SMOKE done"