# Stint repository grounding plan

Updated: 2026-09-23
Canonical execution state for the repository grounding mission. The semantic
inventory for open pull requests lives in
[`STINT_OPEN_PR_GROUNDING_LEDGER.md`](STINT_OPEN_PR_GROUNDING_LEDGER.md); the
concise recovery note lives in
[`STINT_REPOSITORY_GROUNDING_HANDOFF.md`](STINT_REPOSITORY_GROUNDING_HANDOFF.md).

## Verified starting point

- Repository: `Marguelgtz/Stint`, default branch `main`.
- Starting `origin/main`: `9634bf762a2dc9021747eb786db7fd23ccab84e9` (matches the
  requested approximate SHA).
- Source worktrees are isolated under `/tmp`; the canonical docs are being prepared on `docs/grounding-ledger-handoff-20260923` from current `main` `6f4c81f`. The original dirty checkout remains untouched.
- The user's original checkout is on `fix/p0-ninfer-session-safety` at
  `792bb508dfcd7d64e293359dfbed7b497b10dafa` and contains untracked notes,
  investigation material, smoke outputs, and a local `stint` binary. It has not
  been modified or cleaned.
- Baseline `go test -count=1 ./...` passes on starting `main`.
- Current `main` has the pinned NInfer source SHA
  `981b685ea2124fdaed023123d2e63fd29d529ab8`, model revision
  `18dfc887423fa5aabf3cb56fac41490e462b3fab`, model SHA-256
  `eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e`,
  RTX 4090 / CUDA 12.8 qualification, and native 262144 context support.
  Ordinary startup still clones and builds NInfer from source.
- Both dashboards are grounded on current `main`: ordinary runtime dashboard semantics landed in #117, and the Hermes Deep Dashboard phase/checkpoint/landing evidence landed in #118.
- CI behavior is verified: PR checks execute the synthetic merge tree for merge-candidate coverage; the post-merge push run tests the exact landed `main` SHA. #118 head `6deaf44` passed required checks in run `35856677088`, and exact landed SHA `6f4c81f` passed run `35856861212`.
- GitHub currently reports #110–#113 open, draft, and unmerged with all five
  required Stint checks successful. Their heads are the 2026-09-23 session
  `20260923-022052`; these PRs must remain open and untouched.
- The initial open-PR inventory contained 38 PRs. Safety replacement PR #116
  was merged without deleting its branch as commit
  `ca2f24b1a3058e57d7afee3cdebc2c3132033e9c`; the current open count is back to
  38. Exact heads, base branches, states, changed paths, and current check
  rollups have been captured from GitHub. Full semantic accounting and older
  evidence classification remain active work.
- PR #116 ported current-main lifecycle safety and active Doctor semantics from
  #67/#69/#76. Local full tests, race tests, vet, build, repository fixtures,
  and every PR check passed on head `d3ead04a8d23502b344e0e4c73ba0668d7a25028`
  (run `35851489511`). Its merge commit
  `ca2f24b1a3058e57d7afee3cdebc2c3132033e9c` passed all five CI jobs on exact
  `main` SHA in push run `35851717414`.
- CI behavior is grounded: PR run `35851489511` was attached to exact PR head
  `d3ead04`, while `actions/checkout` tested synthetic merge tree
  `c4b16b3` (base `9634bf7` + that head). The post-merge push run tested exact
  landing commit `ca2f24b`. The existing workflow retains merge-candidate
  coverage and verifies the landed SHA; no check or trigger reduction is needed.

## Invariants

- Never modify the dirty original checkout, delete its untracked files, reset
  unrelated branches, force-push `main`, or delete remote branches.
- Keep #110–#113 open and unmerged; generated Deep Work smoke outputs do not
  enter product `main`.
- Preserve current Hermes-on-box Deep Work, both dashboards, truthful NInfer
  lane semantics, and paid-compute lifecycle safety.
- Historical PR check success is not evidence that its changes belong on current
  `main`; compare semantics and exact head diffs.
- No paid GPU run while deterministic local failures exist. Do not raise offer,
  session, network, or candidate limits to find a host.
- Retain source-build as recovery unless the immutable release path clears the
  full promotion gate, including fresh RTX 4090 live acceptance.
- Mark a plan item `[x]` only when its acceptance evidence is recorded.

## Landed grounding slices

- PR #116 lifecycle safety: merge `ca2f24b1a3058e57d7afee3cdebc2c3132033e9c`; exact push run `35851717414` passed all five required jobs.
- PR #117 ordinary dashboard/telemetry: merge `9a672345c3a1887c7aac5e06a971320cd454495c`; PR exact-head run `35855252071` and push run `35855342269` passed all five required jobs.
- PR #118 Deep Dashboard phase evidence: merge `6f4c81f76118924dfb7b41fa6a85394b2796b399`; exact-head run `35856677088` and push run `35856861212` passed all five required jobs.
- Local verification for #117/#118 included full Go tests, race tests, vet, build, and whitespace checks. Dashboard details and semantic limits are recorded in the ledger.

## Living checkpoints

- [x] Recover repository identity, starting `main`, dirty-checkout boundary, and baseline test result.
- [x] Capture exact CI behavior and verify landed-SHA push checks for #116, #117 and #118.
- [x] Ground lifecycle/provider safety on current main through #116.
- [x] Ground ordinary dashboard, telemetry, cache/lane semantics and recovery against #56/#58/#60/#63/#64/#68/#69/#70 through #117.
- [x] Ground Hermes Deep Dashboard run/task/activity/worker views and port phase, checkpoint and landing evidence through #118; no Cline event model was restored.
- [~] Snapshot and write semantic records for every open PR. The full ledger/handoff are drafted on an isolated docs branch; finish path/semantic review, run checks, merge, then add the exact closure log.
- [~] Close superseded old implementation and generated-evidence PRs only after the ledger is merged; preserve #110–#113 open and unmerged.
- [ ] Audit current NInfer and Qwen upstream candidates and record a deliberate production tuple decision.
- [ ] Port startup timing and durable runtime-deployment provenance to current main; telemetry must not block lifecycle persistence.
- [ ] Build a current-main immutable GitHub Release runtime path for the selected tuple, with hard SHA/manifest/path-safe extraction/runtime checks and a pristine Vast-base fixture. Retain source-build as explicit recovery.
- [ ] Compare source-build and release startup critical paths with comparable RTX 4090 evidence only after deterministic tests, exact-head CI, bounded objective/spend and suitable offers are verified.
- [ ] Review `#48–#51`, #85 and #93 at final disposition: do not merge stale stacks; preserve or rework valid experiments/docs after current-source convergence.
- [ ] Reconcile `.spark/profile.yml` with current critical paths, including deep dashboard/runtime/scripts as applicable.
- [ ] Complete docs/history organization and final whole-repository audit; update this plan and handoff.

## Current blockers and decisions

- No blocker prevents deterministic local work. The NInfer tuple/release architecture has not yet been audited/implemented.
- No paid RTX 4090 comparison has been attempted. Keep the existing budget, offer and network limits unchanged; live qualification remains gated by the full promotion criteria.
- Current production source-build remains the recovery baseline. No prebuilt GitHub Release has been validated or selected.
- #110–#113 remain protected, open, draft and unmerged. Historical #81–#84 are correctly classified as successful; #86–#89 are failed runs for the model-alias and missing-`go` reasons recorded in the ledger.
- The original checkout and its untracked user artifacts remain untouched.
