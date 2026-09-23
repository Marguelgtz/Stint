#!/usr/bin/env python3
import hashlib
import io
from pathlib import Path
import tarfile
import tempfile
import unittest

import ninfer_runtime_bundle as bundle


class RuntimeBundleTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.binary = self.root / "ninfer-serve"
        self.binary.write_bytes(b"fixture runtime\n")
        self.binary.chmod(0o755)

    def tearDown(self):
        self.temp.cleanup()

    def package(self):
        return bundle.package(self.binary, self.root / "dist")

    def test_package_is_reproducible_and_verifies_on_clean_extract(self):
        archive, checksum, manifest = self.package()
        first_hash = hashlib.sha256(archive.read_bytes()).hexdigest()
        first_assets = tuple(path.read_bytes() for path in (archive, checksum, manifest))
        archive2, checksum2, manifest2 = bundle.package(self.binary, self.root / "dist2")
        self.assertEqual(first_hash, hashlib.sha256(archive2.read_bytes()).hexdigest())
        self.assertEqual(first_assets, tuple(path.read_bytes() for path in (archive2, checksum2, manifest2)))
        extracted = self.root / "clean"
        bundle.extract(archive, manifest, checksum, extracted)
        self.assertEqual((extracted / "ninfer-serve").read_bytes(), self.binary.read_bytes())
        self.assertTrue((extracted / "ninfer-serve").stat().st_mode & 0o111)
        self.assertEqual(bundle.verify(archive, manifest, checksum)["sourceCommit"], bundle.SOURCE_COMMIT)

    def test_rejects_traversal_and_link_entries_even_with_consistent_sidecar(self):
        archive_path, _, manifest_path = self.package()
        manifest_data = manifest_path.read_bytes()
        malicious = self.root / archive_path.name
        with tarfile.open(malicious, "w:gz") as archive:
            info = tarfile.TarInfo("../outside")
            info.size = 1
            archive.addfile(info, io.BytesIO(b"x"))
            info = tarfile.TarInfo("manifest.json")
            info.size = len(manifest_data)
            archive.addfile(info, io.BytesIO(manifest_data))
        checksum = self.root / "malicious.sha256"
        checksum.write_text(f"{hashlib.sha256(malicious.read_bytes()).hexdigest()}  {malicious.name}\n")
        with self.assertRaisesRegex(ValueError, "unexpected archive entries"):
            bundle.verify(malicious, manifest_path, checksum)

    def test_rejects_symlink_archive_entries(self):
        archive_path, _, manifest_path = self.package()
        manifest_data = manifest_path.read_bytes()
        malicious = self.root / archive_path.name
        with tarfile.open(malicious, "w:gz") as archive:
            info = tarfile.TarInfo("ninfer-serve")
            info.type = tarfile.SYMTYPE
            info.linkname = "../../outside"
            archive.addfile(info)
            info = tarfile.TarInfo("manifest.json")
            info.size = len(manifest_data)
            archive.addfile(info, io.BytesIO(manifest_data))
        checksum = self.root / "symlink.sha256"
        checksum.write_text(f"{hashlib.sha256(malicious.read_bytes()).hexdigest()}  {malicious.name}\n")
        with self.assertRaisesRegex(ValueError, "not a regular file"):
            bundle.verify(malicious, manifest_path, checksum)

    def test_rejects_mismatched_external_manifest(self):
        archive, checksum, manifest = self.package()
        manifest.write_text("{}\n")
        with self.assertRaisesRegex(ValueError, "external manifest"):
            bundle.verify(archive, manifest, checksum)


if __name__ == "__main__":
    unittest.main()
