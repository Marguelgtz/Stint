# Stint

**Elastic compute for coding agents.**

Stint provisions the right model/GPU topology for the way you are working:

- **Interactive:** one fast GPU optimized for latency.
- **Deep:** multiple cheaper workers optimized for validated work per dollar.
- **Sleep:** keep autonomous workers running under hard budget and safety limits, then tear them down automatically.

Stint does **not** try to replace Cline, OpenCode, Cursor, or other agent harnesses. It provides stable OpenAI-compatible endpoints and a compute/control layer beneath them.

## Initial architecture

```text
Developer / agent harness
        |
        v
     Stint CLI
        |
        +---- Compute Broker ---- Vast / future providers
        |
        +---- Model Router ------ task/profile -> GPU/model/runtime
        |
        +---- Runtime ----------- llama.cpp / future vLLM/SGLang
        |
        +---- Stable endpoints -- localhost:8409 / 8301 / 8302
        |
        `---- Spark integration - repo profile, evidence, future collaboration
```

## Pre-V0 scope

The current personal-integration slice is deliberately conservative:

1. Parse profiles and budgets.
2. Search offers in **dry-run** mode.
3. Select an offer deterministically.
4. Produce a provisioning plan.
5. Onboard Stint itself into Spark observability.
6. Add real Vast instance creation only after the planner is tested.
7. Keep deep-worker collaboration behind a narrow Spark contract.

The first real compute milestone is **one RTX 4090 interactive stint**. The two-3090 deep pool follows after the single-worker lifecycle is reliable.

## Repository layout

```text
apps/cli                     User-facing CLI
packages/core                Domain types and config
packages/provider-vast       Vast marketplace adapter
packages/provider-cloudflare Fallback inference provider contract
packages/runtime-llama       llama.cpp runtime contract
packages/router              Profile/task -> compute routing
packages/contracts           Provider-neutral collaboration contracts
packages/spark               Spark repository profile/onboarding adapter
.spark/profile.yml           Spark repository-risk/evidence profile
```

## Commands we are targeting

```bash
stint plan interactive --hours 5
stint onboard spark
stint start interactive --hours 5
stint deep start --hours 8
stint status
stint sleep
stint down
```

The scaffold currently implements **dry-run planning** plus a **Spark onboarding plan**. It cannot spend money or provision a GPU yet.

## Quick start

```bash
corepack enable
pnpm install
cp stint.config.example.yaml stint.config.yaml
pnpm stint -- plan interactive --hours 5
pnpm stint -- onboard spark
pnpm test
pnpm typecheck
```

## Spark dogfood

Stint is intended to dogfood Spark from the first pull request. The repository contains a checked-in Spark profile at `.spark/profile.yml`, with high-criticality coverage for compute provisioning, model runtime, collaboration, and release automation. CI exposes stable evidence names (`spark-profile`, `typecheck`, `unit-tests`) for Spark to observe.

After pushing the repository to GitHub, install the Spark GitHub App for the repository and open a small PR to verify the end-to-end check/history loop. See [`docs/SPARK.md`](docs/SPARK.md).

## Principles

- Provider credentials stay local by default.
- Never merge to `main` autonomously by default.
- Compute selection is policy-driven, not hard-coded to one GPU.
- Users pay infrastructure providers directly in the first commercial version.
- Optimize eventually for **cost per validated engineering outcome**, not tokens per second alone.
- Spark observes and records change/evidence; Stint owns compute and execution.

See [`docs/architecture.md`](docs/architecture.md), [`docs/SPARK.md`](docs/SPARK.md), and [`docs/roadmap.md`](docs/roadmap.md).

## Licensing

No open-source license has been selected yet. Keep the repository private until the product/licensing strategy is decided.
