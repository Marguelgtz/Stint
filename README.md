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

> **Repository status:** active prototype / pre-V0. Current `main` supports paid interactive sessions and bounded Hermes-on-box Deep Work missions. Deep Work uses detached supervision, persisted policy, independent task verification, and explicit landing; see [`docs/DEEP_WORK.md`](docs/DEEP_WORK.md) before running it.

## Start here

- [Documentation index](docs/README.md) for current guides, operations, architecture, roadmap, and history.
- [Operator instructions](docs/INSTRUCTIONS.md) for setup, start/status/down, recovery, dashboard use, runtime options, and current safety caveats.
- [CLI reference](docs/CLI.md) for the longer command reference.
- [Dashboard guide](docs/DASHBOARD.md) for the terminal cockpit and its state-ownership rules.
- [Telemetry contract](docs/TELEMETRY.md) for status/JSON fields and live inference observation.

## What is on `main`

The current repository boundary was reconciled against `main` on 2026-09-23.

| Area | Current `main` |
| --- | --- |
| Live compute provider | Vast |
| Live profile | `interactive` |
| Planning | Vast `interactive`; Deep Work missions use the separate on-box coordinator |
| Model | `qwen3.8-27b` |
| Runtimes | NInfer for the selected SM89 tuple; llama.cpp fallback/explicit runtime |
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
| Deep Work executor | Hermes-on-box, detached supervisor, persisted coordination and verified landing |

Recent `main` work includes safer paid-session teardown, mutable deadlines, session snapshot telemetry, both dashboards, deeper prompt benchmarking, passive inference observation, dual NInfer lanes, Hermes-on-box Deep Work, startup phase evidence, and a pinned immutable NInfer runtime release. Source-build remains the startup default; the release-bundle path is opt-in pending fresh RTX 4090 acceptance.

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

The selected NInfer build tuple targets RTX 4090 hosts with CUDA 12.8+ and
SM89; fresh Stint live acceptance is still required. The source pin is
`sergiuszm/ninfer-4090` commit
`81b68a20a9a0d9ab47d7e5838887c6d636ab76e0`; the model is NInfer v2 artifact
revision `18dfc887423fa5aabf3cb56fac41490e462b3fab`, SHA-256
`eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e`.

`--ninfer-deployment source-build` remains the default and recovery path. The
opt-in `--ninfer-deployment release-bundle` path downloads Stint's immutable,
SHA-pinned GitHub Release bundle, checks the manifest and both binaries, then
atomically switches the runtime. It fails closed on any acquisition or
verification error; it does not silently compile after a bad or missing
release. Use `stint resume --ninfer-deployment source-build` to explicitly
recover a paid session after a bundle failure. Status snapshots record the
deployment method, runtime and model pins, acquisition/verification durations,
and rental-to-READY time. Candidate builds pass clean-base smoke before
uploading. The publisher checks the originating run, draft provenance and
exact archive SHA pin before publishing the immutable release. The release
tag is created from the exact candidate `main` build; its archive now matches
the SHA pinned in current `main`.

The bundle remains opt-in until a fresh RTX 4090 model-load, response
correctness, two-lane, native-context, Deep Work and teardown acceptance run is
recorded. No comparable source-build versus release-bundle READY-time result
is available yet.

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

Open PR state is not the same as shipped behavior. The exact current branch heads, bases, checks, evidence and dispositions are recorded in the [semantic PR ledger](docs/STINT_OPEN_PR_GROUNDING_LEDGER.md).

The current open PR snapshot contains #85 (parked maintenance experiment) and protected generated Deep Work evidence #110–#113, which must remain open and unmerged. PR #93 was closed after the selected documentation taxonomy and current pages landed through #133. PRs #48–#51 were closed after their runtime replacements were verified; #73 was closed after its reports were preserved under `docs/history/` and recorded by the canonical docs. Historical branches #57–#89 describe the work that led to the current Hermes-on-box architecture, not a list of current product gaps.

## Known gaps on current `main`

These matter when operating the repository today:

1. **The immutable runtime bundle still needs live RTX 4090 qualification.** Its exact archive is published and pinned, but model loading, response correctness, two-lane operation, native-context operation, Deep Work and teardown have not been qualified on a fresh rented host. Keep `release-bundle` opt-in.
2. **Source-build remains the default startup path.** No comparable source-build versus release-bundle READY-time measurement is recorded.
3. **Vast is the only live compute provider.** Deep Work's instance binding and mission policy do not make other providers available.
4. **Some older operational docs remain stale.** Prefer this README, [`docs/INSTRUCTIONS.md`](docs/INSTRUCTIONS.md), [`docs/DEEP_WORK.md`](docs/DEEP_WORK.md), and the canonical grounding documents; verify a behavior against current source when they disagree.

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
