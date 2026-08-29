# Stint

**Elastic compute for coding agents, written in Go.**

Stint keeps the developer-facing endpoint stable while remote inference compute can change underneath it.

```text
Cline / local IDE
      |
      v
http://127.0.0.1:8409/v1
      |
      v
Stint-controlled tunnel
      |
      v
current remote model runtime
```

The first target is one interactive RTX 4090 running Qwen3.8-27B. Two cheaper deep workers follow only after the single-worker lifecycle is reliable.

## Why Go

Stint is a local control plane: provider APIs, process supervision, SSH tunnels, health checks, budgets, timers and eventually multiple autonomous workers. A single Go binary keeps that lifecycle small and inspectable.

## Authentication

Stint separates identity from infrastructure credentials.

- **Product identity:** GitHub, using the same model as Spark: GitHub OAuth for human identity and the GitHub App for repository authorization.
- **Compute provider:** the user's Vast API key remains a local BYOC credential and is not a Stint identity credential.
- **Repository evidence:** Spark owns repository/change evidence and GitHub App integration.

See [`docs/AUTH.md`](docs/AUTH.md).

## Pre-V0 phases

1. **Local foundation + Vast auth** — local credentials, dedicated SSH key, doctor/status.
2. **Live 4090 planner** — query and rank the real Vast marketplace without renting.
3. **Lifecycle + tunnel** — rent, bootstrap Qwen/llama.cpp, expose `127.0.0.1:8409`, destroy safely.
4. **Cline acceptance** — configure Cline once and prove repeated sessions work across changing Vast hosts.

Phase 2 is intentionally read-only. No Stint code can rent or destroy a GPU yet.

## Build

Requires Go 1.23+ and OpenSSH. No Vast CLI, Python, pip or Docker is required on the local machine.

```bash
make build
```

## Local setup

```bash
# hidden prompt; verifies instance-read + marketplace-search access before saving
./bin/stint auth vast

# creates ~/.config/stint/ssh/id_ed25519 specifically for Stint
./bin/stint setup ssh

# add the printed public key once in Vast Account -> Keys -> SSH Keys
./bin/stint doctor
./bin/stint status
```

Stint stores the Vast provider credential at `~/.config/stint/credentials.json` with owner-only permissions. The dedicated private SSH key stays under `~/.config/stint/ssh/`.

## Live marketplace planning

```bash
./bin/stint plan interactive --hours 5
```

This queries the real Vast marketplace and selects a policy-compliant RTX 4090. It does **not** create an instance.

Machine-readable and fixture modes:

```bash
./bin/stint plan interactive --hours 5 --json
./bin/stint plan interactive --hours 5 --fixture
```

The initial interactive policy is intentionally strict:

```text
1 x RTX 4090
<= $0.40/hour total
>= 98.5% reliability
>= 24 GB VRAM
>= 350 W GPU max-power allowance
>= 100 MB/s download
>= 1 direct port
50 GB allocated storage
on-demand only
session ceiling $2.50
```

The client sends these filters to Vast and then re-applies every hard constraint locally before ranking. See [`docs/PHASE2.md`](docs/PHASE2.md).

## Layout

```text
cmd/stint                    CLI entrypoint
internal/config              local paths and provider credential persistence
internal/local               SSH/key/port/terminal helpers
internal/core                profiles, offers, fail-closed policy, ranking, session plans
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
go build ./cmd/stint
```

Optional live search-only integration test:

```bash
STINT_VAST_INTEGRATION=1 VAST_API_KEY=... \
  go test ./internal/provider/vast -run Integration
```

No integration test in Phase 2 creates compute.

## Target command surface

```bash
# implemented through Phase 2
stint auth vast
stint setup ssh
stint doctor
stint status
stint plan interactive --hours 5

# Phase 3+
stint start interactive --hours 5
stint down
stint deep start --hours 8
stint sleep
```

## Cline invariant

Cline will be configured once:

```text
Provider: OpenAI Compatible
Base URL: http://127.0.0.1:8409/v1
Model: qwen3.8-27b
```

Phase 3 makes whichever Vast machine Stint selects appear at that endpoint.

## Spark

Stint dogfoods Spark. `.spark/profile.yml` marks compute provisioning, runtime and collaboration surfaces as high criticality. GitHub Actions emits stable evidence names: `spark-profile`, `go-vet`, and `unit-tests`.

## Safety principles

- Provider credentials stay local by default.
- Product identity is GitHub; provider credentials are not identity.
- No autonomous merge to `main` by default.
- No provider mutation exists in Phase 2.
- Hard hourly and session ceilings are enforced before any future provisioning.
- Server-side marketplace filters are revalidated locally.
- Provider selection is policy-driven, not hard-coded to a host.
- Spark observes change/evidence; Stint owns compute/runtime lifecycle.
