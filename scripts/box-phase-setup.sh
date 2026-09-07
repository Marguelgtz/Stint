#!/usr/bin/env bash
# Configure distinct Hermes custom-provider routes for CP1 reasoning phases.
# The front ports are deliberate: Hermes matches static custom-provider
# extra_body overrides by base_url, and same-URL entries can collide.
set -u
export PATH="$PATH:/usr/local/bin:$HOME/.local/bin"

PHASE_PROXY="${PHASE_PROXY:-/root/stint-phaseproxy.py}"
HERMES_MODEL="${HERMES_MODEL:-qwen3.8-27b}"
PHASING_DIR="${PHASING_DIR:-/root/stint-phasing}"

fail() { echo "PHASE_SETUP_FAIL $*"; exit 1; }
[ -x "$PHASE_PROXY" ] || fail "phase proxy is missing or not executable: $PHASE_PROXY"
command -v hermes >/dev/null 2>&1 || fail "hermes not on PATH"
command -v python3 >/dev/null 2>&1 || fail "python3 not on PATH"

mkdir -p "$PHASING_DIR"
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
  curl -fsS -m 3 "http://127.0.0.1:${port}/v1/models" | grep -q "$HERMES_MODEL" \
    || fail "phase front ${port} is not serving ${HERMES_MODEL}"
done

# The dedicated CP1 profile owns this list. Each entry has a distinct base URL
# and a top-level OpenAI-compatible request override; Hermes carries it into
# the request body as reasoning_effort.
hermes config set custom_providers '[
  {"provider_key":"qwen-stint-xhigh","name":"qwen-stint-xhigh","base_url":"http://127.0.0.1:18091/v1","api_key":"dummy","model":"qwen3.8-27b","extra_body":{"reasoning_effort":"xhigh"}},
  {"provider_key":"qwen-stint-medium","name":"qwen-stint-medium","base_url":"http://127.0.0.1:18092/v1","api_key":"dummy","model":"qwen3.8-27b","extra_body":{"reasoning_effort":"medium"}}
]' || fail "write custom phase providers"
hermes config set model.provider custom:qwen-stint-xhigh || fail "set default phase provider"
hermes config set model.base_url http://127.0.0.1:18091/v1 || fail "set default phase base URL"
hermes config set model.api_key dummy || fail "set model API key"
hermes config set model.default "$HERMES_MODEL" || fail "set model id"

echo "PHASING_READY xhigh=http://127.0.0.1:18091/v1 medium=http://127.0.0.1:18092/v1"
