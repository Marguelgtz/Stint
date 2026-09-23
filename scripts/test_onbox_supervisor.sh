#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
SUPERVISOR="$REPO_ROOT/scripts/onbox-deep-supervisor.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cat >"$TMP/stint" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
[ "${1:-}" = deep ] && [ "${2:-}" = onbox ] || exit 64
state_root="$XDG_STATE_HOME/stint/deep/fixture-session"
mkdir -p "$state_root"
printf 'fixture-session\n' >"$XDG_STATE_HOME/stint/deep/latest"
cat >"$state_root/deep.json" <<JSON
{"sessionId":"fixture-session","phase":"${FAKE_PHASE:-landed}","tasks":[]}
JSON
printf 'fixture handoff\n' >"$state_root/handoff.md"
EOF
chmod 0700 "$TMP/stint"

cat >"$TMP/archive" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
state_dir="$1"
test -r "$state_dir/deep.json"
test -r "$state_dir/handoff.md"
printf '%s\n' "$state_dir" >>"$EVENTS"
exit "${ARCHIVE_EXIT:-0}"
EOF
chmod 0700 "$TMP/archive"

run_supervisor() {
  local name="$1" archive_exit="$2" archive_path="$3" expected_status="$4" phase="${5:-landed}" status
  local root="$TMP/$name-root"
  mkdir -p "$root"
  : >"$TMP/$name-events"
  set +e
  env \
    STINT_ONBOX_BIN="$TMP/stint" \
    STINT_ONBOX_ROOT="$root" \
    STINT_ONBOX_INSTANCE_ID=4242 \
    STINT_ONBOX_DEADLINE=2099-01-01T00:00:00Z \
    STINT_ONBOX_SKIP_WATCHDOG=1 \
    STINT_ONBOX_SKIP_GITHUB=1 \
    STINT_ONBOX_R2_ARCHIVE="$archive_path" \
    STINT_ONBOX_HEARTBEAT_SECONDS=1 \
    EVENTS="$TMP/$name-events" \
    ARCHIVE_EXIT="$archive_exit" \
    FAKE_PHASE="$phase" \
    bash "$SUPERVISOR" run --mission "$TMP/mission.md" \
    >"$TMP/$name-out" 2>"$TMP/$name-err"
  status=$?
  set -e
  if [ "$expected_status" = success ] && [ "$status" -ne 0 ]; then
    cat "$TMP/$name-out" "$TMP/$name-err" >&2
    printf 'supervisor returned %s for a successful final archive\n' "$status" >&2
    return 1
  fi
  if [ "$expected_status" = failure ] && [ "$status" -eq 0 ]; then
    cat "$TMP/$name-out" "$TMP/$name-err" >&2
    printf 'supervisor returned success despite an incomplete final archive\n' >&2
    return 1
  fi
  if [ "$name" != archive-missing ] && [ ! -r "$root/state/stint/deep/fixture-session/deep.json" ]; then
    printf 'supervisor did not preserve the durable state after shutdown\n' >&2
    return 1
  fi
  if [ -n "$archive_path" ] && [ -x "$archive_path" ] && [ ! -s "$TMP/$name-events" ]; then
    cat "$TMP/$name-out" "$TMP/$name-err" >&2
    printf 'configured final archive helper was not called\n' >&2
    return 1
  fi
}

: >"$TMP/mission.md"
run_supervisor archive-success 0 "$TMP/archive" success
run_supervisor archive-failure 17 "$TMP/archive" failure
run_supervisor archive-disabled 0 "" success
run_supervisor archive-missing 0 "$TMP/missing-archive" failure
run_supervisor nonterminal-success 0 "$TMP/archive" failure executing

printf 'on-box supervisor final archive fixtures passed\n'
