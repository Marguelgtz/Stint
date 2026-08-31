# Session snapshot and telemetry

Stint separates **lifecycle authority** from **observations**.

`session.json` remains the authoritative record of the paid session: instance, runtime, context, deadline, lifecycle status, checkpoint, tunnel PID, and watchdog PID. Volatile metrics such as GPU utilization, endpoint latency, temperature, and benchmark performance are not written into `session.json`.

## Collection modes

### Local snapshot

```bash
stint status
stint status --json
```

The local mode is intentionally cheap and does not contact the remote host. It assembles:

- session identity and lifecycle status
- elapsed and remaining time derived from the authoritative deadline
- estimated spend and scheduled exposure
- local tunnel/watchdog process state
- the latest compatible cached performance sample, if one exists

It performs **no SSH** and **no inference request**.

### Refreshed snapshot

```bash
stint status --refresh
stint status --refresh --json
```

Refresh adds passive observations:

1. `GET http://127.0.0.1:8409/v1/models` checks the complete local serving path and confirms that `qwen3.8-27b` is advertised.
2. One read-only SSH round trip checks the expected remote runtime process and executes `nvidia-smi` for GPU utilization, VRAM, power, and temperature.

The endpoint and SSH probes run concurrently. Endpoint probing is bounded to approximately 2 seconds, remote probing to approximately 3 seconds, and the status refresh has a top-level bound of approximately 4 seconds.

A failed observation is represented inside its telemetry domain. It does not change the session lifecycle state, deadline, tunnel, watchdog, or provider resource.

## Performance samples

`stint perf` remains an explicit active benchmark. It sends generation requests through the same local OpenAI-compatible endpoint used by coding clients and measures TTFT, total latency, and decode throughput.

After a successful benchmark, Stint atomically writes the aggregate result to:

```text
~/.local/state/stint/performance.json
```

The cache is mode `0600` and is separate from `session.json`.

A cached performance sample is displayed only when all of these still match the active session:

- Vast instance ID
- inference runtime
- context size

A previous-session, previous-runtime, or previous-context sample is treated as unavailable. A failed benchmark does not replace the previous successful sample.

`stint status` never runs a benchmark automatically.

## Snapshot domains

The snapshot is organized into six domains:

```text
session       identity, runtime, model, context, lifecycle status
 time          start, deadline, elapsed, remaining, expired
 cost          hourly rate, estimated spend, scheduled exposure
 health        tunnel, watchdog, endpoint, remote runtime
 gpu           utilization, VRAM, power, temperature
 performance   cached TTFT/decode sample and its age
```

The future terminal dashboard should consume these domains rather than reading `session.json` or probing SSH directly.

## JSON contract

`stint status --json` and `stint status --refresh --json` expose the same snapshot in machine-readable form.

Top-level shape:

```json
{
  "collectedAt": "2026-08-31T08:00:00Z",
  "active": true,
  "session": {},
  "time": {},
  "cost": {},
  "health": {},
  "gpu": {},
  "performance": {}
}
```

When no active session is recorded:

```json
{
  "collectedAt": "2026-08-31T08:00:00Z",
  "active": false
}
```

Durations use explicit units rather than Go's raw `time.Duration` nanoseconds:

- `elapsedSeconds`
- `remainingSeconds`
- `scheduledDurationSeconds`
- endpoint `latencyMilliseconds`
- performance `ttftMilliseconds`
- performance `totalMilliseconds`
- performance `ageSeconds`

Remote observation objects contain a `refreshed` indicator plus sample time/error metadata so consumers can distinguish **not sampled** from **sampled and unhealthy**.

## Intended dashboard cadence

The telemetry API is designed for a later live TUI with different refresh classes:

```text
~1 second, local only
  elapsed
  remaining
  estimated spend

~10 seconds, passive remote refresh
  endpoint health
  runtime health
  GPU utilization
  VRAM
  temperature
  power

manual / explicit
  TTFT
  decode throughput
```

The one-second countdown does not require SSH. A dashboard should keep the most recent passive sample in memory between remote refreshes.

## Safety invariants

Telemetry must preserve these invariants:

1. Passive observation never writes `session.json`.
2. Status never generates model output.
3. Telemetry failure cannot stop or replace the deadline watchdog.
4. Telemetry failure cannot alter the session deadline.
5. A remote refresh uses read-only commands only.
6. Missing metrics degrade to unavailable rather than failing the full snapshot.
7. Cached performance is never reused across a mismatched instance/runtime/context.
