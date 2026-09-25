#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
SMOKE="$REPO_ROOT/scripts/phase-lane-concurrency-smoke.sh"
TMP="$(mktemp -d)"
NINFER_PID=""
cleanup() {
  if [ -n "$NINFER_PID" ]; then
    kill "$NINFER_PID" 2>/dev/null || true
    wait "$NINFER_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT

mkdir -p "$TMP/home/.local/bin" "$TMP/bin" "$TMP/phasing"

cat >"$TMP/home/.local/bin/hermes" <<'SH'
#!/usr/bin/env bash
set -eu
case "$*" in
  *custom:qwen-stint-xhigh*)
    printf '%s\n' '{"reasoning_effort": "xhigh"}' >>"$PHASING_DIR/wire-xhigh.jsonl"
    printf '%s\n' 'LANE_XHIGH_OK'
    ;;
  *custom:qwen-stint-medium*)
    printf '%s\n' '{"reasoning_effort": "medium"}' >>"$PHASING_DIR/wire-medium.jsonl"
    printf '%s\n' 'LANE_MEDIUM_OK'
    ;;
  *)
    echo "unexpected Hermes invocation: $*" >&2
    exit 1
    ;;
esac
SH
chmod 0700 "$TMP/home/.local/bin/hermes"

python3 -c 'import time; time.sleep(60)' --max-concurrency 2 &
NINFER_PID=$!
export NINFER_PID

cat >"$TMP/bin/pgrep" <<'SH'
#!/usr/bin/env bash
set -eu
printf '%s\n' "$NINFER_PID"
SH

cat >"$TMP/bin/curl" <<'SH'
#!/usr/bin/env bash
set -eu
printf '%s\n' '[{}, {}]'
SH
chmod 0700 "$TMP/bin/pgrep" "$TMP/bin/curl"

output="$(
  HOME="$TMP/home" \
  PATH="$TMP/bin:/usr/bin:/bin" \
  PHASING_DIR="$TMP/phasing" \
  bash "$SMOKE" 2>&1
)"

if ! grep -Fq 'LANE_SMOKE_PASS concurrent=xhigh+medium lanes=2' <<<"$output"; then
  printf '%s\n' "$output" >&2
  echo "lane smoke did not pass when Hermes existed only under HOME/.local/bin" >&2
  exit 1
fi

echo "lane smoke local Hermes PATH fixture passed"
