#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

go build -o "$TMP/stint-real" "$REPO_ROOT/cmd/stint"
mkdir -p "$TMP/home/.config/stint-dryrun/stint/ssh"
printf '{"apiKey":"fixture"}\n' >"$TMP/home/.config/stint-dryrun/stint/credentials.json"
printf 'fixture-private-key\n' >"$TMP/home/.config/stint-dryrun/stint/ssh/id_ed25519"
printf 'fixture-public-key\n' >"$TMP/home/.config/stint-dryrun/stint/ssh/id_ed25519.pub"

cat >"$TMP/stint" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [ "${1:-}" = start ] && [ "${2:-}" = interactive ]; then
  for arg in "$@"; do
    if [ "$arg" = --validate-only ]; then
      printf '%s\n' "$@" >"$STINT_TEST_PREFLIGHT_MARKER"
      exec "$STINT_TEST_REAL_BIN" "$@"
    fi
  done
  echo FAKE_RENT_BLOCKED >&2
  exit 99
fi
if [ "${1:-}" = "deep" ] && [ "${2:-}" = "dash" ]; then
  exit 0
fi
if [ "${1:-}" = down ]; then
  exit 0
fi
exit 64
EOF
chmod 0700 "$TMP/stint"

export HOME="$TMP/home"
export STINT_BIN="$TMP/stint"
export STINT_TEST_REAL_BIN="$TMP/stint-real"
export STINT_TEST_PREFLIGHT_MARKER="$TMP/preflight-args"
export STINT_SMOKE_RUN_ID=deep-smoke-preflight-fixture
export STINT_SMOKE_RUN_ROOT="$TMP/run"
export STINT_SMOKE_CONFIG_ROOT="$TMP/config"
export STINT_SMOKE_ARTIFACT_DIR="$TMP/artifacts"

set +e
bash "$SCRIPT_DIR/run-deep-compression-smoke.sh" >"$TMP/test-out" 2>&1
result=$?
set -e

if [ "$result" -eq 0 ]; then
  cat "$TMP/test-out" >&2
  echo "smoke fixture unexpectedly passed its deliberately blocked rental" >&2
  exit 1
fi
grep -Fq 'start options are valid; no local credentials or provider were accessed' "$TMP/artifacts/launcher.log"
grep -Fq 'FAKE_RENT_BLOCKED' "$TMP/artifacts/launcher.log"
grep -Fq '2 clients, lane smoke=1' "$TMP/artifacts/launcher.log"
for pair in \
  '--runtime:ninfer' \
  '--ninfer-config:native' \
  '--clients:2' \
  '--hours:1.5' \
  '--max-hourly-usd:0.40' \
  '--max-cost-usd:0.60' \
  '--network-candidate-attempts:1' \
  '--min-measured-download-mbps:30' \
  '--min-network-mbps:300' \
  '--validate-only:'; do
  flag="${pair%%:*}"
  value="${pair#*:}"
  grep -Fxq -- "$flag" "$TMP/preflight-args"
  if [ -n "$value" ]; then
    grep -Fxq -- "$value" "$TMP/preflight-args"
  fi
done
if [ -e "$TMP/run/stint/session.json" ]; then
  echo "smoke preflight fixture created local compute session state" >&2
  exit 1
fi

rm -f "$STINT_TEST_PREFLIGHT_MARKER"
export STINT_SMOKE_MAX_HOURLY_USD=0.50
export STINT_SMOKE_MAX_SESSION_COST_USD=0.75
set +e
bash "$SCRIPT_DIR/run-deep-compression-smoke.sh" >"$TMP/test-budget-override-out" 2>&1
result=$?
set -e
if [ "$result" -eq 0 ]; then
  cat "$TMP/test-budget-override-out" >&2
  echo "smoke fixture unexpectedly passed its deliberately blocked rental" >&2
  exit 1
fi
grep -Fq 'max rate $0.50/hour, rental estimate cap $0.75' "$TMP/artifacts/launcher.log"
grep -Fxq -- '--max-hourly-usd' "$TMP/preflight-args"
grep -Fxq -- '0.50' "$TMP/preflight-args"
grep -Fxq -- '--max-cost-usd' "$TMP/preflight-args"
grep -Fxq -- '0.75' "$TMP/preflight-args"

rm -f "$STINT_TEST_PREFLIGHT_MARKER"
export STINT_SMOKE_MAX_HOURLY_USD=0.51
set +e
bash "$SCRIPT_DIR/run-deep-compression-smoke.sh" >"$TMP/test-over-limit-out" 2>&1
result=$?
set -e
if [ "$result" -eq 0 ] || [ -e "$STINT_TEST_PREFLIGHT_MARKER" ]; then
  cat "$TMP/test-over-limit-out" >&2
  echo "over-limit smoke settings reached rental preflight" >&2
  exit 1
fi
grep -Fq 'smoke limits cannot exceed $0.50/hour or a $0.75 rental estimate' "$TMP/artifacts/launcher.log"

rm -f "$STINT_TEST_PREFLIGHT_MARKER"
export STINT_SMOKE_MAX_HOURLY_USD=0.50
export STINT_SMOKE_MAX_SESSION_COST_USD=0.76
set +e
bash "$SCRIPT_DIR/run-deep-compression-smoke.sh" >"$TMP/test-over-total-out" 2>&1
result=$?
set -e
if [ "$result" -eq 0 ] || [ -e "$STINT_TEST_PREFLIGHT_MARKER" ]; then
  cat "$TMP/test-over-total-out" >&2
  echo "over-limit smoke total reached rental preflight" >&2
  exit 1
fi
grep -Fq 'smoke limits cannot exceed $0.50/hour or a $0.75 rental estimate' "$TMP/artifacts/launcher.log"

rm -f "$STINT_TEST_PREFLIGHT_MARKER"
export STINT_SMOKE_CLIENTS=1
export STINT_SMOKE_MAX_HOURLY_USD=0.50
set +e
bash "$SCRIPT_DIR/run-deep-compression-smoke.sh" >"$TMP/test-invalid-lanes-out" 2>&1
result=$?
set -e
if [ "$result" -eq 0 ] || [ -e "$STINT_TEST_PREFLIGHT_MARKER" ]; then
  cat "$TMP/test-invalid-lanes-out" >&2
  echo "incompatible lane settings reached the rental preflight" >&2
  exit 1
fi
grep -Fq 'concurrent lane smoke requires STINT_SMOKE_CLIENTS=2' "$TMP/artifacts/launcher.log"

LAUNCHER="$SCRIPT_DIR/run-deep-compression-smoke.sh"
fixture_line="$(grep -nF 'SMOKE_REPO=/root/stint-deep-dashboard-smoke bash -s' "$LAUNCHER" | head -n 1 | cut -d: -f1)"
provision_line="$(grep -nF 'STINT_TARGET_REPO=/root/stint-deep-dashboard-smoke' "$LAUNCHER" | head -n 1 | cut -d: -f1)"
if [ -z "$fixture_line" ] || [ -z "$provision_line" ] || [ "$fixture_line" -ge "$provision_line" ]; then
  echo "compression fixture must be created before provisioning validates STINT_TARGET_REPO" >&2
  exit 1
fi
grep -Fq 'down --yes' "$LAUNCHER" || {
  echo "unattended smoke cleanup must confirm teardown with --yes" >&2
  exit 1
}
if grep -Fq -- '--worker hermes' "$LAUNCHER"; then
  echo "Deep Work start must not pass the removed --worker flag" >&2
  exit 1
fi
grep -Fq '"$REPO_ROOT/scripts/deep-agent-log-tail.py"' "$LAUNCHER" || {
  echo "fresh-box smoke must transfer the dashboard log-tail helper" >&2
  exit 1
}
grep -Fq 'AGENT_LOG_TAIL=/root/deep-agent-log-tail.py' "$LAUNCHER" || {
  echo "fresh-box smoke must install the transferred log-tail helper" >&2
  exit 1
}
echo "deep-compression smoke preflight passed without provider mutation"
