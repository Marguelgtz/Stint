# Pre-V0 roadmap

## 0. Go foundation

- [x] Go module and CLI.
- [x] Deterministic interactive/deep ranking.
- [x] Spark profile and CI evidence.
- [x] No mutating GPU operations.

## 1. Interactive 4090 planner

- [ ] Read Vast API key from local environment/config.
- [ ] Query live marketplace offers read-only.
- [ ] Normalize Vast offer fields into `core.Offer`.
- [ ] Rank for latency within hard price/reliability limits.
- [ ] Print selected offer, alternatives, estimated session cost, and rejection reasons.
- [ ] Never rent from `stint plan`.

## 2. Interactive lifecycle

- [ ] `stint start interactive --hours N`.
- [ ] Explicit cost confirmation for early pre-V0.
- [ ] Provision instance from selected offer.
- [ ] Bootstrap llama.cpp + Qwen3.8-27B.
- [ ] Health check and fixed localhost tunnel.
- [ ] Persist local session state.
- [ ] Enforce timed teardown even if the foreground CLI exits.
- [ ] `stint status` and `stint down`.

## 3. Deep pair

- [ ] Two 3090 workers under aggregate budget.
- [ ] Separate worktrees and ownership leases.
- [ ] Reciprocal builder/verifier loop through Spark.
- [ ] Checkpoint/recovery for marketplace host failure.
- [ ] Overnight report.
