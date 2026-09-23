#!/usr/bin/env bash
# Emit content-free Deep Work worker telemetry as one JSON object. This script
# only reads local process metadata and whitelisted fields from phase/Hermes
# logs; it never sends inference traffic or prints source/prompt/tool content.
set -eu

PHASING_DIR="${PHASING_DIR:-/root/stint-phasing}"
HERMES_LOG="${HERMES_LOG:-$HOME/.hermes/logs/agent.log}"
STARTED_AT="${STINT_DEEP_STARTED_AT:-}"

python3 - "$PHASING_DIR" "$HERMES_LOG" "$STARTED_AT" <<'PY'
import datetime as dt
import json
import os
import re
import sys

phasing_dir, hermes_log, started_raw = sys.argv[1:]
utc = dt.timezone.utc
local_tz = dt.datetime.now().astimezone().tzinfo

def parse_time(value):
    if not value:
        return None
    try:
        return dt.datetime.fromisoformat(value.replace('Z', '+00:00')).astimezone(utc)
    except ValueError:
        return None

started = parse_time(started_raw)

def after_start(value):
    parsed = parse_time(value)
    return parsed is not None and (started is None or parsed >= started)

def process_args():
    for entry in os.listdir('/proc'):
        if not entry.isdigit():
            continue
        try:
            with open(f'/proc/{entry}/comm', encoding='utf-8') as stream:
                if stream.read().strip() != 'ninfer-serve':
                    continue
            with open(f'/proc/{entry}/cmdline', 'rb') as stream:
                return stream.read().decode('utf-8', 'replace').split('\0')
        except OSError:
            continue
    return []

def arg_value(args, key):
    try:
        index = args.index(key)
        return int(args[index + 1])
    except (ValueError, IndexError):
        return 0

args = process_args()
ninfer = {
    'running': bool(args),
    'maxContext': arg_value(args, '--max-context'),
    'kvCapacity': arg_value(args, '--kv-capacity'),
    'defaultMaxTokens': arg_value(args, '--default-max-tokens'),
}

routes = {'xhighRequests': 0, 'mediumRequests': 0, 'latestPhase': '', 'latestAt': ''}
latest_route_at = None
for level, key in (('xhigh', 'xhighRequests'), ('medium', 'mediumRequests')):
    path = os.path.join(phasing_dir, f'wire-{level}.jsonl')
    try:
        with open(path, encoding='utf-8') as stream:
            for line in stream:
                try:
                    record = json.loads(line)
                except json.JSONDecodeError:
                    continue
                timestamp = record.get('timestamp', '')
                if not after_start(timestamp):
                    continue
                routes[key] += 1
                parsed = parse_time(timestamp)
                if parsed and (latest_route_at is None or parsed >= latest_route_at):
                    latest_route_at = parsed
                    routes['latestPhase'] = str(record.get('phase') or level)
                    routes['latestAt'] = timestamp
    except OSError:
        pass

compression = {'state': 'not_observed', 'completed': 0, 'failed': 0, 'truncated': 0, 'lastAt': ''}
log_time = re.compile(r'^(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d(?:,\d+)?)')
latest_compression_at = None
try:
    with open(hermes_log, encoding='utf-8', errors='replace') as stream:
        for line in stream:
            match = log_time.match(line)
            if not match:
                continue
            try:
                timestamp_dt = dt.datetime.strptime(match.group(1).split(',')[0], '%Y-%m-%d %H:%M:%S').replace(tzinfo=local_tz).astimezone(utc)
            except ValueError:
                continue
            if started is not None and timestamp_dt < started:
                continue
            timestamp = timestamp_dt.isoformat().replace('+00:00', 'Z')
            if 'Context compression summary was truncated' in line:
                compression['truncated'] += 1
                compression['state'] = 'truncated'
                compression['lastAt'] = timestamp
                latest_compression_at = timestamp_dt
            elif 'context compression done:' in line:
                compression['completed'] += 1
                compression['state'] = 'completed'
                compression['lastAt'] = timestamp
                latest_compression_at = timestamp_dt
            elif 'Context compression failed after' in line or 'Compression summary failed:' in line:
                compression['failed'] += 1
                compression['state'] = 'failed'
                compression['lastAt'] = timestamp
                latest_compression_at = timestamp_dt
            elif 'context compression started:' in line and latest_compression_at is None:
                compression['state'] = 'running'
                compression['lastAt'] = timestamp
except OSError:
    pass

print(json.dumps({
    'collectedAt': dt.datetime.now(utc).isoformat().replace('+00:00', 'Z'),
    'ninfer': ninfer,
    'phaseRoutes': routes,
    'compression': compression,
}, separators=(',', ':')))
PY
