#!/usr/bin/env python3
"""Build and verify Stint's immutable NInfer runtime release bundle."""

from __future__ import annotations

import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import tarfile


SOURCE_REPOSITORY = "https://github.com/sergiuszm/ninfer-4090.git"
SOURCE_COMMIT = "81b68a20a9a0d9ab47d7e5838887c6d636ab76e0"
ARTIFACT_REVISION = "18dfc887423fa5aabf3cb56fac41490e462b3fab"
ARTIFACT_SHA256 = "eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e"
ARTIFACT_SIZE_BYTES = 18210531328
CUDA_FLOOR = "12.8"
GPU_ARCHITECTURE = "89"
BASE_IMAGE_TAG = "vastai/base-image:cuda-12.8.1-cudnn-devel-ubuntu24.04-py310"
BASE_IMAGE_DIGEST = "sha256:bf6bb047dbc1105c89a5ac41b9a32205a2f2e022cb24d632d055c5b14a86f7ec"
ENTRYPOINT = "ninfer-serve"
BINARY_NAMES = ("ninfer", "ninfer-serve")
BUNDLE_FORMAT_VERSION = 1


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def asset_names() -> tuple[str, str, str]:
    prefix = f"stint-ninfer-{SOURCE_COMMIT[:8]}-sm89-linux-amd64"
    archive = f"{prefix}.tar.gz"
    return archive, f"{archive}.sha256", f"{prefix}.manifest.json"


def expected_manifest(binaries: dict[str, tuple[str, int]]) -> dict[str, object]:
    return {
        "bundleFormatVersion": BUNDLE_FORMAT_VERSION,
        "runtime": "ninfer",
        "sourceRepository": SOURCE_REPOSITORY,
        "sourceCommit": SOURCE_COMMIT,
        "platform": "linux/amd64",
        "gpuArchitecture": GPU_ARCHITECTURE,
        "cudaFloor": CUDA_FLOOR,
        "artifact": {
            "revision": ARTIFACT_REVISION,
            "sha256": ARTIFACT_SHA256,
            "sizeBytes": ARTIFACT_SIZE_BYTES,
            "format": "NInfer v2",
        },
        "baseImage": {"tag": BASE_IMAGE_TAG, "digest": BASE_IMAGE_DIGEST},
        "entrypoint": ENTRYPOINT,
        "buildIdentifier": f"ninfer-{SOURCE_COMMIT}-cuda128-sm89-v1",
        "binaries": {
            name: {"path": name, "sha256": values[0], "sizeBytes": values[1]}
            for name, values in sorted(binaries.items())
        },
    }


def _manifest_bytes(manifest: dict[str, object]) -> bytes:
    return (json.dumps(manifest, sort_keys=True, indent=2) + "\n").encode()


def package(binaries: dict[str, Path], output_dir: Path) -> tuple[Path, Path, Path]:
    if set(binaries) != set(BINARY_NAMES):
        raise ValueError(f"bundle requires exactly these binaries: {BINARY_NAMES!r}")
    for name, binary in binaries.items():
        if not binary.is_file() or binary.is_symlink():
            raise ValueError(f"runtime binary {name} must be a regular file")
        if not os.access(binary, os.X_OK):
            raise ValueError(f"runtime binary {name} is not executable")

    archive_name, checksum_name, manifest_name = asset_names()
    output_dir.mkdir(parents=True, exist_ok=True)
    binary_manifest = {
        name: (sha256_file(path), path.stat().st_size)
        for name, path in binaries.items()
    }
    manifest = expected_manifest(binary_manifest)
    manifest_data = _manifest_bytes(manifest)
    archive_path = output_dir / archive_name
    manifest_path = output_dir / manifest_name
    checksum_path = output_dir / checksum_name

    manifest_path.write_bytes(manifest_data)
    manifest_path.chmod(0o644)
    # Fix gzip and tar metadata so packaging is reproducible for a given binary.
    with archive_path.open("wb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as gz:
            with tarfile.open(fileobj=gz, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                members = [
                    (name, binaries[name], 0o755, None)
                    for name in BINARY_NAMES
                ] + [("manifest.json", None, 0o644, manifest_data)]
                for name, path, mode, data in members:
                    info = tarfile.TarInfo(name)
                    info.uid = 0
                    info.gid = 0
                    info.uname = ""
                    info.gname = ""
                    info.mtime = 0
                    info.mode = mode
                    if data is not None:
                        info.size = len(data)
                        archive.addfile(info, io.BytesIO(data))
                    else:
                        assert path is not None
                        info.size = path.stat().st_size
                        with path.open("rb") as stream:
                            archive.addfile(info, stream)
    checksum = sha256_file(archive_path)
    checksum_path.write_text(f"{checksum}  {archive_name}\n", encoding="ascii")
    checksum_path.chmod(0o644)
    verify(archive_path, manifest_path, checksum_path)
    return archive_path, checksum_path, manifest_path


def _read_manifest_member(archive: tarfile.TarFile) -> tuple[dict[str, object], bytes]:
    members = archive.getmembers()
    names = [member.name for member in members]
    expected_names = [*BINARY_NAMES, "manifest.json"]
    if sorted(names) != sorted(expected_names) or len(names) != len(expected_names):
        raise ValueError(f"unexpected archive entries: {names!r}")
    for member in members:
        path = PurePosixPath(member.name)
        if path.is_absolute() or ".." in path.parts or "\\" in member.name:
            raise ValueError(f"unsafe archive path: {member.name!r}")
        if not member.isfile():
            raise ValueError(f"archive entry is not a regular file: {member.name!r}")
    manifest_member = archive.getmember("manifest.json")
    for name in BINARY_NAMES:
        if archive.getmember(name).mode & 0o111 == 0:
            raise ValueError(f"runtime binary {name} is not executable in archive")
    stream = archive.extractfile(manifest_member)
    if stream is None:
        raise ValueError("manifest.json is unreadable")
    data = stream.read()
    manifest = json.loads(data)
    return manifest, data


def verify(archive_path: Path, manifest_path: Path, checksum_path: Path) -> dict[str, object]:
    archive_name = archive_path.name
    fields = checksum_path.read_text(encoding="ascii").strip().split()
    if len(fields) != 2 or fields[1] != archive_name or len(fields[0]) != 64:
        raise ValueError("checksum sidecar must contain the expected filename and SHA-256")
    actual_archive_hash = sha256_file(archive_path)
    if fields[0] != actual_archive_hash:
        raise ValueError("archive SHA-256 does not match checksum sidecar")
    with tarfile.open(archive_path, mode="r:gz") as archive:
        manifest, manifest_data = _read_manifest_member(archive)
        if manifest_data != manifest_path.read_bytes():
            raise ValueError("external manifest does not match archived manifest")
        binaries = {}
        for name in BINARY_NAMES:
            member = archive.getmember(name)
            binary_stream = archive.extractfile(member)
            if binary_stream is None:
                raise ValueError(f"runtime binary {name} is unreadable")
            binaries[name] = (hashlib.sha256(binary_stream.read()).hexdigest(), member.size)
        expected = expected_manifest(binaries)
        if manifest != expected:
            raise ValueError("manifest does not match the pinned NInfer production tuple")
    return manifest


def extract(archive_path: Path, manifest_path: Path, checksum_path: Path, destination: Path) -> None:
    verify(archive_path, manifest_path, checksum_path)
    destination.mkdir(parents=True, exist_ok=True, mode=0o700)
    if any(destination.iterdir()):
        raise ValueError("extraction destination must be empty")
    with tarfile.open(archive_path, mode="r:gz") as archive:
        for name in (*BINARY_NAMES, "manifest.json"):
            member = archive.getmember(name)
            source = archive.extractfile(member)
            if source is None:
                raise ValueError(f"archive entry is unreadable: {name}")
            target = destination / name
            with target.open("xb") as output:
                shutil.copyfileobj(source, output)
            target.chmod(0o755 if name in BINARY_NAMES else 0o644)


def main() -> None:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)
    for command in ("package", "verify", "extract"):
        sub = subparsers.add_parser(command)
        if command == "package":
            sub.add_argument("--ninfer", required=True, type=Path)
            sub.add_argument("--ninfer-serve", required=True, type=Path)
            sub.add_argument("--output-dir", required=True, type=Path)
        else:
            sub.add_argument("--archive", required=True, type=Path)
            sub.add_argument("--manifest", required=True, type=Path)
            sub.add_argument("--checksum", required=True, type=Path)
            if command == "extract":
                sub.add_argument("--destination", required=True, type=Path)
    args = parser.parse_args()
    if args.command == "package":
        for path in package({"ninfer": args.ninfer, "ninfer-serve": args.ninfer_serve}, args.output_dir):
            print(path)
    elif args.command == "verify":
        verify(args.archive, args.manifest, args.checksum)
        print("runtime bundle verified")
    else:
        extract(args.archive, args.manifest, args.checksum, args.destination)
        print("runtime bundle extracted safely")


if __name__ == "__main__":
    main()
