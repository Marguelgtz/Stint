# Stint

**A local control plane for disposable GPU inference used by coding agents.**

Stint keeps a stable OpenAI-compatible endpoint on the developer machine while remote GPU compute can be selected, rented, replaced, recovered, observed, and destroyed underneath it.

```text
Cline / Hermes / another OpenAI-compatible client
                         |
                         v
              http://127.0.0.1:8409/v1
                         |
                         v
                 Stint local control plane
                /        |         \
               /         |          \
       marketplace    lifecycle    telemetry
             |            |            |
             +------------+------------+
                          |
                          v
                       Vast GPU
                          |
                          v
                  Qwen3.8-27B runtime
                 NInfer or llama.cpp
```

> **Repository status:** active prototype / pre-V0. The current `main` branch supports paid **interactive** sessions. The Deep Work executor and its on-box autonomous workflow are still in stacked pull requests and are **not** part of `main` yet.

## Start here

- [Operator instructions](docs/INSTRUCTIONS.md) for setup, start/status/down, recovery, dashboard use, runtime options, and current safety caveats.
- [CLI reference](docs/CLI.md) for the longer command reference.
- [Dashboard guide](docs/DASHBOARD.md) for the terminal cockpit and its state-ownership rules.
- [Telemetry contract](docs/TELEMETRY.md) for status/JSON fields and live inference observation.

## What is on `main`

The current shipped repository boundary was reconciled against `main` on 2026-09-11.

| Area | Current `main` |
| --- | --- |
| Live compute provider | Vast |
| Live profile | `interactive` |
| Planning | live `interactive`; fixture-only `deep` profile |
| Model | `qwen3.8-27b` |
| Runtimes | NInfer on qualified RTX 4090 hosts; llama.cpp fallback/explicit runtime |
| Stable client endpoint | `http://127.0.0.1:8409/v1` |
| NInfer context profiles | `coding` 126,976; `precision` 172,032; `native` 262,144 |
| NInfer lanes | one or two client lanes over a shared dynamic KV/context pool |
| Lifecycle | plan, start, resume, extend, shorten, down, deadline watchdog |
| Host qualification | provider ranking, candidate retry, SSH checks, measured model-transfer threshold |
| Session state | persisted lifecycle checkpoints and recoverable paid sessions |
| Telemetry | cached/local status, remote refresh, JSON snapshot, GPU/runtime/endpoint metrics |
| Live inference observation | `/metrics` + `/slots` polling without generating inference traffic |
| Operator UI | `stint dash` terminal cockpit |
| Benchmarking | explicit `stint perf`, including real prompt-depth tests |
| Spark boundary | repository evidence/onboarding integration remains separate from compute lifecycle |
| Deep Work executor | **not on `main`** |

Recent `main` work includes mutable session deadlines, session snapshot telemetry, the live dashboard and recovery controls, deeper prompt benchmarking, passive inference observation, dual NInfer lanes, and retrying stale Vast offers during startup.

## Main commands

The examples use `./bin/stint` from a local build. If the binary is installed on `PATH`, use `stint` instead.

```bash
# Build the current checkout into ./bin/stint.
make build

# Verify and store the Vast API key locally.
./bin/stint auth vast

# Create or reuse Stint's dedicated SSH keypair.
./bin/stint setup ssh

# Check provider access, SSH prerequisites, and local port 8409 before spending.
./bin/stint doctor

# Preview and rank a one-hour interactive session without renting anything.
./bin/stint plan interactive --hours 1

# Rent a one-hour interactive GPU session and bring the local endpoint to READY.
./bin/stint start interactive --hours 1

# Start NInfer explicitly with native 262k context and two lanes.
./bin/stint start interactive --hours 1 --runtime ninfer --ninfer-config native --clients 2

# Read the current local/cached session state without a remote probe.
./bin/stint status

# Refresh endpoint, runtime, GPU, and live inference telemetry read-only.
./bin/stint status --refresh

# Open the live terminal cockpit for the active session.
./bin/stint dash

# Benchmark the active endpoint at a real 32k prompt depth.
./bin/stint perf --prompt-tokens 32768

# Add 30 minutes to the existing auto-destroy deadline, within the cost ceiling.
./bin/stint extend 30m

# Reattach to a preserved RECOVERABLE paid session without intentionally re-renting.
./bin/stint resume

# Immediately destroy the recorded Vast instance and clear the local session state.
./bin/stint down
```

For the full operating sequence and the current `stint down` caveat, read [`docs/INSTRUCTIONS.md`](docs/INSTRUCTIONS.md).

## The current interactive lifecycle

A live interactive start does more than rent a GPU:

1. Query Vast and rank offers under Stint's hard local policy.
2. Select a candidate and ask for confirmation unless `--yes` is supplied.
3. Rent the instance and persist its identity immediately.
4. Start the deadline watchdog for the paid resource.
5. Attach/verify SSH and reject failed startup candidates where the lifecycle still permits replacement.
6. Measure real model-transfer throughput when network qualification is enabled.
7. Select and bootstrap NInfer or llama.cpp.
8. Start Qwen3.8-27B and supervise the SSH tunnel to `127.0.0.1:8409`.
9. Persist `READY` when the OpenAI-compatible endpoint answers.
10. Keep lifecycle, deadline, telemetry, recovery, and teardown state local to Stint.

The persisted interactive lifecycle includes states such as:

```text
RENTING
  -> BOOTING
  -> SSH_CONNECTING
  -> SSH_READY
  -> RUNTIME_BOOTSTRAP
  -> RUNTIME_READY
  -> MODEL_STARTING
  -> MODEL_LOADING
  -> READY

post-SSH recoverable failure -> RECOVERABLE -> stint resume -> READY
```

`session.json` is the local lifecycle authority. Dashboard health such as `DEGRADED` is observational and does not silently rewrite the persisted lifecycle state.

## Runtime behavior

### NInfer

NInfer is currently qualified for RTX 4090 hosts with CUDA 12.8+.

| Config | Context | KV | Typical use |
| --- | ---: | --- | --- |
| `coding` | 126,976 | INT8 | default agent session |
| `precision` | 172,032 | INT8 | larger context with INT8 KV |
| `native` | 262,144 | E8 4-bit | full native context |

`--clients 2` creates two NInfer execution/cache lanes over one shared dynamic KV pool. A runtime lane is not a stable external-client identity, and Stint's telemetry avoids pretending that it is.

### llama.cpp

llama.cpp remains the portable fallback and explicit alternative runtime. `--context` controls its configured context between 1,024 and 131,072 tokens.

With `--runtime auto`, a qualified RTX 4090 selects NInfer. Other qualifying GPUs select llama.cpp. When a two-lane NInfer session was explicitly requested, Stint will not silently fall back to a single-lane llama.cpp session.

## Interactive policy

The built-in interactive profile currently prefers RTX 4090 and then RTX 3090, with hard eligibility that includes:

```text
<= $0.40/hour
>= 98.5% reliability
>= 24 GB VRAM
>= 1 direct port
verified
rentable
not already rented
50 GB storage
$2.50 scheduled session ceiling
```

Network speed, GPU power allowance, reliability, performance, and price also affect ranking. `stint plan` is the safe way to inspect the current marketplace decision before paying.

## Dashboard and telemetry

`stint status` is local/cached by default. `stint status --refresh` adds bounded read-only probes for:

- `/v1/models` serving health
- runtime/process health over SSH
- GPU utilization, VRAM, temperature, and power
- `/metrics` and `/slots` live inference state
- active requests, queue depth, resident context, cache reuse, decode/prefill rates, and lane state when the runtime exposes them

The refresh path does not send a model completion request.

`stint dash` presents the same session and telemetry model as a terminal cockpit. It adds explicit controls for refresh/recovery, benchmarking, deadline changes, and teardown without becoming a second lifecycle authority.

`stint perf` is different: it deliberately sends inference requests to measure actual prompt depth, TTFT, total latency, decode rate, and runtime pressure.

## Architecture on `main`

```text
cmd/stint
  CLI entrypoint plus interactive lifecycle, NInfer orchestration,
  dashboard controller, telemetry collectors, benchmark and recovery paths

internal/core
  provider-neutral profiles, offer policy, evaluation, ranking and plans

internal/provider/vast
  Vast REST boundary, marketplace search and instance operations

internal/session
  authoritative session state and deadline helpers

internal/dashboard
  terminal rendering and input handling

internal/local
  local SSH, port, process and terminal helpers

internal/runtime/llama
  llama.cpp model/runtime configuration

internal/router
  built-in profile resolution

internal/spark
  Spark onboarding/evidence boundary

internal/collaboration
  narrow future collaboration contracts
```

One consequence of the project's fast development is that not every older architecture document reflects this current package/runtime shape. Treat this README and [`docs/INSTRUCTIONS.md`](docs/INSTRUCTIONS.md) as the current repository-level overview.

## Work still in pull requests

Open PR state is not the same as shipped behavior. Several PRs are stacked, generated by Deep Work itself, divergent from current `main`, or superseded by later integration branches. They should not be treated as independently mergeable just because they are open.

### Deep Work line

Deep Work is the largest current body of unmerged product work:

- [#57](https://github.com/Marguelgtz/Stint/pull/57) introduces the bounded mission executor and `stint deep start/status/stop` model.
- [#59](https://github.com/Marguelgtz/Stint/pull/59) moves the Hermes worker onto the compute box.
- [#77](https://github.com/Marguelgtz/Stint/pull/77) adds phase-aware reasoning and a living action plan.
- [#78](https://github.com/Marguelgtz/Stint/pull/78) hardens the long-run configuration and phase routing.
- [#79](https://github.com/Marguelgtz/Stint/pull/79) adds the Deep Work execution dashboard.
- [#80](https://github.com/Marguelgtz/Stint/pull/80) moves checkpoint publication and GitHub ownership onto the GPU.
- [#85](https://github.com/Marguelgtz/Stint/pull/85) adds policy-driven Deep Work GitHub maintenance and release provenance.

PRs #81-#89 are mostly GPU-generated checkpoint/handoff artifacts from Deep Work test sessions. They are useful evidence, but they are not separate product features that should be read as a roadmap.

### Runtime and lifecycle fixes

- [#74](https://github.com/Marguelgtz/Stint/pull/74) fixes the NInfer artifact-revision/size failure by pinning an immutable model revision and deriving the transfer size dynamically.
- [#76](https://github.com/Marguelgtz/Stint/pull/76) is the broader current-stack P0 lifecycle-safety integration: lock-owner metadata, safer `down` preemption, authoritative instance lookup, active-session Doctor diagnostics, and instance-scoped evidence. Its own description notes that older safety work should not be merged wholesale around it.

### Startup experiment

PRs [#48](https://github.com/Marguelgtz/Stint/pull/48) through [#51](https://github.com/Marguelgtz/Stint/pull/51) explore a relocatable NInfer runtime bundle and a faster Vast base-image startup path. That is experimental and not the default path on `main`.

### Documentation reorganization

[#93](https://github.com/Marguelgtz/Stint/pull/93) reorganizes the older flat documentation set. It predates this current-state README pass and will need reconciliation rather than being assumed conflict-free.

A number of older open branches such as #11, #36, #56, #58, and portions of the #60-#70 stack predate or overlap later `main` work. They are repository history and investigation context until deliberately reconciled.

## Known gaps on current `main`

These matter when operating the repository today:

1. **Deep Work is not shipped on `main`.** Do not expect `stint deep ...` from a normal main build.
2. **The NInfer model artifact is still mutable on `main`.** The current source points at a Hugging Face `/resolve/main/` artifact and uses a fixed expected byte count. An upstream replacement can produce progress/checksum failures; #74 is the fix line.
3. **CLI `stint down` is currently immediate.** It does not ask for a typed confirmation and does not verify that Vast has stopped reporting the instance before local state is cleared. Stronger behavior is still in the open safety stack.
4. **Vast is the only live compute provider.** Provider abstraction ideas exist, but current paid operation is Vast-specific.
5. **The project is evolving faster than some older docs.** Check whether a behavior is on `main` or only in a PR before relying on it.

## Build and development

Requires Go 1.23+ and OpenSSH locally.

```bash
make build
go test ./...
go vet ./...
go build ./cmd/stint
```

Optional live search-only integration testing is available for the Vast provider. See the existing test/docs surfaces before enabling any network-backed test.

## Local state and credentials

By default on Linux:

```text
~/.config/stint/credentials.json
~/.config/stint/ssh/id_ed25519
~/.config/stint/ssh/id_ed25519.pub
~/.local/state/stint/session.json
```

The Vast API key and SSH private key stay local. Stint uses owner-only permissions for credential and session-state files.

## Spark

Stint dogfoods Spark, but the boundary is intentional: **Stint owns compute/runtime lifecycle; Spark owns repository/change evidence.** The `.spark/profile.yml` profile and CI evidence remain in the repository without making Spark the provider or lifecycle authority.
