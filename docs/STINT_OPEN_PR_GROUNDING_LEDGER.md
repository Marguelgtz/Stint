# Stint open PR semantic grounding ledger

**Historical inventory:** the complete detailed records below were captured on 2026-09-23 after #118 merged (38 open PRs). Their paths, symbols, and recorded check rollups describe that point-in-time snapshot. Base SHAs are observed PR base refs, not claims that stale branches include current `main`.

**Final open snapshot after #134:** checked on 2026-09-23 after #134 merged. GitHub reported five open PRs: #85 and protected Deep Work evidence #110–#113. Their exact heads, observed base SHAs, current mergeability and legacy check runs are recorded in the final snapshot at the end of this ledger. The protected generated PRs #110–#113 remain untouched.

**Purpose:** account for each open PR by semantics. Green checks alone do not make a stale/stacked PR safe to merge. Generated GPU-run artifacts remain outside product `main`.

**CI checkout clarification:** before #132, pull-request jobs used the default checkout, which selected GitHub's synthetic `refs/pull/N/merge` tree even when the resulting check was attached to the PR head SHA. The #131 log confirmed this with synthetic commit `45644d2`. Thus legacy PR rollups below report their recorded check results, not exact-head proof. PR #132 added explicit head checkout plus a separate merge-tree pass. Existing protected/parked PR heads have not been changed or rerun under that workflow; their exact-head CI state is **NOT ESTABLISHED**.

## Landing evidence during this grounding run

- #116 safety port: head `d3ead04a8d23502b344e0e4c73ba0668d7a25028`; merge `ca2f24b1a3058e57d7afee3cdebc2c3132033e9c`; exact landed-SHA push run `35851717414`, all five required jobs passed.
- #117 ordinary dashboard/telemetry port: head `5ebeee65a2049604c56e5beadfab999450e6e707`; merge `9a672345c3a1887c7aac5e06a971320cd454495c`; PR run `35855252071` passed its attached checks on the legacy synthetic merge tree; landed-SHA push run `35855342269` passed all five required jobs.
- #118 Deep Dashboard phase evidence: head `6deaf44c58cfaa82c9c19e1a2e1cf02727f291e6`; merge `6f4c81f76118924dfb7b41fa6a85394b2796b399`; PR run `35856677088` passed its attached checks on the legacy synthetic merge tree; landed-SHA push run `35856861212` passed all five required jobs.
- #119 initial grounding docs: head `ff5995f06330a591919040752a8a60108c0d691a`; merge `2f0da9a80d9f01fafc3f1d27af6bcc0fafafdf6e`; exact landed-SHA CI run `35859155951` passed.
- #120 Spark profile: head `715e0d600a46e7b947a40aeba6d7d183216b9570`; merge `8bf249d69db4126e49b0e8723ee8d2ed346cdbf5`; exact landed-SHA CI run `35862150760` passed.
- #121 startup phase events: head `1b9ac49568293d254b5a74af83843637347f1a4a`; merge `063067efac7fb996bb57a36abb57655dbf9ff71c`; PR run and exact landed-SHA push run `35867023267` passed.
- #122 immutable bundle workflow: head `6064391838bfeaca7ce98071009451925fc330bc`; merge `cc2f259a0eb3599ec0dabdd9cafc0a5d7246e1e7`; exact landed-SHA push run `35868625724` passed.
- #123 runner/build fixes: head `b663818948c4d9188fe91341b061f5b0ae0b6b45`; merge `b3c029553358950e2fc9369be370a470d869763c`; exact landed-SHA push run `35871556847` passed.
- #124 deployment/provenance: head `04f0f6848b15c4b0b4fdce70561ff968c4037175`; merge `706c78ae60f996c957d5f9ae86dc10bb529b604e`; legacy PR merge-tree run `35882783534` and exact landed-SHA push run `35882955266` passed all five required jobs.
- #126 initial archive pin: head `ddd5c0d4c9c73ad5b787f6e72b0a6d8f57c653a9`; merge `494c87a8d7e329c2e9c5e36983e74b2d01beb6c5`; PR run `35887526088` and landed-SHA run `35887717054` passed all five jobs. Its `16a1d238…` digest later failed to reproduce on retry and was replaced by #128's clean-tested `6725e60c…`.
- #127 candidate/promotion split: head `56cede61cf62917e7546657adfd2f2afa83faff0`; merge `d3706c943540397e0e5be71a01f64b49768f1953`; PR run `35894208985` passed all attached checks on the legacy synthetic merge tree; landed-SHA run `35894364016` passed all five required jobs. Candidate build run `35894635094` succeeded with archive SHA `6725e60c8e3edb2982ad828898210868dd98ea1d4fe4d35f97bfa1e625414416` after clean-base smoke.
- #128 exact candidate pin: head `0cc26d866102fe9df4c54fbed4b74e74182c7b9e`; merge `24a05e663489cd4063fcaa505fc26e3e3a9f732e`; PR run `35898993338` and landed-SHA run `35899139527` passed all five jobs.
- #129 draft handoff / source-run API fix: head `faa0d15776cbcadaafddf633a9d654a3cff2a17a`; merge `58afa4af857cf12e5135d4e78c46b79c96313922`; PR run `35900956163` and landed-SHA run `35901071060` passed all five jobs. Initial publisher run `35899265948` failed before side effects because the workflow token could not read Actions run metadata.
- #130 authenticated draft lookup and asset download: head `42b0440a5b82f2c80ffbaf3311d478125dcf68ed`; merge `23908fc719576376b32ce46fb0a34745c27cc9fb`; PR run `35901794930` and landed-SHA run `35901919089` passed all five jobs. Since candidate run `35894635094` predates #129's automatic draft staging, its original verified Actions artifact was locally reverified and used to create draft release `394934756` with the candidate run/SHA/archive hash in the notes. Publisher run `35902030339` validated that draft and published immutable release `ninfer-runtime-81b68a20-sm89` with the pinned `6725e60c…` archive.
- #125 grounding docs: head `b94cca7feb3933100105d4e11c1d26ba6ca0a7b7`; merge `36d12ac1c3df55cb0a4f1d7ebe5ee557b26316fa`; legacy PR run `35904065343` and landed-main run `35904245746` passed all five required jobs. It corrected stale README/operator guidance, recorded the pinned immutable release and market blocker, and preserved #73's two reports byte-for-byte.
- #131 closeout docs: head `09ba98e0f6b6c59c6a4001e8e44e43b6511c5ba3`; merge `2b5f7342093c520175b608a1a64bbce0b9443f31`; run `35905726077` reported all five jobs successful on the PR head SHA and main run `35905880710` passed all five jobs. Post-merge inspection found the pull-request workflow had physically checked out GitHub's synthetic merge commit `45644d2` (head `09ba98e` into base `36d12ac`), despite the run being attached to the PR head SHA. This exposed that prior PR CI did not structurally prove exact-head checkout.
- #132 CI repair: head `a90b059c81d7a7bfb1fdbc17aff053e40c217429`, base `main` at `2b5f7342093c520175b608a1a64bbce0b9443f31`, merged as `4ff7159c018c75372b31aca7627151e30fed669b`. PR run `35908857053` checked out the exact PR head and passed all five required jobs; `unit-tests` then explicitly checked out synthetic merge tree `1a1b01ba429d8953df2f8519ad31b2ea7bb416cf` and reran Go, Python, and shell suites successfully. Exact landed-main push run `35909255732` passed all five required jobs on `4ff7159…`. Spark Observability was neutral in both pull-request runs.
- #133 documentation taxonomy/current-grounding port: head `58e3a5832dbffdcf683665598e8c0d5335c34760`, base `main` at `4ff7159c018c75372b31aca7627151e30fed669b`, merged as `3586022bd6b2cc4565a4d94e519398222d85a443`. PR run `35911740797` checked the exact head and passed all five required jobs; its unit job separately tested synthetic merge tree `2f349cf2c57e4cdc5e9809019de963558d0b3069` successfully. Exact landed-main run `35911926414` passed all five required jobs.
- #134 final ledger refresh: head `f14c42b846aacc6a66fc1a25f2b1104ad5a6473f`, merged as `25eea6fcc4fdbc9d24588599f849a5afe5b4e613`. PR run `35912743074` passed all five required jobs on the exact head and its unit job passed the synthetic merge-tree test; landed-main push run `35912925386` passed all five required jobs.
- #117 active-lane semantics: `/metrics` `requests_processing`, with `/slots` processing-lane fallback; retained/resident prompt tokens contribute to resident depth, not active count. NInfer cache ratio is prefix-cache hits / (hits + uncached prompt tokens), and NInfer prefill is marked uncached; llama.cpp cache ratio uses cached/total prompt tokens.
- #117 event history intentionally carries no caller/client identity; `session_digest` is not a stable Stint identity.

## Historical NInfer packaging and promotion evidence

- [PR #32](https://github.com/Marguelgtz/Stint/pull/32) merged as `20382ea96338fdb82a1bf6460fb9478743a36084`. It built `ghcr.io/marguelgtz/stint-ninfer:981b685e-cuda12.8` from NInfer `981b685ea2124fdaed023123d2e63fd29d529ab8`, on top of the standard Vast CUDA 12.8.1 cuDNN devel base. The image validated `ninfer-serve --help` and its dynamic libraries and bridged the prebuilt binary into the then-current Stint runtime path. It proved the build-time image experiment, not current image loading, SSH observability, or rental-to-READY behavior.
- Current `main` does not select the GHCR image. The path was later excluded because provider image loading/startup had not been revalidated against current lifecycle behavior and placed more bootstrap behavior inside Vast's image path. The release-bundle route retains the standard base and SSH-visible lifecycle, but it has not had a comparable live `READY`-time A/B against source-build or GHCR. Candidate build targets, archive integrity, and clean-base extraction smoke are not startup-time evidence.
- Current runtime stays pinned to NInfer `81b68a20…`, CUDA 12.8 / SM89 and Qwen v2 revision `18dfc887…`. Upstream `rtx4090-port` is at `aeeba414…`; code commit `328d9aa…` fixes the vision per-item cap. Current Qwen `main` is a v3 `sm_120a` artifact and is not an SM89 candidate. DFlash2's upstream PR #7 reports RTX 4090D 48 GB results at K=3, but notes roughly 25 GiB process use near 258K and inability to fit a second full 262K lane on 24 GB. These are upstream/community findings, not Stint measurements; keep MTP3 selected until Stint 24 GB dual-lane correctness and stability are measured.

## Disposition summary

| PR | Current disposition | Current semantic replacement / action |
|---:|---|---|
| #11 | PARTIALLY ABSORBED | Current-main implementations above; bounded context semantics are in current main: commits `76e2c8f` and `e1bf4e6` added configurable/shared interactive limits, while `b4f9617` added llama.cpp context validation. Retire only the Cline coupling. |
| #36 | OBSOLETE | Hermes-on-box architecture in #107 (merge 2041498); no helper replacement is needed. |
| #48 | CLOSED | Closed 2026-09-23 after #125 merged; [closure comment](https://github.com/Marguelgtz/Stint/pull/48#issuecomment-5801200925). #122–#124 and #127–#130 replace the prototype with selected-tuple build, smoke, exact pin, immutable release and fail-closed opt-in deployment; candidate `35894635094` and archive SHA `6725e60c…` are verified. |
| #49 | SUPERSEDED | Closed 2026-09-23 after #125 merged; [closure comment](https://github.com/Marguelgtz/Stint/pull/49#issuecomment-5801201032). #121 records durable best-effort phase events (CI `35867023267`); comparable source-build/release READY timing remains future work. |
| #50 | CLOSED | Closed 2026-09-23 after #125 merged; [closure comment](https://github.com/Marguelgtz/Stint/pull/50#issuecomment-5801201067). #122/#123/#127–#130 provide tuple-pinned candidate build, smoke, verified assets and immutable publication; #124 supplies opt-in deployment. |
| #51 | CLOSED | Closed 2026-09-23 after #125 merged; [closure comment](https://github.com/Marguelgtz/Stint/pull/51#issuecomment-5801200967). #124 and #127–#130 replace it with fail-closed opt-in deployment and verified exact-pin publication; live GPU qualification remains the promotion gate. |
| #56 | SUPERSEDED | #116 merge ca2f24b for lifecycle safety; #117 merge 9a67234 for dashboard/telemetry; final phase view is unrelated #118 merge 6f4c81f. |
| #57 | SUPERSEDED | #107 merge 2041498 (head 7adf523) is authoritative Hermes-on-box Deep Work; #118 merge 6f4c81f grounds Deep Dashboard evidence. |
| #58 | SUPERSEDED | #117 merge 9a67234 (dashboard semantics and tests). |
| #59 | SUPERSEDED | #107 merge 2041498; current live evidence #110–#113. |
| #60 | SUPERSEDED | #117 merge 9a67234; current NInfer README at https://github.com/sergiuszm/ninfer-4090 is supporting upstream evidence. |
| #63 | SUPERSEDED | #117 merge 9a67234. |
| #64 | SUPERSEDED | #117 merge 9a67234. |
| #67 | SUPERSEDED | #116 merge ca2f24b; exact push CI run 35851717414. |
| #68 | SUPERSEDED | #117 merge 9a67234. |
| #69 | PARTIALLY ABSORBED | #116 merge ca2f24b plus #117 merge 9a67234. |
| #70 | PARTIALLY ABSORBED | #117 merge 9a67234 for event log and truthful no-identity rendering; client-tagging is deliberately retired. |
| #73 | CLOSED | Closed 2026-09-23 after #125 merged; [closure comment](https://github.com/Marguelgtz/Stint/pull/73#issuecomment-5801200927). Both unique reports are byte-identical copies in `docs/history/` with source PR/commit provenance and recorded SHA-256s. |
| #74 | SUPERSEDED | Current-main commit 1945a5033a0db98a85aa66f75819716e7995488e (verified pin and transfer-size discovery). |
| #76 | SUPERSEDED | #116 merge ca2f24b; exact push CI run 35851717414. |
| #77 | SUPERSEDED | #107 merge 2041498 (head 7adf523). |
| #78 | SUPERSEDED | #107 merge 2041498 and current runtime configuration. |
| #79 | SUPERSEDED | #107 merge 2041498 for current architecture plus #118 merge 6f4c81f; #118 local tests and exact CI 35856677088 passed. |
| #80 | SUPERSEDED | #107 merge 2041498 (head 7adf523) and live evidence session 20260923-022052. |
| #81 | CLOSED, EVIDENCE ONLY | Not code-replaced; preserved as historical evidence, with newer successful session #110–#113 still open. |
| #82 | CLOSED, EVIDENCE ONLY | Historical evidence only; current #110–#113 is the preferred live evidence. |
| #83 | CLOSED, EVIDENCE ONLY | Historical evidence only; current #110–#113 is preferred. |
| #84 | CLOSED, EVIDENCE ONLY | Historical evidence only; newer session #110–#113 stays open. |
| #85 | EXPERIMENT / PARKED | No replacement; keep separate until pagination, base authority, policy/environment agreement and complete merge evidence are independently redesigned. |
| #86 | CLOSED, EVIDENCE ONLY | Not code-replaced; failure is durably classified here. New successful session is #110–#113. |
| #87 | CLOSED, EVIDENCE ONLY | Not code-replaced; failure/handoff remains inspectable in PR history and ledger. |
| #88 | CLOSED, EVIDENCE ONLY | Not code-replaced; failure is recorded here; current preferred evidence is #110–#113. |
| #89 | CLOSED, EVIDENCE ONLY | Not code-replaced; preserve PR history and this ledger. |
| #93 | CLOSED, PORT SELECTED TAXONOMY | Closed 2026-09-23 after #133 merged; [replacement/closure comment](https://github.com/Marguelgtz/Stint/pull/93#issuecomment-5801853350). #133 adds the docs index and current operations/roadmap/architecture pages; old Cline/pre-V0 content is retained and labeled historical. |
| #110 | KEEP OPEN AS CURRENT EVIDENCE | No replacement; this is the latest successful evidence. |
| #111 | KEEP OPEN AS CURRENT EVIDENCE | No replacement; part of latest live evidence chain. |
| #112 | KEEP OPEN AS CURRENT EVIDENCE | No replacement; part of latest live evidence chain. |
| #113 | KEEP OPEN AS CURRENT EVIDENCE | No replacement; this is the latest live evidence chain. |
| #131 | LANDED | Closeout documentation; PR run `35905726077` exposed a synthetic-merge checkout gap later fixed by #132; exact main CI `35905880710`. |
| #132 | LANDED | `.github/workflows/ci.yml` now checks exact PR head and reruns integration suite on merge tree; PR run `35908857053`, merge `4ff7159…`, main run `35909255732`. |
| #133 | LANDED | Selected documentation taxonomy/current-grounding port; PR run `35911740797` verified exact head and synthetic merge tree; main run `35911926414`. |

## Historical open PR snapshot (after #125 merged and #48–#51/#73 closed)

This snapshot was checked after #125 merged and #48–#51/#73 closed. It records
the six PRs open at that time, before #132 and before the closures of #81–#84
and #86–#89. Base SHAs are the exact observed base refs; the generated evidence
line #110–#113 remains protected and unmerged. The final post-#93 snapshot is
recorded separately at the end of this ledger.

| PR | State | Head branch @ exact SHA | Base branch @ observed SHA | Legacy PR checks / exact-head proof | Current disposition |
|---:|---|---|---|---|---|
| #85 | OPEN, draft | `feat/deep-work-github-modes` @ `dd034898cc1c89138bfc18ddc47e7c3dfc6b34e5` | `fix/deep-work-onbox-publishing` @ `62f89ddcd0b98962f6b5f7c0d1360ac43e5ff3a6` | five required checks green on legacy synthetic merge tree; exact-head not proven; Spark neutral | Keep parked; separately redesign authority/pagination/policy gates |
| #93 | OPEN | `docs/reorganize-documentation` @ `2fd8988ab195a05f20330b2f0d5475f4a1457f97` | `main` @ `74ef5db14e0fe4b0e8e9865baeb5a9321c5eb5fb` | spark-profile/go-vet/unit-tests green on legacy synthetic merge tree; race/build absent; exact-head not proven; base is stale | Keep open for rework against current docs paths |
| #110 | OPEN, draft | `stint/deep-20260923-022052-01-stint-plan-001` @ `a051c94a0f02b5d65a4e5a4fb2b7b6ee86e892f4` | `main` @ `bdd55c57b7fbdb7a2128c8ed14861cf3f527c04d` | five required checks green on legacy synthetic merge tree; exact-head not proven; Spark neutral | Keep open and unmerged |
| #111 | OPEN, draft | `stint/deep-20260923-022052-02-phase-001` @ `f7349155788c9ec0b7a0086af9b2a8f66c704cce` | #110 branch @ `a051c94a0f02b5d65a4e5a4fb2b7b6ee86e892f4` | five required checks green on legacy synthetic merge tree; exact-head not proven; Spark neutral | Keep open and unmerged |
| #112 | OPEN, draft | `stint/deep-20260923-022052-03-phase-002` @ `553c20c49f8c798cbd1074b03870fb63e642ee7a` | #111 branch @ `f7349155788c9ec0b7a0086af9b2a8f66c704cce` | five required checks green on legacy synthetic merge tree; exact-head not proven; Spark neutral | Keep open and unmerged |
| #113 | OPEN, draft | `stint/deep-20260923-022052-handoff` @ `7f7fd4344e9e470f19c1d2e95d75ec06357fbc00` | #112 branch @ `553c20c49f8c798cbd1074b03870fb63e642ee7a` | five required checks green on legacy synthetic merge tree; exact-head not proven; Spark neutral | Keep open and unmerged |

### #124 — feat: add opt-in immutable NInfer runtime deployment

- **PR / head:** [#124](https://github.com/Marguelgtz/Stint/pull/124), `feat/ninfer-release-deployment-20260923` at `04f0f6848b15c4b0b4fdce70561ff968c4037175`; merged as `706c78ae60f996c957d5f9ae86dc10bb529b604e`.
- **Base:** `main` at `b3c029553358950e2fc9369be370a470d869763c`.
- **State / mergeability:** MERGED; ready for review at merge.
- **Legacy PR check rollup:** run `35882783534`; `spark-profile`, `go-vet`, `unit-tests`, `race-tests`, and `build-check` all SUCCESS; Spark Observability NEUTRAL.
- **Exact landed-main CI:** push run `35882955266`; all five required jobs passed on merge SHA `706c78ae60f996c957d5f9ae86dc10bb529b604e`.
- **Original intent:** Restore a modern immutable NInfer release deployment path, startup provenance, and a deliberate source-build recovery mode without changing default paid startup.
- **Changed paths (15):** `README.md`, `cmd/stint/help.go`, `cmd/stint/resumable_start.go`, `cmd/stint/resumable_start_test.go`, `cmd/stint/resume.go`, `cmd/stint/runtime.go`, `cmd/stint/runtime_test.go`, `cmd/stint/session_snapshot.go`, `cmd/stint/session_snapshot_test.go`, `cmd/stint/status_telemetry.go`, `docs/INSTRUCTIONS.md`, `internal/session/startup_events.go`, `internal/session/startup_events_test.go`, `internal/session/state.go`, `scripts/build_ninfer_runtime_bundle.sh`.
- **Important symbols:** `normalizeNInferDeployment`, `allowNInferLlamaFallback`, `bootstrapNInfer`, `--ninfer-deployment`, `applyResumeNInferDeploymentOverride`, `StartupEvent`, saved deployment provenance and startup phase events.
- **Tests:** Runtime/deployment selection, release-bundle failure/recovery and provenance cases; startup event persistence; resumable start and snapshot/status telemetry cases. Local full Go tests/race/vet/build passed; Python bundle tests and clean-base extraction passed.
- **Unique semantics introduced:** `source-build` stays default; `release-bundle` is explicit and fail-closed, cannot fall back to llama on acquisition/validation error, requires a qualifying RTX 4090 candidate, stores runtime provenance, records startup boundaries after durable lifecycle writes, overlaps model prefetch with preparation, and permits explicit source-build resume recovery.
- **Current-main status:** Landed. Immutable release `ninfer-runtime-81b68a20-sm89` is published and matches the main SHA pin. Fresh RTX 4090 model-load, two-lane, native-context, correctness, Deep Work and teardown acceptance remains incomplete; keep opt-in mode unpromoted.
- **Replacement / where it lives:** Replaces #49 timing prototype with #121 and #48/#50/#51 stale bundle stack with #121–#130. The initial local pin `f58ee66d…` and first main-build pin `16a1d238…` were disproved by later exact builds. Candidate `35894635094` passed clean-base smoke; #128 pinned SHA `6725e60c…`; #129–#130 fixed draft handoff/promotion; publisher run `35902030339` verified the published immutable release.
- **Dependencies:** Runs `35883203990` and `35887934779` completed 332/332 source steps and help checks but failed before clean-base smoke/upload because archive digests differed. Candidate `35894635094` on `d3706c9` passed build, clean-base extraction/CLI/`ldd` smoke and upload. Pin merge `24a05e6` and publisher/workflow merges `58afa4a` and `23908fc` passed their legacy PR merge-tree runs and landed-main CI; publisher run `35902030339` passed and reports `immutable: true`.
- **Conflicts with current architecture:** None identified; source build remains the recovery route and release deployment remains opt-in pending live acceptance.
- **Associated live evidence:** No new paid GPU acceptance was run for #124. Latest successful Deep Work evidence remains #110–#113, session `20260923-022052`.
- **Disposition:** **LANDED — retain the live acceptance gate; do not claim release-bundle production promotion yet.**

### #125 — docs: finalize Stint grounding ledger and history

- **PR / exact head:** [#125](https://github.com/Marguelgtz/Stint/pull/125), `docs/stint-grounding-final-20260923` at merged head `b94cca7feb3933100105d4e11c1d26ba6ca0a7b7`.
- **Base:** `main` at observed base SHA `23908fc719576376b32ce46fb0a34745c27cc9fb` before merge.
- **State / mergeability:** MERGED as `36d12ac1c3df55cb0a4f1d7ebe5ee557b26316fa` on 2026-09-23.
- **Legacy PR check rollup:** PR run `35904065343` and landed-main run `35904245746` passed all five required jobs.
- **Original intent:** Finalize one current grounding plan, one semantic PR ledger and one concise handoff, preserving #73’s unique CP1 evidence before its PR closes.
- **Changed paths (8):** `README.md`, `docs/INSTRUCTIONS.md`, `docs/STINT_REPOSITORY_GROUNDING_PLAN.md`, `docs/STINT_OPEN_PR_GROUNDING_LEDGER.md`, `docs/STINT_REPOSITORY_GROUNDING_HANDOFF.md`, `docs/history/CP1_DRYRUN1_INCIDENT.md`, `docs/history/CP1_DRYRUN_FINDINGS.md`, `docs/history/README.md`.
- **Important symbols:** Documentation only; canonical execution state, semantic dispositions, release and live-acceptance gate, history provenance.
- **Tests:** `git diff --check` passed; copied #73 reports compared byte-for-byte to source blobs and SHA-256s are recorded. Go/Python/runtime results are referenced from the landed runtime work.
- **Unique semantics introduced:** Records #116–#130 current-main convergence, runtime tuple rationale, candidate hash and immutable release evidence, publisher API fixes, unqualified opt-in release policy, current open-PR state, and #73 incident/findings history. Corrects stale repository overview/operator instructions that said Deep Work and lifecycle safety were still only in PRs.
- **Current-main status:** Landed. A later closeout refresh records the post-closure open-PR snapshot and dispositions; neither documentation change affects runtime code or enables release-bundle default.
- **Replacement / where it lives:** Replaces the initial #119 grounding snapshot with a post-#130 account; #73 reports are preserved under `docs/history/`.
- **Dependencies:** Candidate `35894635094` on `d3706c9` succeeded with SHA `6725e60c…`; #128 pin, #129 staged draft/API repair, and #130 draft-asset lookup fix are merged. Publisher `35902030339` verified and published immutable release `ninfer-runtime-81b68a20-sm89`. Keep live RTX 4090 acceptance explicitly incomplete.
- **Conflicts with current architecture:** None identified. This PR explicitly leaves #110–#113 open/unmerged and keeps #85/#93 for experiment/docs rework.
- **Associated live evidence / incidents:** PRs #110–#113 remain protected; #73 report provenance is source PR commit `e78ceef308d85c9cac7c71e7d172bed7c66c4182`.
- **Disposition:** **LANDED; #48–#51 and #73 were closed after replacement evidence landed. #110–#113 remain protected and open.**

### #126 — fix: pin exact NInfer runtime bundle hash

- **PR / exact head:** [#126](https://github.com/Marguelgtz/Stint/pull/126), `fix/ninfer-bundle-hash-pin-20260923` at `ddd5c0d4c9c73ad5b787f6e72b0a6d8f57c653a9`.
- **Base:** `main` at observed base SHA `706c78ae60f996c957d5f9ae86dc10bb529b604e`.
- **State / mergeability:** MERGED; ready for review at merge.
- **Legacy PR check rollup:** PR run `35887526088`; all five required jobs passed; Spark Observability was neutral.
- **Exact landed-main CI:** push run `35887717054`; all five required jobs passed on merge SHA `494c87a8d7e329c2e9c5e36983e74b2d01beb6c5`.
- **Original intent:** Correct Stint's pinned archive SHA after exact-main CI produced a different clean-build archive than the earlier local candidate.
- **Changed paths (3):** `cmd/stint/runtime.go`, `cmd/stint/runtime_test.go`, `internal/session/startup_events_test.go`.
- **Important symbols:** `ninferRuntimeBundleSHA256` and its pin assertions/serialization fixture.
- **Tests:** Full `go test -count=1 ./...`, race tests, `go vet ./...`, binary build and `git diff --check` passed locally.
- **Unique semantics introduced:** Changed the Stint archive pin from local candidate `f58ee66d05e5d1932b030a05cfd9a7e1e8570579a47b78476cf845c3abcde6e0` to the first exact-main candidate SHA `16a1d238defe0d14eac6a354a541023074da741d9ee1aee0c736bb3ed74ed1c8`. Retry `35887934779` emitted a different digest, `ce0fa3aea7b2e7f6b575e3176dfa8fe0d206987bdd5ccb9446adbff9423ec2a5`; neither archive passed clean-base smoke or was uploaded.
- **Current-main status:** Its `16a1d238…` pin was provisional and later replaced by #128 with SHA `6725e60c…`, the archive from the successful uploaded, clean-base-smoke-tested candidate.
- **Replacement / where it lives:** #127 moved hard-pin enforcement to the publisher; #128 binds `ninferRuntimeBundleSHA256` to candidate `35894635094`. Publisher `35902030339` verified the actual release asset against that current-main constant.
- **Dependencies:** Candidate run `35894635094` on `d3706c9` passed clean-base smoke/upload. #128, #129 and #130 merged with green legacy PR merge-tree and landed-main CI; immutable release publication passed.
- **Conflicts with current architecture:** None identified; source-build remains default and bundle deployment remains opt-in.
- **Associated evidence:** Run `35883203990` compiled 332/332 steps and passed binary help checks, then failed its hash gate before clean-base extraction/upload; no release was created.
- **Disposition:** **LANDED as `494c87a8`; its digest was disproved by run `35887934779` and superseded by #128's verified `6725e60c…` pin.**

### #127 — fix: separate NInfer candidate build and promotion

- **PR / exact head:** [#127](https://github.com/Marguelgtz/Stint/pull/127), `fix/ninfer-candidate-build-flow-20260923` at `56cede61cf62917e7546657adfd2f2afa83faff0`.
- **Base:** `main` at `494c87a8d7e329c2e9c5e36983e74b2d01beb6c5`.
- **State / mergeability:** MERGED as `d3706c943540397e0e5be71a01f64b49768f1953`.
- **Legacy PR check rollup:** PR run `35894208985`; build, unit, race, vet and Spark profile checks all passed; Spark Observability was skipped/neutral.
- **Exact landed-main CI:** push run `35894364016`; all five required jobs passed on the merge SHA.
- **Original intent:** Let a candidate finish clean-base smoke and upload while keeping release publication bound to a hard-coded Stint archive SHA.
- **Changed paths (6):** `.github/workflows/ninfer-runtime-bundle.yml`, `.github/workflows/publish-ninfer-runtime-release.yml`, `README.md`, `scripts/build_ninfer_runtime_bundle.sh`, `scripts/ninfer_runtime_bundle.py`, `scripts/test_ninfer_runtime_bundle.py`.
- **Important symbols:** `verify(..., expected_archive_sha256=...)`; optional CLI `--expected-sha256`; candidate archive summary and publisher ancestry/pin gate.
- **Tests:** Five bundle tests; Python byte-compilation; `bash -n`; PyYAML and all embedded workflow `bash -n` blocks; `git diff --check`; legacy PR merge-tree and exact-landed CI all green.
- **Unique semantics introduced:** Candidate generation and release promotion are separate. The candidate workflow completes archive safety, extraction, CLI and dependency smoke, then uploads the exact output. The publisher requires a successful `main` candidate run that is an ancestor of current main, allows only runtime pin/test-fixture commits after it, verifies the actual archive against Stint's merged SHA, and only then creates the immutable release. Runtime bundle remains opt-in.
- **Current-main status:** Landed. Candidate run `35894635094` succeeded on merge SHA `d3706c9`, passed clean-base smoke, and its artifact was pinned by #128 and published immutably after #129–#130.
- **Replacement / where it lives:** Replaces #126's assumption that a clean NInfer rebuild will reproduce a previously observed archive digest. Publisher validates the exact release asset against the current-main SHA pin and its originating build-run metadata.
- **Dependencies:** Resolved by candidate run `35894635094`, pin PR #128, workflow PRs #129–#130, and successful publisher run `35902030339`.
- **Conflicts with current architecture:** None identified; `source-build` remains default and release deployment stays explicit/fail-closed.
- **Associated live evidence:** No paid RTX 4090 run; #110–#113 remain the latest successful Deep Work evidence and stay open/unmerged.
- **Disposition:** **LANDED — candidate build and immutable publication verified; keep release-bundle startup opt-in until fresh RTX 4090 acceptance passes.**

### #128 — fix: pin clean-tested NInfer runtime bundle

- **PR / exact head:** [#128](https://github.com/Marguelgtz/Stint/pull/128), `fix/ninfer-runtime-bundle-sha-20260923` at `0cc26d866102fe9df4c54fbed4b74e74182c7b9e`; merged as `24a05e663489cd4063fcaa505fc26e3e3a9f732e`.
- **Base:** `main` at observed base `d3706c943540397e0e5be71a01f64b49768f1953`.
- **State / CI:** MERGED. PR run `35898993338` and landed-main run `35899139527` passed all five required jobs.
- **Original intent:** Bind opt-in runtime acquisition to the actual archive emitted by the successful clean-base-smoke candidate workflow.
- **Changed paths:** `cmd/stint/runtime.go`, `cmd/stint/runtime_test.go`, `internal/session/startup_events_test.go`.
- **Important symbols/tests:** `ninferRuntimeBundleSHA256`; release provenance and startup-event fixture assertions.
- **Unique semantics introduced:** Replaced provisional `16a1d238…` with archive SHA-256 `6725e60c8e3edb2982ad828898210868dd98ea1d4fe4d35f97bfa1e625414416`; the candidate archive was 939,381,613 bytes.
- **Current-main / evidence:** The pinned bytes were built from NInfer `81b68a20…`, smoke-tested on the recorded clean base by run `35894635094`, and published as immutable release `ninfer-runtime-81b68a20-sm89` by run `35902030339`.
- **Disposition:** **LANDED — exact release archive verified against the current-main pin; runtime remains opt-in pending live GPU acceptance.**

### #129 — fix: stage NInfer release candidates before promotion

- **PR / exact head:** [#129](https://github.com/Marguelgtz/Stint/pull/129), `fix/ninfer-release-promotion-workflow-20260923` at `faa0d15776cbcadaafddf633a9d654a3cff2a17a`; merged as `58afa4af857cf12e5135d4e78c46b79c96313922`.
- **Base:** `main` at observed `24a05e663489cd4063fcaa505fc26e3e3a9f732e`.
- **State / CI:** MERGED. PR run `35900956163` and landed-main run `35901071060` passed all five required jobs.
- **Original intent:** Repair publication after run `35899265948` stopped at Actions-run metadata with `Resource not accessible by integration` and before creating a release.
- **Changed paths:** `.github/workflows/ninfer-runtime-bundle.yml`, `.github/workflows/publish-ninfer-runtime-release.yml`.
- **Unique semantics introduced:** Successful candidate builds stage all three verified files as a draft with run/source/digest notes. Publisher validates public source-run metadata without Actions API permission, requires explicit immutable-setting confirmation, and verifies immutable publication as a postcondition. Source-build remains default.
- **Disposition:** **LANDED — draft handoff and least-privilege publisher; draft lookup/download was subsequently corrected by #130.**

### #130 — fix: resolve NInfer draft releases by asset id

- **PR / exact head:** [#130](https://github.com/Marguelgtz/Stint/pull/130), `fix/ninfer-release-promotion-workflow-20260923` at `42b0440a5b82f2c80ffbaf3311d478125dcf68ed`; merged as `23908fc719576376b32ce46fb0a34745c27cc9fb`.
- **Base:** `main` at observed `58afa4af857cf12e5135d4e78c46b79c96313922`.
- **State / CI:** MERGED. PR run `35901794930` and landed-main run `35901919089` passed all five required jobs.
- **Original intent:** Fix the draft handoff after GitHub's release-by-tag endpoint returned 404 for draft tag `ninfer-runtime-81b68a20-sm89`.
- **Changed paths:** `.github/workflows/ninfer-runtime-bundle.yml`, `.github/workflows/publish-ninfer-runtime-release.yml`.
- **Unique semantics introduced:** The build and publisher find drafts through the authenticated release list; the publisher fetches each exact asset by authenticated release-asset ID, validates the bytes, then publishes and verifies by immutable release ID.
- **Verification:** Publisher run `35902030339` passed source-run/lineage, draft provenance, three-asset, archive SHA, sidecar, manifest tuple and immutable postcondition checks. The published archive digest is `sha256:6725e60c8e3edb2982ad828898210868dd98ea1d4fe4d35f97bfa1e625414416`.
- **Disposition:** **LANDED — release `ninfer-runtime-81b68a20-sm89` is immutable and matches the Stint pin.**

## Detailed PR records (historical inventory with current dispositions)

### #11 — Fix Cline model limits and add configurable context

- **PR / exact head:** [#11](https://github.com/Marguelgtz/Stint/pull/11), `fix/model-limits` at `0f3a30b6aedf78fc91ae9cf128432fdbecf6a8d0`.
- **Base:** `main` at observed base SHA `111655339c2df9d0815861655db8825dc022dec4`.
- **State / mergeability:** OPEN; ready for review; `UNKNOWN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Synchronize interactive Qwen model limits across Stint/Cline and make context configurable.
- **Actual changed files (7 returned):** <details><summary>show complete path list</summary>

`cmd/stint/lifecycle.go`, `cmd/stint/model_limits.go`, `cmd/stint/resumable_start.go`, `cmd/stint/resumable_start_test.go`, `internal/runtime/llama/config.go`, `internal/runtime/llama/config_test.go`, `internal/session/state.go`

</details>
- **Important changed symbols:** validateInteractiveContext; effectiveInteractiveContext; remoteModelLaunchCommand; InteractiveQwen.
- **Tests added or modified:** TestRemoteModelLaunchUsesRequestedContext; TestValidateInteractiveContext; TestEffectiveInteractiveContextPreservesLegacySessions; TestInteractiveQwenLimits.
- **Unique semantics introduced:** Shared interactive-runtime config and bounded context selection remain useful. Current main represents context as session state and has a separate llama.cpp bound plus explicit NInfer profiles; the Cline-specific shared-config framing is obsolete.
- **Current-main status:** PARTIAL: current context bounds/config live in cmd/stint/llama_context.go, internal/runtime/llama/config.go, cmd/stint/ninfer_config.go and resumable-start tests. No Cline product surface remains.
- **Replacement / where it lives:** Current-main implementations above; bounded context semantics are in current main: commits `76e2c8f` and `e1bf4e6` added configurable/shared interactive limits, while `b4f9617` added llama.cpp context validation. Retire only the Cline coupling.
- **Dependencies:** Targets `main` at observed base SHA `111655339c2df9d0815861655db8825dc022dec4`; compare against current main because this base ref is stale.
- **Conflicts with current architecture:** Head is based on main 1116553 and is stale versus current NInfer architecture; do not merge its old default as a production context pin.
- **Associated live evidence / incidents:** None attached.
- **Disposition:** **PARTIALLY ABSORBED — close after ledger; preserve generic context-validation semantics and retire Cline-specific wiring.**

### #36 — Add a Cline configuration helper

- **PR / exact head:** [#36](https://github.com/Marguelgtz/Stint/pull/36), `feat/cline-config-helper` at `0daaf6444a81b14d76b12f7d264b0e1529bccc55`.
- **Base:** `feat/model-transfer-qualification` at observed base SHA `5baa04d0f6d856519c549ee57863aad69a1ec885`.
- **State / mergeability:** OPEN; ready for review; `DIRTY`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Add stint cline configure to repair stale Cline model/provider/context settings.
- **Actual changed files (3 returned):** <details><summary>show complete path list</summary>

`cmd/stint/cline.go`, `cmd/stint/cline_test.go`, `cmd/stint/main.go`

</details>
- **Important changed symbols:** runCline; runClineConfigure; configureClineState; mergeClineModelInfo.
- **Tests added or modified:** TestConfigureClineStateRepairsProviderModelAndContext; TestConfigureClineStateIsIdempotent.
- **Unique semantics introduced:** Cline config editing is a product-specific helper for a retired worker path.
- **Current-main status:** No current Stint command or runtime depends on this helper. Current Deep Work runs Hermes on the bound compute host.
- **Replacement / where it lives:** Hermes-on-box architecture in #107 (merge 2041498); no helper replacement is needed.
- **Dependencies:** Stacks on branch `feat/model-transfer-qualification` at observed base SHA `5baa04d0f6d856519c549ee57863aad69a1ec885`; that branch is not one of this snapshot's open PR heads, so its merged/closed ancestry must be checked before merge.
- **Conflicts with current architecture:** Targets a stale model-transfer-qualification branch and adds Cline-only CLI/help.
- **Associated live evidence / incidents:** None.
- **Disposition:** **OBSOLETE — close after ledger; Cline configuration is deliberately retired.**

### #48 — Prototype relocatable NInfer runtime bundle

- **PR / exact head:** [#48](https://github.com/Marguelgtz/Stint/pull/48), `feat/ninfer-runtime-bundle` at `bb3eea21fcd6204d00737fdc8fba7398b6e46b74`.
- **Base:** `fix/stale-vast-offer-retry` at observed base SHA `6df14566c89b3eac143b7550563116fbede74c3c`.
- **State / mergeability:** CLOSED after replacement verification; was draft and `UNSTABLE`.
- **Legacy PR check rollup:** build-and-smoke=FAILURE, spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Build the pinned NInfer source into a relocatable runtime bundle and run it on a clean CUDA 12.8 Vast base fixture without target-stage compilation.
- **Actual changed files (3 returned):** <details><summary>show complete path list</summary>

`.github/workflows/ninfer-bundle.yml`, `docs/NINFER_BUNDLE.md`, `images/ninfer/Bundle.Dockerfile`

</details>
- **Important changed symbols:** Bundle.Dockerfile build stages; ninfer-bundle workflow clean-base and compressed-artifact smoke.
- **Tests added or modified:** GitHub Actions clean-base and compressed-artifact smoke in .github/workflows/ninfer-bundle.yml; no Go unit tests.
- **Unique semantics introduced:** Establishes bundle build/export and pristine-base validation as an alternative to compiling NInfer on each rental. The historical run produced a large bundle and checksum but failed while checking the exported artifact from the wrong working directory.
- **Current-main status:** #122–#130 replace this prototype with a tuple-pinned build, clean-base smoke, immutable release and explicit opt-in deployment. Live RTX 4090 qualification remains incomplete.
- **Replacement / where it lives:** #122 `cc2f259`, #123 `b3c0295`, #124 `706c78a`, #127 `d3706c9`, #128 `24a05e6`, #129 `58afa4a`, and #130 `23908fc`; release tag `ninfer-runtime-81b68a20-sm89` carries the clean-tested `6725e60c…` artifact.
- **Dependencies:** Stacks on branch `fix/stale-vast-offer-retry` at observed base SHA `6df14566c89b3eac143b7550563116fbede74c3c`; that branch is not one of this snapshot's open PR heads, so its merged/closed ancestry must be checked before merge.
- **Conflicts with current architecture:** Old bundle targets runtime 981b685e and historical CUDA/base assumptions; exact NInfer/artifact tuple audit must precede any productionization.
- **Associated live evidence / incidents:** Historical CI: build/build-smoke succeeded; final artifact checksum verification failed due wrong working directory (per PR record).
- **Disposition:** **CLOSED after the exact-pin release was verified; keep startup opt-in pending live GPU acceptance.**

### #49 — Record startup lifecycle timing events

- **PR / exact head:** [#49](https://github.com/Marguelgtz/Stint/pull/49), `feat/startup-timing-log` at `441a307212e053d51e4c1c3913567e5a37d7611a`.
- **Base:** `feat/ninfer-runtime-bundle` at observed base SHA `bb3eea21fcd6204d00737fdc8fba7398b6e46b74`.
- **State / mergeability:** CLOSED as superseded; was draft and `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Append persistent startup lifecycle events to compare startup paths using measured phase timings.
- **Actual changed files (3 returned):** <details><summary>show complete path list</summary>

`internal/session/startup_events.go`, `internal/session/startup_events_test.go`, `internal/session/state.go`

</details>
- **Important changed symbols:** StartupEvent; StartupEventsPath; appendStartupEvent; LoadStartupEvents.
- **Tests added or modified:** TestSaveAppendsStartupEventsWithoutChangingLifecycleState; TestClearPreservesStartupHistory; TestNonStartupStatusIsNotRecorded.
- **Unique semantics introduced:** Append-only best-effort startup history independent of authoritative lifecycle writes.
- **Current-main status:** Best-effort append-only startup timing now lives in `internal/session/startup_events.go`; writes follow durable lifecycle persistence and do not change authoritative lifecycle success.
- **Replacement / where it lives:** #121 merge `063067e`; exact landed-main run `35867023267` passed all five required jobs.
- **Dependencies:** Stacks directly on open PR #48 branch `feat/ninfer-runtime-bundle` at observed base SHA `bb3eea21fcd6204d00737fdc8fba7398b6e46b74`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Stacks on #48’s old bundle prototype and old state shape; needs a current-main port rather than merging this branch.
- **Associated live evidence / incidents:** No comparable source-build versus bundle READY timing is recorded yet.
- **Disposition:** **SUPERSEDED — timing semantics landed in #121.**

### #50 — Publish NInfer runtime bundle as immutable release

- **PR / exact head:** [#50](https://github.com/Marguelgtz/Stint/pull/50), `ci/ninfer-bundle-release` at `e5e4ea798536342501e902abb6454b48e4feb2f0`.
- **Base:** `feat/startup-timing-log` at observed base SHA `441a307212e053d51e4c1c3913567e5a37d7611a`.
- **State / mergeability:** CLOSED after replacement verification; was draft and `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Manually publish one immutable GitHub Release for an exact source-pinned NInfer bundle.
- **Actual changed files (1 returned):** <details><summary>show complete path list</summary>

`.github/workflows/ninfer-bundle-release.yml`

</details>
- **Important changed symbols:** ninfer-bundle-release workflow.
- **Tests added or modified:** Workflow/release validation only; no application unit tests.
- **Unique semantics introduced:** Adds release publication for versioned runtime bundles without changing paid startup.
- **Current-main status:** #122/#123/#127–#130 provide tuple-pinned bundle construction, clean-base smoke, asset handoff and immutable publication; #124 consumes it as opt-in deployment. Exact release asset verifies against current `main`.
- **Replacement / where it lives:** #122 `cc2f259`, #123 `b3c0295`, #124 `706c78a`, #127 `d3706c9`, #128 `24a05e6`, #129 `58afa4a`, #130 `23908fc`; verified tag `ninfer-runtime-81b68a20-sm89`.
- **Dependencies:** Stacks directly on open PR #49 branch `feat/startup-timing-log` at observed base SHA `441a307212e053d51e4c1c3913567e5a37d7611a`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Old workflow/tag schema depends on #48/#51 and previous runtime tuple; do not run as production publisher unchanged.
- **Associated live evidence / incidents:** Candidate `35894635094` passed clean pinned-base smoke; publisher `35902030339` verified and published all three assets with archive SHA `6725e60c…`.
- **Disposition:** **CLOSED after exact-main build and immutable release were verified.**

### #51 — Start NInfer from Vast base image plus runtime bundle

- **PR / exact head:** [#51](https://github.com/Marguelgtz/Stint/pull/51), `feat/ninfer-base-image-bundle` at `946097fa7289cc2b4ed7764769374ec1192b8f73`.
- **Base:** `ci/ninfer-bundle-release` at observed base SHA `e5e4ea798536342501e902abb6454b48e4feb2f0`.
- **State / mergeability:** CLOSED after replacement verification; was draft and `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Add an opt-in runtime-bundle startup path on the standard Vast base while preserving an image control/default.
- **Actual changed files (2 returned):** <details><summary>show complete path list</summary>

`cmd/stint/llama_fast_start_test.go`, `cmd/stint/ninfer_vast.go`

</details>
- **Important changed symbols:** ninferDeploymentMode; vastNInferBundleBridgeOnStart; runtime bootstrap bridge.
- **Tests added or modified:** TestNInferDefaultRemainsPinnedPrebuiltImage; TestNInferBundleDeploymentUsesPlainVastBase; TestNInferUnknownDeploymentFallsBackToControlImage; TestNInferBundleOnStartWritesLazyRuntimeBridge; TestNInferBootstrapOverlapsModelPrefetchWithBundleResolution.
- **Unique semantics introduced:** Demonstrates runtime acquisition/verification can run after SSH while model transfer overlaps; production default remains control path.
- **Current-main status:** #124 has an explicit fail-closed release-bundle mode, saved provenance and source-build resume recovery. Source-build remains the default; the bundle path has not cleared fresh RTX 4090/Deep Work acceptance.
- **Replacement / where it lives:** #124 merge `706c78a`; #128 replaced provisional `16a1d238…` with tested archive SHA `6725e60c…`, published in `ninfer-runtime-81b68a20-sm89`.
- **Dependencies:** Stacks directly on open PR #50 branch `ci/ninfer-bundle-release` at observed base SHA `e5e4ea798536342501e902abb6454b48e4feb2f0`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** This opt-in path relies on #48/#50 old release format and bundle SHA scheme; runtime tuple not freshly re-evaluated.
- **Associated live evidence / incidents:** No current RTX 4090 bundle-vs-source READY comparison.
- **Disposition:** **CLOSED after #124 and #127–#130 replaced this implementation; retain the live-acceptance gate.**

### #56 — Show resident NInfer context by lane in dashboard

- **PR / exact head:** [#56](https://github.com/Marguelgtz/Stint/pull/56), `feat/dashboard-client-context` at `6f8f11371afe70712b49b358391170c0fc1f9363`.
- **Base:** `feat/ninfer-dual-lanes` at observed base SHA `0a4f07d3c37454ae1c32d5351bca29b669957e58`.
- **State / mergeability:** OPEN; draft; `DIRTY`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS.
- **Original intent:** Integrate resident NInfer context, live telemetry, recovery, performance, deadline and dashboard surfaces in a broad stacked branch.
- **Actual changed files (85 returned):** <details><summary>show complete path list</summary>

`.github/workflows/ci.yml`, `.github/workflows/ninfer-image.yml`, `.spark/profile.yml`, `README.md`, `cmd/stint/dash.go`, `cmd/stint/dashboard.go`, `cmd/stint/dashboard_benchmark.go`, `cmd/stint/dashboard_context.go`, `cmd/stint/dashboard_context_test.go`, `cmd/stint/dashboard_recovery.go`, `cmd/stint/dashboard_recovery_controller_test.go`, `cmd/stint/dashboard_recovery_test.go`, `cmd/stint/dashboard_test.go`, `cmd/stint/deadline_watchdog.go`, `cmd/stint/deadline_watchdog_test.go`, `cmd/stint/help.go`, `cmd/stint/help_clients_test.go`, `cmd/stint/help_dashboard.go`, `cmd/stint/help_dashboard_test.go`, `cmd/stint/help_deadline.go`, `cmd/stint/help_deadline_test.go`, `cmd/stint/help_status_telemetry.go`, `cmd/stint/help_test.go`, `cmd/stint/infer_probe.go`, `cmd/stint/infer_probe_test.go`, `cmd/stint/lifecycle.go`, `cmd/stint/lifecycle_lock.go`, `cmd/stint/lifecycle_lock_test.go`, `cmd/stint/llama_fast_start_test.go`, `cmd/stint/main.go`, `cmd/stint/network_candidate_retry.go`, `cmd/stint/network_candidate_retry_test.go`, `cmd/stint/network_qualification.go`, `cmd/stint/network_qualification_test.go`, `cmd/stint/ninfer_clients.go`, `cmd/stint/ninfer_clients_test.go`, `cmd/stint/ninfer_config_test.go`, `cmd/stint/ninfer_dual_lane_launch_test.go`, `cmd/stint/ninfer_vast.go`, `cmd/stint/perf.go`, `cmd/stint/perf_prompt.go`, `cmd/stint/perf_test.go`, `cmd/stint/performance_store.go`, `cmd/stint/performance_store_test.go`, `cmd/stint/resumable_start.go`, `cmd/stint/resumable_start_test.go`, `cmd/stint/resume.go`, `cmd/stint/runtime.go`, `cmd/stint/runtime_surface_test.go`, `cmd/stint/runtime_test.go`, `cmd/stint/session_deadline.go`, `cmd/stint/session_deadline_test.go`, `cmd/stint/session_snapshot.go`, `cmd/stint/session_snapshot_test.go`, `cmd/stint/session_telemetry.go`, `cmd/stint/session_telemetry_test.go`, `cmd/stint/ssh_permissions_test.go`, `cmd/stint/status_context.go`, `cmd/stint/status_telemetry.go`, `cmd/stint/status_telemetry_test.go`, `docs/CLI.md`, `docs/DASHBOARD.md`, `docs/NINFER.md`, `docs/TELEMETRY.md`, `images/ninfer/Dockerfile`, `internal/collaboration/contracts.go`, `internal/core/plan.go`, `internal/dashboard/context.go`, `internal/dashboard/context_test.go`, `internal/dashboard/render.go`, `internal/dashboard/render_test.go`, `internal/dashboard/terminal.go`, `internal/dashboard/terminal_test.go`, `internal/provider/vast/client.go`, `internal/provider/vast/command.go`, `internal/provider/vast/instance.go`, `internal/provider/vast/instance_test.go`, `internal/router/profile.go`, `internal/runtime/llama/config.go`, `internal/session/deadline.go`, `internal/session/deadline_test.go`, `internal/session/state.go`, `internal/session/state_clients_test.go`, `internal/spark/onboard.go`, `internal/spark/onboard_test.go`

</details>
- **Important changed symbols:** dashboardController; dashboardClientContexts; dashboardSessionRecoverable; inferFromEpoch; runDynamicWatchdog; runPerf.
- **Tests added or modified:** Representative suites: dashboard_context_test.go; dashboard_recovery*_test.go; infer_probe_test.go; deadline_watchdog_test.go; lifecycle_lock_test.go; performance_store_test.go; session_telemetry_test.go.
- **Unique semantics introduced:** The branch combines broad old-stack dashboard/runtime work. Preserve actual current semantics: distinguish processing from retained context, surface lanes and telemetry, recover from probe failures, and keep lifecycle controls guarded.
- **Current-main status:** Dashboard baseline and its tests are present. The remaining high-value ordinary-dashboard semantics from this stack were reconciled in #117; lifecycle safety was separately ported in #116. Do not re-import its image/runtime or old stack wholesale.
- **Replacement / where it lives:** #116 merge ca2f24b for lifecycle safety; #117 merge 9a67234 for dashboard/telemetry; final phase view is unrelated #118 merge 6f4c81f.
- **Dependencies:** Stacks on branch `feat/ninfer-dual-lanes` at observed base SHA `0a4f07d3c37454ae1c32d5351bca29b669957e58`; that branch is not one of this snapshot's open PR heads, so its merged/closed ancestry must be checked before merge.
- **Conflicts with current architecture:** Large, heavily stacked branch based on feat/ninfer-dual-lanes and contains many already-landed or independently superseded files; not a clean current-main candidate.
- **Associated live evidence / incidents:** Its source notes include live NInfer observations later refined in #60 and upstream README; identity is deliberately left unattributed.
- **Disposition:** **SUPERSEDED — close after ledger; semantics were separated and re-established on current main.**

### #57 — Add Deep Work MVP (stint deep start/status/stop)

- **PR / exact head:** [#57](https://github.com/Marguelgtz/Stint/pull/57), `feat/deep-work-mvp` at `3aa9a2f6aecad767d05e7b8e0fd2481429f7815e`.
- **Base:** `fix/vast-marketplace-resilience` at observed base SHA `bbf303768a96486fca77e6206a161af9f0a3df3d`.
- **State / mergeability:** OPEN; ready for review; `CLEAN` (state at the historical inventory snapshot).
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Introduce Deep Work start/status/stop with Cline workers, durable mission state, verification, resumability and landing.
- **Actual changed files (28 returned):** <details><summary>show complete path list</summary>

`STINT_DEEP_WORK_INVESTIGATION.md`, `cmd/stint/deep_events.go`, `cmd/stint/deep_executor.go`, `cmd/stint/deep_executor_test.go`, `cmd/stint/deep_handoff.go`, `cmd/stint/deep_landing.go`, `cmd/stint/deep_loop.go`, `cmd/stint/deep_loop_test.go`, `cmd/stint/deep_resume.go`, `cmd/stint/deep_resume_test.go`, `cmd/stint/deep_run.go`, `cmd/stint/deep_start.go`, `cmd/stint/deep_status.go`, `cmd/stint/help.go`, `cmd/stint/main.go`, `docs/DEEP_WORK.md`, `docs/DEEP_WORK_MVP_EXECUTION.md`, `docs/DEEP_WORK_SETUP.md`, `docs/STINT_DEEP_WORK_VISION.md`, `internal/deep/codec.go`, `internal/deep/context.go`, `internal/deep/coordinator_pid.go`, `internal/deep/deep_test.go`, `internal/deep/mission.go`, `internal/deep/policy.go`, `internal/deep/state.go`, `internal/deep/state_persist.go`, `internal/deep/task.go`

</details>
- **Important changed symbols:** deepCoordinator.run; runDeepStart; runDeepResume; buildHandoff; deepCoordinator.land; clineExecutor.
- **Tests added or modified:** deep_executor_test.go; deep_loop_test.go; deep_resume_test.go; internal/deep/deep_test.go.
- **Unique semantics introduced:** First end-to-end orchestration model established durable tasks, retries, independent verification, deadline landing and handoff. Its Cline/operator-host worker architecture is historical.
- **Current-main status:** Current Deep Work retains durable coordination, verification, landing and handoff but uses Hermes-on-box, detached supervision, compute binding and provider-safe lifecycle.
- **Replacement / where it lives:** #107 merge 2041498 (head 7adf523) is authoritative Hermes-on-box Deep Work; #118 merge 6f4c81f grounds Deep Dashboard evidence.
- **Dependencies:** Stacks on branch `fix/vast-marketplace-resilience` at observed base SHA `bbf303768a96486fca77e6206a161af9f0a3df3d`; that branch is not one of this snapshot's open PR heads, so its merged/closed ancestry must be checked before merge.
- **Conflicts with current architecture:** Cline worker path conflicts with the explicitly authoritative Hermes-on-box architecture; branch carries historical investigation/vision docs that must not become current guidance.
- **Associated live evidence / incidents:** Later GPU-owned session PRs #81–#84 and #110–#113 provide real run evidence; do not merge this source stack.
- **Disposition:** **SUPERSEDED — close after ledger; retain only generic coordinator invariants represented by current Deep Work.**

### #58 — Show live NInfer lanes separately in dashboard

- **PR / exact head:** [#58](https://github.com/Marguelgtz/Stint/pull/58), `feat/dashboard-live-lanes` at `3963e7c368d295941a290c690c945f9e824ff967`.
- **Base:** `feat/dashboard-client-context` at observed base SHA `6f8f11371afe70712b49b358391170c0fc1f9363`.
- **State / mergeability:** OPEN; draft; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Show each live NInfer lane and whether it is processing, resident, or empty, with shared decode labeling.
- **Actual changed files (5 returned):** <details><summary>show complete path list</summary>

`cmd/stint/dashboard_context.go`, `cmd/stint/dashboard_context_test.go`, `cmd/stint/infer_probe.go`, `internal/dashboard/context.go`, `internal/dashboard/context_test.go`

</details>
- **Important changed symbols:** dashboardLaneLiveLabel; laneIsActive; contextBar.
- **Tests added or modified:** TestDashboardContextsProjectObservedLaneState; TestDashboardContextsKeepEmptyLanesVisible; TestRenderHomeKeepsEmptyLaneVisible; TestContextLegendMarksDecodeSharedAcrossActiveLanes.
- **Unique semantics introduced:** Per-lane live rows, empty-lane visibility and shared-vs-lane attribution cues; no client identity from digest.
- **Current-main status:** Current ordinary dashboard shows every slot and current processing/resident status; decode is labeled engine/shared; lane identity is not caller identity.
- **Replacement / where it lives:** #117 merge 9a67234 (dashboard semantics and tests).
- **Dependencies:** Stacks directly on open PR #56 branch `feat/dashboard-client-context` at observed base SHA `6f8f11371afe70712b49b358391170c0fc1f9363`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Based on old dashboard-client-context branch; preserve the semantics, not its stale stack.
- **Associated live evidence / incidents:** NInfer `/metrics` and `/slots` investigation documented in #60; current upstream README confirms prompt token/cache distinction.
- **Disposition:** **SUPERSEDED — close after ledger; semantics are present in #117.**

### #59 — deep: --worker hermes — run the Deep Work agent on the compute box

- **PR / exact head:** [#59](https://github.com/Marguelgtz/Stint/pull/59), `feat/deep-work-hermes-worker` at `b48cc3b682444af8328da3b22a9ae9ef4a7aa020`.
- **Base:** `feat/deep-work-with-dashboard` at observed base SHA `3426442a8ba5923bc296cf0eb5de1d0a6b4e0c4c`.
- **State / mergeability:** OPEN; ready for review; `CLEAN` (state at the historical inventory snapshot).
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS.
- **Original intent:** Run Hermes on the compute box through SSH for Deep Work execution, verification and git, retaining coordinator/state/handoff.
- **Actual changed files (17 returned):** <details><summary>show complete path list</summary>

`cmd/stint/deep_landing.go`, `cmd/stint/deep_loop.go`, `cmd/stint/deep_remote.go`, `cmd/stint/deep_remote_test.go`, `cmd/stint/deep_resume.go`, `cmd/stint/deep_resume_test.go`, `cmd/stint/deep_run.go`, `cmd/stint/deep_start.go`, `cmd/stint/help.go`, `cmd/stint/main.go`, `cmd/stint/resumable_start.go`, `cmd/stint/resume.go`, `docs/CP1_DRYRUN_MISSION.md`, `docs/DEEP_WORK_GPU_HERMES_PLAN.md`, `internal/deep/state.go`, `scripts/box-smoke.sh`, `scripts/provision-box.sh`

</details>
- **Important changed symbols:** hermesExecutor; remoteGit; runVerifyCmdRemote; deepRunSession.
- **Tests added or modified:** deep_remote_test.go; deep_resume_test.go; resumable-start and runtime shell tests.
- **Unique semantics introduced:** Moves worker filesystem/shell/verification work onto compute; uses remote git/worktree operations and keeps local coordinator responsibilities separate.
- **Current-main status:** Current main uses Hermes on-box with detached supervisor, bound compute instance, durable state and verified checkpoint publication.
- **Replacement / where it lives:** #107 merge 2041498; current live evidence #110–#113.
- **Dependencies:** Stacks on branch `feat/deep-work-with-dashboard` at observed base SHA `3426442a8ba5923bc296cf0eb5de1d0a6b4e0c4c`; that branch is not one of this snapshot's open PR heads, so its merged/closed ancestry must be checked before merge.
- **Conflicts with current architecture:** Historical implementation is an earlier branch. Merge would risk replacing later supervisor, publisher and rebind safety.
- **Associated live evidence / incidents:** Successful session #110–#113; older successful session #81–#84.
- **Disposition:** **SUPERSEDED — close after ledger; preserve architecture semantics through current implementation.**

### #60 — docs: correct NInfer lane semantics and health probe endpoints

- **PR / exact head:** [#60](https://github.com/Marguelgtz/Stint/pull/60), `stint/easy-wins-1-docs` at `672cd7c0d4ee42360d00fbaa004ab64726b0b56c`.
- **Base:** `feat/deep-work-hermes-worker` at observed base SHA `b48cc3b682444af8328da3b22a9ae9ef4a7aa020`.
- **State / mergeability:** OPEN; ready for review; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS.
- **Original intent:** Correct NInfer telemetry interpretation, forbid session_digest identity attribution and document /v1/models probing/degraded fallback.
- **Actual changed files (5 returned):** <details><summary>show complete path list</summary>

`cmd/stint/infer_probe.go`, `cmd/stint/infer_probe_test.go`, `cmd/stint/session_telemetry.go`, `docs/NINFER.md`, `docs/TELEMETRY.md`

</details>
- **Important changed symbols:** inferFromEpoch; inferenceHTTPGet; session telemetry rendering.
- **Tests added or modified:** TestParsePrometheusTextNInfer; TestParseSlotLanesNInfer; TestProbeInferenceMetricsDisabledFallsBackToSlots; TestProbeInferenceSecondEpochUnavailableKeepsFirstEpoch.
- **Unique semantics introduced:** NInfer computed prefill excludes prefix-cache hits; hits are a separate counter. `session_digest` is not a stable Stint caller identity. Health uses `/v1/models`; a slow tunnel can leave a first-epoch sample.
- **Current-main status:** Current docs/code explicitly identify no client identity, distinguish NInfer and llama cache semantics, and keep last-good data with errors/age.
- **Replacement / where it lives:** #117 merge 9a67234; current NInfer README at https://github.com/sergiuszm/ninfer-4090 is supporting upstream evidence.
- **Dependencies:** Stacks directly on open PR #59 branch `feat/deep-work-hermes-worker` at observed base SHA `b48cc3b682444af8328da3b22a9ae9ef4a7aa020`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** The historical note’s exact digest lifecycle wording is not generalized; current guarantee is only that it is not stable Stint identity.
- **Associated live evidence / incidents:** Observed during 2026-09-03 live NInfer investigation.
- **Disposition:** **SUPERSEDED — close after ledger; retain current truthful semantics, not historical overstatement.**

### #63 — feat: warn on a stale local deadline in stint status

- **PR / exact head:** [#63](https://github.com/Marguelgtz/Stint/pull/63), `stint/easy-wins-4-stale-deadline-warning` at `20d71e14271c6ea83a60969e4e4ab3254f908281`.
- **Base:** `stint/easy-wins-3-probe-budget` at observed base SHA `4f9b915165eb504045f15a2686401e2126beb073`.
- **State / mergeability:** OPEN; ready for review; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS.
- **Original intent:** Warn when local saved deadline may be stale while a tunnel/lifecycle state suggests a live or recoverable session.
- **Actual changed files (2 returned):** <details><summary>show complete path list</summary>

`cmd/stint/session_snapshot.go`, `cmd/stint/session_snapshot_stale_test.go`

</details>
- **Important changed symbols:** stateStaleness; staleStateWarning; buildSessionSnapshot.
- **Tests added or modified:** session_snapshot_stale_test.go; status telemetry tests.
- **Unique semantics introduced:** State freshness warning conditioned on age/deadline and whether live provider/tunnel state should exist.
- **Current-main status:** Current status/dashboard exposes state age and stale warning with tunnel-running or READY/RECOVERABLE guards; it does not claim status refresh rewrites lifecycle state.
- **Replacement / where it lives:** #117 merge 9a67234.
- **Dependencies:** Stacks on branch `stint/easy-wins-3-probe-budget` at observed base SHA `4f9b915165eb504045f15a2686401e2126beb073`; that branch is not one of this snapshot's open PR heads, so its merged/closed ancestry must be checked before merge.
- **Conflicts with current architecture:** Stacks on a historical probe-budget branch; warning must remain observational, not implicit provider synchronization.
- **Associated live evidence / incidents:** Incident observed 2026-09-03: local deadline lagged an extension made elsewhere.
- **Disposition:** **SUPERSEDED — close after ledger; #117 carries the grounded stale-state signal.**

### #64 — feat: keep the last-good dashboard inference sample on failed refreshes

- **PR / exact head:** [#64](https://github.com/Marguelgtz/Stint/pull/64), `stint/medium-1-dashboard-last-good` at `b9eb1b3926262409aa2dcbd58be103b6fd3d32f4`.
- **Base:** `stint/easy-wins-4-stale-deadline-warning` at observed base SHA `20d71e14271c6ea83a60969e4e4ab3254f908281`.
- **State / mergeability:** OPEN; ready for review; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS.
- **Original intent:** Retain last-good inference sample across failed dashboard refreshes and show its age/error, resetting on instance changes.
- **Actual changed files (8 returned):** <details><summary>show complete path list</summary>

`cmd/stint/cache_reuse_test.go`, `cmd/stint/dashboard.go`, `cmd/stint/dashboard_last_good_test.go`, `cmd/stint/infer_probe.go`, `cmd/stint/infer_probe_test.go`, `cmd/stint/status_telemetry.go`, `docs/TELEMETRY.md`, `internal/dashboard/render.go`

</details>
- **Important changed symbols:** dashboardController.applyRefresh; dashboardInference; last-good sample cache.
- **Tests added or modified:** dashboard_last_good_test.go; cache_reuse_test.go; infer_probe_test.go; dashboard render tests.
- **Unique semantics introduced:** Distinguishes stale-but-present telemetry from no sample and a new compute instance; dashboard remains observational.
- **Current-main status:** Last-good sample retained per instance, age/error shown, reset on instance replacement, and recovered observations replace it.
- **Replacement / where it lives:** #117 merge 9a67234.
- **Dependencies:** Stacks directly on open PR #63 branch `stint/easy-wins-4-stale-deadline-warning` at observed base SHA `20d71e14271c6ea83a60969e4e4ab3254f908281`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Stacks on stale #63; current implementation uses current dashboard/controller types.
- **Associated live evidence / incidents:** Observed false unavailable during slow tunnel / two-client load on 2026-09-03.
- **Disposition:** **SUPERSEDED — close after ledger; #117 replaces this behavior.**

### #67 — feat: type-to-confirm + destroy verification for stint down and the watchdog

- **PR / exact head:** [#67](https://github.com/Marguelgtz/Stint/pull/67), `stint/medium-6-destroy-confirmation` at `2acc198d46150654b0e9decea243309cb0d70a73`.
- **Base:** `stint/medium-3-ninfer-cache-reuse` at observed base SHA `71abe24e1429809d80f25eb14a3af7eeb949cfac`.
- **State / mergeability:** OPEN; ready for review; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Require explicit down confirmation, verify Vast disappearance before state clear, and harden watchdog teardown.
- **Actual changed files (5 returned):** <details><summary>show complete path list</summary>

`cmd/stint/deadline_watchdog.go`, `cmd/stint/help.go`, `cmd/stint/lifecycle.go`, `cmd/stint/lifecycle_down_test.go`, `docs/CLI.md`

</details>
- **Important changed symbols:** runDown; destroyExpiredSession; verifyVastInstanceGone.
- **Tests added or modified:** lifecycle_down_test.go; deadline_watchdog_test.go.
- **Unique semantics introduced:** Protect paid compute: type-to-confirm, unattended `--yes`, verified instance disappearance, preserve state on uncertain destroy.
- **Current-main status:** Current down/watchdog path includes confirmation and provider disappearance verification; lifecycle safety also serializes mutation and refuses unsafe owner signaling.
- **Replacement / where it lives:** #116 merge ca2f24b; exact push CI run 35851717414.
- **Dependencies:** Stacks on branch `stint/medium-3-ninfer-cache-reuse` at observed base SHA `71abe24e1429809d80f25eb14a3af7eeb949cfac`; that branch is not one of this snapshot's open PR heads, so its merged/closed ancestry must be checked before merge.
- **Conflicts with current architecture:** Old branch is stacked on cache-reuse lineage and has no need to merge after safety port.
- **Associated live evidence / incidents:** 2026-09-04 dry-run incident: instance billed long after failed bootstrap; #116 addresses safety behavior.
- **Disposition:** **SUPERSEDED — close after ledger; #116 carries lifecycle safety.**

### #68 — feat: agents semantics fix + per-lane live rows (merge of the live-branch dashboard)

- **PR / exact head:** [#68](https://github.com/Marguelgtz/Stint/pull/68), `stint/medium-4-agents-semantics` at `8db77e820530ade8f875c51f1b8e3babf4709e2a`.
- **Base:** `stint/medium-6-destroy-confirmation` at observed base SHA `2acc198d46150654b0e9decea243309cb0d70a73`.
- **State / mergeability:** OPEN; ready for review; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Correct active-agent count to reflect actual processing and merge per-lane dashboard rows.
- **Actual changed files (5 returned):** <details><summary>show complete path list</summary>

`cmd/stint/dashboard_context.go`, `cmd/stint/dashboard_context_test.go`, `cmd/stint/infer_probe.go`, `internal/dashboard/context.go`, `internal/dashboard/context_test.go`

</details>
- **Important changed symbols:** inferFromEpoch; dashboardClientContexts; contextBar.
- **Tests added or modified:** dashboard_context_test.go; internal/dashboard/context_test.go.
- **Unique semantics introduced:** `requests_processing` is active count; retained context is not active execution. Preserve lane rendering.
- **Current-main status:** Active count uses requests_processing with slots fallback; resident depth is prompt-token sum; slots remain visible.
- **Replacement / where it lives:** #117 merge 9a67234.
- **Dependencies:** Stacks directly on open PR #67 branch `stint/medium-6-destroy-confirmation` at observed base SHA `2acc198d46150654b0e9decea243309cb0d70a73`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** The PR is a large stack merge atop #67, not a safe isolated landing.
- **Associated live evidence / incidents:** Derived from live dual-lane observations; current upstream NInfer README defines its counters.
- **Disposition:** **SUPERSEDED — close after ledger; current semantics are in #117.**

### #69 — feat: session-state durability (archive + watchdog) and staleness signal

- **PR / exact head:** [#69](https://github.com/Marguelgtz/Stint/pull/69), `stint/medium-5-session-durability` at `2e21edea7d9df252ff8ed5aaa0f99ec52a84acb8`.
- **Base:** `stint/medium-4-agents-semantics` at observed base SHA `8db77e820530ade8f875c51f1b8e3babf4709e2a`.
- **State / mergeability:** OPEN; ready for review; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Archive session state, harden exact-instance watchdog behavior and expose local-state staleness.
- **Actual changed files (14 returned):** <details><summary>show complete path list</summary>

`cmd/stint/dashboard.go`, `cmd/stint/deadline_watchdog.go`, `cmd/stint/lifecycle.go`, `cmd/stint/network_qualification.go`, `cmd/stint/resumable_start.go`, `cmd/stint/resume.go`, `cmd/stint/session_archive.go`, `cmd/stint/session_archive_test.go`, `cmd/stint/session_snapshot.go`, `cmd/stint/session_staleness_test.go`, `cmd/stint/session_telemetry.go`, `cmd/stint/status_telemetry.go`, `docs/NINFER.md`, `docs/TELEMETRY.md`

</details>
- **Important changed symbols:** ArchiveSession; staleStateWarning; spawnWatchdog; destroyExpiredSession.
- **Tests added or modified:** session_archive_test.go; session_staleness_test.go; deadline_watchdog_test.go.
- **Unique semantics introduced:** Instance-stamped archive before clearing active state; watchdog ownership/instance safety; truthful freshness signal.
- **Current-main status:** Lifecycle archive/watchdog/safety semantics are in #116; staleness status/dashboard is in #117.
- **Replacement / where it lives:** #116 merge ca2f24b plus #117 merge 9a67234.
- **Dependencies:** Stacks directly on open PR #68 branch `stint/medium-4-agents-semantics` at observed base SHA `8db77e820530ade8f875c51f1b8e3babf4709e2a`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Stacks on #68 and bundles unrelated dashboard/runtime files; use semantic ports only.
- **Associated live evidence / incidents:** 2026-09-04 paid-instance/destroy incidents; 2026-09-03 stale deadline.
- **Disposition:** **PARTIALLY ABSORBED — close after ledger; each live semantic is independently replaced by #116/#117.**

### #70 — feat: operator-side lane correlation, client tag, and explicit no-identity rendering

- **PR / exact head:** [#70](https://github.com/Marguelgtz/Stint/pull/70), `stint/medium-7-lane-correlation` at `ae041bcda6b77f19f2ee9d32e1feffe9a02247b2`.
- **Base:** `stint/medium-5-session-durability` at observed base SHA `2e21edea7d9df252ff8ed5aaa0f99ec52a84acb8`.
- **State / mergeability:** OPEN; ready for review; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Record operator-side lane state transitions, declared-client tags and explicit no-identity output.
- **Actual changed files (8 returned):** <details><summary>show complete path list</summary>

`cmd/stint/dashboard.go`, `cmd/stint/dashboard_lane_events_test.go`, `cmd/stint/help.go`, `cmd/stint/resumable_start.go`, `cmd/stint/session_snapshot.go`, `internal/dashboard/lane_events_test.go`, `internal/dashboard/render.go`, `internal/session/state.go`

</details>
- **Important changed symbols:** observeLaneEvents; laneStateKind; pushLaneEvent; LaneEvent.
- **Tests added or modified:** dashboard_lane_events_test.go; internal/dashboard/lane_events_test.go.
- **Unique semantics introduced:** Bounded in-memory transition log is useful; no caller identity can be inferred. Optional tag labels only declared clients and is not lane attribution.
- **Current-main status:** Current dashboard records bounded, newest-first lane events without caller identity. It deliberately omits client tags because no stable lane/client mapping exists.
- **Replacement / where it lives:** #117 merge 9a67234 for event log and truthful no-identity rendering; client-tagging is deliberately retired.
- **Dependencies:** Stacks directly on open PR #69 branch `stint/medium-5-session-durability` at observed base SHA `2e21edea7d9df252ff8ed5aaa0f99ec52a84acb8`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Tag can suggest attribution that the engine does not provide; this would weaken truthful UI semantics.
- **Associated live evidence / incidents:** Historical proposal came from live NInfer identity investigation; current upstream `/slots` fields still do not establish Stint caller identity.
- **Disposition:** **PARTIALLY ABSORBED — close after ledger; retain event history and retire client tagging.**

### #73 — docs: P4 dry run records — run-1 incident + run-2 findings

- **PR / exact head:** [#73](https://github.com/Marguelgtz/Stint/pull/73), `docs/cp1-dryrun-records` at `e78ceef308d85c9cac7c71e7d172bed7c66c4182`.
- **Base:** `feat/deep-work-hermes-worker` at observed base SHA `b48cc3b682444af8328da3b22a9ae9ef4a7aa020`.
- **State / mergeability:** CLOSED on 2026-09-23 after #125 merged; it was ready for review and `CLEAN` at the historical inventory snapshot.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Preserve two browsable markdown records of the P4 dry-run incident and follow-up findings.
- **Actual changed files (2 returned):** <details><summary>show complete path list</summary>

`docs/CP1_DRYRUN1_INCIDENT.md`, `docs/CP1_DRYRUN_FINDINGS.md`

</details>
- **Important changed symbols:** None (documentation-only).
- **Tests added or modified:** No automated tests; docs are evidence records.
- **Unique semantics introduced:** Unique historical incident/findings narrative. Both reports are preserved byte-for-byte on current `main`.
- **Current-main status:** Reports are in `docs/history/CP1_DRYRUN1_INCIDENT.md` and `docs/history/CP1_DRYRUN_FINDINGS.md`; SHA-256 values are `6db95e309e5cc8c4cfeb1340e1a2e76c18fe21a8a501a184c17a21f0fbaea319` and `7e55f1cacd53b26c9dddc04d246430c4794777d863f79cf5f7731e58a88d76c9`.
- **Replacement / where it lives:** Merged by #125 (`36d12ac`); the two verbatim historical reports and `docs/history/README.md` are on `main`.
- **Dependencies:** Stacks directly on open PR #59 branch `feat/deep-work-hermes-worker` at observed base SHA `b48cc3b682444af8328da3b22a9ae9ef4a7aa020`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Old base is a Hermes branch and docs organization is being deferred; do not merge the stale branch mechanically.
- **Associated live evidence / incidents:** P4 synthetic dry-run session records, per PR body.
- **Disposition:** **CLOSED after replacement verified — #125 merged and the reports are preserved byte-for-byte.**

### #74 — fix: pin NInfer artifact revision and remove hardcoded transfer size

- **PR / exact head:** [#74](https://github.com/Marguelgtz/Stint/pull/74), `fix/ninfer-artifact-revision-size` at `800b6d1688d3e720f5faca904e2db1e44272d023`.
- **Base:** `stint/medium-7-lane-correlation` at observed base SHA `ae041bcda6b77f19f2ee9d32e1feffe9a02247b2`.
- **State / mergeability:** OPEN; ready for review; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Pin the Qwen artifact revision and discover transfer size rather than relying on mutable Hugging Face main and a hard-coded byte count.
- **Actual changed files (4 returned):** <details><summary>show complete path list</summary>

`cmd/stint/network_qualification.go`, `cmd/stint/network_qualification_test.go`, `cmd/stint/runtime.go`, `cmd/stint/runtime_test.go`

</details>
- **Important changed symbols:** parseTransferTotalBytes; ninferTransferSampleCommand; remoteModelLaunchCommandForState.
- **Tests added or modified:** TestNInferTransferSampleDiscoversArtifactSize; TestNInferModelArtifactIsRevisionPinned; TestNInferProgressDoesNotHardcodeArtifactSize; TestNInferGeneratedShellIsValid.
- **Unique semantics introduced:** Immutable artifact revision and observed transfer size prevent mismatched checksum/progress after upstream replacement.
- **Current-main status:** Production model revision/hash are pinned and size is discovered; current source references artifact rev 18dfc887 and SHA eec39564….
- **Replacement / where it lives:** Current-main commit 1945a5033a0db98a85aa66f75819716e7995488e (verified pin and transfer-size discovery).
- **Dependencies:** Stacks directly on open PR #70 branch `stint/medium-7-lane-correlation` at observed base SHA `ae041bcda6b77f19f2ee9d32e1feffe9a02247b2`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Open head is based on the dashboard client-tag branch and would reapply old runtime shell edits.
- **Associated live evidence / incidents:** Fixed observed 2026-09-06 download/checksum failure (progress exceeded 100%).
- **Disposition:** **SUPERSEDED — close after ledger; behavior is already on main at 1945a50.**

### #76 — P0: integrate lifecycle safety on current NInfer stack

- **PR / exact head:** [#76](https://github.com/Marguelgtz/Stint/pull/76), `fix/p0-ninfer-session-safety` at `792bb508dfcd7d64e293359dfbed7b497b10dafa`.
- **Base:** `fix/ninfer-artifact-revision-size` at observed base SHA `800b6d1688d3e720f5faca904e2db1e44272d023`.
- **State / mergeability:** OPEN; draft; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Port P0 lifecycle lock ownership, active Doctor, watchdog coordination and Vast instance disappearance verification onto the current artifact-fix line.
- **Actual changed files (12 returned):** <details><summary>show complete path list</summary>

`cmd/stint/doctor_active.go`, `cmd/stint/doctor_active_test.go`, `cmd/stint/help_doctor_active.go`, `cmd/stint/lifecycle.go`, `cmd/stint/lifecycle_lock.go`, `cmd/stint/lifecycle_lock_test.go`, `cmd/stint/lifecycle_lock_watchdog_test.go`, `cmd/stint/session_evidence.go`, `cmd/stint/session_evidence_test.go`, `docs/P0_SESSION_RELIABILITY_DOCTOR.md`, `internal/provider/vast/instance.go`, `internal/provider/vast/instance_test.go`

</details>
- **Important changed symbols:** diagnoseActiveSession; classifyDoctorInputs; acquireLifecycleLockOnce; interruptLifecycleOwner; ShowInstance.
- **Tests added or modified:** doctor_active_test.go; lifecycle_lock_test.go; lifecycle_lock_watchdog_test.go; session_evidence_test.go; provider instance tests.
- **Unique semantics introduced:** Active-session Doctor, auditable lifecycle owner metadata, safe bounded preemption, watchdog protection, preserve state until destroy is verified.
- **Current-main status:** These paid-compute safety semantics are in current main via #116. No unverifiable process owner is signaled.
- **Replacement / where it lives:** #116 merge ca2f24b; exact push CI run 35851717414.
- **Dependencies:** Stacks directly on open PR #74 branch `fix/ninfer-artifact-revision-size` at observed base SHA `800b6d1688d3e720f5faca904e2db1e44272d023`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Branch is based on #74 and includes stale provider API/test code; do not merge the full stack.
- **Associated live evidence / incidents:** 2026-09-06 BOOTING session blocked down behind lifecycle lock; user requested prioritize P0 paid-compute safety.
- **Disposition:** **SUPERSEDED — close after ledger; #116 is the clean current-main port.**

### #77 — deep: phase-aware reasoning (xhigh planning/review, medium execution) + living action plan

- **PR / exact head:** [#77](https://github.com/Marguelgtz/Stint/pull/77), `feat/deep-work-phase-reasoning` at `705a2046fd2b068ffecb8e4745e39fbb0fee4d8b`.
- **Base:** `docs/cp1-dryrun-records` at observed base SHA `e78ceef308d85c9cac7c71e7d172bed7c66c4182`.
- **State / mergeability:** OPEN; ready for review; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS.
- **Original intent:** Select xhigh for planning/review and medium for execution, while recording a living Deep Work action plan.
- **Actual changed files (14 returned):** <details><summary>show complete path list</summary>

`cmd/missionparse/main.go`, `cmd/stint/deep_executor.go`, `cmd/stint/deep_loop.go`, `cmd/stint/deep_remote.go`, `cmd/stint/deep_remote_test.go`, `cmd/stint/deep_resume.go`, `cmd/stint/deep_run.go`, `cmd/stint/deep_start.go`, `internal/deep/context.go`, `internal/deep/deep_test.go`, `internal/deep/mission.go`, `internal/deep/reasoning.go`, `internal/deep/state.go`, `internal/deep/task.go`

</details>
- **Important changed symbols:** reasoningRoute; resolveTaskReasoning; executor input planning/review phases.
- **Tests added or modified:** deep_remote_test.go; deep_test.go; mission parsing tests.
- **Unique semantics introduced:** Phase-aware reasoning route prevents spending xhigh output budget on tool-heavy execution; phase plans preserve current coordination intent.
- **Current-main status:** Current Hermes-on-box Deep Work has phase-aware xhigh planning/review and medium execution routing.
- **Replacement / where it lives:** #107 merge 2041498 (head 7adf523).
- **Dependencies:** Stacks directly on open PR #73 branch `docs/cp1-dryrun-records` at observed base SHA `e78ceef308d85c9cac7c71e7d172bed7c66c4182`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Old deep branch modifies earlier worker path; preserve only route semantics in current Hermes implementation.
- **Associated live evidence / incidents:** P5 run showed VANTA-002 exhausted output budget using xhigh on tool-heavy attempts.
- **Disposition:** **SUPERSEDED — close after ledger; current route semantics live in #107-derived main.**

### #78 — deep: harden CP1 run config and verify phase wire

- **PR / exact head:** [#78](https://github.com/Marguelgtz/Stint/pull/78), `feat/deep-work-run-config` at `bf3c95bfddef7d1441403ea93c361e5091556f3e`.
- **Base:** `feat/deep-work-phase-reasoning` at observed base SHA `705a2046fd2b068ffecb8e4745e39fbb0fee4d8b`.
- **State / mergeability:** OPEN; ready for review; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS.
- **Original intent:** Harden phase selection through Hermes provider overrides and raise CP1 execution profile/budget.
- **Actual changed files (10 returned):** <details><summary>show complete path list</summary>

`cmd/stint/deep_handoff.go`, `cmd/stint/help.go`, `cmd/stint/ninfer_dual_lane_launch_test.go`, `cmd/stint/resumable_start.go`, `cmd/stint/runtime.go`, `docs/DEEP_WORK_GPU_HERMES_PLAN.md`, `scripts/box-phase-setup.sh`, `scripts/box-smoke.sh`, `scripts/phaseproxy.py`, `scripts/provision-box.sh`

</details>
- **Important changed symbols:** phaseproxy provider override; dual-lane runtime setup; resume execution settings.
- **Tests added or modified:** ninfer_dual_lane_launch_test.go; scripts/box-smoke.sh; phaseproxy.py contract checks.
- **Unique semantics introduced:** Custom-provider entries sharing one base URL can apply the wrong effort; phase routing must carry explicit model/provider identity. Also documents bounded 262k/dual-lane smoke setup.
- **Current-main status:** Current Hermes configuration and Deep Work phases are carried on main; production routing should follow current phase/provider configuration, not old smoke override files.
- **Replacement / where it lives:** #107 merge 2041498 and current runtime configuration.
- **Dependencies:** Stacks directly on open PR #77 branch `feat/deep-work-phase-reasoning` at observed base SHA `705a2046fd2b068ffecb8e4745e39fbb0fee4d8b`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Historical smoke scripts/prompt and CP1 configuration are not production runtime policy; preserve as experiment evidence only.
- **Associated live evidence / incidents:** CP1 Hermes GPU phase-routing dry-run evidence; phase route semantics in #77.
- **Disposition:** **SUPERSEDED — close after ledger; current implementation is on main, old smoke setup is historical.**

### #79 — feat: add Deep Work execution dashboard

- **PR / exact head:** [#79](https://github.com/Marguelgtz/Stint/pull/79), `docs/deep-work-dashboard-action-plan` at `4c8e56561a0a56f6f2f65a0c9d5ed98e834739d7`.
- **Base:** `feat/deep-work-run-config` at observed base SHA `bf3c95bfddef7d1441403ea93c361e5091556f3e`.
- **State / mergeability:** OPEN; ready for review; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Add a Deep Work terminal cockpit with run/task/activity/worker views, safe latest-session landing and content-free GPU observer.
- **Actual changed files (32 returned):** <details><summary>show complete path list</summary>

`cmd/stint/deep_dashboard.go`, `cmd/stint/deep_dashboard_test.go`, `cmd/stint/deep_onbox.go`, `cmd/stint/deep_remote.go`, `cmd/stint/deep_remote_test.go`, `cmd/stint/deep_run.go`, `cmd/stint/deep_start.go`, `cmd/stint/help.go`, `cmd/stint/runtime.go`, `cmd/stint/runtime_test.go`, `deep-work/COMPRESSION_SMOKE_MISSION.md`, `deep-work/PHASE_LANE_E2E_MISSION.md`, `docs/DEEP_WORK.md`, `docs/DEEP_WORK_DASHBOARD_ACTION_PLAN.md`, `docs/DEEP_WORK_DASHBOARD_SMOKE_RESULT.md`, `docs/DEEP_WORK_GPU_HERMES_PLAN.md`, `docs/DEEP_WORK_ONBOX_EXECUTION_PLAN.md`, `internal/deep/state.go`, `internal/deepdashboard/render.go`, `internal/deepdashboard/render_test.go`, `scripts/box-phase-setup.sh`, `scripts/box-smoke.sh`, `scripts/deep-compression-smoke-box-setup.sh`, `scripts/deep-observe.sh`, `scripts/launch-onbox-deep.sh`, `scripts/onbox-deep-supervisor.sh`, `scripts/onbox-r2-archive.py`, `scripts/onbox-r2-sync.py`, `scripts/phase-lane-concurrency-smoke.sh`, `scripts/phaseproxy.py`, `scripts/run-deep-compression-smoke.sh`, `scripts/run-onbox-deep-smoke.sh`

</details>
- **Important changed symbols:** runDeepDashboard; deepDashboardController; deepdash.Render; loadDeepDashboardSnapshot.
- **Tests added or modified:** deep_dashboard_test.go; internal/deepdashboard/render_test.go; remote worker tests; observer JSON contract/shell tests.
- **Unique semantics introduced:** Run/tasks/activity/worker views, passive compute/worker telemetry, content-free GPU observation and confirmation-gated latest-session landing; remaining gap was phase/checkpoint/landing evidence and historical-session disable.
- **Current-main status:** Current Hermes Deep Dashboard preserves the useful views/observer; #118 adds dedicated Phase view, task verifier/checkpoint SHA, landing evidence, historical labeling and stricter latest bound READY compute guard.
- **Replacement / where it lives:** #107 merge 2041498 for current architecture plus #118 merge 6f4c81f; #118 local tests and exact CI 35856677088 passed.
- **Dependencies:** Stacks directly on open PR #78 branch `feat/deep-work-run-config` at observed base SHA `bf3c95bfddef7d1441403ea93c361e5091556f3e`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Historical branch is stale/large and includes generated smoke plans/results and scripts; do not import Cline event models or generated evidence.
- **Associated live evidence / incidents:** Current successful run #110–#113 remains open/unmerged; observer is sanitized/content-free.
- **Disposition:** **SUPERSEDED — close after ledger; current Deep Dashboard plus #118 covers the missing useful semantics.**

### #80 — feat: publish Deep Work checkpoints from the GPU

- **PR / exact head:** [#80](https://github.com/Marguelgtz/Stint/pull/80), `fix/deep-work-onbox-publishing` at `62f89ddcd0b98962f6b5f7c0d1360ac43e5ff3a6`.
- **Base:** `docs/deep-work-dashboard-action-plan` at observed base SHA `4c8e56561a0a56f6f2f65a0c9d5ed98e834739d7`.
- **State / mergeability:** OPEN; draft; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Have GPU-owned Deep Work publish exact-source checkpoints/handoff with least-privilege credentials and archive/sync support.
- **Actual changed files (17 returned):** <details><summary>show complete path list</summary>

`cmd/stint/deep_landing.go`, `cmd/stint/deep_loop.go`, `cmd/stint/deep_loop_test.go`, `cmd/stint/deep_onbox.go`, `cmd/stint/deep_onbox_test.go`, `cmd/stint/deep_remote.go`, `deep-work/PHASE_LANE_E2E_PLAN_SEED.md`, `docs/DEEP_WORK_ONBOX_EXECUTION_PLAN.md`, `docs/ONBOX_DEEP_SMOKE_20260908_REPORT.md`, `internal/deep/task.go`, `scripts/box-smoke.sh`, `scripts/launch-onbox-deep.sh`, `scripts/onbox-deep-supervisor.sh`, `scripts/onbox-github-publish.py`, `scripts/onbox-r2-archive.py`, `scripts/onbox-r2-sync.py`, `scripts/run-onbox-deep-smoke.sh`

</details>
- **Important changed symbols:** onbox-github-publish.py; launch-onbox-deep.sh; supervisor completion gates; deepCoordinator landing/checkpoint flow.
- **Tests added or modified:** test_onbox_github_publish.py; on-box supervisor completion archive workflow checks; shell syntax/smoke checks.
- **Unique semantics introduced:** Detached compute-owned publisher, clean exact-HEAD source clone, mission-policy-bound credentials, verified task checkpoints/handoff and configured R2 archive.
- **Current-main status:** Current main has Hermes-on-box detached supervisor, checkpoint publisher, policy-bound authority, resumable landing and verified teardown; current successful run exercised #110–#113.
- **Replacement / where it lives:** #107 merge 2041498 (head 7adf523) and live evidence session 20260923-022052.
- **Dependencies:** Stacks directly on open PR #79 branch `docs/deep-work-dashboard-action-plan` at observed base SHA `4c8e56561a0a56f6f2f65a0c9d5ed98e834739d7`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** This branch’s base is #79; re-merging risks reverting later hardening and mixes generated run material with implementation.
- **Associated live evidence / incidents:** Successful #110–#113 exact checkpoint/handoff SHAs in this ledger.
- **Disposition:** **SUPERSEDED — close after ledger; current architecture is already on main.**

### #81 — deep: 20260908-194552 PLAN-001

- **PR / exact head:** [#81](https://github.com/Marguelgtz/Stint/pull/81), `stint/deep-20260908-194552-01-plan-001` at `b7280a22d1e48c976c3bbaa4617eb1e104fb92aa`.
- **Base:** `fix/deep-work-onbox-publishing` at observed base SHA `85423b76d14c6bf702097c3bc3a7b475c513e8eb`.
- **State / mergeability:** CLOSED, unmerged on 2026-09-23T12:17:41Z; draft at close.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** GPU-owned successful Deep Work checkpoint PLAN-001.
- **Actual changed files (1 returned):** <details><summary>show complete path list</summary>

`deep-work/phase-plan.md`

</details>
- **Important changed symbols:** None (generated phase-plan content).
- **Tests added or modified:** The attached legacy PR checks passed on the synthetic merge tree; exact-head validation was not separately established. Generated output is not product test coverage.
- **Unique semantics introduced:** Successful 2026-09-08 session 20260908-194552 plan checkpoint.
- **Current-main status:** Generated output must remain outside product main; evidence is represented by this ledger and source PRs remain inspectable.
- **Replacement / where it lives:** Not code-replaced; preserved as historical evidence, with newer successful session #110–#113 still open.
- **Dependencies:** Was stacked directly on PR #80 branch `fix/deep-work-onbox-publishing` at observed base SHA `85423b76d14c6bf702097c3bc3a7b475c513e8eb`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Generated checkpoint branch is stacked on old publisher chain #80; never merge into product main.
- **Associated live evidence / incidents:** Session 20260908-194552; verified commit/head b7280a22d1e48c976c3bbaa4617eb1e104fb92aa.
- **Disposition:** **CLOSED UNMERGED — EVIDENCE ONLY; retained in PR history and this ledger.**

### #82 — deep: 20260908-194552 PHASE-001

- **PR / exact head:** [#82](https://github.com/Marguelgtz/Stint/pull/82), `stint/deep-20260908-194552-02-phase-001` at `aae3dce18010bd8f63fb0864a1fdb8b722af1d1a`.
- **Base:** `stint/deep-20260908-194552-01-plan-001` at observed base SHA `b7280a22d1e48c976c3bbaa4617eb1e104fb92aa`.
- **State / mergeability:** CLOSED, unmerged on 2026-09-23T12:17:43Z; draft at close.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** GPU-owned successful Deep Work checkpoint PHASE-001.
- **Actual changed files (2 returned):** <details><summary>show complete path list</summary>

`deep-work/phase-plan.md`, `phase-lane-smoke/medium-ready.txt`

</details>
- **Important changed symbols:** None (generated phase-plan and medium-ready evidence).
- **Tests added or modified:** The attached legacy PR checks passed on the synthetic merge tree; exact-head validation was not separately established. Generated output is not product test coverage.
- **Unique semantics introduced:** Successful 2026-09-08 session 20260908-194552 PHASE-001; recorded exactly “medium execution ready”.
- **Current-main status:** Generated output stays outside product main; evidence and route behavior are preserved separately.
- **Replacement / where it lives:** Historical evidence only; current #110–#113 is the preferred live evidence.
- **Dependencies:** Was stacked directly on PR #81 branch `stint/deep-20260908-194552-01-plan-001` at observed base SHA `b7280a22d1e48c976c3bbaa4617eb1e104fb92aa`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Generated output stacked on #81; never merge.
- **Associated live evidence / incidents:** Session 20260908-194552; checkpoint aae3dce18010bd8f63fb0864a1fdb8b722af1d1a; `phase-lane-smoke/medium-ready.txt`.
- **Disposition:** **CLOSED UNMERGED — EVIDENCE ONLY; retained in PR history and this ledger.**

### #83 — deep: 20260908-194552 PHASE-002

- **PR / exact head:** [#83](https://github.com/Marguelgtz/Stint/pull/83), `stint/deep-20260908-194552-03-phase-002` at `2beeef207af2fa7f5d0809dbd323a27bc76b6f27`.
- **Base:** `stint/deep-20260908-194552-02-phase-001` at observed base SHA `aae3dce18010bd8f63fb0864a1fdb8b722af1d1a`.
- **State / mergeability:** CLOSED, unmerged on 2026-09-23T12:17:45Z; draft at close.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** GPU-owned successful Deep Work checkpoint PHASE-002.
- **Actual changed files (2 returned):** <details><summary>show complete path list</summary>

`deep-work/phase-plan.md`, `phase-lane-smoke/final.txt`

</details>
- **Important changed symbols:** None (generated phase-plan and final-result evidence).
- **Tests added or modified:** The attached legacy PR checks passed on the synthetic merge tree; exact-head validation was not separately established. Generated output is not product test coverage.
- **Unique semantics introduced:** Successful 2026-09-08 session 20260908-194552 PHASE-002; recorded “phase lane e2e complete”.
- **Current-main status:** Generated output stays outside product main; implementation remains current on main.
- **Replacement / where it lives:** Historical evidence only; current #110–#113 is preferred.
- **Dependencies:** Was stacked directly on PR #82 branch `stint/deep-20260908-194552-02-phase-001` at observed base SHA `aae3dce18010bd8f63fb0864a1fdb8b722af1d1a`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Generated output stacked on #82; never merge.
- **Associated live evidence / incidents:** Session 20260908-194552; checkpoint 2beeef207af2fa7f5d0809dbd323a27bc76b6f27; `phase-lane-smoke/final.txt`.
- **Disposition:** **CLOSED UNMERGED — EVIDENCE ONLY; retained in PR history and this ledger.**

### #84 — deep: 20260908-194552 handoff

- **PR / exact head:** [#84](https://github.com/Marguelgtz/Stint/pull/84), `stint/deep-20260908-194552-handoff` at `df69d4a9c736790a20c29b9c2c743df5127af823`.
- **Base:** `stint/deep-20260908-194552-03-phase-002` at observed base SHA `2beeef207af2fa7f5d0809dbd323a27bc76b6f27`.
- **State / mergeability:** CLOSED, unmerged on 2026-09-23T12:17:47Z; draft at close.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** GPU-owned final handoff for successful Deep Work session 20260908-194552.
- **Actual changed files (1 returned):** <details><summary>show complete path list</summary>

`DEEP_WORK_HANDOFF.md`

</details>
- **Important changed symbols:** None (generated handoff).
- **Tests added or modified:** The attached legacy PR checks passed on the synthetic merge tree; exact-head validation was not separately established. Generated output is not product test coverage.
- **Unique semantics introduced:** Final successful historical handoff for the 2026-09-08 run.
- **Current-main status:** Handoff evidence remains in PR history and this ledger; not product source.
- **Replacement / where it lives:** Historical evidence only; newer session #110–#113 stays open.
- **Dependencies:** Was stacked directly on PR #83 branch `stint/deep-20260908-194552-03-phase-002` at observed base SHA `2beeef207af2fa7f5d0809dbd323a27bc76b6f27`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Generated output stacked on #83; never merge.
- **Associated live evidence / incidents:** Session 20260908-194552; final handoff commit df69d4a9c736790a20c29b9c2c743df5127af823.
- **Disposition:** **CLOSED UNMERGED — EVIDENCE ONLY; retained in PR history and this ledger.**

### #85 — feat: add policy-driven Deep Work GitHub maintenance

- **PR / exact head:** [#85](https://github.com/Marguelgtz/Stint/pull/85), `feat/deep-work-github-modes` at `dd034898cc1c89138bfc18ddc47e7c3dfc6b34e5`.
- **Base:** `fix/deep-work-onbox-publishing` at observed base SHA `62f89ddcd0b98962f6b5f7c0d1360ac43e5ff3a6`.
- **State / mergeability:** OPEN; draft; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Add typed maintenance modes and GPU-owned PR inventory/review/update/merge operations with action audit.
- **Actual changed files (33 returned):** <details><summary>show complete path list</summary>

`Makefile`, `cmd/missionparse/main.go`, `cmd/stint/deep_dashboard.go`, `cmd/stint/deep_handoff.go`, `cmd/stint/deep_landing.go`, `cmd/stint/deep_loop.go`, `cmd/stint/deep_loop_test.go`, `cmd/stint/deep_onbox.go`, `cmd/stint/deep_remote.go`, `cmd/stint/deep_resume.go`, `cmd/stint/deep_start.go`, `cmd/stint/deep_status.go`, `cmd/stint/help.go`, `cmd/stint/main.go`, `cmd/stint/version.go`, `cmd/stint/version_test.go`, `deep-work/STINT_PR_MAINTENANCE_MISSION.md`, `docs/DEEP_WORK.md`, `docs/DEEP_WORK_ONBOX_EXECUTION_PLAN.md`, `docs/VERSIONING.md`, `internal/deep/context.go`, `internal/deep/deep_test.go`, `internal/deep/mission.go`, `internal/deep/policy_types.go`, `internal/deep/state.go`, `internal/deep/state_persist.go`, `internal/deep/task.go`, `internal/deepdashboard/render.go`, `scripts/launch-onbox-deep.sh`, `scripts/onbox-deep-supervisor.sh`, `scripts/onbox-github-publish.py`, `scripts/onbox-r2-archive.py`, `scripts/test_onbox_github_publish.py`

</details>
- **Important changed symbols:** mission GitHub policy types; maintenance task loop; SHA-locked merge gates; publisher action audit.
- **Tests added or modified:** deep_loop_test.go; deep_test.go; test_onbox_github_publish.py.
- **Unique semantics introduced:** High-authority autonomous GitHub maintenance: inventory, review replies, PR changes, merge queue/gates and append-only action log.
- **Current-main status:** Not part of current production Deep Work. Current authority remains mission-policy-bound engineering checkpoint/handoff publication.
- **Replacement / where it lives:** No replacement; keep separate until pagination, base authority, policy/environment agreement and complete merge evidence are independently redesigned.
- **Dependencies:** Stacks directly on open PR #80 branch `fix/deep-work-onbox-publishing` at observed base SHA `62f89ddcd0b98962f6b5f7c0d1360ac43e5ff3a6`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Risks generic push/base-main authority, incomplete collection boundaries, and policy vs environment mismatch. Do not merge wholesale.
- **Associated live evidence / incidents:** No validated maintenance-mode live run.
- **Disposition:** **EXPERIMENT / PARKED — keep open as a separate high-authority experiment.**

### #86 — deep: 20260909-012307 PLAN-001

- **PR / exact head:** [#86](https://github.com/Marguelgtz/Stint/pull/86), `stint/deep-20260909-012307-01-plan-001` at `bce93166f7302e2aae10453fd4eb0bd41c8bb9e9`.
- **Base:** `main` at observed base SHA `a4f162fee8155560e1054e70ca2c361177978793`.
- **State / mergeability:** CLOSED, unmerged on 2026-09-23T12:17:49Z; draft at close.
- **Legacy PR check rollup:** Spark Observability=NEUTRAL.
- **Original intent:** GPU-owned PLAN-001 checkpoint from failed autonomous-maintenance session.
- **Actual changed files (135 returned):** <details><summary>show complete path list</summary>

`.github/workflows/ci.yml`, `.github/workflows/ninfer-image.yml`, `.spark/profile.yml`, `README.md`, `STINT_DEEP_WORK_INVESTIGATION.md`, `cmd/stint/cache_reuse_test.go`, `cmd/stint/dash.go`, `cmd/stint/dashboard.go`, `cmd/stint/dashboard_benchmark.go`, `cmd/stint/dashboard_context.go`, `cmd/stint/dashboard_context_test.go`, `cmd/stint/dashboard_recovery.go`, `cmd/stint/dashboard_recovery_controller_test.go`, `cmd/stint/dashboard_recovery_test.go`, `cmd/stint/dashboard_test.go`, `cmd/stint/deadline_watchdog.go`, `cmd/stint/deadline_watchdog_test.go`, `cmd/stint/deep_events.go`, `cmd/stint/deep_executor.go`, `cmd/stint/deep_executor_test.go`, `cmd/stint/deep_handoff.go`, `cmd/stint/deep_landing.go`, `cmd/stint/deep_loop.go`, `cmd/stint/deep_loop_test.go`, `cmd/stint/deep_remote.go`, `cmd/stint/deep_remote_test.go`, `cmd/stint/deep_resume.go`, `cmd/stint/deep_resume_test.go`, `cmd/stint/deep_run.go`, `cmd/stint/deep_start.go`, `cmd/stint/deep_status.go`, `cmd/stint/help.go`, `cmd/stint/help_candidate_replenishment.go`, `cmd/stint/help_candidate_replenishment_test.go`, `cmd/stint/help_clients_test.go`, `cmd/stint/help_dashboard.go`, `cmd/stint/help_dashboard_test.go`, `cmd/stint/help_deadline.go`, `cmd/stint/help_deadline_test.go`, `cmd/stint/help_status_telemetry.go`, `cmd/stint/help_test.go`, `cmd/stint/infer_probe.go`, `cmd/stint/infer_probe_test.go`, `cmd/stint/lifecycle.go`, `cmd/stint/lifecycle_lock.go`, `cmd/stint/lifecycle_lock_test.go`, `cmd/stint/llama_fast_start_test.go`, `cmd/stint/main.go`, `cmd/stint/network_candidate_retry.go`, `cmd/stint/network_candidate_retry_test.go`, `cmd/stint/network_qualification.go`, `cmd/stint/network_qualification_test.go`, `cmd/stint/ninfer_clients.go`, `cmd/stint/ninfer_clients_test.go`, `cmd/stint/ninfer_config_test.go`, `cmd/stint/ninfer_dual_lane_launch_test.go`, `cmd/stint/ninfer_vast.go`, `cmd/stint/perf.go`, `cmd/stint/perf_prompt.go`, `cmd/stint/perf_test.go`, `cmd/stint/performance_store.go`, `cmd/stint/performance_store_test.go`, `cmd/stint/resumable_start.go`, `cmd/stint/resumable_start_test.go`, `cmd/stint/resume.go`, `cmd/stint/runtime.go`, `cmd/stint/runtime_surface_test.go`, `cmd/stint/runtime_test.go`, `cmd/stint/session_deadline.go`, `cmd/stint/session_deadline_test.go`, `cmd/stint/session_snapshot.go`, `cmd/stint/session_snapshot_test.go`, `cmd/stint/session_telemetry.go`, `cmd/stint/session_telemetry_test.go`, `cmd/stint/ssh_permissions_test.go`, `cmd/stint/startup_timing.go`, `cmd/stint/startup_timing_test.go`, `cmd/stint/status_context.go`, `cmd/stint/status_telemetry.go`, `cmd/stint/status_telemetry_test.go`, `cmd/stint/version.go`, `cmd/stint/version_test.go`, `deep-work/action-plan.md`, `docs/CLI.md`, `docs/CP1_DRYRUN1_INCIDENT.md`, `docs/CP1_DRYRUN_FINDINGS.md`, `docs/CP1_DRYRUN_MISSION.md`, `docs/DASHBOARD.md`, `docs/DEEP_WORK.md`, `docs/DEEP_WORK_GPU_HERMES_PLAN.md`, `docs/DEEP_WORK_MVP_EXECUTION.md`, `docs/DEEP_WORK_SETUP.md`, `docs/NINFER.md`, `docs/OPEN_PR_AUDIT_2026-09-07.md`, `docs/STINT_DEEP_WORK_VISION.md`, `docs/TELEMETRY.md`, `docs/VERSIONING.md`, `images/ninfer/Dockerfile`, `internal/collaboration/contracts.go`, `internal/core/plan.go`, `internal/dashboard/context.go`, `internal/dashboard/context_test.go`, `internal/dashboard/render.go`, `internal/dashboard/render_test.go`, `internal/dashboard/terminal.go`, `internal/dashboard/terminal_test.go`, `internal/deep/codec.go`, `internal/deep/context.go`, `internal/deep/coordinator_pid.go`, `internal/deep/deep_test.go`, `internal/deep/mission.go`, `internal/deep/policy.go`, `internal/deep/state.go`, `internal/deep/state_persist.go`, `internal/deep/task.go`, `internal/provider/vast/client.go`, `internal/provider/vast/client_test.go`, `internal/provider/vast/command.go`, `internal/provider/vast/cuda_policy.go`, `internal/provider/vast/cuda_policy_test.go`, `internal/provider/vast/instance.go`, `internal/provider/vast/instance_test.go`, `internal/provider/vast/offer_error.go`, `internal/provider/vast/offer_error_integration_test.go`, `internal/provider/vast/offer_error_test.go`, `internal/router/profile.go`, `internal/runtime/llama/config.go`, `internal/session/deadline.go`, `internal/session/deadline_test.go`, `internal/session/state.go`, `internal/session/state_clients_test.go`, `internal/spark/onboard.go`, `internal/spark/onboard_test.go`, `scripts/box-smoke.sh`, `scripts/provision-box.sh`

</details>
- **Important changed symbols:** Representative symbols in the copied source snapshot: `runDashboard`; `dashboardClientContexts`; `runDynamicWatchdog`; `deepCoordinator.run`; `runDeepStart`; `runDeepDashboard`; `runDeepOnBox`; session PR itself records a generated workspace checkpoint, not a code-reviewed landing candidate.
- **Tests added or modified:** Test files included in the generated workspace snapshot (not session acceptance coverage): `cmd/stint/cache_reuse_test.go`, `cmd/stint/dashboard_context_test.go`, `cmd/stint/dashboard_recovery_controller_test.go`, `cmd/stint/dashboard_recovery_test.go`, `cmd/stint/dashboard_test.go`, `cmd/stint/deadline_watchdog_test.go`, `cmd/stint/deep_executor_test.go`, `cmd/stint/deep_loop_test.go`, `cmd/stint/deep_remote_test.go`, `cmd/stint/deep_resume_test.go`, `cmd/stint/help_candidate_replenishment_test.go`, `cmd/stint/help_clients_test.go`, `cmd/stint/help_dashboard_test.go`, `cmd/stint/help_deadline_test.go`, `cmd/stint/help_test.go`, `cmd/stint/infer_probe_test.go`, `cmd/stint/lifecycle_lock_test.go`, `cmd/stint/llama_fast_start_test.go`, `cmd/stint/network_candidate_retry_test.go`, `cmd/stint/network_qualification_test.go`, `cmd/stint/ninfer_clients_test.go`, `cmd/stint/ninfer_config_test.go`, `cmd/stint/ninfer_dual_lane_launch_test.go`, `cmd/stint/perf_test.go`, `cmd/stint/performance_store_test.go`, `cmd/stint/resumable_start_test.go`, `cmd/stint/runtime_surface_test.go`, `cmd/stint/runtime_test.go`, `cmd/stint/session_deadline_test.go`, `cmd/stint/session_snapshot_test.go`, `cmd/stint/session_telemetry_test.go`, `cmd/stint/ssh_permissions_test.go`, `cmd/stint/startup_timing_test.go`, `cmd/stint/status_telemetry_test.go`, `cmd/stint/version_test.go`, `internal/dashboard/context_test.go`, `internal/dashboard/render_test.go`, `internal/dashboard/terminal_test.go`, `internal/deep/deep_test.go`, `internal/provider/vast/client_test.go`, `internal/provider/vast/cuda_policy_test.go`, `internal/provider/vast/instance_test.go`, `internal/provider/vast/offer_error_integration_test.go`, `internal/provider/vast/offer_error_test.go`, `internal/session/deadline_test.go`, `internal/session/state_clients_test.go`, `internal/spark/onboard_test.go`. The PR check rollup is recorded above; the live session failure is recorded below.
- **Unique semantics introduced:** The PR is a generated 135-file workspace checkpoint plus session plan, not a single-purpose implementation. Source snapshots include old Cline worker/events, a GHCR image experiment, old dashboard context files and maintenance/version helpers. Current production semantics were audited separately and live in #107/#116/#117; Cline/GHCR/maintenance portions remain retired or parked. The snapshot itself adds no distinct accepted product behavior.
- **Current-main status:** Source semantics are split: Hermes Deep Work is in #107, lifecycle safety in #116, and current dashboard/lane semantics in #117. Cline event/worker code, old GHCR image files and #85 maintenance/version surfaces are intentionally absent; generated session files stay out of product main.
- **Replacement / where it lives:** Not code-replaced; failure is durably classified here. New successful session is #110–#113.
- **Dependencies:** Targets `main` at observed base SHA `a4f162fee8155560e1054e70ca2c361177978793`; compare against current main because this base ref is stale.
- **Conflicts with current architecture:** Snapshot contains a broad unmerged source/docs tree on stale main base; generated output is not an implementation PR.
- **Associated live evidence / incidents:** Session 20260909-012307; plan SHA bce93166f7302e2aae10453fd4eb0bd41c8bb9e9. Attempt used unknown `custom:qwen-stint-xhigh/medium` models and lacked `go` on the box.
- **Disposition:** **CLOSED UNMERGED — EVIDENCE ONLY; retained in PR history and this ledger.**

### #87 — deep: 20260909-012307 handoff

- **PR / exact head:** [#87](https://github.com/Marguelgtz/Stint/pull/87), `stint/deep-20260909-012307-handoff` at `55ad3e8ccc323a9f6858b889808ad094201ee3bd`.
- **Base:** `stint/deep-20260909-012307-01-plan-001` at observed base SHA `bce93166f7302e2aae10453fd4eb0bd41c8bb9e9`.
- **State / mergeability:** CLOSED, unmerged on 2026-09-23T12:17:51Z; draft at close.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=FAILURE, Spark Observability=NEUTRAL.
- **Original intent:** Final GPU-owned handoff for failed autonomous-maintenance session 20260909-012307.
- **Actual changed files (1 returned):** <details><summary>show complete path list</summary>

`DEEP_WORK_HANDOFF.md`

</details>
- **Important changed symbols:** None (generated handoff).
- **Tests added or modified:** build-check failed on the legacy synthetic merge tree; the other attached checks passed there, but exact-head validation was not separately established and overall required CI failed.
- **Unique semantics introduced:** Failed session handoff records unsuccessful attempt.
- **Current-main status:** Evidence only; never product code.
- **Replacement / where it lives:** Not code-replaced; failure/handoff remains inspectable in PR history and ledger.
- **Dependencies:** Was stacked directly on PR #86 branch `stint/deep-20260909-012307-01-plan-001` at observed base SHA `bce93166f7302e2aae10453fd4eb0bd41c8bb9e9`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Stacked on failed #86; do not merge.
- **Associated live evidence / incidents:** Session 20260909-012307; handoff SHA 55ad3e8ccc323a9f6858b889808ad094201ee3bd; `go` missing and model aliases unknown.
- **Disposition:** **CLOSED UNMERGED — EVIDENCE ONLY; retained in PR history and this ledger.**

### #88 — deep: 20260909-014522 PLAN-001

- **PR / exact head:** [#88](https://github.com/Marguelgtz/Stint/pull/88), `stint/deep-20260909-014522-01-plan-001` at `f8cd1cbf519d108672f7757b46a465f607c086b5`.
- **Base:** `main` at observed base SHA `a4f162fee8155560e1054e70ca2c361177978793`.
- **State / mergeability:** CLOSED, unmerged on 2026-09-23T12:17:53Z; draft at close.
- **Legacy PR check rollup:** Spark Observability=NEUTRAL.
- **Original intent:** GPU-owned PLAN-001 checkpoint from second failed autonomous-maintenance session.
- **Actual changed files (135 returned):** <details><summary>show complete path list</summary>

`.github/workflows/ci.yml`, `.github/workflows/ninfer-image.yml`, `.spark/profile.yml`, `README.md`, `STINT_DEEP_WORK_INVESTIGATION.md`, `cmd/stint/cache_reuse_test.go`, `cmd/stint/dash.go`, `cmd/stint/dashboard.go`, `cmd/stint/dashboard_benchmark.go`, `cmd/stint/dashboard_context.go`, `cmd/stint/dashboard_context_test.go`, `cmd/stint/dashboard_recovery.go`, `cmd/stint/dashboard_recovery_controller_test.go`, `cmd/stint/dashboard_recovery_test.go`, `cmd/stint/dashboard_test.go`, `cmd/stint/deadline_watchdog.go`, `cmd/stint/deadline_watchdog_test.go`, `cmd/stint/deep_events.go`, `cmd/stint/deep_executor.go`, `cmd/stint/deep_executor_test.go`, `cmd/stint/deep_handoff.go`, `cmd/stint/deep_landing.go`, `cmd/stint/deep_loop.go`, `cmd/stint/deep_loop_test.go`, `cmd/stint/deep_remote.go`, `cmd/stint/deep_remote_test.go`, `cmd/stint/deep_resume.go`, `cmd/stint/deep_resume_test.go`, `cmd/stint/deep_run.go`, `cmd/stint/deep_start.go`, `cmd/stint/deep_status.go`, `cmd/stint/help.go`, `cmd/stint/help_candidate_replenishment.go`, `cmd/stint/help_candidate_replenishment_test.go`, `cmd/stint/help_clients_test.go`, `cmd/stint/help_dashboard.go`, `cmd/stint/help_dashboard_test.go`, `cmd/stint/help_deadline.go`, `cmd/stint/help_deadline_test.go`, `cmd/stint/help_status_telemetry.go`, `cmd/stint/help_test.go`, `cmd/stint/infer_probe.go`, `cmd/stint/infer_probe_test.go`, `cmd/stint/lifecycle.go`, `cmd/stint/lifecycle_lock.go`, `cmd/stint/lifecycle_lock_test.go`, `cmd/stint/llama_fast_start_test.go`, `cmd/stint/main.go`, `cmd/stint/network_candidate_retry.go`, `cmd/stint/network_candidate_retry_test.go`, `cmd/stint/network_qualification.go`, `cmd/stint/network_qualification_test.go`, `cmd/stint/ninfer_clients.go`, `cmd/stint/ninfer_clients_test.go`, `cmd/stint/ninfer_config_test.go`, `cmd/stint/ninfer_dual_lane_launch_test.go`, `cmd/stint/ninfer_vast.go`, `cmd/stint/perf.go`, `cmd/stint/perf_prompt.go`, `cmd/stint/perf_test.go`, `cmd/stint/performance_store.go`, `cmd/stint/performance_store_test.go`, `cmd/stint/resumable_start.go`, `cmd/stint/resumable_start_test.go`, `cmd/stint/resume.go`, `cmd/stint/runtime.go`, `cmd/stint/runtime_surface_test.go`, `cmd/stint/runtime_test.go`, `cmd/stint/session_deadline.go`, `cmd/stint/session_deadline_test.go`, `cmd/stint/session_snapshot.go`, `cmd/stint/session_snapshot_test.go`, `cmd/stint/session_telemetry.go`, `cmd/stint/session_telemetry_test.go`, `cmd/stint/ssh_permissions_test.go`, `cmd/stint/startup_timing.go`, `cmd/stint/startup_timing_test.go`, `cmd/stint/status_context.go`, `cmd/stint/status_telemetry.go`, `cmd/stint/status_telemetry_test.go`, `cmd/stint/version.go`, `cmd/stint/version_test.go`, `deep-work/action-plan.md`, `docs/CLI.md`, `docs/CP1_DRYRUN1_INCIDENT.md`, `docs/CP1_DRYRUN_FINDINGS.md`, `docs/CP1_DRYRUN_MISSION.md`, `docs/DASHBOARD.md`, `docs/DEEP_WORK.md`, `docs/DEEP_WORK_GPU_HERMES_PLAN.md`, `docs/DEEP_WORK_MVP_EXECUTION.md`, `docs/DEEP_WORK_SETUP.md`, `docs/NINFER.md`, `docs/OPEN_PR_AUDIT_2026-09-07.md`, `docs/STINT_DEEP_WORK_VISION.md`, `docs/TELEMETRY.md`, `docs/VERSIONING.md`, `images/ninfer/Dockerfile`, `internal/collaboration/contracts.go`, `internal/core/plan.go`, `internal/dashboard/context.go`, `internal/dashboard/context_test.go`, `internal/dashboard/render.go`, `internal/dashboard/render_test.go`, `internal/dashboard/terminal.go`, `internal/dashboard/terminal_test.go`, `internal/deep/codec.go`, `internal/deep/context.go`, `internal/deep/coordinator_pid.go`, `internal/deep/deep_test.go`, `internal/deep/mission.go`, `internal/deep/policy.go`, `internal/deep/state.go`, `internal/deep/state_persist.go`, `internal/deep/task.go`, `internal/provider/vast/client.go`, `internal/provider/vast/client_test.go`, `internal/provider/vast/command.go`, `internal/provider/vast/cuda_policy.go`, `internal/provider/vast/cuda_policy_test.go`, `internal/provider/vast/instance.go`, `internal/provider/vast/instance_test.go`, `internal/provider/vast/offer_error.go`, `internal/provider/vast/offer_error_integration_test.go`, `internal/provider/vast/offer_error_test.go`, `internal/router/profile.go`, `internal/runtime/llama/config.go`, `internal/session/deadline.go`, `internal/session/deadline_test.go`, `internal/session/state.go`, `internal/session/state_clients_test.go`, `internal/spark/onboard.go`, `internal/spark/onboard_test.go`, `scripts/box-smoke.sh`, `scripts/provision-box.sh`

</details>
- **Important changed symbols:** Representative symbols in the copied source snapshot: `runDashboard`; `dashboardClientContexts`; `runDynamicWatchdog`; `deepCoordinator.run`; `runDeepStart`; `runDeepDashboard`; `runDeepOnBox`; session PR itself records a generated workspace checkpoint, not a code-reviewed landing candidate.
- **Tests added or modified:** Test files included in the generated workspace snapshot (not session acceptance coverage): `cmd/stint/cache_reuse_test.go`, `cmd/stint/dashboard_context_test.go`, `cmd/stint/dashboard_recovery_controller_test.go`, `cmd/stint/dashboard_recovery_test.go`, `cmd/stint/dashboard_test.go`, `cmd/stint/deadline_watchdog_test.go`, `cmd/stint/deep_executor_test.go`, `cmd/stint/deep_loop_test.go`, `cmd/stint/deep_remote_test.go`, `cmd/stint/deep_resume_test.go`, `cmd/stint/help_candidate_replenishment_test.go`, `cmd/stint/help_clients_test.go`, `cmd/stint/help_dashboard_test.go`, `cmd/stint/help_deadline_test.go`, `cmd/stint/help_test.go`, `cmd/stint/infer_probe_test.go`, `cmd/stint/lifecycle_lock_test.go`, `cmd/stint/llama_fast_start_test.go`, `cmd/stint/network_candidate_retry_test.go`, `cmd/stint/network_qualification_test.go`, `cmd/stint/ninfer_clients_test.go`, `cmd/stint/ninfer_config_test.go`, `cmd/stint/ninfer_dual_lane_launch_test.go`, `cmd/stint/perf_test.go`, `cmd/stint/performance_store_test.go`, `cmd/stint/resumable_start_test.go`, `cmd/stint/runtime_surface_test.go`, `cmd/stint/runtime_test.go`, `cmd/stint/session_deadline_test.go`, `cmd/stint/session_snapshot_test.go`, `cmd/stint/session_telemetry_test.go`, `cmd/stint/ssh_permissions_test.go`, `cmd/stint/startup_timing_test.go`, `cmd/stint/status_telemetry_test.go`, `cmd/stint/version_test.go`, `internal/dashboard/context_test.go`, `internal/dashboard/render_test.go`, `internal/dashboard/terminal_test.go`, `internal/deep/deep_test.go`, `internal/provider/vast/client_test.go`, `internal/provider/vast/cuda_policy_test.go`, `internal/provider/vast/instance_test.go`, `internal/provider/vast/offer_error_integration_test.go`, `internal/provider/vast/offer_error_test.go`, `internal/session/deadline_test.go`, `internal/session/state_clients_test.go`, `internal/spark/onboard_test.go`. The PR check rollup is recorded above; the live session failure is recorded below.
- **Unique semantics introduced:** The PR is a generated 135-file workspace checkpoint plus session plan, not a single-purpose implementation. Source snapshots include old Cline worker/events, a GHCR image experiment, old dashboard context files and maintenance/version helpers. Current production semantics were audited separately and live in #107/#116/#117; Cline/GHCR/maintenance portions remain retired or parked. The snapshot itself adds no distinct accepted product behavior.
- **Current-main status:** Source semantics are split: Hermes Deep Work is in #107, lifecycle safety in #116, and current dashboard/lane semantics in #117. Cline event/worker code, old GHCR image files and #85 maintenance/version surfaces are intentionally absent; generated session files stay out of product main.
- **Replacement / where it lives:** Not code-replaced; failure is recorded here; current preferred evidence is #110–#113.
- **Dependencies:** Targets `main` at observed base SHA `a4f162fee8155560e1054e70ca2c361177978793`; compare against current main because this base ref is stale.
- **Conflicts with current architecture:** Stale unmerged snapshot based on older main; not a source implementation candidate.
- **Associated live evidence / incidents:** Session 20260909-014522; plan SHA f8cd1cbf519d108672f7757b46a465f607c086b5. Attempt used unknown `custom:qwen-stint-xhigh/medium` model names and lacked `go` on the box.
- **Disposition:** **CLOSED UNMERGED — EVIDENCE ONLY; retained in PR history and this ledger.**

### #89 — deep: 20260909-014522 handoff

- **PR / exact head:** [#89](https://github.com/Marguelgtz/Stint/pull/89), `stint/deep-20260909-014522-handoff` at `9288b5ba50f0c41cf86549d2d225e26715def71d`.
- **Base:** `stint/deep-20260909-014522-01-plan-001` at observed base SHA `f8cd1cbf519d108672f7757b46a465f607c086b5`.
- **State / mergeability:** CLOSED, unmerged on 2026-09-23T12:17:56Z; draft at close.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=FAILURE, Spark Observability=NEUTRAL.
- **Original intent:** Final GPU-owned handoff for failed autonomous-maintenance session 20260909-014522.
- **Actual changed files (1 returned):** <details><summary>show complete path list</summary>

`DEEP_WORK_HANDOFF.md`

</details>
- **Important changed symbols:** None (generated handoff).
- **Tests added or modified:** build-check failed; other attached required Stint checks passed, but overall required CI failed.
- **Unique semantics introduced:** Failed session handoff for the second unsuccessful attempt.
- **Current-main status:** Evidence only; never product code.
- **Replacement / where it lives:** Not code-replaced; preserve PR history and this ledger.
- **Dependencies:** Was stacked directly on PR #88 branch `stint/deep-20260909-014522-01-plan-001` at observed base SHA `f8cd1cbf519d108672f7757b46a465f607c086b5`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Stacked on failed #88; do not merge.
- **Associated live evidence / incidents:** Session 20260909-014522; handoff SHA 9288b5ba50f0c41cf86549d2d225e26715def71d; missing `go`, unrecognized configured models.
- **Disposition:** **CLOSED UNMERGED — EVIDENCE ONLY; retained in PR history and this ledger.**

### #93 — docs: organize operational references and history

- **PR / exact head:** [#93](https://github.com/Marguelgtz/Stint/pull/93), `docs/reorganize-documentation` at `2fd8988ab195a05f20330b2f0d5475f4a1457f97`.
- **Base:** `main` at observed base SHA `74ef5db14e0fe4b0e8e9865baeb5a9321c5eb5fb`.
- **State / mergeability:** CLOSED, unmerged on 2026-09-23T19:51:25Z; ready for review at close.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, Spark Observability=NEUTRAL; exact-head validation was not established by the pre-#132 workflow.
- **Original intent:** Move flat docs into maintained guides, operations, planning, history and reference sections under docs/README.md.
- **Actual changed files (9 returned):** <details><summary>show complete path list</summary>

`README.md`, `docs/README.md`, `docs/guides/authentication.md`, `docs/guides/spark.md`, `docs/history/phase-1.md`, `docs/history/phase-2.md`, `docs/operations/next-stint.md`, `docs/planning/roadmap.md`, `docs/reference/architecture.md`

</details>
- **Important changed symbols:** None (documentation-only).
- **Tests added or modified:** No docs structure/link validation attached beyond the current required Stint checks.
- **Unique semantics introduced:** Proposed a docs index and guide/operations/planning/history/reference groups. The taxonomy was useful, but the proposed next-step, roadmap, and architecture content was stale Cline/pre-V0 guidance and required reconciliation.
- **Current-main status:** Selected taxonomy is on main through #133 merge `3586022bd6b2cc4565a4d94e519398222d85a443`: `docs/README.md`; guides at `docs/guides/`; preserved early-phase and pre-V0 pages under `docs/history/`; current operations, roadmap, and architecture at `docs/operations/next-stint.md`, `docs/planning/roadmap.md`, and `docs/reference/architecture.md`. The canonical plan, ledger, and handoff remain in `docs/` root.
- **Replacement / where it lives:** Replaced by #133, head `58e3a5832dbffdcf683665598e8c0d5335c34760`, exact-head PR CI `35911740797`, synthetic merge-tree SHA `2f349cf2c57e4cdc5e9809019de963558d0b3069`, and landed-main CI `35911926414`. Old Cline-era phases, `$0.60` discovery details, pre-V0 roadmap, and early architecture were moved under `docs/history/` with a historical notice instead of being carried as current instructions.
- **Dependencies:** Targets `main` at observed base SHA `74ef5db14e0fe4b0e8e9865baeb5a9321c5eb5fb`; compare against current main because this base ref is stale.
- **Conflicts with current architecture:** Its base `74ef5db` is stale. Its `next-stint.md` still plans first Cline-ready compute and includes an old `$0.60/hour` discovery description; roadmap/architecture pages describe pre-V0 states. These contents were archived and marked historical, while current pages were written from verified `main`.
- **Associated live evidence / incidents:** None. Replacement comment and closure evidence: [#93 issue comment](https://github.com/Marguelgtz/Stint/pull/93#issuecomment-5801853350); PR closed at `2026-09-23T19:51:25Z`.
- **Disposition:** **CLOSED AFTER SELECTED REPLACEMENT LANDED — taxonomy ported by #133; no stale content was merged.**

### #110 — deep: 20260923-022052 STINT-PLAN-001

- **PR / exact head:** [#110](https://github.com/Marguelgtz/Stint/pull/110), `stint/deep-20260923-022052-01-stint-plan-001` at `a051c94a0f02b5d65a4e5a4fb2b7b6ee86e892f4`.
- **Base:** `main` at observed base SHA `bdd55c57b7fbdb7a2128c8ed14861cf3f527c04d`.
- **State / mergeability:** OPEN; draft; `UNKNOWN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Successful current GPU-owned STINT-PLAN-001 checkpoint.
- **Actual changed files (1 returned):** <details><summary>show complete path list</summary>

`deep-work/phase-plan.md`

</details>
- **Important changed symbols:** None (generated phase-plan content).
- **Tests added or modified:** The attached legacy PR check rollup is green on the synthetic merge tree; exact-head validation is not separately established. Generated artifact remains outside main.
- **Unique semantics introduced:** Preferred successful live Deep Work evidence, session 20260923-022052.
- **Current-main status:** Keep externally inspectable and unmerged; do not copy generated output into product source.
- **Replacement / where it lives:** No replacement; this is the latest successful evidence.
- **Dependencies:** Targets `main` at observed base SHA `bdd55c57b7fbdb7a2128c8ed14861cf3f527c04d`; compare against current main because this base ref is stale.
- **Conflicts with current architecture:** Must remain an open draft PR despite stack ancestry.
- **Associated live evidence / incidents:** Session 20260923-022052; checkpoint a051c94a0f02b5d65a4e5a4fb2b7b6ee86e892f4.
- **Disposition:** **KEEP OPEN AS CURRENT EVIDENCE — explicit mission constraint.**

### #111 — deep: 20260923-022052 PHASE-001

- **PR / exact head:** [#111](https://github.com/Marguelgtz/Stint/pull/111), `stint/deep-20260923-022052-02-phase-001` at `f7349155788c9ec0b7a0086af9b2a8f66c704cce`.
- **Base:** `stint/deep-20260923-022052-01-stint-plan-001` at observed base SHA `a051c94a0f02b5d65a4e5a4fb2b7b6ee86e892f4`.
- **State / mergeability:** OPEN; draft; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Successful current GPU-owned PHASE-001 checkpoint.
- **Actual changed files (2 returned):** <details><summary>show complete path list</summary>

`deep-work/phase-plan.md`, `phase-lane-smoke/medium-ready.txt`

</details>
- **Important changed symbols:** None (generated phase-plan and medium-ready evidence).
- **Tests added or modified:** The attached legacy PR check rollup is green on the synthetic merge tree; exact-head validation is not separately established. Generated artifact remains outside main.
- **Unique semantics introduced:** Current session PHASE-001 checkpoint including medium execution readiness.
- **Current-main status:** Keep externally inspectable and unmerged.
- **Replacement / where it lives:** No replacement; part of latest live evidence chain.
- **Dependencies:** Stacks directly on open PR #110 branch `stint/deep-20260923-022052-01-stint-plan-001` at observed base SHA `a051c94a0f02b5d65a4e5a4fb2b7b6ee86e892f4`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Stacked on #110 and must remain draft/open.
- **Associated live evidence / incidents:** Session 20260923-022052; checkpoint f7349155788c9ec0b7a0086af9b2a8f66c704cce; `medium-ready` evidence.
- **Disposition:** **KEEP OPEN AS CURRENT EVIDENCE — explicit mission constraint.**

### #112 — deep: 20260923-022052 PHASE-002

- **PR / exact head:** [#112](https://github.com/Marguelgtz/Stint/pull/112), `stint/deep-20260923-022052-03-phase-002` at `553c20c49f8c798cbd1074b03870fb63e642ee7a`.
- **Base:** `stint/deep-20260923-022052-02-phase-001` at observed base SHA `f7349155788c9ec0b7a0086af9b2a8f66c704cce`.
- **State / mergeability:** OPEN; draft; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Successful current GPU-owned PHASE-002 checkpoint.
- **Actual changed files (2 returned):** <details><summary>show complete path list</summary>

`deep-work/phase-plan.md`, `phase-lane-smoke/final.txt`

</details>
- **Important changed symbols:** None (generated phase-plan and final-result evidence).
- **Tests added or modified:** The attached legacy PR check rollup is green on the synthetic merge tree; exact-head validation is not separately established. Generated artifact remains outside main.
- **Unique semantics introduced:** Current session PHASE-002 checkpoint; concurrent phase-lane work completed.
- **Current-main status:** Keep externally inspectable and unmerged.
- **Replacement / where it lives:** No replacement; part of latest live evidence chain.
- **Dependencies:** Stacks directly on open PR #111 branch `stint/deep-20260923-022052-02-phase-001` at observed base SHA `f7349155788c9ec0b7a0086af9b2a8f66c704cce`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Stacked on #111 and must remain draft/open.
- **Associated live evidence / incidents:** Session 20260923-022052; checkpoint 553c20c49f8c798cbd1074b03870fb63e642ee7a; `phase-lane-smoke/final.txt`.
- **Disposition:** **KEEP OPEN AS CURRENT EVIDENCE — explicit mission constraint.**

### #113 — deep: 20260923-022052 handoff

- **PR / exact head:** [#113](https://github.com/Marguelgtz/Stint/pull/113), `stint/deep-20260923-022052-handoff` at `7f7fd4344e9e470f19c1d2e95d75ec06357fbc00`.
- **Base:** `stint/deep-20260923-022052-03-phase-002` at observed base SHA `553c20c49f8c798cbd1074b03870fb63e642ee7a`.
- **State / mergeability:** OPEN; draft; `CLEAN`.
- **Legacy PR check rollup:** spark-profile=SUCCESS, go-vet=SUCCESS, unit-tests=SUCCESS, race-tests=SUCCESS, build-check=SUCCESS, Spark Observability=NEUTRAL.
- **Original intent:** Final handoff for successful current Deep Work session 20260923-022052.
- **Actual changed files (1 returned):** <details><summary>show complete path list</summary>

`DEEP_WORK_HANDOFF.md`

</details>
- **Important changed symbols:** None (generated handoff).
- **Tests added or modified:** The attached legacy PR check rollup is green on the synthetic merge tree; exact-head validation is not separately established. Generated artifact remains outside main.
- **Unique semantics introduced:** Latest successful run’s final handoff.
- **Current-main status:** Keep externally inspectable and unmerged.
- **Replacement / where it lives:** No replacement; this is the latest live evidence chain.
- **Dependencies:** Stacks directly on open PR #112 branch `stint/deep-20260923-022052-03-phase-002` at observed base SHA `553c20c49f8c798cbd1074b03870fb63e642ee7a`. Its upstream PR must be semantically replaced before treating this branch as independent.
- **Conflicts with current architecture:** Stacked on #112 and must remain draft/open.
- **Associated live evidence / incidents:** Session 20260923-022052; handoff SHA 7f7fd4344e9e470f19c1d2e95d75ec06357fbc00.
- **Disposition:** **KEEP OPEN AS CURRENT EVIDENCE — explicit mission constraint.**

## Final current open PR snapshot

Checked on 2026-09-23 at `20:12 UTC`, after PR #134 merged and before this
snapshot-refresh PR was opened. GitHub reported exactly five open PRs. The
attached legacy PR runs have five required checks green, but the pre-#132
workflow used the synthetic merge tree; **exact-head CI is not established**.
Keep #85 parked and leave #110–#113 open, unmerged, and untouched.

| PR | State / merge state | Head branch @ exact SHA | Base branch @ observed SHA | Legacy PR check rollup / exact-head CI | Disposition |
|---:|---|---|---|---|---|
| #85 | OPEN, draft / CLEAN | `feat/deep-work-github-modes` @ `dd034898cc1c89138bfc18ddc47e7c3dfc6b34e5` | `fix/deep-work-onbox-publishing` @ `62f89ddcd0b98962f6b5f7c0d1360ac43e5ff3a6` | Five required checks green in run `34299224074` on legacy synthetic merge tree; exact-head not established; Spark Observability skipped | Parked high-authority maintenance experiment; redesign authority, pagination, and policy gates separately |
| #110 | OPEN, draft / CLEAN | `stint/deep-20260923-022052-01-stint-plan-001` @ `a051c94a0f02b5d65a4e5a4fb2b7b6ee86e892f4` | `main` @ `bdd55c57b7fbdb7a2128c8ed14861cf3f527c04d` | Five required checks green in run `35810117675` on legacy synthetic merge tree; exact-head not established; Spark Observability skipped | Keep open as protected successful evidence |
| #111 | OPEN, draft / CLEAN | `stint/deep-20260923-022052-02-phase-001` @ `f7349155788c9ec0b7a0086af9b2a8f66c704cce` | #110 branch @ `a051c94a0f02b5d65a4e5a4fb2b7b6ee86e892f4` | Five required checks green in run `35810176615` on legacy synthetic merge tree; exact-head not established; Spark Observability skipped | Keep open as protected successful evidence |
| #112 | OPEN, draft / CLEAN | `stint/deep-20260923-022052-03-phase-002` @ `553c20c49f8c798cbd1074b03870fb63e642ee7a` | #111 branch @ `f7349155788c9ec0b7a0086af9b2a8f66c704cce` | Five required checks green in run `35810244081` on legacy synthetic merge tree; exact-head not established; Spark Observability skipped | Keep open as protected successful evidence |
| #113 | OPEN, draft / CLEAN | `stint/deep-20260923-022052-handoff` @ `7f7fd4344e9e470f19c1d2e95d75ec06357fbc00` | #112 branch @ `553c20c49f8c798cbd1074b03870fb63e642ee7a` | Five required checks green in run `35810248016` on legacy synthetic merge tree; exact-head not established; Spark Observability skipped | Keep open as protected successful evidence |
