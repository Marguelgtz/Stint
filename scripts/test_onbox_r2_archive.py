#!/usr/bin/env python3
"""Keep final R2 evidence within the established allow-list."""
import importlib.util
import pathlib
import unittest


MODULE = pathlib.Path(__file__).with_name("onbox-r2-archive.py")
SPEC = importlib.util.spec_from_file_location("onbox_r2_archive", MODULE)
ARCHIVE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ARCHIVE)


class R2ArchiveAllowlistTests(unittest.TestCase):
    def test_runtime_samples_are_allowed_but_raw_agent_content_is_not(self):
        self.assertIn("ninfer-runtime.jsonl", ARCHIVE.ALLOWED_FILES)
        for forbidden in ("agent.log", "prompts.jsonl", "source.tar", "credentials.json", "r2.env"):
            self.assertNotIn(forbidden, ARCHIVE.ALLOWED_FILES)


if __name__ == "__main__":
    unittest.main()
