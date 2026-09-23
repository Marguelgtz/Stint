#!/usr/bin/env bash
# Build a clean on-box git repository and a deterministic, high-entropy-ish
# fixture for the forced Hermes compression smoke. Run on a fresh GPU box.
set -eu

SMOKE_REPO="${SMOKE_REPO:-/root/stint-deep-dashboard-smoke}"

if [ -e "$SMOKE_REPO" ]; then
  echo "SMOKE_SETUP_FAIL repository already exists: $SMOKE_REPO" >&2
  exit 1
fi

mkdir -p "$SMOKE_REPO"
git -C "$SMOKE_REPO" init -b main >/dev/null
git -C "$SMOKE_REPO" config user.name "Stint Compression Smoke"
git -C "$SMOKE_REPO" config user.email "compression-smoke@stint.local"

# Twelve commands in the mission each read one 16 KiB chunk. The fixture's
# generated hex fields resist low-token repetition, so the agent's tool-result
# history crosses the deliberately lowered 20K-token smoke threshold.
python3 - "$SMOKE_REPO/context-fixture.txt" <<'PY'
import hashlib
import sys

path = sys.argv[1]
with open(path, "w", encoding="ascii") as stream:
    for number in range(12288):
        digest = hashlib.sha256(f"stint-compression-smoke:{number}".encode()).hexdigest()
        stream.write(f"{number:05d} {digest} {digest[::-1]}\n")
PY

printf '%s\n' '# Hermes compression dashboard smoke fixture' > "$SMOKE_REPO/README.md"
git -C "$SMOKE_REPO" add README.md context-fixture.txt
git -C "$SMOKE_REPO" commit -m "smoke: add compression fixture" >/dev/null
echo "SMOKE_SETUP_READY repo=$SMOKE_REPO bytes=$(wc -c < "$SMOKE_REPO/context-fixture.txt")"
