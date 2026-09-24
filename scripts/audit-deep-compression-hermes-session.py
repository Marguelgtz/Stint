#!/usr/bin/env python3
"""Audit the Hermes tool calls for one disposable Deep Work compression smoke.

The output is intentionally limited to call order and exit codes. It never
exports the fixture contents, Hermes prompt, or tool output.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import re
import sqlite3
import sys
import time
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


def _epoch(value: Any) -> float | None:
    if isinstance(value, (int, float)):
        return float(value)
    if isinstance(value, str):
        try:
            return dt.datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()
        except ValueError:
            try:
                return float(value)
            except ValueError:
                return None
    return None


def _iso_timestamp(value: Any) -> str | None:
    stamp = _epoch(value)
    if stamp is None:
        return None
    return dt.datetime.fromtimestamp(stamp, dt.timezone.utc).isoformat().replace("+00:00", "Z")


def _event_for_call(
    session_id: str,
    call_id: str,
    command: str,
    tool_result: dict[str, Any],
) -> dict[str, Any] | None:
    exit_code = tool_result.get("exit_code")
    timestamp = _iso_timestamp(tool_result.get("_timestamp"))
    if not isinstance(exit_code, int) or timestamp is None:
        return None

    normalized = _normalize(command)
    event: dict[str, Any] | None = None
    match = re.search(
        r"(?:^|[;&|]\s*)dd if=context-fixture\.txt bs=16384 count=1 skip=(\d+) status=none(?:$|[;&|\s])",
        normalized,
    )
    if match:
        skip = int(match.group(1))
        if 0 <= skip < CHUNK_COUNT:
            event = {"eventType": "chunk_read", "chunkIndex": skip}
    elif normalized == _normalize(f"printf '%s\\n' '{ARTIFACT_TEXT}' > compression-smoke.ok"):
        event = {"eventType": "artifact_write"}
    elif normalized == _normalize(
        f"test -f compression-smoke.ok && grep -Fqx '{ARTIFACT_TEXT}' compression-smoke.ok"
    ):
        event = {"eventType": "artifact_verify"}

    if event is None:
        return None
    # Keep a stable, opaque key so repeated database snapshots can be deduped
    # without writing Hermes session ids or tool call ids into the journal.
    event["callKey"] = hashlib.sha256(f"{session_id}:{call_id}".encode()).hexdigest()[:20]
    event["exitCode"] = exit_code
    event["timestamp"] = timestamp
    return event


def _collect_events(
    db_path: Path,
    started_after: float,
    worktree: str | None = None,
    worktree_root: str | None = None,
) -> dict[str, Any]:
    result: dict[str, Any] = {"events": [], "sessionIds": [], "terminalCallCount": 0, "failure": None}
    if not db_path.is_file():
        result["failure"] = "Hermes session database is missing"
        return result

    try:
        conn = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True, timeout=5)
        conn.row_factory = sqlite3.Row
        tables = {row[0] for row in conn.execute("SELECT name FROM sqlite_master WHERE type='table'")}
        if not {"sessions", "messages"}.issubset(tables):
            result["failure"] = "Hermes session database lacks sessions/messages tables"
            return result
        sessions = conn.execute(
            "SELECT id, started_at, cwd FROM sessions WHERE started_at >= ? ORDER BY started_at, id",
            (started_after,),
        ).fetchall()
        target = os.path.normpath(worktree) if worktree else None
        root = os.path.normpath(worktree_root) if worktree_root else None
        matching = [
            row for row in sessions
            if row["cwd"] and (
                (target is not None and os.path.normpath(row["cwd"]) == target)
                or (root is not None and os.path.commonpath((root, os.path.normpath(row["cwd"]))) == root)
            )
        ]
        # Hermes versions that omit cwd from the session row still leave only
        # this one new CLI conversation in the isolated box during the window.
        selected = matching or sessions
        ids = [str(row["id"]) for row in selected]
        if not ids:
            result["failure"] = "no Hermes session was created during the Deep Work task"
            return result
        result["sessionIds"] = ids
        marks = ",".join("?" for _ in ids)
        messages = conn.execute(
            f"SELECT id, session_id, role, content, tool_calls, tool_call_id, timestamp "
            f"FROM messages WHERE session_id IN ({marks}) ORDER BY timestamp, id",
            ids,
        ).fetchall()
    except sqlite3.Error as exc:
        # Database locks and transient write activity are expected while the
        # observer polls; do not copy database error details into evidence.
        result["failure"] = f"cannot read Hermes session database ({type(exc).__name__})"
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
    calls: list[tuple[str, str, str, dict[str, Any]]] = []
    for row in messages:
        if row["role"] != "assistant" or not row["tool_calls"]:
            continue
        for call_id, command in _call_parts(row["tool_calls"]):
            calls.append((str(row["session_id"]), call_id, command, tool_results.get(call_id, {})))

    result["terminalCallCount"] = len(calls)
    for session_id, call_id, command, tool_result in calls:
        event = _event_for_call(session_id, call_id, command, tool_result)
        if event is not None:
            result["events"].append(event)
    result["events"].sort(key=lambda event: _epoch(event["timestamp"]) or 0)
    return result


def _audit_events(
    events: list[dict[str, Any]],
    session_ids: list[str],
    terminal_call_count: int,
) -> dict[str, Any]:
    result: dict[str, Any] = {
        "ok": False,
        "sessionIds": session_ids,
        "successfulChunkReads": [],
        "lastChunkReadAt": None,
        "artifactWriteExitCode": None,
        "artifactWriteAt": None,
        "artifactVerifyExitCode": None,
        "artifactVerifyAt": None,
        "terminalCallCount": terminal_call_count,
        "failures": [],
    }
    successful_reads = [
        event for event in events
        if event.get("eventType") == "chunk_read" and event.get("exitCode") == 0
    ]
    result["successfulChunkReads"] = [event.get("chunkIndex") for event in successful_reads]
    first_reads: dict[int, dict[str, Any]] = {}
    for event in successful_reads:
        chunk = event.get("chunkIndex")
        if isinstance(chunk, int) and 0 <= chunk < CHUNK_COUNT:
            first_reads.setdefault(chunk, event)
    ordered_first_reads = [first_reads[index] for index in sorted(first_reads)]
    if list(first_reads) != list(range(CHUNK_COUNT)):
        result["failures"].append(
            "Hermes did not successfully call all twelve dd reads in order"
        )
    read_times = [_epoch(event.get("timestamp")) for event in ordered_first_reads]
    if any(value is None for value in read_times) or read_times != sorted(read_times):
        result["failures"].append("Hermes fixture reads did not complete in order")
    if read_times and read_times[-1] is not None:
        result["lastChunkReadAt"] = _iso_timestamp(read_times[-1])

    write_events = [event for event in events if event.get("eventType") == "artifact_write"]
    verify_events = [event for event in events if event.get("eventType") == "artifact_verify"]
    read_complete_at = (
        read_times[-1]
        if len(read_times) == CHUNK_COUNT and read_times[-1] is not None
        else None
    )
    successful_writes = [event for event in write_events if event.get("exitCode") == 0]
    chosen_write = next(
        (
            event for event in successful_writes
            if read_complete_at is not None
            and (_epoch(event.get("timestamp")) or 0) > read_complete_at
        ),
        write_events[-1] if write_events else None,
    )
    if chosen_write is not None:
        result["artifactWriteExitCode"] = chosen_write.get("exitCode")
        result["artifactWriteAt"] = chosen_write.get("timestamp")
    if result["artifactWriteExitCode"] != 0:
        result["failures"].append("Hermes artifact write command did not return exit 0")
    write_time = _epoch(result["artifactWriteAt"])
    if read_complete_at is None or write_time is None or write_time <= read_complete_at:
        result["failures"].append("Hermes artifact write did not follow the twelve successful fixture reads")

    successful_verifies = [event for event in verify_events if event.get("exitCode") == 0]
    chosen_verify = next(
        (
            event for event in successful_verifies
            if write_time is not None
            and (_epoch(event.get("timestamp")) or 0) > write_time
        ),
        verify_events[-1] if verify_events else None,
    )
    if chosen_verify is not None:
        result["artifactVerifyExitCode"] = chosen_verify.get("exitCode")
        result["artifactVerifyAt"] = chosen_verify.get("timestamp")
    if result["artifactVerifyExitCode"] != 0:
        result["failures"].append("Hermes artifact verification command did not return exit 0")
    verify_time = _epoch(result["artifactVerifyAt"])
    if write_time is None or verify_time is None or verify_time <= write_time:
        result["failures"].append("Hermes artifact verification did not follow its successful write")
    result["ok"] = not result["failures"]
    return result


def audit(db_path: Path, started_after: float, worktree: str) -> dict[str, Any]:
    collected = _collect_events(db_path, started_after, worktree=worktree)
    if collected["failure"]:
        result = _audit_events([], collected["sessionIds"], collected["terminalCallCount"])
        result["failures"].insert(0, collected["failure"])
        return result
    return _audit_events(collected["events"], collected["sessionIds"], collected["terminalCallCount"])


def audit_journal(journal_path: Path) -> dict[str, Any]:
    events: list[dict[str, Any]] = []
    failures: list[str] = []
    seen_call_keys: set[str] = set()
    try:
        with journal_path.open(encoding="utf-8") as stream:
            for line in stream:
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    failures.append("Hermes audit journal contains an invalid record")
                    continue
                if (
                    not isinstance(event, dict)
                    or event.get("eventType") not in {"chunk_read", "artifact_write", "artifact_verify"}
                ):
                    failures.append("Hermes audit journal contains an invalid event")
                    continue
                call_key = event.get("callKey")
                exit_code = event.get("exitCode")
                timestamp = _iso_timestamp(event.get("timestamp"))
                if (
                    not isinstance(call_key, str)
                    or not re.fullmatch(r"[0-9a-f]{20}", call_key)
                    or not isinstance(exit_code, int)
                    or timestamp is None
                ):
                    failures.append("Hermes audit journal contains an invalid event")
                    continue
                if call_key in seen_call_keys:
                    continue
                seen_call_keys.add(call_key)
                safe_event = {
                    "eventType": event["eventType"],
                    "callKey": call_key,
                    "exitCode": exit_code,
                    "timestamp": timestamp,
                }
                if event["eventType"] == "chunk_read":
                    chunk = event.get("chunkIndex")
                    if not isinstance(chunk, int) or not 0 <= chunk < CHUNK_COUNT:
                        failures.append("Hermes audit journal contains an invalid event")
                        continue
                    safe_event["chunkIndex"] = chunk
                events.append(safe_event)
    except OSError:
        failures.append("Hermes audit journal is missing or unreadable")
    result = _audit_events(events, [], len(events))
    result["auditSource"] = "durable-journal"
    result["failures"] = failures + result["failures"]
    result["ok"] = not result["failures"]
    return result


def watch_journal(
    db_path: Path,
    started_after: float,
    worktree_root: str,
    journal_path: Path,
    stop_file: Path,
    interval: float,
) -> None:
    journal_path.parent.mkdir(parents=True, exist_ok=True)
    seen: set[str] = set()
    while True:
        snapshot = _collect_events(db_path, started_after, worktree_root=worktree_root)
        with journal_path.open("a", encoding="utf-8") as journal:
            for event in snapshot["events"]:
                call_key = event.get("callKey")
                if not isinstance(call_key, str) or call_key in seen:
                    continue
                journal.write(json.dumps(event, separators=(",", ":")) + "\n")
                journal.flush()
                os.fsync(journal.fileno())
                seen.add(call_key)
        if stop_file.exists():
            return
        time.sleep(interval)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--db", type=Path, default=Path.home() / ".hermes/state.db")
    parser.add_argument("--started-after", type=float, help="UTC Unix timestamp")
    parser.add_argument("--worktree")
    parser.add_argument("--worktree-root")
    parser.add_argument("--journal", type=Path, help="audit a sanitized append-only journal")
    parser.add_argument("--watch-journal", type=Path, help="poll Hermes DB and append safe event metadata")
    parser.add_argument("--stop-file", type=Path, help="stop a journal watcher after this file appears")
    parser.add_argument("--watch-interval", type=float, default=0.25)
    args = parser.parse_args()
    if args.watch_journal:
        if args.started_after is None or not args.worktree_root or not args.stop_file or args.watch_interval <= 0:
            parser.error(
                "--watch-journal requires --started-after, --worktree-root, "
                "--stop-file, and a positive --watch-interval"
            )
        watch_journal(
            args.db,
            args.started_after,
            args.worktree_root,
            args.watch_journal,
            args.stop_file,
            args.watch_interval,
        )
        return 0
    if args.journal:
        result = audit_journal(args.journal)
    elif args.started_after is not None and args.worktree:
        result = audit(args.db, args.started_after, args.worktree)
    else:
        parser.error("provide --journal or both --started-after and --worktree")
    json.dump(result, sys.stdout, sort_keys=True)
    sys.stdout.write("\n")
    return 0 if result["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
