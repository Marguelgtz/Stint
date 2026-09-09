#!/usr/bin/env python3
"""Fixture tests for the deterministic on-box GitHub merge gate."""
import importlib.util
import pathlib
import unittest


MODULE = pathlib.Path(__file__).with_name("onbox-github-publish.py")
SPEC = importlib.util.spec_from_file_location("onbox_publish", MODULE)
PUBLISH = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PUBLISH)


class MergeGateFixture(unittest.TestCase):
    sha = "a" * 40

    def setUp(self):
        self.cfg = {
            "repository": "o/r",
            "base": "main",
            "allowed_authors": ["alice"],
            "mode": "maintenance",
            "approval": "internal",
            "ledger": "",
        }
        self.pr = {
            "number": 7,
            "state": "open",
            "draft": False,
            "user": {"login": "alice"},
            "head": {"ref": "feature", "sha": self.sha},
            "base": {"ref": "main"},
            "mergeable": True,
            "mergeable_state": "clean",
        }
        self.context = {
            "pull": self.pr,
            "reviews": [{"user": {"login": "reviewer"}, "state": "APPROVED"}],
            "reviewThreads": [],
            "reviewThreadsError": "",
            "checkRuns": [{"name": "tests", "status": "completed", "conclusion": "success"}],
            "files": [{"filename": "README.md"}],
        }
        PUBLISH.pull_request_context = lambda _cfg, _number: self.context
        PUBLISH.api_request = lambda *_args, **_kwargs: {"state": "success"}

    def approval(self):
        return {"headSha": self.sha, "decision": "approved", "evidence": "xhigh review", "filesReviewed": ["README.md"]}

    def test_fully_eligible(self):
        allowed, reasons, _ = PUBLISH.merge_gate(self.cfg, 7, self.approval())
        self.assertTrue(allowed, reasons)

    def test_rejects_safety_conditions(self):
        for field, value, expected in [
            ("draft", True, "draft"),
            ("mergeable_state", "dirty", "MERGEABLE"),
        ]:
            with self.subTest(field=field):
                self.pr[field] = value
                allowed, reasons, _ = PUBLISH.merge_gate(self.cfg, 7, self.approval())
                self.assertFalse(allowed)
                self.assertTrue(any(expected.lower() in reason.lower() for reason in reasons), reasons)

    def test_rejects_pending_comments_deep_and_changed_head(self):
        self.context["reviewThreads"] = [{"isResolved": False, "isOutdated": False}]
        self.context["checkRuns"] = [{"name": "tests", "status": "in_progress", "conclusion": None}]
        self.pr["head"]["ref"] = "stint/deep-20260909"
        allowed, reasons, _ = PUBLISH.merge_gate(self.cfg, 7, {**self.approval(), "headSha": "b" * 40})
        self.assertFalse(allowed)
        joined = " ".join(reasons).lower()
        self.assertIn("deep work", joined)
        self.assertIn("unresolved", joined)
        self.assertIn("changed", joined)
        self.assertIn("in_progress", joined)


if __name__ == "__main__":
    unittest.main()
