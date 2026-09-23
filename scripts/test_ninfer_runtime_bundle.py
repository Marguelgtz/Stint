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
        self.binaries = {name: self.root / name for name in bundle.BINARY_NAMES}
        for index, binary in enumerate(self.binaries.values()):
            binary.write_bytes(f"fixture runtime {index}\n".encode())
            binary.chmod(0o755)

    def tearDown(self):
        self.temp.cleanup()

    def package(self):
        return bundle.package(self.binaries, self.root / "dist")

    def test_package_is_reproducible_and_verifies_on_clean_extract(self):
        archive, checksum, manifest = self.package()
        first_hash = hashlib.sha256(archive.read_bytes()).hexdigest()
        first_assets = tuple(path.read_bytes() for path in (archive, checksum, manifest))
        archive2, checksum2, manifest2 = bundle.package(self.binaries, self.root / "dist2")
        self.assertEqual(first_hash, hashlib.sha256(archive2.read_bytes()).hexdigest())
        self.assertEqual(first_assets, tuple(path.read_bytes() for path in (archive2, checksum2, manifest2)))
        extracted = self.root / "clean"
        bundle.extract(archive, manifest, checksum, extracted)
        for name, binary in self.binaries.items():
            self.assertEqual((extracted / name).read_bytes(), binary.read_bytes())
            self.assertTrue((extracted / name).stat().st_mode & 0o111)
        verified = bundle.verify(archive, manifest, checksum)
        self.assertEqual(verified["sourceCommit"], bundle.SOURCE_COMMIT)
        self.assertEqual(verified["artifact"]["sizeBytes"], 18210531328)
        self.assertEqual(verified["artifact"]["format"], "NInfer v2")

    def test_rejects_traversal_and_link_entries_even_with_consistent_sidecar(self):
        archive_path, _, manifest_path = self.package()
        manifest_data = manifest_path.read_bytes()
        malicious = self.root / archive_path.name
        with tarfile.open(malicious, "w:gz") as archive:
            info = tarfile.TarInfo("../outside")
            info.size = 1
            archive.addfile(info, io.BytesIO(b"x"))
            for name, binary in self.binaries.items():
                info = tarfile.TarInfo(name)
                info.size = binary.stat().st_size
                with binary.open("rb") as stream:
                    archive.addfile(info, stream)
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
            info = tarfile.TarInfo("ninfer")
            info.size = self.binaries["ninfer"].stat().st_size
            with self.binaries["ninfer"].open("rb") as stream:
                archive.addfile(info, stream)
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

    def test_optional_release_pin_must_match_archive_bytes(self):
        archive, checksum, manifest = self.package()
        actual = hashlib.sha256(archive.read_bytes()).hexdigest()
        bundle.verify(archive, manifest, checksum, expected_archive_sha256=actual)
        with self.assertRaisesRegex(ValueError, "Stint's pinned runtime bundle"):
            bundle.verify(archive, manifest, checksum, expected_archive_sha256="0" * 64)


if __name__ == "__main__":
    unittest.main()
