# Deep Work stabilization evidence — 2026-09-24

This report records the repository and GitHub state used to scope the Deep Work stabilization changes. The inspection began from `main` at `88b69f0f7f1aea6f30b75d8fee4f52ab8544d163` (merge of #152); a fresh GitHub listing confirmed the PR state below. The latest relevant session was `20260924-114452`.

## What current evidence showed

The release-bundle run in PR #153 and the later run in PR #155 are different runs and should not be conflated.

PR #153 records RTX 4090 instance `52392010`, release `ninfer-runtime-81b68a20-sm89`, `$0.374/hour`, 11m54s to NInfer READY, 262144 context/KV, and a 200000-token perf request that reported 178,904 prompt tokens, 130.53s TTFT, 68 output tokens, 22.4/24 GB VRAM, and unavailable decode. Its regular route, shell, deny-policy, and two-lane checks passed. Session `20260924-101158` completed three compressions and verified the marker, but the harness exited nonzero: one HTTP 400 among 29 medium requests, and its Hermes tool journal did not record the artifact write/verify calls. The instance was destroyed. Codex left two P2 findings on that harness: honor `STINT_REQUIRE_COMPRESSION=0` for compression-order checking, and pass configured artifact path/text into the audit. The harness is not treated as release qualification.

PR #155 records the later RTX 4090 instance `52405187` at 6m41s to READY. Its xhigh planning task took 12m58s. The user-provided resumed-run evidence adds the decisive outcome: `ONBOX-001` did not start with 13m39s before the landing cutoff because the configured maximum was 15m; resume then used a 3-minute Hermes timeout. `DASH-001` and `DASH-REVIEW-001` timed out, yet passing generic verification marked both verified against unchanged code. No requested Go implementation survived. The handoff was published before resume; a later landing changed durable identity; final publication retried the same deterministic mismatch 12 times; the landing reason could remain stale. The final diff contained only the action plan and handoff. The 3-minute override was a bad pressure response, not a production timeout recommendation.

Other supplied forensic findings were preserved as design constraints: the dashboard's worker observation depended too much on local READY/tunnel state; raw Hermes activity remained at `/root/.hermes/logs/agent.log`; `/metrics` and `/slots` were not archived; session configuration had two NInfer clients while the engine exposed four slot rows; Deep Work tasks were serial; and benchmark decode may be unavailable by design under the PR #143 buffered-SSE protection. Live engine decode during Hermes execution was unresolved because runtime samples were missing. R2's sanitized heartbeat and final allow-list were already serving their intended role; R2 was not the run failure and was not redesigned.

## Relevant pull-request review and disposition

| PR | Current evidence at inspection | Disposition |
| --- | --- | --- |
| #153 | Smoke harness with unresolved Codex P2 findings; useful release-bundle facts are recorded above. | Close as stale/non-qualifying smoke evidence; retain its findings and run facts here. |
| #154 | Generated `docs/DEEP_WORK_ACTION_PLAN.md` for session `20260924-114452`. Codex P2 found contradictory requirements for how an empty per-task reasoning value should be displayed. | Close as a session checkpoint, not implementation. Preserve the plan's purpose and review finding here; implement configured-reasoning display in the stabilization code. |
| #155 | Handoff for session `20260924-114452`. Codex P2 correctly found that `stint deep resume` cannot resume a `hermes-onbox` session; it requires `stint deep onbox --resume` in the supervisor context. Its body also claimed #156/#157 were closed while both were still open. | Close as stale handoff evidence after preserving the later resume findings above; fix the on-box resume recommendation in generated handoffs. |
| #156 | `DASH-001` body claimed reasoning display, but its diff contained only a handoff file. Codex P1 found no projection, renderer, or tests. | Close as false-positive checkpoint; include the actual reasoning projection and regression coverage in implementation. |
| #157 | Empty diff; review checkpoint only. | Close as empty checkpoint. |
| #110–#113 | Older session `20260923-022052`: generated plan, two one-line phase-lane marker files, and a handoff. These are disposable run artifacts rather than maintained implementation. No Codex review comments were present. | Close as stale fixture evidence; the current smoke fixtures and their test results remain in the repository. |
| #85 | Separate, broad GitHub maintenance-mode feature stack; not part of the current Deep Work acceptance failure. | Retain open because it is an independent feature proposal, not a stale run checkpoint. |

The GitHub listing at inspection had no open PR newer than #157. The older #110–#113 stack was the only other open session-artifact chain reviewed here.

## Stabilization changes landed after the initial inspection

| PR | Merge commit | Landed behavior |
| --- | --- | --- |
| #158 | `af7438d3100af6482021aff77a0ac976b1b4b03c` | Executor success is required for task acceptance; executor/verifier evidence is separate; review dependencies are gated; task timeout is a maximum with a persisted effective budget; on-box resume reopens a new landing epoch and updates handoff guidance. |
| #159 | `478f96f428c736ffeed06139beaf63e5f5950a71` | Resumed final handoffs use versioned identities with preserved drift/history; deterministic publication conflicts stop retrying; repeat publication of the active versioned handoff is idempotent. Codex's P1 repeat-sync finding was fixed and covered by a third-sync regression test before merge. |

## Stabilization contract

The implementation that follows this report makes task acceptance require a successful executor, passing independent repository verification, a checkpoint commit, and durable persistence. Executor and verifier evidence remain separate. Review tasks wait for declared implementation prerequisites. The task timeout is a maximum; the coordinator shortens it only while preserving verification/checkpoint reserves and otherwise defers work for landing. On-box resume starts a new landing epoch while retaining the previous reason and identity as history. Resumed handoff publication gets a versioned branch and preserves old publication/drift history; deterministic policy or identity failures stop retrying.

The Worker view uses a bounded, redacted SSH tail of the existing Hermes log and stays separate from execution lifecycle. A GPU-side NInfer sampler retains only allow-listed `/metrics` counters and `/slots` fields in bounded `ninfer-runtime.jsonl`; configured clients and engine slot rows are reported as distinct counts. R2 heartbeats remain sanitized snapshots (`latest.json` and timestamped snapshots); final R2 evidence adds the bounded NInfer sample to its file allow-list but excludes Hermes logs, prompts, source, and credentials.

The timing values remain distinct: the rental cap supplies the compute deadline; `LandBefore` protects final verification/publication time; on-box production defaults to a 15-minute per-Hermes maximum and two attempts; NInfer's pending request timeout is 600 seconds; the observer cadence is 10 seconds; R2 heartbeat cadence is 20 seconds; transient publication retries are 12 attempts with a 5-second delay. None of these make a 3-minute coding timeout appropriate.
