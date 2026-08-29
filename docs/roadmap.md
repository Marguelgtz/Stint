# Roadmap

## Milestone 0 — Planner + Spark dogfood

- [x] Monorepo scaffold
- [x] Core topology/profile types
- [x] Deterministic offer ranking function
- [x] Dry-run CLI plan command
- [x] Spark package/integration boundary
- [x] `.spark/profile.yml` for the Stint repository
- [x] Stable CI evidence names for Spark (`spark-profile`, `typecheck`, `unit-tests`)
- [x] Spark onboarding CLI/checklist
- [ ] Create/push `Marguelgtz/stint`
- [ ] Install Spark GitHub App for the Stint repo
- [ ] Verify first Stint PR appears in Spark trajectory/history
- [ ] YAML Stint config loader
- [ ] Vast CLI/API search adapter
- [ ] Price/reliability policy tests against normalized live offers

## Milestone 1 — Interactive stint

- [ ] Search Vast for qualifying 4090-class offers
- [ ] Create instance
- [ ] Bootstrap llama.cpp runtime
- [ ] Load Qwen3.8-27B + MTP
- [ ] Health check
- [ ] Establish stable local endpoint (`localhost:8409`)
- [ ] Hard timed teardown
- [ ] Cloudflare fallback
- [ ] Record provisioning/runtime changes through normal Spark-observed PRs

## Milestone 2 — Deep pool

- [ ] Provision two 3090-class workers
- [ ] Stable endpoints (`8301`, `8302`)
- [ ] Worker health/lease state
- [ ] Runtime budget enforcement
- [ ] Sleep transition: tear down interactive worker, retain deep pool

## Milestone 3 — Spark collaboration

- [ ] Extend Spark adapter contract beyond repo observability
- [ ] Reciprocal builder/reviewer scheduling
- [ ] Worktree isolation
- [ ] Task/file ownership leases
- [ ] Review artifacts and rework loop
- [ ] Morning report

## Milestone 4 — Routing intelligence

- [ ] Model/GPU benchmark registry
- [ ] Task-type routing
- [ ] Heterogeneous model verification
- [ ] Cost per validated outcome telemetry
