#!/usr/bin/env python3
"""Audit the Hermes tool calls for one disposable Deep Work compression smoke.

The output is intentionally limited to call order and exit codes. It never
exports the fixture contents, Hermes prompt, or tool output.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import re
import sqlite3
import sys
from pathlib import Path
from typing import Any


CHUNK_COUNT = 12
ARTIFACT_TEXT = "twelve chunks read after compression smoke"


def _json_value(value: Any) -> Any:
    if not isinstance(value, str):
        return value
    try:
        return json.loads(value)
    except (json.JSONDecodeError, TypeError):
        return value


def _terminal_result(content: Any) -> dict[str, Any]:
    value = _json_value(content)
    if isinstance(value, dict):
        return value
    if isinstance(value, list):
        for block in value:
            if isinstance(block, dict):
                nested = _terminal_result(block.get("text", block.get("content")))
                if "exit_code" in nested:
                    return nested
    return {}


def _call_parts(raw: Any) -> list[tuple[str, str]]:
    calls = _json_value(raw)
    if not isinstance(calls, list):
        return []
    parsed: list[tuple[str, str]] = []
    for call in calls:
        if not isinstance(call, dict):
            continue
        function = call.get("function")
        if not isinstance(function, dict) or function.get("name") != "terminal":
            continue
        arguments = _json_value(function.get("arguments"))
        if not isinstance(arguments, dict):
            continue
        command = arguments.get("command")
        if isinstance(command, str):
            parsed.append((str(call.get("id") or ""), command))
    return parsed


def _normalize(command: str) -> str:
    return re.sub(r"\s+", " ", command).strip()


def audit(db_path: Path, started_after: float, worktree: str) -> dict[str, Any]:
    result: dict[str, Any] = {
        "ok": False,
        "sessionIds": [],
        "successfulChunkReads": [],
        "lastChunkReadAt": None,
        "artifactWriteExitCode": None,
        "artifactWriteAt": None,
        "artifactVerifyExitCode": None,
        "artifactVerifyAt": None,
        "failures": [],
    }
    if not db_path.is_file():
        result["failures"].append("Hermes session database is missing")
        return result

    try:
        conn = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True, timeout=5)
        conn.row_factory = sqlite3.Row
        tables = {row[0] for row in conn.execute("SELECT name FROM sqlite_master WHERE type='table'")}
        if not {"sessions", "messages"}.issubset(tables):
            result["failures"].append("Hermes session database lacks sessions/messages tables")
            return result
        sessions = conn.execute(
            "SELECT id, started_at, cwd FROM sessions WHERE started_at >= ? ORDER BY started_at, id",
            (started_after,),
        ).fetchall()
        target = os.path.normpath(worktree)
        matching = [row for row in sessions if row["cwd"] and os.path.normpath(row["cwd"]) == target]
        # Hermes versions that omit cwd from the session row still leave only
        # this one new CLI conversation in the isolated box during the window.
        selected = matching or sessions
        ids = [str(row["id"]) for row in selected]
        if not ids:
            result["failures"].append("no Hermes session was created during the Deep Work task")
            return result
        result["sessionIds"] = ids
        marks = ",".join("?" for _ in ids)
        messages = conn.execute(
            f"SELECT id, session_id, role, content, tool_calls, tool_call_id, timestamp "
            f"FROM messages WHERE session_id IN ({marks}) ORDER BY timestamp, id",
            ids,
        ).fetchall()
    except sqlite3.Error as exc:
        result["failures"].append(f"cannot read Hermes session database: {exc}")
        return result
    finally:
        if "conn" in locals():
            conn.close()

    tool_results = {
        str(row["tool_call_id"]): {
            **_terminal_result(row["content"]),
            "_timestamp": row["timestamp"],
        }
        for row in messages
        if row["role"] == "tool" and row["tool_call_id"]
    }
    calls: list[tuple[str, str, dict[str, Any]]] = []
    for row in messages:
        if row["role"] != "assistant" or not row["tool_calls"]:
            continue
        for call_id, command in _call_parts(row["tool_calls"]):
            calls.append((call_id, command, tool_results.get(call_id, {})))

    chunk_reads: list[dict[str, Any]] = []
    for call_id, command, tool_result in calls:
        normalized = _normalize(command)
        match = re.search(
            r"(?:^|[;&|]\s*)dd if=context-fixture\.txt bs=16384 count=1 skip=(\d+) status=none(?:$|[;&|\s])",
            normalized,
        )
        if match:
            try:
                skip = int(match.group(1))
            except ValueError:
                continue
            if 0 <= skip < CHUNK_COUNT:
                chunk_reads.append({
                    "skip": skip,
                    "exitCode": tool_result.get("exit_code"),
                    "timestamp": tool_result.get("_timestamp"),
                })

    result["successfulChunkReads"] = [
        read["skip"] for read in chunk_reads if read["exitCode"] == 0
    ]
    successful_read_times = [
        read["timestamp"] for read in chunk_reads
        if read["exitCode"] == 0 and read["timestamp"] is not None
    ]
    if successful_read_times != sorted(successful_read_times):
        result["failures"].append("Hermes fixture reads did not complete in order")
    if successful_read_times:
        result["lastChunkReadAt"] = dt.datetime.fromtimestamp(
            successful_read_times[-1], dt.timezone.utc
        ).isoformat().replace("+00:00", "Z")
    if result["successfulChunkReads"] != list(range(CHUNK_COUNT)):
        result["failures"].append(
            "Hermes did not successfully call all twelve dd reads in order"
        )

    write = _normalize(f"printf '%s\\n' '{ARTIFACT_TEXT}' > compression-smoke.ok")
    verify = _normalize(
        f"test -f compression-smoke.ok && grep -Fqx '{ARTIFACT_TEXT}' compression-smoke.ok"
    )
    write_timestamp = None
    verify_timestamp = None
    for _call_id, command, tool_result in calls:
        normalized = _normalize(command)
        if normalized == write:
            result["artifactWriteExitCode"] = tool_result.get("exit_code")
            if tool_result.get("_timestamp") is not None:
                write_timestamp = tool_result["_timestamp"]
                result["artifactWriteAt"] = dt.datetime.fromtimestamp(
                    write_timestamp, dt.timezone.utc
                ).isoformat().replace("+00:00", "Z")
        if normalized == verify:
            result["artifactVerifyExitCode"] = tool_result.get("exit_code")
            if tool_result.get("_timestamp") is not None:
                verify_timestamp = tool_result["_timestamp"]
                result["artifactVerifyAt"] = dt.datetime.fromtimestamp(
                    verify_timestamp, dt.timezone.utc
                ).isoformat().replace("+00:00", "Z")

    if result["artifactWriteExitCode"] != 0:
        result["failures"].append("Hermes artifact write command did not return exit 0")
    if result["artifactVerifyExitCode"] != 0:
        result["failures"].append("Hermes artifact verification command did not return exit 0")
    last_read_timestamp = successful_read_times[-1] if successful_read_times else None
    if last_read_timestamp is None or write_timestamp is None or write_timestamp <= last_read_timestamp:
        result["failures"].append("Hermes artifact write did not follow the twelve successful fixture reads")
    if write_timestamp is None or verify_timestamp is None or verify_timestamp <= write_timestamp:
        result["failures"].append("Hermes artifact verification did not follow its successful write")
    result["terminalCallCount"] = len(calls)
    result["ok"] = not result["failures"]
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--db", type=Path, default=Path.home() / ".hermes/state.db")
    parser.add_argument("--started-after", type=float, required=True, help="UTC Unix timestamp")
    parser.add_argument("--worktree", required=True)
    args = parser.parse_args()
    result = audit(args.db, args.started_after, args.worktree)
    json.dump(result, sys.stdout, sort_keys=True)
    sys.stdout.write("\n")
    return 0 if result["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
