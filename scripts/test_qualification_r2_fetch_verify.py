import hashlib
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock


MODULE_PATH = Path(__file__).with_name("qualification-r2-fetch-verify.py")
SPEC = importlib.util.spec_from_file_location("qualification_fetch_verify", MODULE_PATH)
VERIFY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(VERIFY)


class FakeR2:
    def __init__(self, objects):
        self.objects = objects

    def list_objects_v2(self, **kwargs):
        prefix = kwargs["Prefix"]
        keys = sorted(key for key in self.objects if key.startswith(prefix))
        return {"Contents": [{"Key": key} for key in keys], "IsTruncated": False}

    def get_object(self, **kwargs):
        return {"Body": BytesBody(self.objects[kwargs["Key"]])}


class BytesBody:
    def __init__(self, content):
        self.content = content

    def read(self, limit):
        return self.content[:limit]


class OffboxQualificationTests(unittest.TestCase):
    def setUp(self):
        self.prefix = "vanta/onbox/run-1"
        content = b"# safe mission\n"
        self.manifest = json.dumps({
            "schemaVersion": "stint-deep-qualification/v1",
            "runId": "run-1",
            "artifacts": [{"path": "mission.md", "bytes": len(content), "sha256": hashlib.sha256(content).hexdigest()}],
        }).encode()
        self.objects = {
            f"{self.prefix}/qualification/snapshots/snapshot-1/mission.md": content,
            f"{self.prefix}/qualification/snapshots/snapshot-1/qualification-manifest.json": self.manifest,
        }

    def test_download_preserves_bundle_tree_and_cli_verifies_every_snapshot(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            snapshots = VERIFY.download_bundles(FakeR2(self.objects), "deep-work", self.prefix, root)
            self.assertEqual(snapshots, ["snapshot-1"])
            with mock.patch.object(VERIFY.subprocess, "run", return_value=mock.Mock(returncode=0, stdout="verified", stderr="")) as run:
                records = VERIFY.verify_downloaded_bundles(root, snapshots, "/tmp/stint", "run-1")
            self.assertEqual(records[0]["manifestSha256"], hashlib.sha256(self.manifest).hexdigest())
            self.assertEqual(run.call_args.args[0][-3:], ["verify", "--bundle", str(root / "snapshot-1")])

    def test_download_rejects_path_traversal_and_verifier_rejects_digest_mismatch(self):
        unsafe = {f"{self.prefix}/qualification/snapshots/snapshot-1/../../outside": b"x"}
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaisesRegex(ValueError, "unsafe R2 qualification key"):
                VERIFY.download_bundles(FakeR2(unsafe), "deep-work", self.prefix, Path(tmp))

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            snapshots = VERIFY.download_bundles(FakeR2(self.objects), "deep-work", self.prefix, root)
            (root / "snapshot-1" / "mission.md").write_text("X safe mission\n", encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "hash mismatch"):
                VERIFY.verify_downloaded_bundles(root, snapshots, "/tmp/stint", "run-1")

    def test_verifier_rejects_symlink_directories(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            snapshots = VERIFY.download_bundles(FakeR2(self.objects), "deep-work", self.prefix, root)
            bundle = root / "snapshot-1"
            (bundle / "linked-dir").symlink_to(bundle, target_is_directory=True)
            with self.assertRaisesRegex(ValueError, "qualification directory"):
                VERIFY.verify_downloaded_bundles(root, snapshots, "/tmp/stint", "run-1")


if __name__ == "__main__":
    unittest.main()
