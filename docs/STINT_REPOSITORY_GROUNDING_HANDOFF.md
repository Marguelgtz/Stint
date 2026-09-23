# Stint repository grounding handoff

**Status:** source and runtime convergence landed; the first pinned immutable NInfer runtime release is published. Fresh RTX 4090 acceptance and final PR/documentation closeout remain. Release-bundle startup is still opt-in.

## Repository and landed work

- Starting main: `9634bf762a2dc9021747eb786db7fd23ccab84e9`.
- Current main before final docs/cleanup: `23908fc719576376b32ce46fb0a34745c27cc9fb`.
- Grounding/runtime merges #116–#130 preserve lifecycle/provider safety, both dashboards, Hermes-on-box Deep Work, Spark path evidence, startup phase events, and the tuple-pinned runtime bundle build/deployment path. Exact commit and CI records are in [`STINT_REPOSITORY_GROUNDING_PLAN.md`](STINT_REPOSITORY_GROUNDING_PLAN.md).
- The original dirty checkout at `792bb508dfcd7d64e293359dfbed7b497b10dafa` and its untracked user artifacts remain untouched.
- The current open PR graph has 12 PRs immediately before #125's final docs update: #48–#51, #73, #85, #93, #110–#113, and #125. Keep #110–#113 open, unmerged and untouched. Close #48–#51 and #73 only after the canonical docs record their replacements and preserve #73's reports.

## Current architecture and NInfer tuple

- Current Deep Work is Hermes-on-box with detached supervision, durable coordination, independent verification, bound compute, resumable landing and provider teardown. Do not import old Deep Work branches wholesale.
- Ordinary and Deep Dashboards, truthful NInfer lane semantics, durable lifecycle state, provider-safe cleanup and Spark path observation are on `main`.
- Selected first SM89 release tuple: NInfer `81b68a20a9a0d9ab47d7e5838887c6d636ab76e0`; Qwen revision `18dfc887423fa5aabf3cb56fac41490e462b3fab`; artifact SHA-256 `eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e`; CUDA 12.8, RTX 4090/SM89, native 262144 context, E8 4-bit KV and MTP3.
- Qwen v3 remains incompatible with the selected SM89/CUDA tuple; DFlash2 does not fit the required native context at K=3 per upstream notes. A newer upstream 4090 vision-cap fix remains a follow-up candidate. These upstream notes are not Stint performance results.
- `source-build` remains the default and recovery path. `release-bundle` is explicit and fail-closed. No fresh RTX 4090 model-load, two-lane, native-context, correctness, Deep Work, teardown or comparable READY-time acceptance has been run; do not promote the release path or claim performance benefit.

## Verified candidate and immutable release

- Exact-main candidate workflow run `35894635094` succeeded on source SHA `d3706c943540397e0e5be71a01f64b49768f1953`. All 332 build targets passed; clean pinned-base extraction, CLI help and dependency (`ldd`) smoke passed; all three candidate assets uploaded.
- The archive is 939,381,613 bytes with SHA-256 `6725e60c8e3edb2982ad828898210868dd98ea1d4fe4d35f97bfa1e625414416`. PR #128 pinned this exact tested archive; PR CI `35898993338` and landed-main CI `35899139527` passed all five jobs.
- PR #129 fixed workflow-token access by staging verified output as a provenance-bound draft and reading public run metadata; PR CI `35900956163`, landed-main CI `35901071060` passed. PR #130 fixed draft lookup/download through the authenticated release list and asset IDs; PR CI `35901794930`, landed-main CI `35901919089` passed.
- Candidate `35894635094` predates #129's draft-staging step, so its original successful Actions artifact was downloaded, locally reverified and staged as draft release `394934756` with matching run/source/digest provenance; no rebuild was substituted. Publisher run `35902030339` revalidated source run, Stint lineage, draft provenance, archive SHA, sidecar and manifest before publishing. GitHub reports release [`ninfer-runtime-81b68a20-sm89`](https://github.com/Marguelgtz/Stint/releases/tag/ninfer-runtime-81b68a20-sm89) as `immutable: true`; the published archive digest matches the `main` pin.
- No paid GPU was used for this build or publication. The earlier failed build digests `16a1d238…` and `ce0fa3ae…` were not published; they are retained in the plan as disproved provisional pins.
- Read-only market inspection at `2026-09-23 18:37 UTC` returned 39 offers, including 24 RTX 4090s; none met the current interactive profile, all 24 failing the $0.40/hour ceiling and nine also failing reliability. The top interactive plan selected an RTX 3090, not suitable for this SM89 acceptance. No instance was rented.

## Remaining work

1. Refresh and merge PR #125 with the candidate, pin, immutable release and publisher-fix evidence. Preserve the existing #73 CP1 reports byte-for-byte under `docs/history/`.
2. After that docs merge, close #48–#51 as replaced by #121–#130 and close #73 after its reports land. Keep #85 parked, #93 for separate documentation rework, and #110–#113 untouched. Refresh the ledger's final open-PR snapshot after the closures.
3. Recheck RTX 4090 availability later under unchanged limits: $0.40/hour, $2.50 total session, at most three candidates, 500 Mbps advertised and 40 MB/s measured. Do not raise caps. Current offer pricing/reliability blocks fresh model-load, two-lane, native-context, correctness, Deep Work and teardown acceptance; preserve opt-in behavior.
4. Update this handoff to the final `main` SHA after docs/cleanup and report exact remaining blockers.

**Next Stint task:** qualify the pinned immutable runtime on a fresh RTX 4090, including two lanes, native context, correctness, Deep Work and teardown; evaluate the newer upstream vision fix separately.

**Next Spark ↔ Stint task:** check Spark's path-aware profile against the final runtime/bootstrap design after the live acceptance decision.
