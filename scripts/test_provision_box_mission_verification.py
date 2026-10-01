#!/usr/bin/env python3
"""Regression test for extracting shell commands from mission Verification."""
import subprocess
import sys
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PROVISION = ROOT / "scripts" / "provision-box.sh"
MARKER = 'TARGET_VERIFY="$(python3 - "$TARGET_MISSION" <<\'PY\'\n'


def embedded_verification_parser() -> str:
    source = PROVISION.read_text(encoding="utf-8")
    start = source.index(MARKER) + len(MARKER)
    end = source.index("\nPY\n)", start)
    return source[start:end]


def extract(parser: str, mission: str) -> str:
    with tempfile.TemporaryDirectory() as temp:
        path = Path(temp) / "mission.md"
        path.write_text(mission, encoding="utf-8")
        result = subprocess.run(
            [sys.executable, "-c", parser, str(path)],
            check=True,
            capture_output=True,
            text=True,
        )
        return result.stdout.rstrip("\n")


def main() -> None:
    parser = embedded_verification_parser()
    fenced = """# Mission

## Verification
```sh
pnpm test && pnpm typecheck && git diff --check
```

## Tasks
- [ ] TASK-001: Verify
"""
    plain = """# Mission

## Verification
pnpm test && pnpm typecheck && git diff --check

## Tasks
- [ ] TASK-001: Verify
"""
    expected = "pnpm test && pnpm typecheck && git diff --check"
    assert extract(parser, fenced) == expected, "fenced verifier retained Markdown syntax"
    assert extract(parser, plain) == expected, "plain verifier changed unexpectedly"
    print("mission Verification extraction passed for fenced and plain commands")


if __name__ == "__main__":
    main()
