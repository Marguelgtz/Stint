# Stint Repository Grounding Handoff

**Status:** in progress. This is the current recovery point, not mission completion.

## Repository and landed work

- Starting `main`: `9634bf762a2dc9021747eb786db7fd23ccab84e9`.
- Current `main`: `6f4c81f76118924dfb7b41fa6a85394b2796b399`.
- Grounding merges: #116 safety (`ca2f24b`), #117 ordinary dashboard (`9a67234`), #118 Deep Dashboard phase evidence (`6f4c81f`). Each exact landed SHA passed the five required CI jobs; runs are `35851717414`, `35855342269`, and `35856861212` respectively.
- The original checkout at `792bb508dfcd7d64e293359dfbed7b497b10dafa` remains dirty and untouched. Work used isolated `/tmp` worktrees.

## Current behavior

- `stint dash` separates active processing from retained/resident context, shows every NInfer slot, uses runtime-specific cache/prefill interpretations, preserves same-instance last-good telemetry, shows staleness/errors, and records bounded unattributed lane transitions.
- `stint deep dash` has Run, Tasks, Activity, Worker and Phase views; shows verifier/checkpoint SHAs and landing evidence; historic sessions cannot land; latest-session landing requires its bound READY compute.
- Lifecycle safety is on current main: verified destroy disappearance before clearing paid-session state, protected watchdog/owner behavior and active Doctor.
- Current NInfer tuple is still runtime `981b685ea2124fdaed023123d2e63fd29d529ab8`, artifact rev `18dfc887423fa5aabf3cb56fac41490e462b3fab`, artifact SHA `eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e`, SM89/RTX 4090, CUDA floor 12.8, native context 262144. Deliberate upstream candidate audit is still pending.
- Normal fresh startup still builds the pinned NInfer source. There is no promoted immutable release bundle, runtime provenance, or comparable bundle-vs-source READY measurement yet.

## PR and evidence state

- Full point-in-time record of 38 open PRs, exact head/base SHAs, check rollups, changed paths, symbols/tests and semantic dispositions: [`STINT_OPEN_PR_GROUNDING_LEDGER.md`](STINT_OPEN_PR_GROUNDING_LEDGER.md).
- Keep latest successful GPU evidence #110–#113 (session `20260923-022052`) open and unmerged. Checkpoint SHAs: `a051c94a0f02b5d65a4e5a4fb2b7b6ee86e892f4`, `f7349155788c9ec0b7a0086af9b2a8f66c704cce`, `553c20c49f8c798cbd1074b03870fb63e642ee7a`; handoff `7f7fd4344e9e470f19c1d2e95d75ec06357fbc00`.
- Older #81–#84 were successful session `20260908-194552`; #86–#89 were failed sessions due unrecognized `custom:qwen-stint-xhigh/medium` names and missing `go`. The ledger captures checkpoints and handoff SHAs. Close those generated PRs unmerged after these docs land.
- #73 contains unique historical incident/findings docs absent from current main; preserve them before closing. #48–#51 remain parked runtime source experiments; #85 remains separate high-authority maintenance experiment; #93 remains docs rework against current paths.

## Next work

1. Merge and verify this canonical plan/ledger/handoff docs change, then close only superseded branches whose semantics/evidence are now recorded. Leave #110–#113 untouched.
2. Audit current NInfer-4090 and Qwen3.8 artifact history; select a production tuple before building/publishing a bundle.
3. Port best-effort startup phase timing and durable runtime-deployment provenance.
4. Implement and locally qualify a fail-closed immutable GitHub Release bundle on the exact Vast CUDA base. Keep source-build as deliberate recovery.
5. Only then consider a bounded RTX 4090 A/B/Deep Work run; do not raise spend or offer limits to obtain a host.
6. Reconcile `.spark/profile.yml`, rework #93 after code convergence, and run the final docs/CI/runtime/source audit.

**Next Stint task:** finish tuple-first runtime modernization and startup provenance with deterministic fixtures.
**Next Spark ↔ Stint task:** keep the Spark profile aligned with lifecycle, provider, dashboard, deep execution, runtime bootstrap and script paths; verify Spark’s path-aware observation on the finalized runtime design.
