#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

go build -o "$TMP/stint-real" ./cmd/stint
mkdir -p "$TMP/home/.config/stint-dryrun/stint"
printf '{"apiKey":"fixture"}\n' >"$TMP/home/.config/stint-dryrun/stint/credentials.json"
printf 'R2_ACCOUNT_ID=fixture\nR2_ACCESS_KEY_ID=fixture\nR2_SECRET_ACCESS_KEY=fixture\n' >"$TMP/home/.config/vanta-r2.env"
mkdir -p "$TMP/home/.config"
printf 'fixture-token\n' >"$TMP/token"
chmod 0600 "$TMP/token"

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
export STINT_ONBOX_RUN_ID=smoke-preflight-fixture
export STINT_ONBOX_RUN_ROOT="$TMP/run"
export STINT_ONBOX_CONFIG_ROOT="$TMP/config"
export STINT_ONBOX_ARTIFACT_DIR="$TMP/artifacts"
export STINT_ONBOX_MISSION="$REPO_ROOT/deep-work/PHASE_LANE_E2E_MISSION.md"
export STINT_ONBOX_ACTION_PLAN="$REPO_ROOT/deep-work/PHASE_LANE_E2E_PLAN_SEED.md"
export STINT_GITHUB_TOKEN_FILE="$TMP/token"
export STINT_GITHUB_REPOSITORY=owner/repository
export STINT_GITHUB_BASE=main

set +e
bash "$REPO_ROOT/scripts/run-onbox-deep-smoke.sh" >"$TMP/test-out" 2>&1
result=$?
set -e

if [ "$result" -eq 0 ]; then
  cat "$TMP/test-out" >&2
  echo "smoke fixture unexpectedly passed its deliberately blocked rental" >&2
  exit 1
fi
grep -Fq 'maximum cost $2' "$TMP/artifacts/launcher.log"
grep -Fq 'FAKE_RENT_BLOCKED' "$TMP/artifacts/launcher.log"
grep -Fxq -- '--validate-only' "$TMP/preflight-args"
grep -Fxq -- '--max-hourly-usd' "$TMP/preflight-args"
grep -Fxq -- '1.33' "$TMP/preflight-args"
grep -Fxq -- '--max-cost-usd' "$TMP/preflight-args"
grep -Fxq -- '2' "$TMP/preflight-args"
if [ -e "$TMP/run/stint/session.json" ]; then
  echo "smoke preflight fixture created local compute session state" >&2
  exit 1
fi
echo "on-box smoke CLI parser preflight passed without provider mutation"
