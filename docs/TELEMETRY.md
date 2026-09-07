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
3. A two-epoch scrape of the engine's `/metrics` and `/slots` endpoints through the same local tunnel observes live inference activity without sending any inference traffic.

The endpoint and SSH probes run concurrently with the inference probe. Endpoint probing is bounded to approximately 2 seconds, remote probing to approximately 3 seconds, and the status refresh has a top-level bound of approximately 4 seconds. The inference probe scrapes once, waits a 1.2s epoch gap, and scrapes again; each fetch is bounded to 1.2s so both epochs stay inside the refresh budget.

A failed observation is represented inside its telemetry domain. It does not change the session lifecycle state, deadline, tunnel, watchdog, or provider resource.

## Performance samples

`stint perf` remains an explicit active benchmark. It sends generation requests through the same local OpenAI-compatible endpoint used by coding clients and measures TTFT, total latency, decode throughput, and the measured prompt token count from the endpoint's usage report. The benchmark prompt is built deterministically to a configurable depth (`--prompt-tokens`, default 8192, range 32-200000) so that TTFT and the post-run VRAM sample reflect real prompt encoding rather than decode-only behavior. Prompt depth plus maximum completion tokens must fit inside the active context; the configured context is a ceiling, not the measured depth.

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

## Live inference observation

Independent from the cached benchmark sample, `--refresh` (and the dashboard) observe live engine activity by polling two read-only surfaces through the local tunnel:

- `/metrics` — Prometheus counters. Both runtimes publish the shared `llamacpp:*` series (`prompt_tokens_total`, `prompt_tokens_cached_total`, `tokens_predicted_total`, `requests_processing`, `requests_deferred`); NInfer additionally publishes `ninfer:prefix_cache_hit_tokens_total` and draft/speculative counters. On NInfer the re-published `llamacpp:prompt_tokens_total` counts **non-cached** prompt tokens only (verified on a live instance, 2026-09-03); the cached portion is tracked by `ninfer:prefix_cache_hit_tokens_total`, so prefill rates and cache-reuse ratios derived from these counters must be interpreted per runtime.
- `/slots` — one lane object per concurrent runtime lane (llama.cpp slot or NInfer lane), including per-lane prompt token depth, processing state, and — on NInfer — `retained` (context held after request completion) and `session_digest`. `session_digest` is runtime metadata only: a per-completion history fingerprint that changes on every completion; it is **not** a stable agent identity and must not be used to attribute a lane to a client.
- Endpoint health — probed separately via `GET /v1/models` (model advertised). NInfer does not respond on `/` or `/health` (verified on a live instance, 2026-09-03), so health checks must use `/v1/models`.

From two epochs separated by a 1.2s gap (each fetch capped at 2.5s) the snapshot derives:

- active agents: lanes that are processing or hold a resident prompt
- deferred/queued requests from the engine
- resident prompt depth: the deepest per-lane prompt token count
- decode and prefill token rates from counter deltas
- cache reuse ratio (cached prompt tokens ÷ prompt tokens, clamped at 100%)
- speculative accept ratio (accepted draft tokens ÷ draft tokens)

The domain degrades by surface: missing `/metrics` still yields lane-level activity from `/slots`; missing both yields an unavailable reason (for llama.cpp: launch with `--metrics --slots`; NInfer serves both by default). A slow tunnel that outlives the parent probe budget drops the second epoch and degrades to a single-epoch lane snapshot (token rates unavailable) instead of reporting the engine unavailable. The parent probe budget is per consumer: `stint status --refresh` stays tight (4 s) for fast CLI feedback, while the dashboard auto-refresh allows 6 s so the second epoch usually fits on a slow tunnel, well under its 10 s cadence. Live observation never sends an inference request and never mutates the remote session; it is also never mixed with the cached `performance` benchmark domain.

## Snapshot domains

The snapshot is organized into seven domains:

```text
session       identity, runtime, model, context, lifecycle status
 time          start, deadline, elapsed, remaining, expired
 cost          hourly rate, estimated spend, scheduled exposure
 health        tunnel, watchdog, endpoint, remote runtime
 gpu           utilization, VRAM, power, temperature
 inference     live agents, resident prompt depth, token rates, lanes
 performance   cached TTFT/decode sample, its measured prompt depth, and its age
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
  "inference": {},
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
- performance `promptTokens`
- performance `ageSeconds`

Remote observation objects contain a `refreshed` indicator plus sample time/error metadata so consumers can distinguish **not sampled** from **sampled and unhealthy**.

The `inference` block uses: `refreshed`, `available`, `processing`, `deferred`, `agents`, `residentDepth` (tokens), `decodeTokensSec` / `prefillTokensSec` (null when the runtime does not publish the counter or only one epoch is usable), `cacheReuseRatio` / `specAcceptRatio` (0–1, null when not applicable), per-lane objects under `lanes`, and `unavailableReason` when the probe could not observe the engine.

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
  live inference: agents, resident prompt depth, token rates, lanes

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
8. Live inference observation is strictly read-only: it polls `/metrics` and `/slots` over the local tunnel, never sends an inference request, and never mutates the remote session.
