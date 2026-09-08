#!/usr/bin/env python3
"""Archive safe final Deep Work state and handoff files to R2."""
import json
import os
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
    allowed = {"deep.json", "mission.md", "handoff.md", "incidents.jsonl"}
    uploaded = 0
    for name in sorted(allowed):
        path = os.path.join(state_dir, name)
        if os.path.isfile(path):
            client.upload_file(path, bucket, f"{prefix}/{name}", ExtraArgs={"ContentType": "application/octet-stream"})
            uploaded += 1
    print(f"R2_ARCHIVE_OK objects={uploaded} s3://{bucket}/{prefix}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

