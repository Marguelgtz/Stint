# Architecture

## Product boundary

Stint answers: **where, when, and on what compute should agent work run?**

Spark answers: **what changed, what evidence exists, how did the change progress, and eventually how should multiple autonomous workers hand work/review evidence across that trajectory?**

Agent harnesses answer: **which filesystem, terminal, browser, and repository tools may the model invoke?**

Keeping these boundaries explicit allows Stint to support multiple providers, runtimes, and agent harnesses while dogfooding Spark as the repository observability layer from pre-V0.

## Modules

### Compute broker

Inputs:

- GPU requirements
- reliability floor
- hourly/session budget
- geography/network preferences
- interruptibility policy

Outputs:

- ranked offers
- selected offer
- estimated session cost

### Model runtime

Responsible for turning provisioned compute into a healthy OpenAI-compatible endpoint.

Pre-V0 target: llama.cpp + Qwen3.8-27B on one RTX 4090.

### Router

Maps an intent profile to a topology.

Examples:

- `interactive` -> 1 fast 4090-class worker
- `deep` -> 2 inexpensive 3090-class workers
- `sleep` -> destroy interactive worker, retain autonomous pool

### Spark integration

`@stint/spark` owns only the Stint-facing integration surface:

- Stint's `.spark/profile.yml` source-of-truth
- profile serialization/drift checks
- onboarding checklist
- future compact task/review/evidence handoff contracts

Spark remains a separate GitHub App and service. Stint must not copy Spark's evaluator, persistence layer, installation tokens, or dashboard concerns into this repository.

### Collaboration contract

Stint should not embed Spark's full domain model. It emits and consumes a small contract:

- worker availability
- task assignment
- endpoint
- budget/runtime limits
- checkpoint/teardown signals
- candidate commit ready for review
- review finding / resolution
- deterministic evidence attached to a candidate

Spark owns durable change/evidence trajectory. The agent harness owns filesystem/tool execution. Stint owns worker orchestration.

## Repository onboarding

The repository is Spark-ready when:

1. `.spark/profile.yml` is on the default branch.
2. GitHub Actions exposes the evidence names referenced by that profile.
3. The Spark GitHub App is installed for the repository.
4. A pull request receives a Spark check for its exact head SHA.

## Safety defaults

- Real provisioning requires explicit command/action.
- Session budget is mandatory before unattended operation.
- Instances have a hard expiry.
- Autonomous workers cannot merge to main by default.
- Secrets must not be passed in task artifacts.
- Spark observes source-derived metadata/evidence but Stint does not hand it provider credentials.
