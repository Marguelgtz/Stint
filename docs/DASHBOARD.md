# Stint live dashboard

`stint dashboard` is the interactive terminal cockpit for an active Stint session. It is a presentation and control surface over the existing session snapshot, telemetry, performance, deadline, and teardown paths; it is not a second lifecycle authority.

## Start

```bash
stint dashboard
```

Disable colors with either:

```bash
stint dashboard --no-color
NO_COLOR=1 stint dashboard
```

When stdin or stdout is not a TTY, `dashboard` falls back to a static refreshed status snapshot rather than emitting ANSI terminal sequences.

## Views

| Key | View | Contents |
| --- | --- | --- |
| `1` | Home | Session identity, countdown, cost, health, GPU and cached performance |
| `2` | Performance | Latest explicit benchmark details |
| `3` | Config | Authoritative model/runtime/context/session configuration |
| `4` | Logs | Bounded tail of local `tunnel.log` and `watchdog.log` |
| `←` / `↑` | Previous view | Moves to the previous numbered view and wraps from Home to Logs |
| `→` / `↓` | Next view | Moves to the next numbered view and wraps from Logs to Home |
| `Tab` | Next view | Cycles through the four views |

Arrow-key escape sequences are parsed as one logical navigation event. They do not leak the leading Escape byte into modal handling.

## Actions

| Key | Action |
| --- | --- |
| `r` | Refresh passive endpoint/runtime/GPU telemetry now |
| `b` | Preview and explicitly run one 128-token performance benchmark |
| `+` | Extend the session deadline |
| `-` | Shorten the session deadline |
| `d` | Open destructive teardown confirmation |
| `q` / `Ctrl+C` | Exit the dashboard only; compute remains active |

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

`b` opens a confirmation before any generation request is sent. Confirming starts one asynchronous benchmark using the same OpenAI-compatible benchmark primitives and performance cache as `stint perf`:

```text
1 run × 128 max tokens
```

The dashboard continues rendering while the benchmark runs. Successful results update `performance.json`; failed benchmarks do not overwrite the previous successful sample.

No dashboard timer automatically generates tokens.

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

Explicit only
  TTFT/decode performance benchmark
```

The one-second tick is arithmetic over the current snapshot. It performs no SSH, HTTP, inference request, provider request, or `session.json` write.

Only one passive remote refresh may run at a time. The existing telemetry collector bounds endpoint and SSH probes and preserves partial results when one domain fails.

## Terminal behavior

The v1 dashboard intentionally has no external TUI dependency. It uses the standard library plus the host `stty` command and an alternate terminal screen.

Input is non-canonical and byte-oriented (`-icanon -echo -isig`) while normal terminal output processing remains enabled. A short `VMIN=0` / `VTIME=1` idle boundary lets Stint distinguish a standalone Escape key from CSI cursor-key sequences without blocking input or putting terminal output into raw mode.

On every normal exit path it restores the saved terminal mode, cursor, and primary screen. `SIGWINCH` causes a size refresh and redraw. Rendering supports narrow terminals by switching from the wide multi-column Home identity row to a compact stacked presentation and by bounding log output to the visible height.

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
- deadline
- watchdog/tunnel identity
- runtime state
- provider state
- billing authority

`session.json` remains lifecycle authority. Passive telemetry remains observational. The only persistent telemetry written by the dashboard is the same explicit performance sample cache already used by `stint perf`.

## Failure behavior

If the active session disappears while the dashboard is open, the next passive refresh transitions to `NO ACTIVE SESSION` rather than crashing.

Endpoint, SSH, runtime, and GPU failures remain independent. For example an endpoint can remain healthy while SSH telemetry is temporarily unavailable; the session countdown and cached performance remain visible.

Blocking lifecycle actions temporarily leave the alternate screen while the existing command path runs, then reopen the dashboard and reload current state. This keeps one implementation of lifecycle safety while the dashboard remains a disposable UI process.
