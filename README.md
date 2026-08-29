# Stint

**Elastic compute for coding agents, written in Go.**

Stint provisions the right model/GPU topology for the way you are working:

- **Interactive:** one fast GPU optimized for latency, starting with RTX 4090 + Qwen3.8-27B.
- **Deep:** multiple cheaper workers optimized for validated work per dollar, starting with two RTX 3090s.
- **Sleep:** keep autonomous workers running under hard budget and safety limits, then tear them down automatically.

Stint sits below Cline, OpenCode, Cursor, and similar harnesses. Its core invariant is that local tools keep a stable OpenAI-compatible endpoint while remote compute can move between providers.

```text
Cline -> http://127.0.0.1:8409/v1 -> Stint tunnel -> current remote model runtime
```

## Why Go

Stint is a long-running local control-plane CLI/daemon: it provisions remote workers, owns SSH tunnels, watches health, schedules teardown, enforces budgets, and eventually coordinates multiple autonomous workers. A single static Go binary is a good fit for that lifecycle and keeps the pre-V0 dependency surface small.

## Pre-V0 phases

1. **Local foundation + Vast auth**: credentials, dedicated SSH key, doctor/status. Implemented on the Phase 1 branch.
2. **Live 4090 planner**: query and rank the real Vast marketplace without renting.
3. **Lifecycle + tunnel**: rent, bootstrap Qwen/llama.cpp, expose `127.0.0.1:8409`, destroy safely.
4. **Cline acceptance**: configure Cline once and prove repeated sessions work across changing Vast hosts.

No code can rent or destroy a GPU yet.

## Phase 1 setup

Requires Go 1.23+ and OpenSSH. No Vast CLI, Python, or pip is required.

```bash
make build

# Hidden interactive prompt; the key is verified before it is persisted.
./bin/stint auth vast

# Alternative for an already-exported environment variable.
./bin/stint auth vast --from-env

# Creates ~/.config/stint/ssh/id_ed25519 specifically for Stint.
./bin/stint setup ssh

# Add the printed public key once in Vast Account -> Keys -> SSH Keys,
# then inspect local/API readiness.
./bin/stint doctor
./bin/stint status
```

Stint stores the Vast credential at `~/.config/stint/credentials.json` with owner-only permissions. The dedicated private SSH key stays under `~/.config/stint/ssh/` and is never sent to the model.

The eventual Cline configuration will remain fixed:

```text
Provider: OpenAI Compatible
Base URL: http://127.0.0.1:8409/v1
Model: qwen3.8-27b
```

## Layout

```text
cmd/stint                    CLI entrypoint
internal/config              local paths and credential persistence
internal/local               SSH/key/port/terminal helpers
internal/core                profiles, offers, ranking, session plans
internal/router              profile resolution
internal/provider/vast       direct Vast REST API boundary
internal/provider/cloudflare fallback provider boundary
internal/runtime/llama       llama.cpp runtime configuration
internal/spark               Spark onboarding/evidence boundary
internal/collaboration       future reciprocal-worker contracts
.spark/profile.yml           Spark risk/evidence profile
```

## Development

```bash
go test ./...
go vet ./...
go run ./cmd/stint plan interactive --hours 5
go run ./cmd/stint onboard spark
```

## Current and target commands

```bash
# implemented
stint auth vast
stint setup ssh
stint doctor
stint status
stint plan interactive --hours 5

# next
stint start interactive --hours 5
stint deep start --hours 8
stint sleep
stint down
```

## Spark

Stint dogfoods Spark from the first PR. `.spark/profile.yml` marks compute provisioning, runtime, and collaboration surfaces as high criticality. GitHub Actions emits stable evidence names: `spark-profile`, `go-vet`, and `unit-tests`.

## Safety principles

- Provider credentials stay local by default.
- Credentials are verified before being written and stored with mode `0600`.
- A dedicated Stint SSH key is used instead of taking over the user's default key.
- No autonomous merge to `main` by default.
- Hard hourly/session budget ceilings before provisioning.
- Provider selection remains policy-driven, not hard-coded to a single host.
- Spark observes change/evidence; Stint owns compute/runtime lifecycle.
