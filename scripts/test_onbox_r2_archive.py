import hashlib
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).with_name("onbox-r2-archive.py")
SPEC = importlib.util.spec_from_file_location("onbox_r2_archive", MODULE_PATH)
archive = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(archive)


class QualificationArchiveTests(unittest.TestCase):
    def make_snapshot(self, root):
        (root / "deep.json").write_text(json.dumps({"runId": "run-1", "runEventWatermark": 2}), encoding="utf-8")
        snapshot = root / "qualification" / "snapshots" / "epoch-01-seq-2"
        snapshot.mkdir(parents=True)
        artifacts = {
            "deep.json": json.dumps({"runId": "run-1", "runEventWatermark": 2}).encode(),
            "run-events.jsonl": b'{"runId":"run-1","sequence":1}\n{"runId":"run-1","sequence":2}\n',
        }
        entries = []
        for name, data in artifacts.items():
            (snapshot / name).write_bytes(data)
            entries.append({"path": name, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest()})
        manifest = {
            "schemaVersion": "stint-deep-qualification/v1",
            "runId": "run-1",
            "lastEventSequence": 2,
            "projectionWatermark": 2,
            "artifacts": entries,
        }
        (snapshot / "qualification-manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
        return snapshot

    def test_upload_list_contains_only_manifested_snapshot_and_manifest_last(self):
        with tempfile.TemporaryDirectory() as tmp:
            snapshot = self.make_snapshot(Path(tmp))
            uploads = archive.qualification_files(tmp)
            self.assertEqual([rel for _, rel in uploads], [
                "qualification/snapshots/epoch-01-seq-2/deep.json",
                "qualification/snapshots/epoch-01-seq-2/run-events.jsonl",
                "qualification/snapshots/epoch-01-seq-2/qualification-manifest.json",
            ])
            self.assertEqual(uploads[-1][0], snapshot / "qualification-manifest.json")

    def test_tampered_or_unmanifested_snapshot_fails_closed(self):
        with tempfile.TemporaryDirectory() as tmp:
            snapshot = self.make_snapshot(Path(tmp))
            (snapshot / "deep.json").write_bytes(b"x" * (snapshot / "deep.json").stat().st_size)
            with self.assertRaisesRegex(ValueError, "digest mismatch"):
                archive.qualification_files(tmp)

        with tempfile.TemporaryDirectory() as tmp:
            snapshot = self.make_snapshot(Path(tmp))
            (snapshot / "extra.txt").write_text("unmanifested", encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "unmanifested"):
                archive.qualification_files(tmp)

    def test_discontinuous_journal_fails_closed(self):
        with tempfile.TemporaryDirectory() as tmp:
            snapshot = self.make_snapshot(Path(tmp))
            journal = b'{"runId":"run-1","sequence":1}\n{"runId":"run-1","sequence":3}\n'
            (snapshot / "run-events.jsonl").write_bytes(journal)
            manifest_path = snapshot / "qualification-manifest.json"
            manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
            entry = next(item for item in manifest["artifacts"] if item["path"] == "run-events.jsonl")
            entry["bytes"] = len(journal)
            entry["sha256"] = hashlib.sha256(journal).hexdigest()
            manifest_path.write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "discontinuous"):
                archive.qualification_files(tmp)


if __name__ == "__main__":
    unittest.main()
