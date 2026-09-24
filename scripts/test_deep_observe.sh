#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/phasing"
: >"$TMP/hermes.log"

python3 - "$TMP/phasing" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
records = {
    "wire-xhigh.jsonl": [
        {"timestamp": "2026-09-23T02:01:00Z", "phase": "xhigh", "response_status": 200},
    ],
    "wire-medium.jsonl": [
        {"timestamp": "2026-09-23T02:02:00Z", "phase": "medium", "response_status": 200},
        {"timestamp": "2026-09-23T02:03:00Z", "phase": "medium", "response_status": 503},
        {"timestamp": "2026-09-23T01:59:00Z", "phase": "medium", "response_status": 200},
    ],
}
for name, entries in records.items():
    (root / name).write_text("".join(json.dumps(entry) + "\n" for entry in entries), encoding="utf-8")
PY

PHASING_DIR="$TMP/phasing" HERMES_LOG="$TMP/hermes.log" \
  STINT_DEEP_STARTED_AT=2026-09-23T02:00:00Z \
  "$SCRIPT_DIR/deep-observe.sh" >"$TMP/observer.json"
python3 - "$TMP/observer.json" <<'PY'
import json, sys
observer = json.load(open(sys.argv[1], encoding="utf-8"))
routes = observer["phaseRoutes"]
assert routes["xhighRequests"] == 1 and routes["xhighFailures"] == 0, routes
assert routes["mediumRequests"] == 2 and routes["mediumFailures"] == 1, routes
assert routes["mediumStatusCodes"] == {"200": 1, "503": 1}, routes
assert routes["mediumLatestFailureStatus"] == 503, routes
assert routes["mediumLatestFailureAt"] == "2026-09-23T02:03:00Z", routes
PY

python3 - "$TMP/phasing/wire-medium.jsonl" <<'PY'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
path.write_text(json.dumps({"timestamp": "2026-09-23T02:02:00Z", "phase": "medium", "response_status": 200}) + "\n", encoding="utf-8")
PY
PHASING_DIR="$TMP/phasing" HERMES_LOG="$TMP/hermes.log" \
  STINT_DEEP_STARTED_AT=2026-09-23T02:00:00Z \
  "$SCRIPT_DIR/deep-observe.sh" >"$TMP/observer.json"
python3 - "$TMP/observer.json" <<'PY'
import json, sys
routes = json.load(open(sys.argv[1], encoding="utf-8"))["phaseRoutes"]
assert routes["xhighRequests"] == 1 and routes["xhighFailures"] == 0, routes
assert routes["mediumRequests"] == 1 and routes["mediumFailures"] == 0, routes
assert routes["mediumStatusCodes"] == {"200": 1}, routes
assert routes["mediumLatestFailureStatus"] is None, routes
PY

echo "deep observer phase counts, start-time filtering, and failure counts passed"
