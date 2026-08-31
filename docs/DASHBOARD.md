# Stint live dashboard

`stint dash` is the interactive terminal cockpit for an active Stint session. `stint dashboard` remains a compatibility alias. The cockpit is a presentation and control surface over the existing session snapshot, telemetry, performance, deadline, resume, and teardown paths; it is not a second lifecycle authority.

## Start

```bash
stint dash
```

Disable colors with either:

```bash
stint dash --no-color
NO_COLOR=1 stint dash
```

When stdin or stdout is not a TTY, `dash` falls back to a static refreshed status snapshot rather than emitting ANSI terminal sequences.

## Views

| Key | View | Contents |
| --- | --- | --- |
| `1` | Home | Session identity, countdown, cost, health, live inference strip, GPU and cached performance |
| `2` | Performance | Latest explicit benchmark details plus a LIVE TRAFFIC section with observed engine activity |
| `3` | Config | Authoritative model/runtime/context/session configuration |
| `4` | Logs | Bounded tail of local `tunnel.log` and `watchdog.log` |
| `←` / `↑` | Previous view | Moves to the previous numbered view and wraps from Home to Logs |
| `→` / `↓` | Next view | Moves to the next numbered view and wraps from Logs to Home |
| `Tab` | Next view | Cycles through the four views |

Arrow-key escape sequences are parsed as one logical navigation event. They do not leak the leading Escape byte into modal handling.

## Actions

| Key | Action |
| --- | --- |
| `r` | Refresh passive endpoint/runtime/GPU/live-inference telemetry; when the recorded session is `RECOVERABLE`, preview Resume instead |
| `b` | Preview and explicitly run one 128-token performance benchmark while the session is `READY` |
| `+` | Extend the session deadline |
| `-` | Shorten the session deadline |
| `d` | Open destructive teardown confirmation |
| `q` / `Ctrl+C` | Exit the dashboard only; compute remains active |

### Recoverable sessions

`RECOVERABLE` comes from persisted Stint lifecycle state. It is not inferred from a failed telemetry probe. A paid instance can enter this state when startup or resume has progressed far enough that preserving and reattaching to the same instance is safer than re-renting.

The dashboard keeps the saved checkpoint and last recoverable error visible through the snapshot contract and shows a recovery notice such as:

```text
● RECOVERABLE
Paid instance preserved at RUNTIME_READY · press r to resume
```

For a recoverable session, `r` opens a confirmation rather than performing an ordinary passive refresh:

```text
RESUME SESSION?

Instance       49337900
Checkpoint     RUNTIME_READY
Remaining      31m

This reattaches the tunnel/runtime/model without renting new compute.
```

Enter confirms; Escape cancels. The dashboard does not implement recovery itself. It temporarily restores the normal terminal screen and invokes the same in-process `runResume` path as `stint resume`, preserving the existing lifecycle lock, deadline checks, watchdog repair, Vast-instance verification, SSH/tunnel repair, runtime/model recovery, and resumable-failure semantics. It then reopens the dashboard and reloads the current session state.

An expired session never advertises Resume. `EXPIRED` takes precedence over `RECOVERABLE` because the deadline lifecycle remains authoritative.

### State-aware presentation

The dashboard distinguishes lifecycle authority from observations:

- `READY`: persisted session is ready and refreshed serving-path telemetry is healthy or has not contradicted it.
- `RECOVERABLE`: authoritative persisted lifecycle state says the paid instance can be resumed.
- `DEGRADED`: persisted state is `READY`, but refreshed endpoint or runtime telemetry currently shows the serving path is unhealthy. This is display-only and never rewrites `session.json`.
- `EXPIRED`: the canonical deadline has passed.
- `OFFLINE`: no active session is recorded.
- startup states such as `BOOTING`, `SSH_CONNECTING`, `RUNTIME_BOOTSTRAP`, and `MODEL_LOADING` remain visible as their persisted lifecycle values.

A telemetry failure therefore cannot silently promote a session to `RECOVERABLE`. Only the lifecycle code can persist that state. Benchmarking is disabled unless the displayed session is `READY`; deadline and teardown controls remain available through their existing guarded lifecycle paths.

### Deadline controls

`+` and `-` first open a duration selector:

```text
1   15m
2   30m
3   1h
4   Custom
```

Custom input accepts the same positive Go-style durations as the CLI, for example `20m`, `45m`, or `1h30m`.

Before applying a deadline change, the dashboard shows the current remaining time/deadline, proposed deadline, exposure change, projected session cost, and session cost ceiling. Enter confirms; Escape cancels.

The dashboard does not implement deadline mutation itself. After confirmation it invokes the same in-process Stint deadline command path used by `stint extend` / `stint shorten`, including lifecycle locking, optimistic revalidation, cost-ceiling checks, watchdog verification, and atomic session-state persistence.

### Performance benchmark

`b` opens a confirmation before any generation request is sent, and only while the session is `READY`. Confirming starts one asynchronous benchmark using the same OpenAI-compatible benchmark primitives and performance cache as `stint perf`:

```text
1 run × 128 max tokens
```

The dashboard continues rendering while the benchmark runs. Successful results update `performance.json`; failed benchmarks do not overwrite the previous successful sample.

No dashboard timer automatically generates tokens.

The Performance view also shows a **LIVE TRAFFIC** section: agents, resident prompt depth, decode/prefill rates, queue, cache reuse, speculative acceptance, and per-lane detail polled from the engine's `/metrics` and `/slots` endpoints. Live traffic is *observed*, never benchmarked; the benchmarked sample still comes only from an explicit `b` action, and the two sections never share values.

### Down

`d` only opens the confirmation. Destruction requires uppercase `D` from the confirmation modal. This calls the existing `stint down` lifecycle path.

`q`, `Q`, Ctrl+C, SIGTERM and SIGHUP only close the dashboard and restore the terminal. They never destroy compute.

## Refresh model

The dashboard uses three separate update classes:

```text
Every 1 second
  elapsed
  remaining
  estimated spend
  benchmark sample age

About every 10 seconds
  /v1/models endpoint health
  SSH/runtime process health
  GPU utilization
  VRAM
  temperature
  power
  /metrics + /slots live inference observation (two epochs, 1.2s gap)

Explicit only
  TTFT/decode performance benchmark
  recovery through the existing Resume lifecycle path
```

The one-second tick is arithmetic over the current snapshot. It performs no SSH, HTTP, inference request, provider request, or `session.json` write.

Only one passive remote refresh may run at a time. The existing telemetry collector bounds endpoint and SSH probes and preserves partial results when one domain fails.

## Terminal behavior

The v1 dashboard intentionally has no external TUI dependency. It uses the standard library plus the host `stty` command and an alternate terminal screen.

Input is non-canonical, no-echo, and byte-oriented while normal terminal output processing remains enabled. The terminal uses blocking `VMIN=1` reads so an idle terminal cannot be mistaken for EOF and close the dashboard. Cursor-key CSI sequences are parsed as one logical navigation event.

On every normal exit path it restores the saved terminal mode, cursor, and primary screen. `SIGWINCH` causes a size refresh and redraw. Rendering supports narrow terminals by switching from the wide multi-column Home identity row to a compact stacked presentation and by bounding log output to the visible height.

Blocking lifecycle actions such as Resume, extend, shorten, and down temporarily restore the primary terminal screen while the existing command path runs, then reopen the dashboard and reload current state.

## State ownership

The dashboard may retain only presentation state such as:

- selected view
- terminal size
- current snapshot projection
- modal state
- refreshing/benchmarking state
- notices/errors
- bounded local log lines

It does not own or persist:

- instance identity
- authoritative session status
- deadline
- watchdog/tunnel identity
- runtime state
- provider state
- billing authority

`session.json` remains lifecycle authority. Passive telemetry remains observational. The only persistent telemetry written by the dashboard is the same explicit performance sample cache already used by `stint perf`.

## Failure behavior

If the active session disappears while the dashboard is open, the next passive refresh transitions to `NO ACTIVE SESSION` rather than crashing.

Endpoint, SSH, runtime, and GPU failures remain independent. For example an endpoint can remain healthy while SSH telemetry is temporarily unavailable; the session countdown and cached performance remain visible. A persisted READY session with a failed refreshed serving-path observation is displayed as `DEGRADED`, but the observation does not rewrite lifecycle state.

If lifecycle code has explicitly preserved the paid instance as `RECOVERABLE`, the dashboard surfaces the checkpoint and offers the existing Resume path rather than creating replacement compute.
