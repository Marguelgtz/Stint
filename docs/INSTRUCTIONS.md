# Stint operator instructions

This guide describes the behavior that is currently on `main`. It is intentionally separate from experimental and stacked pull-request work.

> Stint is still an active prototype and it controls paid GPU resources. Read the teardown notes before starting a paid session.

## What Stint currently does

Stint is a local Go control plane for disposable remote inference compute. The current live path:

```text
coding agent / IDE
       |
       v
http://127.0.0.1:8409/v1
       |
       v
Stint local lifecycle + SSH tunnel
       |
       v
Vast GPU
       |
       v
Qwen3.8-27B through NInfer or llama.cpp
```

The live profile on `main` is `interactive`. A `deep` profile exists in planning fixtures, but the Deep Work executor itself is not on `main` yet.

## Prerequisites

- Go 1.23+
- OpenSSH
- a Vast account and API key
- sufficient Vast balance for the session you start
- local port `8409` available

Stint stores provider credentials and its SSH key under the user config directory, normally `~/.config/stint/`. Runtime/session state is stored under `$XDG_STATE_HOME/stint` or `~/.local/state/stint/`.

## Build

```bash
# Build the local Stint binary from the current checkout.
make build

# Confirm which CLI version this checkout reports.
./bin/stint version
```

The examples below use `./bin/stint`. If Stint is installed on your `PATH`, use `stint` instead.

## First-time setup

```bash
# Verify and store the Vast API key locally.
./bin/stint auth vast

# Create or reuse Stint's dedicated ed25519 SSH keypair.
./bin/stint setup ssh

# Check Vast access, OpenSSH, the Stint SSH key, and local port 8409.
./bin/stint doctor
```

`stint setup ssh` prints the public key and its local path. Follow the command output if the key still needs to be added to Vast.

## Plan before spending

```bash
# Inspect and rank live Vast offers without renting anything.
./bin/stint plan interactive --hours 1

# Return the same read-only plan as machine-readable JSON.
./bin/stint plan interactive --hours 1 --json
```

Planning is read-only. It does not rent, destroy, or mutate provider compute.

The current interactive policy prefers RTX 4090 and then RTX 3090, requires at least 24 GB VRAM, verified/rentable inventory, at least one direct port, at least 98.5% reliability, and a maximum hourly rate of $0.40. The profile-level scheduled session ceiling is $2.50.

## Start an interactive session

```bash
# Start a one-hour session and let Stint choose the qualified runtime automatically.
./bin/stint start interactive --hours 1

# Start NInfer explicitly with the full 262,144-token native context and two lanes.
./bin/stint start interactive --hours 1 --runtime ninfer --ninfer-config native --clients 2

# Use the same native two-lane setup with a 30 MB/s measured-download floor and up to five host attempts.
./bin/stint start interactive \
  --hours 1 \
  --runtime ninfer \
  --ninfer-config native \
  --clients 2 \
  --min-measured-download-mbps 30 \
  --network-candidate-attempts 5
```

Without `--yes`, Stint prints the selected offer and asks before renting.

Current runtime behavior:

| Runtime/config | Context | Notes |
| --- | ---: | --- |
| `--runtime auto` | runtime dependent | RTX 4090 selects NInfer; other qualifying GPUs use llama.cpp. NInfer bootstrap may fall back to llama.cpp unless the requested lane configuration requires NInfer. |
| `--runtime ninfer --ninfer-config coding` | 126,976 | INT8 KV, one or two NInfer lanes. |
| `--runtime ninfer --ninfer-config precision` | 172,032 | INT8 KV, one or two NInfer lanes. |
| `--runtime ninfer --ninfer-config native` | 262,144 | Native context, E8 4-bit KV, one or two NInfer lanes. |
| `--runtime llama.cpp --context <tokens>` | 1,024 to 131,072 | Explicit llama.cpp context. |

`--clients 2` is NInfer-only. The lanes share the configured context/KV pool dynamically; they are not permanently assigned to individual IDE conversations.

When READY, the client endpoint is:

```text
Base URL: http://127.0.0.1:8409/v1
Model:    qwen3.8-27b
```

## Inspect a running session

```bash
# Read the local/cached session snapshot without contacting the remote host.
./bin/stint status

# Refresh endpoint, runtime, GPU, and live-inference telemetry with bounded read-only probes.
./bin/stint status --refresh

# Print the assembled session snapshot as JSON for scripts or debugging.
./bin/stint status --refresh --json

# Open the interactive terminal cockpit for the current session.
./bin/stint dash
```

`status --refresh` observes inference through `/metrics` and `/slots`; it does not generate a model request. The dashboard uses the same lifecycle and telemetry state rather than creating a second source of truth.

Useful dashboard keys:

| Key | Action |
| --- | --- |
| `1`..`4` | Home, Performance, Config, Logs |
| arrows / `Tab` | move between views |
| `r` | refresh, or offer Resume when the session is `RECOVERABLE` |
| `b` | explicitly run a benchmark |
| `+` / `-` | extend or shorten the session deadline |
| `d` | open the dashboard teardown confirmation |
| `q` | close the dashboard only; compute stays running |

## Benchmark inference

```bash
# Benchmark a normal mid-session prompt depth through the local endpoint.
./bin/stint perf --prompt-tokens 8192

# Measure a deeper long-context request with one run.
./bin/stint perf --prompt-tokens 131072 --runs 1
```

`stint perf` sends real inference traffic. It reports time to first token, total latency, decode speed, and related runtime measurements. Successful samples are cached for status/dashboard display.

## Change the deadline

```bash
# Add 30 minutes to the currently recorded auto-destroy deadline.
./bin/stint extend 30m

# Move the currently recorded auto-destroy deadline 15 minutes earlier.
./bin/stint shorten 15m
```

Deadline changes are relative to the existing deadline, not the current clock time. `extend` remains bounded by the active profile's maximum scheduled cost.

The detached deadline watchdog re-reads the authoritative session state, so an accepted deadline change is observed without replacing the watchdog or restarting the model.

## Recover after an interruption

If startup progressed far enough to preserve the paid instance, Stint records the session as recoverable instead of immediately renting replacement compute.

```bash
# See the saved lifecycle checkpoint and suggested next action.
./bin/stint status

# Reattach the SSH tunnel/runtime/model to the already-paid saved instance.
./bin/stint resume
```

`resume` does not intentionally rent new compute. If the saved deadline has already passed, the recovery path tears the expired session down instead.

## Stop and destroy compute

```bash
# Immediately stop the local tunnel/watchdog, destroy the recorded Vast instance, and clear local session state.
./bin/stint down
```

### Important current-main teardown behavior

On the current `main` branch, CLI `stint down` is immediately destructive once invoked. It does **not** ask for a typed confirmation and it treats a successful Vast destroy API call as sufficient before clearing local session state.

The dashboard's `d` action has its own confirmation UI, but that does not change direct CLI behavior.

Open lifecycle-safety work adds stronger confirmation, provider-side disappearance verification, and broader lock/preemption diagnostics. Until that work lands, do not assume those safeguards exist on `main`.

## Current-main lifecycle model

```text
no session
    |
    | start
    v
RENTING -> BOOTING -> SSH_CONNECTING -> SSH_READY
                                      |
                                      v
                          RUNTIME_BOOTSTRAP -> RUNTIME_READY
                                                   |
                                                   v
                                  MODEL_STARTING -> MODEL_LOADING -> READY
                                                                    |
                                    +-------------------------------+------------------+
                                    |                               |                  |
                                  extend                          shorten             down
                                    |                               |                  |
                               later deadline                 earlier deadline     destroy

A qualifying post-SSH failure can preserve the paid instance as RECOVERABLE -> resume -> READY.
The deadline watchdog remains the time-bounded teardown authority for the recorded session.
```

## Files and local state

Typical Linux paths:

```text
~/.config/stint/credentials.json     Vast API key, mode 0600
~/.config/stint/ssh/id_ed25519       Stint SSH private key
~/.config/stint/ssh/id_ed25519.pub   Stint SSH public key
~/.local/state/stint/session.json    authoritative recorded session state
```

`XDG_STATE_HOME`, when set, replaces `~/.local/state` for state files.

## Known gaps on current main

These are important distinctions between shipped `main` behavior and work that exists only in pull requests:

1. **Deep Work is not on `main`.** The unattended mission executor, Hermes-on-box worker, phase-aware reasoning, Deep Work dashboard, GPU-owned GitHub publishing, and policy-driven maintenance are still in stacked PRs.
2. **The NInfer artifact path on `main` is still mutable.** The current source uses a Hugging Face `/resolve/main/` model URL together with a fixed expected transfer size. PR #74 pins an immutable revision and derives the real artifact size dynamically.
3. **CLI teardown does not yet verify provider disappearance.** Stronger `down` confirmation, destroy verification, lock-owner diagnostics, active-session Doctor behavior, and instance-scoped evidence are in the open lifecycle-safety stack, especially PR #76.
4. **The runtime-bundle startup path is experimental.** PRs #48-#51 explore replacing the full custom NInfer startup path with a relocatable runtime bundle; that path is not the default on `main`.
5. **Vast is the only live provider.** Other provider and protocol ideas are not part of the current live implementation.

## Development checks

```bash
# Run the full Go test suite.
go test ./...

# Run static vet checks.
go vet ./...

# Build the CLI package directly.
go build ./cmd/stint
```

Additional references:

- [`CLI.md`](CLI.md) for the long-form command reference
- [`DASHBOARD.md`](DASHBOARD.md) for dashboard behavior and state ownership
- [`TELEMETRY.md`](TELEMETRY.md) for the status/telemetry JSON contract
- [`architecture.md`](architecture.md) for the older architecture snapshot; use the top-level README for the current repository boundary
