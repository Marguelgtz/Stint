#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
SETUP="$REPO_ROOT/scripts/box-phase-setup.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

printf '#!/usr/bin/env sh\nexit 0\n' >"$TMP/proxy"
printf '#!/usr/bin/env sh\nexit 0\n' >"$TMP/observe"
chmod 0700 "$TMP/proxy" "$TMP/observe"

set +e
output="$(PHASE_PROXY="$TMP/proxy" DEEP_OBSERVE="$TMP/observe" \
  AGENT_LOG_TAIL="$TMP/missing-log-tail.py" PHASING_DIR="$TMP/phasing" \
  bash "$SETUP" 2>&1)"
status=$?
set -e
if [ "$status" -eq 0 ] || ! grep -Fq "Deep Work agent log-tail helper is missing" <<<"$output"; then
  printf '%s\n' "$output" >&2
  echo "phase setup did not fail clearly when the log-tail helper was absent" >&2
  exit 1
fi

echo "phase setup missing log-tail helper fixture passed"
