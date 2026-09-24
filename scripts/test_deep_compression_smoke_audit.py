#!/usr/bin/env python3
"""Provider-free checks for the sanitized Hermes transcript audit."""

import importlib.util
import json
import sqlite3
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("audit-deep-compression-hermes-session.py")
SPEC = importlib.util.spec_from_file_location("deep_compression_audit", SCRIPT)
AUDIT = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
SPEC.loader.exec_module(AUDIT)


class HermesAuditTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.db = Path(temp.name) / "state.db"
        with sqlite3.connect(self.db) as conn:
            conn.executescript(
                "CREATE TABLE sessions (id TEXT, started_at REAL, cwd TEXT);"
                "CREATE TABLE messages (id INTEGER, session_id TEXT, role TEXT, "
                "content TEXT, tool_calls TEXT, tool_call_id TEXT, timestamp REAL);"
            )
            conn.execute(
                "INSERT INTO sessions VALUES (?, ?, ?)",
                ("hermes-smoke", 100.0, "/repo/.stint-deep/smoke"),
            )
        self.next_id = 1

    def add_call(self, call_id, command, exit_code=0):
        with sqlite3.connect(self.db) as conn:
            conn.execute(
                "INSERT INTO messages VALUES (?, ?, ?, ?, ?, ?, ?)",
                (
                    self.next_id,
                    "hermes-smoke",
                    "assistant",
                    None,
                    json.dumps(
                        [
                            {
                                "id": call_id,
                                "type": "function",
                                "function": {
                                    "name": "terminal",
                                    "arguments": json.dumps({"command": command}),
                                },
                            }
                        ]
                    ),
                    None,
                    float(self.next_id),
                ),
            )
            self.next_id += 1
            conn.execute(
                "INSERT INTO messages VALUES (?, ?, ?, ?, ?, ?, ?)",
                (
                    self.next_id,
                    "hermes-smoke",
                    "tool",
                    json.dumps({"output": "large fixture output omitted", "exit_code": exit_code}),
                    None,
                    call_id,
                    float(self.next_id),
                ),
            )
            self.next_id += 1

    def populate_success(self, retry=False):
        for skip in range(12):
            self.add_call(
                f"dd-{skip}",
                f"dd if=context-fixture.txt bs=16384 count=1 skip={skip} status=none",
            )
        if retry:
            self.add_call(
                "dd-0-retry",
                "dd if=context-fixture.txt bs=16384 count=1 skip=0 status=none",
            )
        self.add_call(
            "write",
            "printf '%s\\n' 'twelve chunks read after compression smoke' > compression-smoke.ok",
        )
        self.add_call(
            "verify",
            "test -f compression-smoke.ok && "
            "grep -Fqx 'twelve chunks read after compression smoke' compression-smoke.ok",
        )

    def test_success_requires_ordered_reads_and_write_verify(self):
        self.populate_success()
        result = AUDIT.audit(self.db, 90.0, "/repo/.stint-deep/smoke")
        self.assertTrue(result["ok"], result)
        self.assertEqual(result["successfulChunkReads"], list(range(12)))
        self.assertEqual(result["artifactWriteExitCode"], 0)
        self.assertEqual(result["artifactVerifyExitCode"], 0)
        self.assertNotIn("large fixture output omitted", json.dumps(result))

    def test_failed_read_fails_closed(self):
        self.populate_success()
        with sqlite3.connect(self.db) as conn:
            conn.execute(
                "UPDATE messages SET content = ? WHERE tool_call_id = 'dd-4'",
                (json.dumps({"output": "dd failed", "exit_code": 1}),),
            )
        result = AUDIT.audit(self.db, 90.0, "/repo/.stint-deep/smoke")
        self.assertFalse(result["ok"])
        self.assertNotIn(4, result["successfulChunkReads"])
        self.assertIn("Hermes did not successfully call all twelve dd reads in order", result["failures"])

    def test_successful_retry_does_not_hide_the_ordered_first_pass(self):
        self.populate_success(retry=True)
        result = AUDIT.audit(self.db, 90.0, "/repo/.stint-deep/smoke")
        self.assertTrue(result["ok"], result)
        self.assertEqual(result["successfulChunkReads"], [*range(12), 0])

    def test_watched_journal_survives_hermes_compression_pruning(self):
        self.populate_success()
        with tempfile.TemporaryDirectory() as temp:
            journal = Path(temp) / "events.jsonl"
            stop = Path(temp) / "stop"
            stop.touch()
            AUDIT.watch_journal(
                self.db,
                90.0,
                "/repo/.stint-deep",
                journal,
                stop,
                0.001,
            )
            with sqlite3.connect(self.db) as conn:
                conn.execute("DELETE FROM messages")
            result = AUDIT.audit_journal(journal)
            self.assertTrue(result["ok"], result)
            encoded = journal.read_text(encoding="utf-8")
            self.assertNotIn("command", encoded)
            self.assertNotIn("context-fixture.txt", encoded)
            self.assertNotIn("compression-smoke.ok", encoded)


if __name__ == "__main__":
    unittest.main()
