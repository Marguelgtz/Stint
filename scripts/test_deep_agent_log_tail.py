#!/usr/bin/env python3
"""Fixture tests for bounded and secret-redacted Hermes log tails."""
import importlib.util
import pathlib
import tempfile
import unittest


MODULE = pathlib.Path(__file__).with_name("deep-agent-log-tail.py")
SPEC = importlib.util.spec_from_file_location("deep_agent_log_tail", MODULE)
TAIL = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(TAIL)


class DeepAgentLogTailTests(unittest.TestCase):
    def test_tail_is_bounded_by_lines_and_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory, "agent.log")
            path.write_text("".join(f"line {index}\n" for index in range(100)), encoding="utf-8")
            output = TAIL.tail_log(str(path), lines=7, max_bytes=4096)
            self.assertEqual(output.splitlines(), [f"line {index}" for index in range(93, 100)])

    def test_tail_redacts_common_credentials(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory, "agent.log")
            path.write_text(
                "Authorization: Bearer abcdefghijklmnop\n"
                "api_key=sk-0123456789abcdefghijkl\n"
                "github=ghp_abcdefghijklmnopqrstuvwxyz\n",
                encoding="utf-8",
            )
            output = TAIL.tail_log(str(path))
            for secret in ("abcdefghijklmnop", "sk-0123456789", "ghp_abcdefghijklmnopqrstuvwxyz"):
                self.assertNotIn(secret, output)
            self.assertIn("[REDACTED]", output)

    def test_tail_redacts_cloud_and_chat_tokens(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory, "agent.log")
            aws_key = "AKIA" + "A" * 16
            slack_token = "xoxb-" + "B" * 20
            google_key = "AIza" + "C" * 35
            path.write_text(f"{aws_key} {slack_token} {google_key}\n", encoding="utf-8")
            output = TAIL.tail_log(str(path))
            for secret in (aws_key, slack_token, google_key):
                self.assertNotIn(secret, output)

    def test_tail_redacts_json_quoted_secret_keys(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory, "agent.log")
            path.write_text(
                '{"password":"hunter2","api_key":"ordinary-secret",'
                '"authorization":"Bearer json-token-value"}\n',
                encoding="utf-8",
            )
            output = TAIL.tail_log(str(path))
            for secret in ("hunter2", "ordinary-secret", "json-token-value"):
                self.assertNotIn(secret, output)
            self.assertIn('"password":"[REDACTED]"', output)
            self.assertIn('"api_key":"[REDACTED]"', output)

    def test_tail_redacts_compound_environment_credentials_and_authorization_values(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory, "agent.log")
            path.write_text(
                "R2_SECRET_ACCESS_KEY=ordinary-secret\n"
                "AWS_ACCESS_KEY_ID=AKIAABCDEFGHIJKLMNOP\n"
                "Authorization: Basic Zm9vOmJhcg==\n"
                '{"authorization":"Basic json-user:json-password",'
                '"R2_ACCESS_KEY_ID":"compound-json-secret"}\n',
                encoding="utf-8",
            )
            output = TAIL.tail_log(str(path))
            for secret in (
                "ordinary-secret",
                "AKIAABCDEFGHIJKLMNOP",
                "Zm9vOmJhcg==",
                "json-user:json-password",
                "compound-json-secret",
            ):
                self.assertNotIn(secret, output)
            self.assertGreaterEqual(output.count("[REDACTED]"), 5)

    def test_unavailable_log_is_reported_by_cli(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(FileNotFoundError):
                TAIL.tail_log(str(pathlib.Path(directory, "missing.log")))


if __name__ == "__main__":
    unittest.main()
