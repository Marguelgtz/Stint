#!/usr/bin/env python3
"""Preserve bounded, allow-listed NInfer /metrics and /slots samples."""
import argparse
import datetime as dt
import json
import math
import os
import re
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path


METRICS = {
    "llamacpp:prompt_tokens_total",
    "llamacpp:prompt_tokens_cached_total",
    "llamacpp:tokens_predicted_total",
    "llamacpp:requests_processing",
    "llamacpp:requests_deferred",
    "ninfer:prefix_cache_hit_tokens_total",
    "ninfer:draft_tokens_total",
    "ninfer:draft_accepted_tokens_total",
    "llamacpp:spec_decode_num_draft_tokens_total",
    "llamacpp:spec_decode_num_accepted_tokens_total",
}
MAX_SAMPLES = 1200
MAX_BYTES = 6 * 1024 * 1024
_sample_counts = {}


def utc_now():
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def fetch(url, timeout=2.0):
    request = urllib.request.Request(url, headers={"User-Agent": "stint-deep-runtime-observer"})
    with urllib.request.urlopen(request, timeout=timeout) as response:
        if response.status < 200 or response.status >= 300:
            raise RuntimeError(f"HTTP {response.status}")
        return response.read(1024 * 1024).decode("utf-8", errors="replace")


def parse_metrics(payload):
    result = {}
    for line in payload.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        match = re.match(r"^([^\s{]+)(?:\{[^}]*\})?\s+([^\s]+)", line)
        if not match or match.group(1) not in METRICS:
            continue
        try:
            value = float(match.group(2))
        except ValueError:
            continue
        if math.isfinite(value):
            result[match.group(1)] = value
    return result


def safe_slots(payload):
    raw = json.loads(payload)
    if not isinstance(raw, list):
        raise ValueError("/slots response is not a list")
    slots = []
    for item in raw[:32]:
        if not isinstance(item, dict):
            continue
        slot = {}
        for key in ("id", "is_processing", "retained", "n_ctx", "n_prompt_tokens", "n_prompt_tokens_cache"):
            value = item.get(key)
            if isinstance(value, float) and not math.isfinite(value):
                continue
            if isinstance(value, (bool, int, float, str)) and len(str(value)) <= 128:
                slot[key] = value
        speculative = item.get("speculative")
        if isinstance(speculative, float) and not math.isfinite(speculative):
            speculative = None
        if isinstance(speculative, (bool, int, float, str)) and len(str(speculative)) <= 128:
            slot["speculative"] = speculative
        slots.append(slot)
    return slots


def read_clients(path):
    try:
        with open(path, encoding="utf-8") as stream:
            value = json.load(stream).get("clients")
        return value if isinstance(value, int) and value > 0 else None
    except (OSError, ValueError, TypeError):
        return None


def derive_rates(previous, counters, elapsed):
    if not previous or elapsed <= 0:
        return {}

    def rate(name):
        before, now = previous.get(name), counters.get(name)
        if before is None or now is None or now < before:
            return None
        return round((now - before) / elapsed, 3)

    noncached = rate("llamacpp:prompt_tokens_total")
    cached = rate("ninfer:prefix_cache_hit_tokens_total")
    rates = {}
    if noncached is not None:
        rates["prefillNonCachedTokensPerSecond"] = noncached
    if cached is not None:
        rates["prefixCacheHitTokensPerSecond"] = cached
    if noncached is not None and cached is not None:
        rates["prefillTokensPerSecond"] = round(noncached + cached, 3)
        rates["prefillRateBasis"] = "llamacpp non-cached prompt counter + NInfer prefix-cache-hit counter"
    decode = rate("llamacpp:tokens_predicted_total")
    if decode is not None:
        rates["liveEngineDecodeTokensPerSecond"] = decode
        rates["decodeRateBasis"] = "live engine tokens_predicted_total counter; separate from benchmark decode"
    return rates


def append_bounded(path, payload):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    cache_key = str(path.resolve())
    count = _sample_counts.get(cache_key)
    if count is None:
        try:
            with open(path, "rb") as existing:
                count = sum(1 for line in existing if line.strip())
        except FileNotFoundError:
            count = 0
    line = (json.dumps(payload, separators=(",", ":"), sort_keys=True) + "\n").encode("utf-8")
    with open(path, "ab") as stream:
        os.chmod(path, 0o600)
        stream.write(line)
    count += 1
    _sample_counts[cache_key] = count
    if path.stat().st_size <= MAX_BYTES and count <= MAX_SAMPLES:
        return
    data = path.read_bytes().splitlines()[-MAX_SAMPLES:]
    fd, temp_name = tempfile.mkstemp(prefix="ninfer-runtime-", dir=path.parent)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, "wb") as stream:
            stream.write(b"\n".join(data) + b"\n")
        os.replace(temp_name, path)
        _sample_counts[cache_key] = len(data)
    finally:
        try:
            os.unlink(temp_name)
        except FileNotFoundError:
            pass


def latest_state_dir(latest_path):
    session = Path(latest_path).read_text(encoding="utf-8").strip()
    if not re.fullmatch(r"[A-Za-z0-9_-]+", session):
        raise ValueError("invalid latest Deep Work session id")
    return Path(latest_path).parent / session


def sample(latest_path, session_path, previous, previous_mono):
    observed_at = utc_now()
    endpoints = {}
    counters = {}
    slots = []
    try:
        counters = parse_metrics(fetch("http://127.0.0.1:8080/metrics"))
        endpoints["metrics"] = "ok"
    except Exception as exc:
        endpoints["metrics"] = f"unavailable: {type(exc).__name__}: {str(exc)[:180]}"
    try:
        slots = safe_slots(fetch("http://127.0.0.1:8080/slots"))
        endpoints["slots"] = "ok"
    except Exception as exc:
        endpoints["slots"] = f"unavailable: {type(exc).__name__}: {str(exc)[:180]}"
    monotonic_now = time.monotonic()
    rates = derive_rates(previous, counters, monotonic_now - previous_mono) if previous_mono is not None else {}
    payload = {
        "observedAt": observed_at,
        "configuredClients": read_clients(session_path),
        "exposedEngineSlotRows": len(slots),
        "slots": slots,
        "requestsProcessing": counters.get("llamacpp:requests_processing"),
        "requestsDeferred": counters.get("llamacpp:requests_deferred"),
        "counters": counters,
        "rates": rates,
        "endpoints": endpoints,
    }
    state_dir = latest_state_dir(latest_path)
    if state_dir.is_dir():
        append_bounded(state_dir / "ninfer-runtime.jsonl", payload)
    return counters, monotonic_now


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--latest", required=True, help="Deep Work latest-session pointer")
    parser.add_argument("--session", required=True, help="configured local session.json")
    parser.add_argument("--interval", type=float, default=10.0)
    parser.add_argument("--max-samples", type=int, default=MAX_SAMPLES)
    args = parser.parse_args()
    previous = {}
    previous_mono = None
    samples = 0
    while samples < max(1, min(args.max_samples, MAX_SAMPLES)):
        if not Path(args.latest).is_file():
            time.sleep(1.0)
            continue
        try:
            if not latest_state_dir(args.latest).is_dir():
                time.sleep(1.0)
                continue
            previous, previous_mono = sample(args.latest, args.session, previous, previous_mono)
        except Exception as exc:
            # Preserve observer failures locally where a session directory exists;
            # sampling failure must never stop the Deep Work coordinator.
            try:
                directory = latest_state_dir(args.latest)
                append_bounded(directory / "ninfer-runtime.jsonl", {
                    "observedAt": utc_now(), "configuredClients": read_clients(args.session),
                    "exposedEngineSlotRows": 0, "slots": [], "counters": {}, "rates": {},
                    "endpoints": {"observer": f"unavailable: {type(exc).__name__}: {str(exc)[:180]}"},
                })
            except Exception:
                pass
        samples += 1
        time.sleep(max(1.0, min(args.interval, 60.0)))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
