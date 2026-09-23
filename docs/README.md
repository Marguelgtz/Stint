# Stint documentation

This index separates current operating guidance from planning and historical
records. The top-level [README](../README.md) describes the product boundary.

## Current operation

- [Operator instructions](INSTRUCTIONS.md): setup, plan, start, recovery, status,
  teardown, and the current runtime options.
- [Next Stint](operations/next-stint.md): the next acceptance work and its
  fixed-budget gate.
- [Dashboard](DASHBOARD.md) and [telemetry](TELEMETRY.md): the two observation
  surfaces and their data contracts.
- [Deep Work](DEEP_WORK.md), [Deep Work setup](DEEP_WORK_SETUP.md), and the
  [Deep Dashboard action plan](DEEP_WORK_DASHBOARD_ACTION_PLAN.md).
- [NInfer](NINFER.md) and the [CLI reference](CLI.md).

## Architecture and planning

- [Architecture](reference/architecture.md): current interactive, Deep Work,
  provider, and Spark boundaries.
- [Roadmap](planning/roadmap.md): short sequencing view. Acceptance details and
  verification state live in the canonical [grounding plan](STINT_REPOSITORY_GROUNDING_PLAN.md).
- [Authentication](guides/authentication.md) and [Spark integration](guides/spark.md).

## Canonical repository grounding

- [Grounding plan](STINT_REPOSITORY_GROUNDING_PLAN.md): current execution state
  and acceptance gates.
- [Open PR semantic ledger](STINT_OPEN_PR_GROUNDING_LEDGER.md): PR intent,
  exact refs, evidence, replacements, and dispositions.
- [Repository handoff](STINT_REPOSITORY_GROUNDING_HANDOFF.md): concise recovery
  point for the next Stint/Spark work.

## History

- [Phase 1](history/phase-1.md) and [Phase 2](history/phase-2.md): early
  project-stage records; their policy and remaining-work statements are historical.
- [CP1 reports](history/README.md): preserved source evidence from the retired
  CP1 run.
- [Pre-runtime next-stint plan](history/pre-runtime-next-stint-2026-09.md),
  [pre-V0 roadmap](history/pre-v0-roadmap.md), and
  [early architecture snapshot](history/pre-v0-architecture.md): retained for
  context and superseded by the current operation, roadmap, and architecture
  pages above.

Run-specific Deep Work reports and generated GPU evidence stay attached to
their evidence PRs. The successful session `20260923-022052` is represented by
open PRs #110–#113 and is intentionally not copied into product `main`.
