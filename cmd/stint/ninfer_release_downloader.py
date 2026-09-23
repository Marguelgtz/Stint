#!/usr/bin/env python3
"""Download a pinned NInfer release asset using verified HTTP byte ranges."""

import argparse
import concurrent.futures
import hashlib
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import time
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


BLOCK_BYTES = 1024 * 1024
DEFAULT_CHUNK_BYTES = 16 * 1024 * 1024
DEFAULT_WORKERS = 8
MAX_ATTEMPTS = 5
REQUEST_TIMEOUT_SECONDS = 60


class DownloadError(RuntimeError):
    pass


class RangeUnsupported(DownloadError):
    pass


def reject_symlink(path):
    try:
        mode = path.lstat().st_mode
    except FileNotFoundError:
        return
    if stat.S_ISLNK(mode):
        raise DownloadError(f"refusing symlink download path: {path}")
    if not stat.S_ISREG(mode) and not stat.S_ISDIR(mode):
        raise DownloadError(f"refusing non-regular download path: {path}")


def sha256_file(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(BLOCK_BYTES), b""):
            digest.update(block)
    return digest.hexdigest()


def range_request(url, start, end):
    request = Request(
        url,
        headers={"Accept-Encoding": "identity", "Range": f"bytes={start}-{end}"},
    )
    return urlopen(request, timeout=REQUEST_TIMEOUT_SECONDS)


def remote_size(url):
    for attempt in range(MAX_ATTEMPTS):
        try:
            response = range_request(url, 0, 0)
            break
        except HTTPError as error:
            if error.code in (400, 405, 416, 501):
                error.close()
                raise RangeUnsupported(f"server does not accept byte ranges (HTTP {error.code})")
            if attempt + 1 == MAX_ATTEMPTS:
                raise DownloadError(f"could not probe release asset ranges: HTTP {error.code}") from error
            error.close()
        except URLError as error:
            if attempt + 1 == MAX_ATTEMPTS:
                raise DownloadError(f"could not probe release asset ranges: {error}") from error
        time.sleep(min(8, 2**attempt))

    with response:
        if response.status != 206:
            raise RangeUnsupported(f"server ignored byte range probe (HTTP {response.status})")
        match = re.fullmatch(r"bytes 0-0/(\d+)", response.headers.get("Content-Range", ""))
        if not match:
            raise DownloadError("release asset returned an invalid Content-Range probe")
        body = response.read(2)
        if len(body) != 1:
            raise DownloadError("release asset returned an invalid byte-range probe body")
        return int(match.group(1))


def fetch_chunk(url, parts_dir, index, start, end, total):
    part = parts_dir / f"part-{index:06d}.bin"
    temporary = parts_dir / f"part-{index:06d}.tmp"
    reject_symlink(part)
    reject_symlink(temporary)
    expected_bytes = end - start + 1
    if part.is_file() and part.stat().st_size == expected_bytes:
        return
    if part.exists():
        part.unlink()

    for attempt in range(MAX_ATTEMPTS):
        if temporary.exists():
            temporary.unlink()
        try:
            response = range_request(url, start, end)
            with response:
                if response.status != 206:
                    raise DownloadError(f"range {start}-{end} returned HTTP {response.status}")
                expected_range = f"bytes {start}-{end}/{total}"
                if response.headers.get("Content-Range") != expected_range:
                    raise DownloadError(f"range {start}-{end} returned an unexpected Content-Range")
                content_length = response.headers.get("Content-Length")
                if content_length:
                    try:
                        reported_length = int(content_length)
                    except ValueError as error:
                        raise DownloadError(f"range {start}-{end} returned an invalid Content-Length") from error
                    if reported_length != expected_bytes:
                        raise DownloadError(f"range {start}-{end} returned an unexpected Content-Length")
                written = 0
                with temporary.open("xb") as output:
                    while True:
                        block = response.read(BLOCK_BYTES)
                        if not block:
                            break
                        output.write(block)
                        written += len(block)
                    output.flush()
                    os.fsync(output.fileno())
                if written != expected_bytes:
                    raise DownloadError(f"range {start}-{end} ended after {written} of {expected_bytes} bytes")
                os.replace(temporary, part)
                return
        except (HTTPError, URLError, TimeoutError, OSError) as error:
            if temporary.exists():
                temporary.unlink()
            if attempt + 1 == MAX_ATTEMPTS:
                raise DownloadError(f"range {start}-{end} failed after {MAX_ATTEMPTS} attempts: {error}") from error
            time.sleep(min(8, 2**attempt))


def download_with_ranges(url, output, expected_sha256, parts_dir, chunk_bytes, workers):
    total = remote_size(url)
    if total <= 0:
        raise DownloadError("release asset reported an empty size")
    reject_symlink(parts_dir)
    parts_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    ranges = [
        (index, start, min(total - 1, start + chunk_bytes - 1))
        for index, start in enumerate(range(0, total, chunk_bytes))
    ]

    for generation in range(2):
        with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as executor:
            futures = [
                executor.submit(fetch_chunk, url, parts_dir, index, start, end, total)
                for index, start, end in ranges
            ]
            completed = 0
            report_every = max(1, len(ranges) // 8)
            for future in concurrent.futures.as_completed(futures):
                future.result()
                completed += 1
                if completed % report_every == 0 or completed == len(ranges):
                    print(f"NInfer release asset transfer: {completed}/{len(ranges)} ranges.", flush=True)

        temporary = output.with_name(output.name + ".assembling")
        reject_symlink(temporary)
        if temporary.exists():
            temporary.unlink()
        digest = hashlib.sha256()
        written = 0
        with temporary.open("xb") as assembled:
            for index, start, end in ranges:
                part = parts_dir / f"part-{index:06d}.bin"
                reject_symlink(part)
                expected_bytes = end - start + 1
                if not part.is_file() or part.stat().st_size != expected_bytes:
                    raise DownloadError(f"cached range {index} is missing or has the wrong size")
                with part.open("rb") as source:
                    for block in iter(lambda: source.read(BLOCK_BYTES), b""):
                        assembled.write(block)
                        digest.update(block)
                        written += len(block)
            assembled.flush()
            os.fsync(assembled.fileno())
        if written == total and digest.hexdigest() == expected_sha256:
            os.replace(temporary, output)
            shutil.rmtree(parts_dir)
            print(f"Downloaded and SHA-256 verified NInfer release asset using {len(ranges)} byte ranges.")
            return
        temporary.unlink(missing_ok=True)
        if generation == 0:
            for child in parts_dir.iterdir():
                reject_symlink(child)
                if child.is_file():
                    child.unlink()
            continue
        for child in parts_dir.iterdir():
            reject_symlink(child)
            if child.is_file():
                child.unlink()
        raise DownloadError("assembled NInfer release asset does not match its pinned SHA-256")


def download_single(url, output, expected_sha256):
    temporary = output.with_name(output.name + ".single-part")
    reject_symlink(temporary)
    for attempt in range(2):
        if attempt and temporary.exists():
            temporary.unlink()
        command = [
            "curl", "--fail", "--location", "--retry", "5", "--retry-all-errors",
            "--retry-delay", "2", "--connect-timeout", "20", "--continue-at", "-",
            "--output", str(temporary), url,
        ]
        try:
            subprocess.run(command, check=True)
        except subprocess.CalledProcessError:
            if attempt:
                raise
            temporary.unlink(missing_ok=True)
            print("Resumable curl failed; retrying the release asset from byte zero.", file=sys.stderr)
            continue
        if sha256_file(temporary) == expected_sha256:
            os.replace(temporary, output)
            print("Downloaded and SHA-256 verified NInfer release asset with resumable curl.")
            return
        if attempt:
            raise DownloadError("resumed NInfer release asset does not match its pinned SHA-256")
    raise DownloadError("could not verify NInfer release asset")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--chunk-size", type=int, default=DEFAULT_CHUNK_BYTES)
    parser.add_argument("--workers", type=int, default=DEFAULT_WORKERS)
    parser.add_argument("url")
    parser.add_argument("output", type=Path)
    parser.add_argument("expected_sha256")
    args = parser.parse_args()
    if args.chunk_size < 1 or args.workers < 1:
        parser.error("chunk size and worker count must be positive")
    if not re.fullmatch(r"[0-9a-f]{64}", args.expected_sha256):
        parser.error("expected SHA-256 must be 64 lowercase hex characters")

    output = args.output
    reject_symlink(output.parent)
    output.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    reject_symlink(output)
    parts_dir = output.with_name(output.name + ".parts")
    reject_symlink(parts_dir)
    if output.is_file() and sha256_file(output) == args.expected_sha256:
        print("Reusing already verified NInfer release asset.")
        return
    if output.exists():
        output.unlink()

    try:
        download_with_ranges(args.url, output, args.expected_sha256, parts_dir, args.chunk_size, args.workers)
    except RangeUnsupported as error:
        print(f"{error}; using resumable curl fallback.", file=sys.stderr)
        download_single(args.url, output, args.expected_sha256)


if __name__ == "__main__":
    try:
        main()
    except (DownloadError, HTTPError, URLError, OSError, subprocess.CalledProcessError) as error:
        print(f"NInfer release asset download failed: {error}", file=sys.stderr)
        sys.exit(1)
