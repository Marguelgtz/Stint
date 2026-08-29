# Stint

**Elastic compute for coding agents, written in Go.**

Stint provisions the right model/GPU topology for the way you are working:

- **Interactive:** one fast GPU optimized for latency, starting with RTX 4090 + Qwen3.8-27B.
- **Deep:** multiple cheaper workers optimized for validated work per dollar, starting with two RTX 3090s.
- **Sleep:** keep autonomous workers running under hard budget and safety limits, then tear them down automatically.

Stint sits below Cline, OpenCode, Cursor, and similar harnesses. It will expose stable OpenAI-compatible local endpoints while the actual compute can move between providers.

## Why Go

Stint is a long-running local control-plane CLI/daemon: it provisions remote workers, owns SSH tunnels, watches health, schedules teardown, enforces budgets, and eventually coordinates multiple autonomous workers. A single static Go binary is a good fit for that lifecycle and keeps the pre-V0 dependency surface small.

## Pre-V0

The repository is currently deliberately safe:

1. Built-in interactive and deep profiles.
2. Deterministic dry-run offer ranking.
3. Session-cost planning.
4. Spark repository onboarding/profile.
5. No code can rent or destroy a GPU yet.

The next milestone is a **read-only live Vast 4090 marketplace planner**. Mutating Vast operations come only after the selector is validated against real offers.

## Layout

```text
cmd/stint                    CLI entrypoint
internal/core                profiles, offers, ranking, session plans
internal/router              profile resolution
internal/provider/vast       Vast provider boundary
internal/provider/cloudflare fallback provider boundary
internal/runtime/llama       llama.cpp runtime configuration
internal/spark               Spark onboarding/evidence boundary
internal/collaboration       future reciprocal-worker contracts
.spark/profile.yml           Spark risk/evidence profile
```

## Run

Requires Go 1.23+.

```bash
go test ./...
go vet ./...
go run ./cmd/stint plan interactive --hours 5
go run ./cmd/stint onboard spark
```

Build a local binary:

```bash
make build
./bin/stint plan interactive --hours 5
```

## Target commands

```bash
stint plan interactive --hours 5
stint start interactive --hours 5
stint deep start --hours 8
stint status
stint sleep
stint down
```

## Spark

Stint dogfoods Spark from the first PR. `.spark/profile.yml` marks compute provisioning, runtime, and collaboration surfaces as high criticality. GitHub Actions emits stable evidence names: `spark-profile`, `go-vet`, and `unit-tests`.

## Safety principles

- Provider credentials stay local by default.
- No autonomous merge to `main` by default.
- Hard hourly/session budget ceilings before provisioning.
- Provider selection remains policy-driven, not hard-coded to a single host.
- Spark observes change/evidence; Stint owns compute/runtime lifecycle.
