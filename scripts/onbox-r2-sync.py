#!/usr/bin/env python3
"""Upload one sanitized on-box Deep Work heartbeat to Cloudflare R2.

The script intentionally accepts only the supervisor's already-sanitized JSON
snapshot. It never reads Hermes logs, prompts, source files, or credentials from
the Deep Work state directory.
"""
import datetime
import json
import os
import socket
import sys


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


def provenance_metadata(snapshot):
    provenance = snapshot.get("provenance") or {}
    values = {
        "stint-origin": str(provenance.get("origin") or os.environ.get("STINT_ONBOX_ORIGIN", "unknown")),
        "stint-instance-id": str(provenance.get("instanceId") or os.environ.get("STINT_ONBOX_INSTANCE_ID", "")),
        "stint-hostname": str(provenance.get("hostname") or socket.gethostname()),
        "stint-uploader": "onbox-r2-sync",
        "machine": str(provenance.get("hostname") or socket.gethostname()),
    }
    return {key: value[:256] for key, value in values.items() if value}


def main():
    if len(sys.argv) != 2:
        print("usage: onbox-r2-sync.py <sanitized-heartbeat.json>", file=sys.stderr)
        return 2
    snapshot_path = sys.argv[1]
    with open(snapshot_path, encoding="utf-8") as stream:
        snapshot = json.load(stream)
    session = snapshot.get("session", "")
    if not session or any(ch not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-" for ch in session):
        print("invalid session id", file=sys.stderr)
        return 2
    env_path = os.environ.get("STINT_R2_ENV_FILE", "/var/lib/stint-onbox/config/r2.env")
    env = load_env(env_path)
    account = env.get("R2_ACCOUNT_ID", "")
    access = env.get("R2_ACCESS_KEY_ID", "")
    secret = env.get("R2_SECRET_ACCESS_KEY", "")
    bucket = env.get("R2_BUCKET", "deep-work")
    if not account or not access or not secret or not bucket:
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
    extra = {"ContentType": "application/json", "Metadata": provenance_metadata(snapshot)}
    client.upload_file(snapshot_path, bucket, f"{prefix}/latest.json", ExtraArgs=extra)
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    client.upload_file(snapshot_path, bucket, f"{prefix}/heartbeats/{stamp}.json", ExtraArgs=extra)
    print(f"R2_HEARTBEAT_OK s3://{bucket}/{prefix}/latest.json")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
