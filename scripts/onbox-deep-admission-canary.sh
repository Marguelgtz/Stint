#!/usr/bin/env bash
# Exercise one disposable, journaled Stint on-box task through the real local
# Hermes/NInfer route before the production supervisor is allowed to start.
set -Eeuo pipefail

STINT_BIN="${STINT_ONBOX_BIN:-/root/stint-onbox/bin/stint}"
INSTANCE_ID="${STINT_INSTANCE_ID:-}"
DEADLINE="${STINT_DEADLINE:-}"
MODEL="${HERMES_MODEL:-qwen3.8-27b}"
[ -x "$STINT_BIN" ] || { echo "ADMISSION_CANARY_FAIL Stint binary is missing" >&2; exit 1; }
[[ "$INSTANCE_ID" =~ ^[1-9][0-9]*$ ]] || { echo "ADMISSION_CANARY_FAIL instance identity is invalid" >&2; exit 1; }
[ -n "$DEADLINE" ] || { echo "ADMISSION_CANARY_FAIL compute deadline is missing" >&2; exit 1; }
command -v hermes >/dev/null 2>&1 || { echo "ADMISSION_CANARY_FAIL Hermes is not on PATH" >&2; exit 1; }

TMP="$(mktemp -d /tmp/stint-deep-admission.XXXXXX)"
chmod 0700 "$TMP"
trap 'rm -rf -- "$TMP"' EXIT
mkdir -p "$TMP/repo" "$TMP/state/stint" "$TMP/home"
chmod 0700 "$TMP/state" "$TMP/state/stint" "$TMP/home"

git -C "$TMP/repo" init -q -b main
git -C "$TMP/repo" config user.name "Stint Admission Canary"
git -C "$TMP/repo" config user.email "admission-canary@stint.local"
printf '# Disposable Deep Work admission canary\n' >"$TMP/repo/README.md"
git -C "$TMP/repo" add README.md
git -C "$TMP/repo" commit -qm "seed disposable admission canary"

cat >"$TMP/mission.md" <<'MISSION'
# Stint on-box admission canary

## Objective

Prove that the production on-box executor can make and verify one small change.

## Success

- The canary marker is committed and the task and landing checks pass.

## Constraints

- Work only in the supplied disposable repository.
- Do not access the network or modify files outside the repository.

## GitHub

- mode: none
- approval: internal

## Tasks

- [ ] CANARY-001: Create `admission-canary.txt` containing exactly `STINT_ADMISSION_OK` followed by one newline.
  - acceptance: the file bytes equal the admission marker and one trailing newline, with no other content.
  - verify: python3 -c "from pathlib import Path; assert Path('admission-canary.txt').read_bytes() == b'STINT_ADMISSION_OK\n'"

## Verification

python3 -c "from pathlib import Path; assert Path('admission-canary.txt').read_bytes() == b'STINT_ADMISSION_OK\n'"
MISSION
chmod 0600 "$TMP/mission.md"

python3 - "$TMP/state/stint/session.json" "$INSTANCE_ID" "$DEADLINE" <<'PY'
import datetime, json, os, sys
path, instance, deadline = sys.argv[1:]
now = datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00", "Z")
payload = {
    "instanceId": int(instance), "profile": "deep-onbox", "runtime": "ninfer",
    "runtimeRequest": "ninfer", "status": "READY", "checkpoint": "READY",
    "startedAt": now, "deadline": deadline,
}
with open(path, "w", encoding="utf-8") as stream:
    json.dump(payload, stream, separators=(",", ":"))
    stream.write("\n")
os.chmod(path, 0o600)
PY

export XDG_STATE_HOME="$TMP/state"
export HOME="${HOME:-/root}"
export STINT_ONBOX_SKIP_GITHUB=1
timeout "${STINT_ONBOX_ADMISSION_CANARY_TIMEOUT:-12m}" \
  "$STINT_BIN" deep onbox \
  --mission "$TMP/mission.md" \
  --repo "$TMP/repo" \
  --deadline "$DEADLINE" \
  --provider custom:qwen-stint-medium \
  --model "$MODEL" \
  --reasoning medium \
  --task-timeout 8m \
  --max-attempts 1 >"$TMP/coordinator.log" 2>&1 || {
    echo "ADMISSION_CANARY_FAIL journaled on-box task did not complete" >&2
    exit 1
  }

python3 - "$TMP/state/stint/deep" "$TMP/repo" <<'PY'
import glob, json, pathlib, subprocess, sys
state_root, repo = map(pathlib.Path, sys.argv[1:])
latest = (state_root / "latest").read_text(encoding="utf-8").strip()
run_dir = state_root / latest
state = json.loads((run_dir / "deep.json").read_text(encoding="utf-8"))
if state.get("phase") != "landed" or state.get("missionOutcome") != "succeeded":
    raise SystemExit("ADMISSION_CANARY_FAIL landing or mission outcome did not succeed")
if state.get("landingVerificationOutcome") != "passed":
    raise SystemExit("ADMISSION_CANARY_FAIL final generic verification did not pass")
tasks = state.get("tasks", [])
if len(tasks) != 1 or tasks[0].get("id") != "CANARY-001" or tasks[0].get("status") != "verified":
    raise SystemExit("ADMISSION_CANARY_FAIL task verification was not recorded")
receipts = [pathlib.Path(path) for path in glob.glob(str(run_dir / "executor-receipts" / "*.json"))]
if len(receipts) != 1:
    raise SystemExit("ADMISSION_CANARY_FAIL expected exactly one durable executor receipt")
receipt = json.loads(receipts[0].read_text(encoding="utf-8"))
if receipt.get("launched") is not True or receipt.get("processQuiescent") is not True:
    raise SystemExit("ADMISSION_CANARY_FAIL executor receipt does not confirm launch and quiescence")
if not receipt.get("repositoryAfterTreeSha"):
    raise SystemExit("ADMISSION_CANARY_FAIL receipt has no Git tree identity")
events = [json.loads(line) for line in (run_dir / "run-events.jsonl").read_text(encoding="utf-8").splitlines() if line]
types = [event.get("type") for event in events]
for required in ("executor.started", "executor.result", "verification.result", "task.checkpoint_created", "run.landed"):
    if required not in types:
        raise SystemExit(f"ADMISSION_CANARY_FAIL journal is missing {required}")
worktree = pathlib.Path(state.get("worktreePath", "")).resolve()
repo = repo.resolve()
if not worktree.is_relative_to(repo) or not worktree.is_dir():
    raise SystemExit("ADMISSION_CANARY_FAIL persisted Deep Work tree is not a repository worktree")
tree = subprocess.check_output(["git", "-C", str(worktree), "rev-parse", "HEAD^{tree}"], text=True).strip()
if state.get("landingCheckpointTreeSha") != tree or tasks[0].get("checkpointTreeSha") != tree:
    raise SystemExit("ADMISSION_CANARY_FAIL verified, task, and landing trees do not match")
marker = worktree / "admission-canary.txt"
if not marker.is_file() or marker.read_text(encoding="utf-8") != "STINT_ADMISSION_OK\n":
    raise SystemExit("ADMISSION_CANARY_FAIL expected Git-visible marker is missing")
PY

if pgrep -af '[h]ermes chat --query-file /tmp/stint-deep-prompt' >/dev/null 2>&1; then
  echo "ADMISSION_CANARY_FAIL Hermes executor process remains after coordinator return" >&2
  exit 1
fi

echo "ADMISSION_CANARY_OK one journaled Hermes invocation, receipt, quiescence, verifier, checkpoint, and landing confirmed"
