#!/usr/bin/env bash
# Configure distinct Hermes custom-provider routes for CP1 reasoning phases.
# The front ports are deliberate: Hermes matches static custom-provider
# extra_body overrides by base_url, and same-URL entries can collide.
set -u
export PATH="$PATH:/usr/local/bin:$HOME/.local/bin"

PHASE_PROXY="${PHASE_PROXY:-/root/stint-phaseproxy.py}"
DEEP_OBSERVE="${DEEP_OBSERVE:-/root/deep-observe.sh}"
HERMES_MODEL="${HERMES_MODEL:-qwen3.8-27b}"
PHASING_DIR="${PHASING_DIR:-/root/stint-phasing}"

fail() { echo "PHASE_SETUP_FAIL $*"; exit 1; }
[ -x "$PHASE_PROXY" ] || fail "phase proxy is missing or not executable: $PHASE_PROXY"
[ -x "$DEEP_OBSERVE" ] || fail "deep observer is missing or not executable: $DEEP_OBSERVE"
command -v hermes >/dev/null 2>&1 || fail "hermes not on PATH"
command -v python3 >/dev/null 2>&1 || fail "python3 not on PATH"

mkdir -p "$PHASING_DIR"
install -m 0755 "$DEEP_OBSERVE" "$PHASING_DIR/deep-observe"
start_proxy() {
  local port="$1" level="$2" pidfile="$PHASING_DIR/proxy-${level}.pid"
  if [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
    return
  fi
  : >"$PHASING_DIR/wire-${level}.jsonl"
  nohup python3 "$PHASE_PROXY" "$port" http://127.0.0.1:8080 \
    "$PHASING_DIR/wire-${level}.jsonl" "$level" \
    >"$PHASING_DIR/proxy-${level}.log" 2>&1 &
  echo "$!" >"$pidfile"
}

start_proxy 18091 xhigh
start_proxy 18092 medium
sleep 2
for port in 18091 18092; do
  model_payload="$(curl -fsS -m 3 "http://127.0.0.1:${port}/v1/models")" \
    || fail "phase front ${port} model probe failed"
  printf '%s' "$model_payload" | grep -q "$HERMES_MODEL" \
    || fail "phase front ${port} is not serving ${HERMES_MODEL}"
  printf '%s' "$model_payload" | grep -Eq '"context_window"[[:space:]]*:[[:space:]]*262144' \
    || fail "phase front ${port} does not advertise the native 262144-token window"
done

# The dedicated CP1 profile owns this list. Each entry has a distinct base URL
# and a top-level OpenAI-compatible request override; Hermes carries it into
# the request body as reasoning_effort.
hermes config set custom_providers '[
  {"provider_key":"qwen-stint-xhigh","name":"qwen-stint-xhigh","base_url":"http://127.0.0.1:18091/v1","api_key":"dummy","model":"qwen3.8-27b","models":{"qwen3.8-27b":{"context_length":262144}},"extra_body":{"reasoning_effort":"xhigh"}},
  {"provider_key":"qwen-stint-medium","name":"qwen-stint-medium","base_url":"http://127.0.0.1:18092/v1","api_key":"dummy","model":"qwen3.8-27b","models":{"qwen3.8-27b":{"context_length":262144}},"extra_body":{"reasoning_effort":"medium"}}
]' || fail "write custom phase providers"
hermes config set model.provider custom:qwen-stint-xhigh || fail "set default phase provider"
hermes config set model.base_url http://127.0.0.1:18091/v1 || fail "set default phase base URL"
hermes config set model.api_key dummy || fail "set model API key"
hermes config set model.default "$HERMES_MODEL" || fail "set model id"
hermes config set model.context_length 262144 || fail "pin model context length"

# Compression is a separate Hermes auxiliary task. Route it to the medium
# forwarder explicitly: named custom-provider request overrides are attached to
# the main agent route, while this task must carry its own body override. The
# 180,000-token cap leaves headroom below NInfer's native 262,144 window if a
# summary attempt or a transient connection failure consumes another turn.
hermes config set auxiliary.compression.provider custom || fail "set compression provider"
hermes config set auxiliary.compression.model "$HERMES_MODEL" || fail "set compression model"
hermes config set auxiliary.compression.base_url http://127.0.0.1:18092/v1 || fail "set compression base URL"
hermes config set auxiliary.compression.api_key dummy || fail "set compression API key"
hermes config set auxiliary.compression.timeout 300 || fail "set compression timeout"
hermes config set auxiliary.compression.extra_body '{"reasoning_effort":"medium"}' || fail "set compression reasoning"
hermes config set compression.enabled true || fail "enable compression"
hermes config set compression.threshold_tokens 180000 || fail "set compression threshold"
hermes config set compression.tail_mode lean || fail "set lean compression tail"
hermes config set compression.protect_first_n 3 || fail "set compression head protection"
hermes config set compression.protect_last_n 20 || fail "set compression tail protection"
hermes config set compression.min_tail_user_messages 1 || fail "set compression user-tail protection"
hermes config set compression.max_attempts 3 || fail "set compression attempts"
hermes config set compression.proactive_prune_tokens 48000 || fail "enable proactive tool prune"
hermes config set compression.proactive_prune_min_result_chars 8000 || fail "set prune result floor"
hermes config set compression.proactive_prune_min_reclaim_tokens 4096 || fail "set prune reclaim floor"
hermes config set compression.context_timeout_seconds 120 || fail "set compression inactivity timeout"
hermes config set compression.context_total_ceiling_seconds 600 || fail "set compression total ceiling"
hermes config set compression.in_place true || fail "set in-place compression"
hermes config set compression.micro_compact false || fail "disable per-turn micro compaction"

echo "PHASING_READY xhigh=http://127.0.0.1:18091/v1 medium=http://127.0.0.1:18092/v1"
