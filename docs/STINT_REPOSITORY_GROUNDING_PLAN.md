# Stint repository grounding plan

Updated: 2026-09-23. This is the canonical execution record for the grounding
mission. The [PR ledger](STINT_OPEN_PR_GROUNDING_LEDGER.md) holds detailed
semantic accounting; the [handoff](STINT_REPOSITORY_GROUNDING_HANDOFF.md) is
the concise recovery point.

## Repository boundary and starting point

- Repository: `Marguelgtz/Stint`, default branch `main`.
- Starting main: `9634bf762a2dc9021747eb786db7fd23ccab84e9`.
- Current main before this docs update: `706c78ae60f996c957d5f9ae86dc10bb529b604e`.
- The user's original dirty checkout at `792bb508dfcd7d64e293359dfbed7b497b10dafa` and its untracked files/local binary remain untouched. Work used isolated `/tmp` worktrees.
- Current main preserves Hermes-on-box Deep Work, both dashboards, NInfer lane semantics, paid-session/provider safety, exact-head CI, and Spark path observation. See the ledger for historical source comparisons.

## Landed work and exact-main CI

Grounding and runtime slices merged after the starting point:

| PR | Merge SHA | Semantics | Exact landed-main CI |
| --- | --- | --- | --- |
| #116 | `ca2f24b1a3058e57d7afee3cdebc2c3132033e9c` | paid-session lifecycle/provider safety | `35851717414` |
| #117 | `9a672345c3a1887c7aac5e06a971320cd454495c` | ordinary dashboard, truthful lanes, freshness and recovery | `35855342269` |
| #118 | `6f4c81f76118924dfb7b41fa6a85394b2796b399` | Deep Dashboard phases/checkpoints/landing evidence | `35856861212` |
| #119 | `2f0da9a80d9f01fafc3f1d27af6bcc0fafafdf6e` | initial grounding plan/ledger/handoff | see GitHub run attached to merge |
| #120 | `8bf249d69db4126e49b0e8723ee8d2ed346cdbf5` | Spark path profile | see GitHub run attached to merge |
| #121 | `063067efac7fb996bb57a36abb57655dbf9ff71c` | best-effort durable startup phase events | `35867023267` |
| #122 | `cc2f259a0eb3599ec0dabdd9cafc0a5d7246e1e7` | selected runtime tuple and immutable bundle workflow | `35868625724` |
| #123 | `b3c029553358950e2fc9369be370a470d869763c` | source-builder runner setup, dual binaries, manifest/publisher fixes | `35871556847` |
| #124 | `706c78ae60f996c957d5f9ae86dc10bb529b604e` | opt-in, fail-closed release-bundle deployment/provenance | `35882955266` |

Each listed exact landed-main run completed all five required Stint jobs. #124
PR-head run `35882783534` also passed all five. PR #115 and #114 are part of
the starting main ancestry (`9634bf7`) and are not grounding merges.

## Runtime decision and current design

Selected first immutable release tuple:

- NInfer source: `81b68a20a9a0d9ab47d7e5838887c6d636ab76e0`.
- Qwen model repository revision: `18dfc887423fa5aabf3cb56fac41490e462b3fab`.
- Model artifact SHA-256: `eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e`; 18,210,531,328 bytes.
- NInfer v2, CUDA 12.8, SM89/RTX 4090; native 262,144 context, E8 4-bit KV and MTP3. Stint's supported launch concurrency is one or two clients.

The source pin is the latest explicitly recorded deployed 4090 port build after
upstream issue #9's planner-accounting fix. Upstream `rtx4090-port` is now five
commits ahead. Its newer `328d9aa82d0c4a6d3540b65dfca7f41c8dec0cc4` fixes a
vision per-item cap and has an upstream regression test/reproduction; Stint is
not fast-forwarding the production pin without full acceptance. Track that as
a follow-up. Qwen v3 is a 20.4 GB container for RTX 5090/CUDA 13.1 and needs
NInfer 98dada or later; the matching 4090 port catch-up has not landed, so v3
is not an SM89 candidate. DFlash2 was rejected for this first tuple: upstream
4090 notes report it does not fit native 262K context at K=3, including the
vision case, and its deterministic output differs from MTP3. These upstream
observations are not Stint performance claims.

PR #124 keeps source-build as the default and explicit recovery route.
`--ninfer-deployment release-bundle` opts into a fail-closed, immutable
GitHub Release acquisition path with checked tag/manifest/archive/member/mode/
hash/binary provenance, atomic install/symlink, visible deployment status,
startup phase timing and overlapping model prefetch. No llama fallback is
allowed for bundle acquisition or validation failure. The release path remains
unpromoted until fresh RTX 4090 acceptance covers model loading, two lanes,
native context, correctness, Deep Work and teardown.

Local deterministic evidence: `go test -count=1 ./...`,
`go test -race -count=1 ./...`, `go vet ./...`, build, four Python bundle
tests, shell syntax, `git diff --check`, exact pinned-source binary help/`ldd`,
and clean pinned-base bundle extraction all passed. The locally packaged
archive SHA-256 is
`f58ee66d05e5d1932b030a05cfd9a7e1e8570579a47b78476cf845c3abcde6e0`.
An earlier local compile completed but its packaging command was interrupted
by my concurrent builder edit; the local repack/re-extraction then passed.
The older release workflow run `35872434913` exposed missing `pkg-config`,
which #123 fixed.

## Active build / promotion gate

Exact-main build run `35883203990` was dispatched on main SHA
`706c78ae60f996c957d5f9ae86dc10bb529b604e` using an ephemeral CUDA-capable
runner. Runner-space validation and Python packaging/unsafe-archive checks
passed; source build, package and clean-image smoke were still running at the
last update. The intended immutable tag is
`ninfer-runtime-81b68a20-sm89`; publisher must verify the exact build run and
same main SHA before creating/publishing it. Do not update main between that
build and its publisher workflow.

Even if release publication succeeds, no live GPU comparison or Deep Work
acceptance is claimed here until measured. Run one bounded qualification under
existing budget/network/candidate caps if a matching offer exists; never raise
those caps just to obtain capacity. If the market has no candidate under those
caps, record that as the live-test blocker and leave release deployment opt-in.

## PR/evidence and history policy

- Keep #110–#113 open, unmerged and untouched; they are successful generated evidence for session `20260923-022052`.
- #48–#51's unique bundle/timing/deployment ideas have been replaced by #121–#124. Close those superseded stacks only after this replacement and release-build outcome are recorded.
- #73's two unique CP1 reports are preserved verbatim under `docs/history/`, provenance points to source commit `e78ceef308d85c9cac7c71e7d172bed7c66c4182`; close after this documentation change merges.
- Keep #85 parked as a separate high-authority maintenance experiment; keep #93 open for rework against current documentation paths.
- All other previously open PRs have been closed/merged and their semantics or evidence are accounted for in the ledger. The final snapshot table will be refreshed after docs merge and closures.

## Checkpoints

- [x] Verify starting main and protect the dirty original checkout.
- [x] Ground lifecycle/provider safety (#116), ordinary dashboard/lane telemetry (#117), Deep Dashboard (#118), and Spark paths (#120).
- [x] Record full point-in-time open-PR semantics and Deep Work evidence (#119); protect #110–#113.
- [x] Port best-effort startup phase events (#121), select tuple/build workflow (#122), and repair safe pinned-source bundle build/publisher (#123).
- [x] Add opt-in release deployment provenance and fail-closed recovery rules (#124); PR and landed-main CI green.
- [x] Audit the pinned NInfer/Qwen tuple, current upstream port changes, v3 and DFlash2 candidates.
- [x] Verify local Go/Python/shell/build/bundle clean-base checks.
- [~] Finish the exact-main clean Vast build, publish/verify the immutable tag if build passes, and refresh docs with observed result.
- [ ] Inspect offers under existing caps and run bounded RTX 4090 model/two-lane/context/Deep Work acceptance if capacity is available; leave opt-in otherwise.
- [~] Merge canonical docs, close only #48–#51 and #73 after their replacement/history records land, then record the final open-PR snapshot and whole-repository state.

## Final-state rules

Completion remains gated on the outcome of the exact-main bundle workflow and
honest handling of live RTX 4090 availability/acceptance. Do not claim normal
startup uses the release bundle: source-build remains the default until the
full live promotion gate passes. Preserve the tuple rationale, release hash,
CI and failure history in this plan/ledger before closing superseded PRs.
