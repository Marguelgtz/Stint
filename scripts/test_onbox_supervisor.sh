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
session_id="${FAKE_SESSION_ID:-fixture-session}"
state_root="$XDG_STATE_HOME/stint/deep/$session_id"
mkdir -p "$state_root"
printf '%s\n' "$session_id" >"$XDG_STATE_HOME/stint/deep/latest"
cat >"$state_root/deep.json" <<JSON
{"sessionId":"$session_id","phase":"${FAKE_PHASE:-landed}","tasks":[]}
JSON
printf 'fixture handoff\n' >"$state_root/handoff.md"
sleep "${FAKE_STINT_DELAY:-0}"
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
  local name="$1" archive_exit="$2" archive_path="$3" expected_status="$4" phase="${5:-landed}" observer_path="${6:-}" seed_old_session="${7:-0}" status
  local root="$TMP/$name-root"
  mkdir -p "$root"
  if [ "$seed_old_session" = 1 ]; then
    mkdir -p "$root/state/stint/deep/old-session"
    printf 'old-session\n' >"$root/state/stint/deep/latest"
    printf '{"sessionId":"old-session","phase":"stopped","tasks":[]}\n' >"$root/state/stint/deep/old-session/deep.json"
  fi
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
    STINT_ONBOX_NINFER_OBSERVER="$observer_path" \
    STINT_ONBOX_HEARTBEAT_SECONDS=1 \
    EVENTS="$TMP/$name-events" \
    ARCHIVE_EXIT="$archive_exit" \
    FAKE_PHASE="$phase" \
    FAKE_STINT_DELAY="$([ -n "$observer_path" ] && printf 2 || printf 0)" \
    OBSERVER_ARGS="$TMP/$name-observer-args" \
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
  if [ -n "$observer_path" ]; then
    if [ ! -r "$TMP/$name-observer-args" ] || \
       ! grep -Fq -- '--session-id' "$TMP/$name-observer-args" || \
       ! grep -Fq 'fixture-session' "$TMP/$name-observer-args" || \
       grep -Fq 'old-session' "$TMP/$name-observer-args"; then
      cat "$TMP/$name-observer-args" 2>/dev/null >&2 || true
      printf 'observer did not pin samples to the new Deep Work session\n' >&2
      return 1
    fi
  fi
}

: >"$TMP/mission.md"
run_supervisor archive-success 0 "$TMP/archive" success
run_supervisor archive-failure 17 "$TMP/archive" failure
run_supervisor archive-disabled 0 "" success
run_supervisor archive-missing 0 "$TMP/missing-archive" failure
run_supervisor nonterminal-success 0 "$TMP/archive" failure executing

cat >"$TMP/fake-observer.py" <<'EOF'
#!/usr/bin/env python3
import json
import os
import sys
import time
with open(os.environ["OBSERVER_ARGS"], "w", encoding="utf-8") as stream:
    json.dump(sys.argv[1:], stream)
while True:
    time.sleep(0.1)
EOF
chmod 0700 "$TMP/fake-observer.py"
run_supervisor observer-pins-new-session 0 "" success landed "$TMP/fake-observer.py" 1

cat >"$TMP/permanent-publisher" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' call >>"$PUBLISH_CALLS"
exit 3
EOF
chmod 0700 "$TMP/permanent-publisher"
: >"$TMP/publisher-calls"
printf 'fixture-token\n' >"$TMP/github-token"
set +e
env \
  STINT_ONBOX_BIN="$TMP/stint" \
  STINT_ONBOX_ROOT="$TMP/permanent-publish-root" \
  STINT_ONBOX_INSTANCE_ID=4242 \
  STINT_ONBOX_DEADLINE=2099-01-01T00:00:00Z \
  STINT_ONBOX_SKIP_WATCHDOG=1 \
  STINT_ONBOX_SKIP_GITHUB=0 \
  STINT_ONBOX_GITHUB_PUBLISH="$TMP/permanent-publisher" \
  STINT_GITHUB_TOKEN_FILE="$TMP/github-token" \
  STINT_GITHUB_REPOSITORY=owner/repository \
  STINT_GITHUB_BASE=main \
  STINT_ONBOX_PUBLISH_RETRIES=12 \
  STINT_ONBOX_PUBLISH_RETRY_SECONDS=0 \
  PUBLISH_CALLS="$TMP/publisher-calls" \
  EVENTS="$TMP/permanent-events" \
  bash "$SUPERVISOR" run --mission "$TMP/mission.md" \
  >"$TMP/permanent-out" 2>"$TMP/permanent-err"
permanent_status=$?
set -e
if [ "$permanent_status" -eq 0 ]; then
  cat "$TMP/permanent-out" "$TMP/permanent-err" >&2
  echo "supervisor treated a permanent publication identity failure as success" >&2
  exit 1
fi
grep -Fq 'stopped after permanent identity/policy failure' "$TMP/permanent-publish-root/supervisor.log"
if grep -Fq 'attempt 1/12 failed; retrying' "$TMP/permanent-publish-root/supervisor.log"; then
  echo "supervisor retried a permanent publication identity failure" >&2
  exit 1
fi

printf 'on-box supervisor final archive fixtures passed\n'
