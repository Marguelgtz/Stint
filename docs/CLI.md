# CLI reference

`stint` is a single Go binary. Every command accepts `--help` (`-h`), and `stint help <command>` prints the same per-command reference. This document is the long form: lifecycle, safety model, and full flag tables.

The CLI exposes a stable OpenAI-compatible endpoint to coding agents:

```text
http://127.0.0.1:8409/v1    (model: qwen3.8-27b)
```

## Quick start

```bash
stint auth vast                   # store + verify your Vast API key
stint setup ssh                   # create the dedicated Stint SSH keypair
stint doctor                      # verify credentials, SSH key, OpenSSH, port 8409
stint plan interactive --hours 5  # read-only plan; never rents
stint start interactive           # rent, boot, tunnel; READY when live
stint status                      # inspect remaining time and the deadline
stint extend 30m                  # move auto-destroy later, within the cost ceiling
stint shorten 15m                 # move auto-destroy earlier
stint down                        # destroy compute and clear the session
```

## Command overview

| Command | Area | What it does |
| --- | --- | --- |
| `stint auth vast` | Setup | Verifies and stores the Vast API key |
| `stint setup ssh` | Setup | Creates the dedicated Stint SSH keypair |
| `stint doctor` | Setup | Checks local prerequisites and Vast access |
| `stint status` | Setup | Shows local state, remaining time, and the active session |
| `stint onboard spark` | Setup | Prints the Spark onboarding plan (read-only) |
| `stint plan <profile>` | Planning | Ranks marketplace offers under hard policy (read-only) |
| `stint start interactive` | Compute | Rents a GPU, boots the model, opens the tunnel |
| `stint resume` | Compute | Reattaches to a saved session after an interruption |
| `stint extend <duration>` | Compute | Moves the active session deadline later |
| `stint shorten <duration>` | Compute | Moves the active session deadline earlier |
| `stint down` | Compute | Destroys the instance, tunnel, and session state |
| `stint perf` | Diagnostics | Benchmarks the local endpoint (TTFT, tok/s) |
| `stint version` | Reference | Prints the CLI version |
| `stint help [command]` | Reference | Command overview or per-command help |

## Setup commands

### `stint auth vast`

Verifies a Vast API key (instance-read and marketplace-search access) and stores it locally with owner-only permissions.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--from-env` | `false` | Read the key from `VAST_API_KEY` instead of a hidden prompt |

- Credentials live at `~/.config/stint/credentials.json` (mode 0600).
- A Vast API key is a compute credential, not a Stint identity credential. Identity remains GitHub via Spark.

### `stint setup ssh`

Creates (or reuses) a dedicated ed25519 SSH keypair under `~/.config/stint/ssh/`. `stint start` attaches the key to rented instances automatically; you add the public key to Vast once.

- Add the printed public key in Vast: **Account → Keys → SSH Keys**.
- The private key never leaves `~/.config/stint/ssh/`.

### `stint doctor`

Verifies everything needed for live planning and paid start: Vast credentials, OpenSSH, the dedicated Stint SSH key, and local port 8409.

- Exit status is non-zero when any check fails.
- Run it after `stint auth vast` and `stint setup ssh`.

### `stint status`

Prints local Stint state (Vast provider, SSH key, Cline endpoint) and, when a session is recorded, the active instance. Session timing is derived from the canonical stored deadline rather than persisted as a second countdown value.

Active sessions include, when available:

- GPU, runtime, context, and hourly rate
 - NInfer config (derived from the persisted context) when the active runtime is NInfer
- start time and elapsed duration
- remaining duration, or `expired` once the deadline has passed
- estimated spend so far and the currently scheduled cost exposure
- checkpoint and last recoverable error
- the exact auto-destroy deadline and suggested next action
- with `--refresh`, an `INFERENCE LIVE` section: active agents, queued requests, resident prompt depth, decode/prefill rates, cache reuse, speculative acceptance, and per-lane detail observed from the engine's read-only `/metrics` and `/slots` endpoints (never an inference request)

When the next action is `stint resume`, the paid instance is preserved and resumable.

### `stint onboard spark`

Shows the Spark onboarding plan for this repository: profile path, dashboard URL, expected GitHub evidence names, and the onboarding steps. Nothing is created or sent.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--dashboard <url>` | hosted Spark dashboard | Spark dashboard URL |

- GitHub Actions emits the evidence jobs Spark observes: `spark-profile`, `go-vet`, `unit-tests`.

## Planning (read-only)

### `stint plan <profile>`

Queries the live Vast marketplace (or deterministic local fixtures), evaluates every candidate against Stint's hard policy, ranks the qualifiers, and prints the selected offer, alternatives, and session cost. **plan never rents or mutates anything on the provider side.**

| Argument | Purpose |
| --- | --- |
| `<profile>` | `interactive` (live or fixture) or `deep` (fixture only) |

| Flag | Default | Purpose |
| --- | --- | --- |
| `--hours <float>` | profile default (interactive 5, deep 8) | Session duration in hours |
| `--fixture` | `false` | Use deterministic local fixture offers instead of the Vast API |
| `--json` | `false` | Print machine-readable JSON (plan, alternatives, diagnostics) |

Policy notes:

- Hard interactive policy: 1× RTX 4090, ≤ $0.40/hour, ≥ 98.5% reliability, ≥ 24 GB VRAM, ≥ 1 direct port, verified + rentable + not rented, 50 GB storage, on-demand only, session ceiling $2.50.
- Discovery is intentionally broader (capped at $0.60/hour) so rejections are explainable; a failed plan prints marketplace diagnostics, including the Vast discovery bisect when the API returns zero candidates.
- The plan output includes `Runtime (auto)`, the runtime `stint start` would select for the chosen offer (NInfer on RTX 4090, llama.cpp elsewhere), so read-only plans stay consistent with paid starts.

## Compute (paid)

### `stint start interactive`

Rents a Vast instance for the selected offer, qualifies the host by sampling the real model-transfer throughput, boots the inference runtime (NInfer on RTX 4090 hosts, llama.cpp otherwise), and serves Qwen3.8-27B at `http://127.0.0.1:8409/v1` through a supervised SSH tunnel. A detached watchdog destroys the instance at the paid deadline even if this process exits.

| Argument | Purpose |
| --- | --- |
| `<profile>` | `interactive` (only live profile) |

| Flag | Default | Purpose |
| --- | --- | --- |
| `--hours <float>` | `1` | Maximum paid session duration in hours |
| `--yes` | `false` | Confirm the selected rental without prompting |
| `--location <text>` | — | Prefer an offer whose location contains this text |
| `--runtime <name>` | `auto` | Inference runtime: `auto`, `ninfer`, or `llama.cpp` |
| `--context <int>` | `16384` | llama.cpp context tokens (1024–131072); rejected when the selected runtime is NInfer |
| `--ninfer-config <name>` | `coding` | NInfer config: `coding`, `precision`, or `native` |
| `--min-network-mbps <float>` | `500` | Minimum Vast advertised download bandwidth in Mbps; `0` disables the prefilter |
| `--min-measured-download-mbps <float>` | `40` | Minimum measured post-SSH model-transfer throughput in MB/s; `0` disables |
| `--network-candidate-attempts <int>` | `3` | Maximum distinct Vast machines to try during provider startup and network qualification |

Operational notes:

- Requires `stint auth vast`, a free local port 8409, and the Stint SSH key (`stint setup ssh`).
- NInfer is qualified for RTX 4090 hosts with CUDA ≥ 12.8 only; with `--runtime auto`, a 4090 uses NInfer and any other qualifying GPU falls back to llama.cpp (auto also falls back if the NInfer bootstrap is unavailable).
- Provider or SSH startup failures reject the host and try the next candidate. Failures **after** SSH is ready preserve the paid instance: run `stint resume` to continue.
- The endpoint is OpenAI-compatible: base URL `http://127.0.0.1:8409/v1`, model `qwen3.8-27b`.
- The start summary prints `Runtime` (`(auto)` when auto-selected) and, for NInfer sessions, the active `NInfer config`. See [`docs/NINFER.md`](NINFER.md) for runtime selection, CUDA/host qualification, config presets, and the pinned Vast image.

### `stint resume`

Continues a recorded session after an interruption: re-establishes the SSH tunnel (releasing stale ports first), verifies or restarts the remote runtime, waits for the model, and reports READY. If the session deadline has already passed, resume destroys the compute and clears the local state.

- Supports interactive sessions only.
- If resume fails again, the paid instance stays resumable and the deadline watchdog keeps running.

### `stint extend <duration>`

Moves the current Stint-managed auto-destroy deadline later. The duration is added to the **existing deadline**, not to the current clock time.

```bash
stint extend 30m
stint extend 1h30m
stint extend 1h --yes
```

| Argument | Purpose |
| --- | --- |
| `<duration>` | Positive Go-style duration such as `15m`, `30m`, `1h`, or `1h30m` |

| Flag | Default | Purpose |
| --- | --- | --- |
| `--yes` | `false` | Apply the reviewed extension without prompting |

Before mutation, Stint prints the current remaining time and deadline, the proposed deadline, additional maximum cost exposure, projected scheduled session cost, and the active profile's session-cost ceiling.

Extensions are refused when their projected scheduled cost would exceed the profile's existing `Session.MaxCostUSD`. The command reports the maximum additional duration currently available under that policy.

`extend` does **not** rerent the instance, restart inference, alter the model/context, or recreate a healthy tunnel/watchdog.

### `stint shorten <duration>`

Moves the current Stint-managed auto-destroy deadline earlier. The duration is subtracted from the **existing deadline**.

```bash
stint shorten 15m
stint shorten 30m --yes
```

| Argument | Purpose |
| --- | --- |
| `<duration>` | Positive Go-style duration such as `15m`, `30m`, `1h`, or `1h30m` |

| Flag | Default | Purpose |
| --- | --- | --- |
| `--yes` | `false` | Apply the reviewed shortening without prompting |

A shortening that would move the deadline to the present or past is rejected. Use `stint down` for immediate teardown. The running model and tunnel otherwise remain available until the new deadline.

### Deadline mutation and watchdog safety

`Deadline` in `session.json` is authoritative. `Remaining` is always derived from `Deadline - now`; it is never persisted independently.

The detached watchdog is deadline-aware rather than a one-shot sleep:

1. It captures the Vast instance it is responsible for.
2. While waiting, it re-reads `session.json`, so extend/shorten changes are observed without process replacement.
3. When the apparent deadline arrives, it acquires the same lifecycle lock used by start/resume/down/deadline mutation.
4. It re-reads the state **under that lock** before destroying compute. An extension that commits at the old expiry boundary therefore wins safely.
5. A watchdog whose recorded instance no longer matches the session exits without touching the replacement instance.
6. If provider destruction fails, state is preserved and the watchdog retries rather than silently abandoning the paid resource. The watchdog also verifies each destroy by polling Vast until the instance disappears; a destroy that was accepted but is still visible is treated as failed and retried the same way.

Interactive confirmation does not hold the lifecycle lock. Stint previews the change first, then after confirmation acquires the lock and verifies that the instance and deadline are unchanged before committing. This prevents an unattended confirmation prompt from blocking auto-destroy.

### `stint down`

Stops the local tunnel and watchdog, destroys the Vast instance, and clears the local session state. Before any destruction it shows the instance and remaining time and requires you to type the literal word `destroy`; any other input aborts with the session left running. `stint down --yes` skips the prompt for unattended use. After the destroy is accepted, `stint down` polls Vast until the instance has disappeared; if it is still visible, a warning is printed and the session state is kept (re-running `stint down` once the instance is gone is a safe no-op). Safe to run when no session is recorded.

- Compute is also destroyed automatically at the session deadline by the watchdog.

## Diagnostics

### `stint perf`

Benchmarks the local OpenAI-compatible endpoint of the active session: time to first token, total latency, and decode speed per run, plus averages. Both NInfer and llama.cpp are measured through the same localhost path, and transient endpoint EOFs are retried.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--runs <int>` | `3` | Number of benchmark requests (1–10) |
| `--tokens <int>` | `256` | Maximum completion tokens per request (32–2048) |

- Requires an active session: run `stint start` or `stint resume` first.

## Safety model

1. **plan never pays.** Planning and doctor are read-only; only `start` rents.
2. **start confirms cost before paying** unless `--yes` is passed.
3. **Compute is bounded by an authoritative mutable deadline.** The detached watchdog survives the foreground CLI and dynamically observes deadline changes.
4. **Extension remains policy-bounded.** `stint extend` cannot raise scheduled exposure above the active profile's existing session-cost ceiling.
5. **Deadline mutation is serialized.** Changes revalidate under the same lifecycle lock used by start/resume/down; a confirmation prompt never holds that lock.
6. **Failures are resumable.** Anything that breaks after SSH is ready keeps the instance paid-and-reachable; `stint resume` reattaches instead of re-renting.
7. **Credentials stay local.** The Vast API key and SSH keypair live under `~/.config/stint/` with owner-only permissions.

## Session lifecycle

```text
                                      ┌── extend ──▶ later deadline
                                      │
no session ──start──▶ RUNNING ────────┼── shorten ─▶ earlier deadline
    ▲                    │            │
    │                    │            └── down ─────▶ destroy now
    │                    │
    │                    └── crash / laptop close ──▶ RESUMABLE
    │                                                     │
    │                                                     │ resume
    └──────────── deadline watchdog / down ───────────────┘
```

`stint status` always derives the current remaining time from the recorded deadline and reports the local session state plus the suggested next action.