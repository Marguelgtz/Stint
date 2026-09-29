#!/usr/bin/env python3
"""Archive safe final Deep Work state and handoff files to R2."""
import datetime
import hashlib
import json
import os
import re
import socket
import sys
from pathlib import Path, PurePosixPath


ALLOWED_FILES = {"deep.json", "mission.md", "handoff.md", "incidents.jsonl", "publication.json", "ninfer-runtime.jsonl"}
QUALIFICATION_ROOT = "qualification/snapshots"
MAX_ARTIFACT_BYTES = 8 * 1024 * 1024
MAX_BUNDLE_BYTES = 32 * 1024 * 1024
SNAPSHOT_ID = re.compile(r"^[A-Za-z0-9_-]{1,96}$")


def qualification_files(state_dir):
    with (Path(state_dir) / "deep.json").open(encoding="utf-8") as stream:
        state = json.load(stream)
    expected_run_id = state.get("runId") or state.get("sessionId", "")
    if not isinstance(expected_run_id, str) or not expected_run_id:
        raise ValueError("Deep Work state has no qualification run identity")
    root = Path(state_dir) / QUALIFICATION_ROOT
    if not root.exists():
        return []
    if root.is_symlink() or not root.is_dir():
        raise ValueError("qualification snapshot root is not a regular directory")
    uploads = []
    for snapshot in sorted(root.iterdir()):
        if not SNAPSHOT_ID.fullmatch(snapshot.name) or snapshot.is_symlink() or not snapshot.is_dir():
            raise ValueError(f"invalid qualification snapshot directory: {snapshot.name}")
        manifest_path = snapshot / "qualification-manifest.json"
        if manifest_path.is_symlink() or not manifest_path.is_file() or manifest_path.stat().st_size > MAX_ARTIFACT_BYTES:
            raise ValueError(f"invalid qualification manifest in snapshot {snapshot.name}")
        manifest_bytes = manifest_path.read_bytes()
        manifest = json.loads(manifest_bytes)
        if not isinstance(manifest, dict) or manifest.get("schemaVersion") != "stint-deep-qualification/v1" or manifest.get("runId") != expected_run_id:
            raise ValueError(f"invalid qualification manifest identity in snapshot {snapshot.name}")
        artifacts = manifest.get("artifacts")
        if not isinstance(artifacts, list):
            raise ValueError(f"invalid qualification artifact list in snapshot {snapshot.name}")
        expected = {"qualification-manifest.json"}
        total = 0
        for artifact in artifacts:
            if not isinstance(artifact, dict):
                raise ValueError(f"invalid qualification artifact record in snapshot {snapshot.name}")
            rel = artifact.get("path", "")
            if not isinstance(rel, str) or not isinstance(artifact.get("bytes"), int) or isinstance(artifact.get("bytes"), bool) or not re.fullmatch(r"[0-9a-f]{64}", str(artifact.get("sha256", ""))):
                raise ValueError(f"invalid qualification artifact metadata in snapshot {snapshot.name}")
            pure_rel = PurePosixPath(rel)
            parts = pure_rel.parts
            if not rel or pure_rel.is_absolute() or pure_rel.as_posix() != rel or ".." in parts or "\\" in rel or rel in expected:
                raise ValueError(f"invalid or duplicate qualification artifact path: {rel!r}")
            path = snapshot.joinpath(*parts)
            parent = path
            while parent != root:
                if parent.is_symlink():
                    raise ValueError(f"qualification artifact path contains a symlink: {rel}")
                parent = parent.parent
            if path.is_symlink() or not path.is_file():
                raise ValueError(f"missing or unsafe qualification artifact: {rel}")
            size = path.stat().st_size
            if size != artifact.get("bytes") or size > MAX_ARTIFACT_BYTES:
                raise ValueError(f"invalid qualification artifact size: {rel}")
            data = path.read_bytes()
            if hashlib.sha256(data).hexdigest() != artifact.get("sha256"):
                raise ValueError(f"qualification artifact digest mismatch: {rel}")
            total += size
            if total > MAX_BUNDLE_BYTES:
                raise ValueError(f"qualification snapshot exceeds {MAX_BUNDLE_BYTES} bytes")
            expected.add(rel)
        actual = set()
        for directory, dirnames, filenames in os.walk(snapshot, followlinks=False):
            current = Path(directory)
            for dirname in list(dirnames):
                if (current / dirname).is_symlink():
                    raise ValueError("qualification snapshot contains a symlink")
            for filename in filenames:
                path = current / filename
                if path.is_symlink() or not path.is_file():
                    raise ValueError("qualification snapshot contains a non-regular file")
                actual.add(path.relative_to(snapshot).as_posix())
        if actual != expected:
            raise ValueError(f"qualification snapshot has unmanifested or missing files: {snapshot.name}")
        journal_entry = next((entry for entry in manifest.get("artifacts", []) if entry.get("path") == "run-events.jsonl"), None)
        if journal_entry:
            sequence = 0
            with (snapshot / "run-events.jsonl").open(encoding="utf-8") as stream:
                for raw in stream:
                    event = json.loads(raw)
                    sequence += 1
                    if event.get("sequence") != sequence or event.get("runId") != manifest["runId"]:
                        raise ValueError(f"qualification journal is discontinuous: {snapshot.name}")
            if sequence != manifest.get("lastEventSequence") or sequence != manifest.get("projectionWatermark"):
                raise ValueError(f"qualification journal watermark mismatch: {snapshot.name}")
        manifest_total = total + len(manifest_bytes)
        if manifest_total > MAX_BUNDLE_BYTES:
            raise ValueError(f"qualification snapshot exceeds {MAX_BUNDLE_BYTES} bytes")
        for rel in sorted(expected - {"qualification-manifest.json"}):
            uploads.append((snapshot / rel, f"{QUALIFICATION_ROOT}/{snapshot.name}/{rel}"))
        uploads.append((manifest_path, f"{QUALIFICATION_ROOT}/{snapshot.name}/qualification-manifest.json"))
    return uploads


def load_env(path):
    values = {}
    with open(path, encoding="utf-8") as stream:
        for raw in stream:
            line = raw.strip()
            if line.startswith("export "):
                line = line[7:]
            if "=" not in line or not line or line.startswith("#"):
                continue
            key, value = line.split("=", 1)
            values[key] = value.strip().strip('"').strip("'")
    return values


def provenance(session):
    return {
        "session": session,
        "origin": os.environ.get("STINT_ONBOX_ORIGIN", "unknown"),
        "instanceId": os.environ.get("STINT_ONBOX_INSTANCE_ID", ""),
        "hostname": socket.gethostname(),
        "uploader": "onbox-r2-archive",
        "uploadedAt": datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
    }


def provenance_metadata(payload):
    values = {
        "stint-origin": str(payload.get("origin", "unknown")),
        "stint-instance-id": str(payload.get("instanceId", "")),
        "stint-hostname": str(payload.get("hostname", "")),
        "stint-uploader": "onbox-r2-archive",
        "machine": str(payload.get("hostname", "")),
    }
    return {key: value[:256] for key, value in values.items() if value}


def main():
    if len(sys.argv) != 2:
        print("usage: onbox-r2-archive.py <deep-state-directory>", file=sys.stderr)
        return 2
    state_dir = os.path.abspath(sys.argv[1])
    with open(os.path.join(state_dir, "deep.json"), encoding="utf-8") as stream:
        state = json.load(stream)
    session = state.get("sessionId", "")
    if not session or any(ch not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-" for ch in session):
        print("invalid session id", file=sys.stderr)
        return 2
    env = load_env(os.environ.get("STINT_R2_ENV_FILE", "/var/lib/stint-onbox/config/r2.env"))
    account = env.get("R2_ACCOUNT_ID", "")
    access = env.get("R2_ACCESS_KEY_ID", "")
    secret = env.get("R2_SECRET_ACCESS_KEY", "")
    bucket = env.get("R2_BUCKET", "deep-work")
    if not account or not access or not secret:
        print("R2 credentials are incomplete", file=sys.stderr)
        return 2
    try:
        import boto3
        from botocore.client import Config
    except ImportError:
        print("boto3 is not installed", file=sys.stderr)
        return 2
    client = boto3.client(
        "s3",
        endpoint_url=f"https://{account}.r2.cloudflarestorage.com",
        aws_access_key_id=access,
        aws_secret_access_key=secret,
        region_name="auto",
        config=Config(signature_version="s3v4"),
    )
    prefix = os.environ.get("STINT_R2_PREFIX", f"vanta/onbox/{session}").strip("/")
    proof = provenance(session)
    metadata = provenance_metadata(proof)
    uploaded = 0
    for name in sorted(ALLOWED_FILES):
        path = os.path.join(state_dir, name)
        if os.path.isfile(path):
            content_type = "application/json" if name.endswith(".json") else "application/octet-stream"
            client.upload_file(path, bucket, f"{prefix}/{name}", ExtraArgs={"ContentType": content_type, "Metadata": metadata})
            uploaded += 1
    try:
        for path, rel in qualification_files(state_dir):
            content_type = "application/json" if path.suffix in {".json", ".jsonl"} else "application/octet-stream"
            client.upload_file(str(path), bucket, f"{prefix}/{rel}", ExtraArgs={"ContentType": content_type, "Metadata": metadata})
            uploaded += 1
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"qualification archive validation failed: {exc}", file=sys.stderr)
        return 2
    body = (json.dumps(proof, sort_keys=True, separators=(",", ":")) + "\n").encode("utf-8")
    client.put_object(
        Bucket=bucket,
        Key=f"{prefix}/provenance.json",
        Body=body,
        ContentType="application/json",
        Metadata=metadata,
    )
    uploaded += 1
    print(f"R2_ARCHIVE_OK objects={uploaded} s3://{bucket}/{prefix}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
