#!/usr/bin/env python3
"""Download and verify qualification bundles from R2 on the operator machine."""
import argparse
import hashlib
import json
import os
import re
import subprocess
import tempfile
from pathlib import Path, PurePosixPath


MAX_ARTIFACT_BYTES = 8 * 1024 * 1024
MAX_BUNDLE_BYTES = 32 * 1024 * 1024
SESSION_ID = re.compile(r"^[A-Za-z0-9_-]{1,96}$")


def load_env(path):
    values = {}
    with open(path, encoding="utf-8") as stream:
        for raw in stream:
            line = raw.strip()
            if line.startswith("export "):
                line = line[7:]
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, value = line.split("=", 1)
            values[key.strip()] = value.strip().strip('"').strip("'")
    return values


def qualification_keys(client, bucket, prefix):
    continuation = None
    while True:
        args = {"Bucket": bucket, "Prefix": prefix.rstrip("/") + "/qualification/snapshots/"}
        if continuation:
            args["ContinuationToken"] = continuation
        page = client.list_objects_v2(**args)
        for item in page.get("Contents", []):
            yield str(item.get("Key", ""))
        if not page.get("IsTruncated"):
            return
        continuation = page.get("NextContinuationToken")
        if not continuation:
            raise ValueError("R2 returned a truncated qualification listing without a continuation token")


def download_bundles(client, bucket, prefix, destination):
    marker = prefix.rstrip("/") + "/qualification/snapshots/"
    snapshots = set()
    totals = {}
    for key in qualification_keys(client, bucket, prefix):
        if not key.startswith(marker):
            raise ValueError("R2 returned an object outside the qualification prefix")
        rel = PurePosixPath(key[len(prefix.rstrip("/")) + 1:])
        parts = rel.parts
        if len(parts) < 3 or parts[0] != "qualification" or parts[1] != "snapshots" or ".." in parts or rel.is_absolute():
            raise ValueError(f"unsafe R2 qualification key: {key!r}")
        snapshot_id = parts[2]
        if not SESSION_ID.fullmatch(snapshot_id):
            raise ValueError(f"invalid qualification snapshot ID in R2 key: {snapshot_id!r}")
        relative_file = PurePosixPath(*parts[3:])
        if not relative_file.parts or relative_file.as_posix() != "/".join(parts[3:]):
            raise ValueError(f"invalid qualification artifact key: {key!r}")
        response = client.get_object(Bucket=bucket, Key=key)
        data = response["Body"].read(MAX_ARTIFACT_BYTES + 1)
        if len(data) > MAX_ARTIFACT_BYTES:
            raise ValueError(f"R2 qualification artifact exceeds its size limit: {key}")
        snapshot_total = totals.get(snapshot_id, 0) + len(data)
        if snapshot_total > MAX_BUNDLE_BYTES:
            raise ValueError(f"R2 qualification snapshot exceeds its total size limit: {snapshot_id}")
        totals[snapshot_id] = snapshot_total
        output = destination / snapshot_id / Path(*relative_file.parts)
        output.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        output.write_bytes(data)
        os.chmod(output, 0o600)
        snapshots.add(snapshot_id)
    if not snapshots:
        raise ValueError("R2 contains no qualification snapshots for this run")
    return sorted(snapshots)


def verify_downloaded_bundles(destination, snapshots, stint_bin, expected_run_id=""):
    records = []
    for snapshot_id in snapshots:
        bundle = destination / snapshot_id
        manifest_path = bundle / "qualification-manifest.json"
        if not manifest_path.is_file() or manifest_path.is_symlink():
            raise ValueError(f"qualification manifest is missing for {snapshot_id}")
        manifest_bytes = manifest_path.read_bytes()
        if len(manifest_bytes) > MAX_ARTIFACT_BYTES:
            raise ValueError(f"qualification manifest exceeds its size limit for {snapshot_id}")
        manifest = json.loads(manifest_bytes)
        if not isinstance(manifest, dict) or not isinstance(manifest.get("artifacts"), list):
            raise ValueError(f"qualification manifest shape is invalid for {snapshot_id}")
        if expected_run_id and manifest.get("runId") != expected_run_id:
            raise ValueError(f"qualification snapshot {snapshot_id} belongs to a different run")
        expected_files = {"qualification-manifest.json"}
        for artifact in manifest.get("artifacts", []):
            if not isinstance(artifact, dict):
                raise ValueError(f"invalid artifact record in {snapshot_id}")
            if (not isinstance(artifact.get("bytes"), int) or isinstance(artifact.get("bytes"), bool) or
                    not re.fullmatch(r"[0-9a-f]{64}", str(artifact.get("sha256", "")))):
                raise ValueError(f"invalid artifact metadata in {snapshot_id}")
            rel = PurePosixPath(str(artifact.get("path", "")))
            if not rel.parts or rel.is_absolute() or ".." in rel.parts or rel.as_posix() != str(artifact.get("path", "")):
                raise ValueError(f"unsafe artifact path in {snapshot_id}: {artifact.get('path')!r}")
            path = bundle.joinpath(*rel.parts)
            if path.is_symlink() or not path.is_file() or path.stat().st_size != artifact.get("bytes") or path.stat().st_size > MAX_ARTIFACT_BYTES:
                raise ValueError(f"missing or invalid artifact in {snapshot_id}: {rel}")
            content = path.read_bytes()
            if hashlib.sha256(content).hexdigest() != artifact.get("sha256"):
                raise ValueError(f"artifact hash mismatch in {snapshot_id}: {rel}")
            expected_files.add(rel.as_posix())
        actual_files = set()
        for directory, dirnames, filenames in os.walk(bundle, followlinks=False):
            current = Path(directory)
            for dirname in dirnames:
                if (current / dirname).is_symlink():
                    raise ValueError(f"unsafe downloaded qualification directory in {snapshot_id}")
            for filename in filenames:
                path = current / filename
                if path.is_symlink() or not path.is_file():
                    raise ValueError(f"unsafe downloaded qualification file in {snapshot_id}")
                actual_files.add(path.relative_to(bundle).as_posix())
        if actual_files != expected_files:
            raise ValueError(f"qualification snapshot {snapshot_id} contains unmanifested or missing files")
        verified = subprocess.run(
            [stint_bin, "deep", "qualification", "verify", "--bundle", str(bundle)],
            check=False, text=True, capture_output=True, timeout=60,
        )
        if verified.returncode != 0:
            raise ValueError(f"Stint rejected off-box bundle {snapshot_id}: {verified.stderr[-2000:]}")
        digest = hashlib.sha256(manifest_bytes).hexdigest()
        records.append({"snapshotId": snapshot_id, "manifestSha256": digest, "stintResult": verified.stdout.strip()})
    return records


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--session", required=True)
    parser.add_argument("--stint-bin", required=True)
    parser.add_argument("--r2-env", default=os.environ.get("STINT_R2_ENV_FILE", ""))
    parser.add_argument("--bucket", default="")
    parser.add_argument("--prefix", default="")
    args = parser.parse_args()
    if not SESSION_ID.fullmatch(args.session):
        parser.error("--session is invalid")
    if not args.r2_env or not os.path.isfile(args.r2_env):
        parser.error("--r2-env or STINT_R2_ENV_FILE must name the private R2 configuration file")
    if not os.path.isfile(args.stint_bin) or not os.access(args.stint_bin, os.X_OK):
        parser.error("--stint-bin must name an executable Stint binary")
    env = load_env(args.r2_env)
    account, access, secret = env.get("R2_ACCOUNT_ID", ""), env.get("R2_ACCESS_KEY_ID", ""), env.get("R2_SECRET_ACCESS_KEY", "")
    bucket = args.bucket or env.get("R2_BUCKET", "deep-work")
    prefix = args.prefix or os.environ.get("STINT_R2_PREFIX", f"vanta/onbox/{args.session}")
    if not account or not access or not secret:
        parser.error("R2 configuration is incomplete")
    try:
        import boto3
        from botocore.client import Config
    except ImportError:
        parser.error("boto3 is required on the operator machine")
    client = boto3.client(
        "s3", endpoint_url=f"https://{account}.r2.cloudflarestorage.com",
        aws_access_key_id=access, aws_secret_access_key=secret, region_name="auto",
        config=Config(signature_version="s3v4"),
    )
    os.umask(0o077)
    with tempfile.TemporaryDirectory(prefix="stint-qualification-offbox-") as temporary:
        destination = Path(temporary)
        snapshots = download_bundles(client, bucket, prefix, destination)
        records = verify_downloaded_bundles(destination, snapshots, args.stint_bin, args.session)
    for record in records:
        print(f"OFFBOX_QUALIFICATION_VERIFIED snapshot={record['snapshotId']} manifest_sha256={record['manifestSha256']}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
