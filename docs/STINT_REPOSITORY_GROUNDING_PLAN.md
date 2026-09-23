# Stint repository grounding plan

Updated: 2026-09-23. This is the canonical execution record for the grounding
mission. The [PR ledger](STINT_OPEN_PR_GROUNDING_LEDGER.md) holds detailed
semantic accounting; the [handoff](STINT_REPOSITORY_GROUNDING_HANDOFF.md) is
the concise recovery point.

## Repository boundary and starting point

- Repository: `Marguelgtz/Stint`, default branch `main`.
- Starting main: `9634bf762a2dc9021747eb786db7fd23ccab84e9`.
- Current main before this docs update: `36d12ac1c3df55cb0a4f1d7ebe5ee557b26316fa` (after #125's docs merge).
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
| #126 | `494c87a8d7e329c2e9c5e36983e74b2d01beb6c5` | exact runtime bundle archive hash from clean source build | `35887717054` |
| #127 | `d3706c943540397e0e5be71a01f64b49768f1953` | candidate build and pin-gated release promotion | `35894364016` |
| #128 | `24a05e663489cd4063fcaa505fc26e3e3a9f732e` | pin the uploaded, clean-base-smoke-tested bundle SHA | `35899139527` |
| #129 | `58afa4af857cf12e5135d4e78c46b79c96313922` | stage candidates as provenance-bound draft releases; least-privilege publisher | `35901071060` |
| #130 | `23908fc719576376b32ce46fb0a34745c27cc9fb` | resolve draft releases via authenticated listing and asset IDs | `35901919089` |
| #125 | `36d12ac1c3df55cb0a4f1d7ebe5ee557b26316fa` | refresh README/operator guidance, canonical grounding docs and verbatim #73 history | `35904245746` |

Each listed exact landed-main run completed all five required Stint jobs. #124
PR-head runs `35882783534`, `35894208985`, `35898993338`, `35900956163`, and
`35901794930` also passed all five. PR #115 and #114 are part of the starting
main ancestry (`9634bf7`) and are not grounding merges.

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
and clean pinned-base bundle extraction all passed. #127 added a fifth bundle
unit test for the release-time hard-pin check; all five pass locally and in
exact-head CI. The earlier locally packaged archive SHA-256 was
`f58ee66d05e5d1932b030a05cfd9a7e1e8570579a47b78476cf845c3abcde6e0`. Two
exact-main builds later emitted different candidate hashes, `16a1d238…` and
`ce0fa3ae…`; neither reached clean-base smoke or uploaded assets before #127.
An earlier local compile completed but its packaging command was interrupted
by my concurrent builder edit; the local repack/re-extraction then passed.
The older release workflow run `35872434913` exposed missing `pkg-config`,
which #123 fixed.

## Active build / promotion gate

Exact-main runs `35883203990` (`706c78ae…`) and `35887934779`
(`494c87a8…`) each compiled all 332 source targets and passed both binary help
checks. They emitted different archive hashes (`16a1d238…` and `ce0fa3ae…`)
and stopped before clean-base smoke or upload at the old pre-smoke pin check.
This disproved #126's first digest as a stable artifact hash.

Candidate run `35894635094` succeeded on `d3706c943540397e0e5be71a01f64b49768f1953`:
332/332 source targets built, clean pinned-base extraction/CLI/`ldd` smoke
passed, and the three candidate assets uploaded. The archive SHA-256 is
`6725e60c8e3edb2982ad828898210868dd98ea1d4fe4d35f97bfa1e625414416` (939,381,613
bytes). PR #128 pinned this exact tested artifact in `cmd/stint/runtime.go`;
PR run `35898993338` and landed-main run `35899139527` passed all five jobs.
The earlier publisher attempt `35899265948` failed before side effects because
the `GITHUB_TOKEN` could not read Actions metadata. PR #129 fixed the handoff
by staging verified candidates as draft releases and using public run metadata;
PR run `35900956163` and landed-main run `35901071060` passed. Draft releases
were omitted by GitHub's release-by-tag endpoint, so PR #130 switched to the
authenticated release list and asset IDs; PR run `35901794930` and landed-main
run `35901919089` passed.

Publisher run `35902030339` verified run `35894635094`, its Stint commit,
allowed lineage through current `main`, draft provenance, all three asset
names, archive SHA, sidecar, manifest tuple, then published the release. The
candidate predates #129's draft-staging step: its original successful Actions
artifact was downloaded, locally reverified and used to create draft release
`394934756` with the same run/source/digest provenance before dispatch. No
rebuild was substituted for that artifact. The
immutable release is
[`ninfer-runtime-81b68a20-sm89`](https://github.com/Marguelgtz/Stint/releases/tag/ninfer-runtime-81b68a20-sm89),
published at `2026-09-23T18:23:36Z`; GitHub reports `immutable: true`. Its
archive asset SHA is the same `6725e60c…` pinned by current `main`. This makes
the opt-in release acquisition path available, but does not qualify it for
default startup: fresh RTX 4090 model-load, two-lane, native-context,
correctness, Deep Work and teardown acceptance remains unmeasured.

No live GPU comparison or Deep Work acceptance is claimed. Search-only Vast
inspection at `2026-09-23 18:37 UTC` returned 39 marketplace records, including
24 RTX 4090s; zero RTX 4090s passed the current profile. All 24 exceeded its
$0.40/hour ceiling, and nine also failed the reliability threshold. The
interactive plan selected an RTX 3090, which does not satisfy the selected
SM89 runtime objective. No instance was rented.

Keep the current limits fixed: $0.40/hour, $2.50 total session ceiling, at
most three distinct candidates, 500 Mbps advertised bandwidth and 40 MB/s
measured throughput. Do not raise a cap to obtain a candidate. Fresh RTX 4090
model-load, two-lane, native-context, correctness, Deep Work and teardown
acceptance is blocked by current offer pricing/reliability; leave release
deployment opt-in and recheck later under the same limits.

## PR/evidence and history policy

- Keep #110–#113 open, unmerged and untouched; they are successful generated evidence for session `20260923-022052`.
- #48–#51 were closed on 2026-09-23 after #125 merged. Their unique bundle/timing/deployment ideas are replaced by #121–#130; the candidate build, exact pin, draft-to-immutable publisher and bundle SHA are verified.
- #73 was closed on 2026-09-23 after #125 merged. Its two unique CP1 reports are preserved verbatim under `docs/history/`; provenance points to source commit `e78ceef308d85c9cac7c71e7d172bed7c66c4182`.
- Keep #85 parked as a separate high-authority maintenance experiment; keep #93 open for rework against current documentation paths.
- Keep #110–#113 open, unmerged and untouched as protected generated Deep Work evidence. The final GitHub snapshot after #125 and the five closures contained exactly six open PRs: #85, #93 and #110–#113. The ledger records their exact heads, bases and check rollups.

## Checkpoints

- [x] Verify starting main and protect the dirty original checkout.
- [x] Ground lifecycle/provider safety (#116), ordinary dashboard/lane telemetry (#117), Deep Dashboard (#118), and Spark paths (#120).
- [x] Record full point-in-time open-PR semantics and Deep Work evidence (#119); protect #110–#113.
- [x] Port best-effort startup phase events (#121), select tuple/build workflow (#122), and repair safe pinned-source bundle build/publisher (#123).
- [x] Add opt-in release deployment provenance and fail-closed recovery rules (#124); PR and landed-main CI green.
- [x] Audit the pinned NInfer/Qwen tuple, current upstream port changes, v3 and DFlash2 candidates.
- [x] Verify local Go/Python/shell/build/bundle clean-base checks.
- [x] Merge hash-pin correction #126 after exact-head CI; exact landed-main run `35887717054` passed all required jobs.
- [x] Replace repeated-build hash assumptions with candidate build and pin-gated promotion (#127); exact PR and landed-main CI passed.
- [x] Build and clean-base smoke candidate run `35894635094`, pin its uploaded archive via #128, and publish/verify immutable release via #129–#130 (`35902030339`).
- [x] Inspect the current market read-only under existing caps; no RTX 4090 qualifies at `2026-09-23 18:37 UTC`, and no compute was rented.
- [!] Fresh RTX 4090 runtime and Deep Work acceptance: no offer passes current $0.40/hour price and reliability policy; do not raise limits.
- [x] Merge #125 with the canonical docs and verbatim #73 history; then close only #48–#51 and #73 after their replacement/history records landed.
- [x] Refresh the final open-PR snapshot after those closures; #85/#93 remain separate and #110–#113 remain protected.

## Final-state rules

The exact-main candidate workflow and immutable publication are verified.
Fresh RTX 4090 acceptance is blocked by current offer pricing/reliability under
the unchanged limits, so leave release-bundle startup opt-in and source-build
as the default. The #125 docs merge, report preservation, and authorized PR
closures are complete; this final ledger refresh records the resulting state.
