#!/usr/bin/env python3
"""Regression tests for the on-box publisher's authority and identity gates."""
import importlib.util
import json
import os
import pathlib
import sys
import subprocess
import tempfile
import unittest
from unittest import mock
import urllib.parse


MODULE = pathlib.Path(__file__).with_name("onbox-github-publish.py")
SPEC = importlib.util.spec_from_file_location("onbox_publish", MODULE)
PUBLISH = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PUBLISH)


class PublisherAuthorityTests(unittest.TestCase):
    def setUp(self):
        self.cfg = {
            "repository": "owner/repository",
            "base": "main",
            "mode": "engineering",
            "allowed_authors": ["alice", "bob"],
            "approval": "internal",
            "api": "https://api.github.com",
            "git_url": "https://github.com/owner/repository.git",
            "draft": True,
        }
        self.state = {
            "sessionId": "20260923-120000",
            "github": {
                "mode": "engineering",
                "repository": "owner/repository",
                "base": "main",
                "allowedAuthors": ["bob", "alice"],
                "approval": "internal",
            },
        }

    def test_publisher_policy_must_match_persisted_session(self):
        PUBLISH.assert_session_github_policy(self.cfg, self.state)
        for key, value in [
            ("mode", "maintenance"),
            ("repository", "other/repository"),
            ("base", "release"),
            ("approval", "github"),
            ("allowedAuthors", ["alice"]),
        ]:
            with self.subTest(key=key):
                changed = json.loads(json.dumps(self.state))
                changed["github"][key] = value
                with self.assertRaisesRegex(RuntimeError, "differs from persisted mission policy"):
                    PUBLISH.assert_session_github_policy(self.cfg, changed)

    def test_persisted_publication_policy_mismatch_is_not_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory, "publication.json")
            original = {"session": self.state["sessionId"], "repository": "owner/repository", "base": "old-base"}
            path.write_text(json.dumps(original), encoding="utf-8")
            PUBLISH.record_error(path, self.cfg, self.state, RuntimeError("base mismatch"))
            self.assertEqual(json.loads(path.read_text(encoding="utf-8")), original)
            error = json.loads(path.with_name("publication-error.json").read_text(encoding="utf-8"))
            self.assertIn("base mismatch", error["error"])

    def test_config_refuses_token_redirect_and_other_push_remote(self):
        with tempfile.TemporaryDirectory() as directory:
            token = pathlib.Path(directory, "token")
            token.write_text("fixture-token\n", encoding="utf-8")
            env = {
                "STINT_GITHUB_TOKEN_FILE": str(token),
                "STINT_GITHUB_REPOSITORY": "owner/repository",
                "STINT_GITHUB_BASE": "main",
            }
            with mock.patch.dict(os.environ, env, clear=True):
                cfg = PUBLISH.config()
                self.assertEqual(cfg["git_url"], self.cfg["git_url"])
            with mock.patch.dict(os.environ, {**env, "STINT_GITHUB_API_URL": "https://attacker.invalid"}, clear=True):
                with self.assertRaisesRegex(RuntimeError, "refusing to send the token elsewhere"):
                    PUBLISH.config()
            with mock.patch.dict(os.environ, {**env, "STINT_GITHUB_GIT_URL": "https://github.com/other/repo.git"}, clear=True):
                with self.assertRaisesRegex(RuntimeError, "must point to STINT_GITHUB_REPOSITORY"):
                    PUBLISH.config()

    def test_push_branch_is_session_scoped_and_cannot_target_base(self):
        worktree = "/unused"
        session = self.state["sessionId"]
        safe = f"stint/deep-{session}-01-task-001"
        with mock.patch.object(PUBLISH, "askpass_env", side_effect=AssertionError("must reject before auth")):
            for branch in ["main", "refs/tags/v1", "feature/arbitrary", f"stint/deep-other-01-task"]:
                with self.subTest(branch=branch):
                    with self.assertRaises(RuntimeError):
                        PUBLISH.push_commit(self.cfg, worktree, "a" * 40, branch, session)
            with self.assertRaisesRegex(RuntimeError, "differs from the persisted repository policy"):
                PUBLISH.push_commit({**self.cfg, "git_url": "https://github.com/other/repository.git"}, worktree, "a" * 40, safe, session)
            with self.assertRaisesRegex(RuntimeError, "configured base"):
                PUBLISH.push_commit({**self.cfg, "base": safe}, worktree, "a" * 40, safe, session)
        calls = []
        with tempfile.TemporaryDirectory() as directory:
            askpass = pathlib.Path(directory, "askpass")
            askpass.write_text("fixture", encoding="utf-8")
            with mock.patch.object(PUBLISH, "askpass_env", return_value=({}, str(askpass))), \
                 mock.patch.object(PUBLISH, "git", side_effect=lambda *args, **kwargs: calls.append(args[1:]) or ""):
                PUBLISH.push_commit(self.cfg, worktree, "a" * 40, safe, session)
            self.assertTrue(any(command == ("push", "--porcelain", self.cfg["git_url"], f"{'a' * 40}:refs/heads/{safe}") for command in calls))
            self.assertFalse(askpass.exists(), "askpass file should be removed after push")

    def test_existing_pr_search_paginates_and_fails_closed_at_limit(self):
        calls = []

        def pages(_cfg, _method, path):
            query = dict(urllib.parse.parse_qsl(urllib.parse.urlsplit(path).query))
            page = int(query["page"])
            calls.append(page)
            return [{"page": page}] * (2 if page < 3 else 1)

        with mock.patch.object(PUBLISH, "api_request", side_effect=pages):
            got = PUBLISH.api_paginated(self.cfg, "/repos/owner/repository/pulls", per_page=2, max_pages=3)
        self.assertEqual(calls, [1, 2, 3])
        self.assertEqual(len(got), 5)

        with mock.patch.object(PUBLISH, "api_request", return_value=[{}] * 2):
            with self.assertRaisesRegex(RuntimeError, "completeness limit"):
                PUBLISH.api_paginated(self.cfg, "/repos/owner/repository/pulls", per_page=2, max_pages=2)

    def test_existing_pr_must_match_expected_head_and_base(self):
        pr = {
            "number": 8,
            "base": {"ref": "main"},
            "head": {"ref": "stint/deep-s-01-task", "sha": "a" * 40, "repo": {"full_name": "owner/repository"}},
            "html_url": "https://github.com/owner/repository/pull/8",
        }
        with mock.patch.object(PUBLISH, "existing_pr", return_value=pr):
            got = PUBLISH.ensure_pr(self.cfg, session="s", branch=pr["head"]["ref"], base="main", title="t", body="b", expected_head="a" * 40)
            self.assertEqual(got["number"], 8)
            with self.assertRaisesRegex(RuntimeError, "head identity differs"):
                PUBLISH.ensure_pr(self.cfg, session="s", branch=pr["head"]["ref"], base="main", title="t", body="b", expected_head="b" * 40)
            with self.assertRaisesRegex(RuntimeError, "base"):
                PUBLISH.ensure_pr(self.cfg, session="s", branch=pr["head"]["ref"], base="release", title="t", body="b", expected_head="a" * 40)

    def test_landing_publication_requires_exact_durable_sha_and_handoff(self):
        with tempfile.TemporaryDirectory() as directory:
            repo = pathlib.Path(directory, "repo")
            repo.mkdir()

            def git(*args):
                return subprocess.run(["git", "-C", str(repo), *args], check=True, text=True, capture_output=True).stdout.strip()

            git("init", "-q", "-b", "main")
            git("config", "user.name", "Fixture")
            git("config", "user.email", "fixture@example.test")
            (repo / "README.md").write_text("fixture\n", encoding="utf-8")
            git("add", "README.md")
            git("commit", "-qm", "fixture")
            head = git("rev-parse", "HEAD")
            handoff = "Final handoff\n"
            handoff_path = pathlib.Path(directory, "handoff.md")
            handoff_path.write_text(handoff, encoding="utf-8")
            state = {
                "phase": "landed",
                "landingVerifyDone": True,
                "landingHandoff": handoff,
                "landingCommit": head,
                "handoffPath": str(handoff_path),
            }
            self.assertEqual(PUBLISH.exact_landing_commit(state, str(repo)), head)
            state["landingCommit"] = "b" * 40
            with self.assertRaisesRegex(RuntimeError, "differs from durable landingCommit"):
                PUBLISH.exact_landing_commit(state, str(repo))
            state["landingCommit"] = head
            handoff_path.write_text("changed\n", encoding="utf-8")
            with self.assertRaisesRegex(RuntimeError, "differs from persisted landingHandoff"):
                PUBLISH.exact_landing_commit(state, str(repo))

    def test_first_landing_then_resume_publishes_versioned_handoff_and_preserves_drift(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            repo = root / "repo"
            repo.mkdir()

            def git(*args):
                return subprocess.run(["git", "-C", str(repo), *args], check=True, text=True, capture_output=True).stdout.strip()

            git("init", "-q", "-b", "main")
            git("config", "user.name", "Fixture")
            git("config", "user.email", "fixture@example.test")
            (repo / "README.md").write_text("first landing\n", encoding="utf-8")
            git("add", "README.md")
            git("commit", "-qm", "first landing")
            first_head = git("rev-parse", "HEAD")
            handoff_path = root / "handoff.md"
            handoff_path.write_text("First landing\n", encoding="utf-8")
            state_path = root / "deep.json"
            publication_path = root / "publication.json"
            state = {
                "sessionId": "20260924-120000",
                "worktreePath": str(repo),
                "phase": "landed",
                "landingVerifyDone": True,
                "landingHandoff": "First landing\n",
                "landingCommit": first_head,
                "handoffPath": str(handoff_path),
                "tasks": [],
                "github": self.state["github"],
            }
            state_path.write_text(json.dumps(state), encoding="utf-8")
            ensured = []

            def ensure(_cfg, *, session, branch, base, title, body, expected_head):
                number = len(ensured) + 10
                record = {"number": number, "branch": branch, "base": base, "head": expected_head, "url": f"https://github.com/owner/repository/pull/{number}"}
                ensured.append(record)
                return {"number": number, "url": record["url"]}

            api_calls = []

            def api(_cfg, method, path, payload=None):
                api_calls.append((method, path, payload))
                if method == "GET" and "/pulls/" in path:
                    number = int(path.rsplit("/", 1)[1])
                    record = next((item for item in ensured if item["number"] == number), None)
                    if record is not None:
                        return {
                            "number": record["number"], "state": "open", "body": "Original handoff",
                            "html_url": record["url"], "base": {"ref": record["base"]},
                            "head": {"ref": record["branch"], "sha": record["head"], "repo": {"full_name": "owner/repository"}},
                        }
                return None

            cfg = {**self.cfg, "token": "fixture", "token_file": "/fixture"}
            with mock.patch.object(PUBLISH, "config", return_value=cfg), \
                 mock.patch.object(PUBLISH, "push_commit"), \
                 mock.patch.object(PUBLISH, "ensure_pr", side_effect=ensure), \
                 mock.patch.object(PUBLISH, "api_request", side_effect=api):
                PUBLISH.sync(str(root))
                first = json.loads(publication_path.read_text(encoding="utf-8"))
                self.assertEqual(first["handoff"]["branch"], "stint/deep-20260924-120000-handoff")

                (repo / "README.md").write_text("resumed landing\n", encoding="utf-8")
                git("add", "README.md")
                git("commit", "-qm", "resumed landing")
                second_head = git("rev-parse", "HEAD")
                handoff_path.write_text("Second landing\n", encoding="utf-8")
                state.update({"landingCommit": second_head, "landingHandoff": "Second landing\n"})
                state_path.write_text(json.dumps(state), encoding="utf-8")
                PUBLISH.sync(str(root))
                # Periodic publication on an unchanged resumed landing is
                # idempotent; it must not supersede its own active handoff.
                PUBLISH.sync(str(root))

            final = json.loads(publication_path.read_text(encoding="utf-8"))
            self.assertEqual(final["handoff"]["commit"], second_head)
            self.assertEqual(final["handoff"]["branch"], f"stint/deep-20260924-120000-handoff-{second_head[:12]}")
            self.assertEqual(final["handoffHistory"][0]["commit"], first_head)
            self.assertEqual(final["handoffHistory"][0]["status"], "superseded")
            self.assertEqual(final["handoffDrifts"][0]["previous"]["commit"], first_head)
            self.assertEqual(final["handoffDrifts"][0]["next"]["commit"], second_head)
            close = next((call for call in api_calls if call[0] == "PATCH"), None)
            self.assertIsNotNone(close, "old handoff PR should be closed after replacement publication")
            self.assertEqual(close[1], "/repos/owner/repository/pulls/10")
            self.assertEqual(close[2]["state"], "closed")
            self.assertIn("Superseded by", close[2]["body"])
            self.assertEqual(len([call for call in api_calls if call[0] == "PATCH"]), 1)
            self.assertEqual(len(ensured), 2, "repeat sync must reuse the active versioned handoff PR")

    def test_permanent_publication_error_returns_nonretryable_exit_code(self):
        with mock.patch.object(sys, "argv", ["publisher", "sync", "/unused"]), \
             mock.patch.object(PUBLISH, "sync", side_effect=PUBLISH.PermanentPublicationError("identity conflict")), \
             mock.patch.object(PUBLISH, "config", return_value=self.cfg), \
             mock.patch.object(PUBLISH, "record_error"):
            self.assertEqual(PUBLISH.main(), 3)


if __name__ == "__main__":
    unittest.main()
