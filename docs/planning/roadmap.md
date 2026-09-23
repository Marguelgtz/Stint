# Stint roadmap

This page summarizes sequencing. The canonical
[grounding plan](../STINT_REPOSITORY_GROUNDING_PLAN.md) owns detailed tasks,
status markers, evidence, and acceptance criteria.

## Current work

1. **Qualify the selected NInfer runtime on a fresh RTX 4090.** Blocked by
   current policy-qualified inventory under the existing price, reliability,
   and session limits. Keep source-build as the default and the verified
   release bundle opt-in until the full live acceptance gate passes.
2. **Choose the runtime deployment default from comparable evidence.** Compare
   source-build and immutable release-bundle startup and recovery. Treat the
   old GHCR image as historical evidence unless its image-loading and
   SSH-observable lifecycle can be revalidated.
3. **Evaluate runtime updates separately.** The upstream vision-cap fix and
   DFlash2 are candidates requiring Stint acceptance; neither is selected by
   upstream recency or throughput alone.
4. **Continue Spark ↔ Stint integration from the measured bootstrap.** Keep
   Spark's repository/evidence boundary separate from Stint's provider and
   runtime ownership.

## Preserved current product surfaces

- Interactive Vast lifecycle, recoverable paid-session state, watchdog,
  teardown verification, `stint dash`, and truthful NInfer lane telemetry.
- Hermes-on-box Deep Work, detached supervision, independent verification,
  explicit landing, `stint deep dash`, and protected generated session
  evidence.
- Spark path-aware checks and repository evidence integration.

The older pre-V0 roadmap is retained in
[`../history/pre-v0-roadmap.md`](../history/pre-v0-roadmap.md) as history, not
as a current task list.
