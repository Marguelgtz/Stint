#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO_ROOT="$(dirname -- "$SCRIPT_DIR")"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$TMP/fake-bin" "$TMP/repo"
cat >"$TMP/fake-bin/ssh" <<'MOCK'
#!/usr/bin/env bash
set -Eeuo pipefail
remote_command="${@: -1}"
printf '%s\n' "$remote_command" >>"$STINT_TEST_SSH_LOG"
case "$remote_command" in
  "test -x '/var/lib/stint-onbox/onbox-deep-supervisor.sh'")
    exit 0
    ;;
  *onbox-deep-supervisor.sh*status*)
    printf '%s\n' 'ONBOX_SUPERVISOR_RUNNING pid=42'
    exit 0
    ;;
  *)
    printf 'unexpected remote operation: %s\n' "$remote_command" >&2
    exit 99
    ;;
esac
MOCK
chmod 0700 "$TMP/fake-bin/ssh"

git -C "$TMP/repo" init -q
git -C "$TMP/repo" config user.email fixture@example.invalid
git -C "$TMP/repo" config user.name Fixture
printf 'fixture\n' >"$TMP/repo/README.md"
git -C "$TMP/repo" add README.md
git -C "$TMP/repo" commit -q -m fixture
git -C "$TMP/repo" remote add origin https://github.com/owner/repository.git

cat >"$TMP/mission.md" <<'MISSION'
# Fixture mission

## Objective
Verify the launcher refuses to replace an active supervisor.

## Tasks
- [ ] FIXTURE-001: Preserve the active on-box run.
  - acceptance: the launcher refuses before changing remote files.
  - verify: true

## GitHub
- mode: engineering
- repository: owner/repository
- base: main
- approval: internal
MISSION

mkdir -p "$TMP/state/stint"
python3 - "$TMP/state/stint/session.json" <<'PY'
import datetime, json, sys
deadline = datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(hours=2)
payload = {
    "instanceId": 42,
    "startedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "deadline": deadline.isoformat(),
    "status": "READY",
}
with open(sys.argv[1], "w", encoding="utf-8") as stream:
    json.dump(payload, stream)
PY
printf 'fixture-key\n' >"$TMP/key"
printf '{"vast":{"api_key":"fixture"}}\n' >"$TMP/credentials.json"
printf 'fixture-token\n' >"$TMP/github-token"
printf '#!/usr/bin/env bash\nexit 0\n' >"$TMP/stint"
chmod 0700 "$TMP/stint"

export PATH="$TMP/fake-bin:$PATH"
export STINT_BOX_HOST=127.0.0.1
export STINT_BOX_PORT=2222
export STINT_BOX_KEY="$TMP/key"
export STINT_MISSION="$TMP/mission.md"
export STINT_REPO="$TMP/repo"
export STINT_GITHUB_TOKEN_FILE="$TMP/github-token"
export STINT_GITHUB_REPOSITORY=owner/repository
export STINT_GITHUB_BASE=main
export STINT_VAST_CREDENTIALS="$TMP/credentials.json"
export STINT_BIN="$TMP/stint"
export STINT_TEST_SSH_LOG="$TMP/ssh.log"
export STINT_SESSION_JSON="$TMP/state/stint/session.json"

set +e
output="$(bash "$REPO_ROOT/scripts/launch-onbox-deep.sh" 2>&1)"
status=$?
set -e
if [ "$status" -eq 0 ]; then
  printf '%s\n' "$output" >&2
  echo "launcher accepted an already-running supervisor" >&2
  exit 1
fi
grep -Fq "an on-box supervisor is already running" <<<"$output"
test "$(wc -l <"$STINT_TEST_SSH_LOG")" -eq 2
grep -Fq "test -x '/var/lib/stint-onbox/onbox-deep-supervisor.sh'" "$STINT_TEST_SSH_LOG"
if grep -Eq 'mkdir|scp|rsync|provision|starting detached' "$STINT_TEST_SSH_LOG"; then
  cat "$STINT_TEST_SSH_LOG" >&2
  echo "launcher mutated the box before checking the active supervisor" >&2
  exit 1
fi

rm -f "$STINT_TEST_SSH_LOG"
export STINT_SOURCE_HEAD=not-the-repository-head
set +e
output="$(bash "$REPO_ROOT/scripts/launch-onbox-deep.sh" 2>&1)"
status=$?
set -e
if [ "$status" -eq 0 ]; then
  printf '%s\n' "$output" >&2
  echo "launcher accepted a source HEAD different from the pre-launch contract" >&2
  exit 1
fi
grep -Fq 'repository HEAD changed after the run contract was prepared' <<<"$output"
test ! -s "$STINT_TEST_SSH_LOG"

echo "on-box launcher active-supervisor and pinned-source preflight passed"
