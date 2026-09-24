#!/usr/bin/env python3
"""Print a bounded, redacted tail of the local Hermes agent log."""
import argparse
import re
import sys
from pathlib import Path


DEFAULT_PATH = "/root/.hermes/logs/agent.log"
MAX_LINES = 80
MAX_BYTES = 16 * 1024
SECRET_FIELD = r"(?:[A-Z0-9]+[_-])*(?:api[_-]?key|access[_-]?key(?:[_-]?id)?|access[_-]?token|refresh[_-]?token|secret(?:[_-]?access)?(?:[_-]?key)?|private[_-]?key|signing[_-]?key|encryption[_-]?key|token|password|authorization|credentials?)"
SECRET_PATTERNS = (
    (re.compile(r"(?i)(\bBearer\s+)[A-Za-z0-9._~+/-]+=*"), r"\1[REDACTED]"),
    (re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9_]{12,}|github_pat_[A-Za-z0-9_]{12,})\b"), "[REDACTED_GITHUB_TOKEN]"),
    (re.compile(r"\b(?:AKIA|ASIA)[A-Z0-9]{16}\b"), "[REDACTED_AWS_KEY_ID]"),
    (re.compile(r"\bxox[baprs]-[A-Za-z0-9-]{10,}\b"), "[REDACTED_SLACK_TOKEN]"),
    (re.compile(r"\bAIza[A-Za-z0-9_-]{35}\b"), "[REDACTED_GOOGLE_API_KEY]"),
    (re.compile(r"\bsk-[A-Za-z0-9_-]{12,}\b"), "[REDACTED_API_KEY]"),
    (re.compile(rf'''(?i)(\bAuthorization\b["']?\s*[:=]\s*)(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^,;\]\x7d]+)'''), r'\1"[REDACTED]"'),
    (re.compile(rf'''(?i)("{SECRET_FIELD}"\s*:\s*)("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s,;\]\x7d]+)'''), r'\1"[REDACTED]"'),
    (re.compile(rf'''(?i)(\b{SECRET_FIELD}\b["']?\s*[:=]\s*)(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s,;\]\x7d]+)'''), r'\1"[REDACTED]"'),
)
ANSI_ESCAPE = re.compile(r"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))")


def redact(line: str) -> str:
    line = ANSI_ESCAPE.sub("", line)
    for pattern, replacement in SECRET_PATTERNS:
        line = pattern.sub(replacement, line)
    return "".join(char for char in line if char.isprintable() or char == "\t")


def tail_log(path: str = DEFAULT_PATH, lines: int = MAX_LINES, max_bytes: int = MAX_BYTES) -> str:
    if lines < 1 or max_bytes < 1:
        raise ValueError("tail bounds must be positive")
    with open(path, "rb") as stream:
        stream.seek(0, 2)
        size = stream.tell()
        stream.seek(max(0, size - max_bytes))
        data = stream.read(max_bytes)
    decoded = data.decode("utf-8", errors="replace")
    if size > max_bytes:
        decoded = decoded.split("\n", 1)[-1]
    result = [redact(line).replace("\x00", "")[-2000:] for line in decoded.splitlines()[-lines:]]
    return "\n".join(result)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--path", default=DEFAULT_PATH)
    args = parser.parse_args()
    try:
        output = tail_log(args.path)
    except OSError as exc:
        print(f"HERMES_LOG_UNAVAILABLE {exc.__class__.__name__}: {exc}", file=sys.stderr)
        return 2
    print(output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
