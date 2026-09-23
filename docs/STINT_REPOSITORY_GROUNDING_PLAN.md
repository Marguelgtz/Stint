# Stint repository grounding plan

Updated: 2026-09-23. This is the canonical execution record for the grounding
mission. The [PR ledger](STINT_OPEN_PR_GROUNDING_LEDGER.md) holds detailed
semantic accounting; the [handoff](STINT_REPOSITORY_GROUNDING_HANDOFF.md) is
the concise recovery point.

## Repository boundary and starting point

- Repository: `Marguelgtz/Stint`, default branch `main`.
- Starting main: `9634bf762a2dc9021747eb786db7fd23ccab84e9`.
- Current main before this live-evidence refresh: `9638ac6fef4ad81a993a5bf1d73ffeffeb30a1ef` (after #138; landed-main CI run `35923648426` passed all five required jobs).
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
| #131 | `2b5f7342093c520175b608a1a64bbce0b9443f31` | record final release/PR closeout snapshot | `35905880710` |
| #132 | `4ff7159c018c75372b31aca7627151e30fed669b` | check exact PR heads and synthetic merge trees in CI | `35909255732` |
| #133 | `3586022bd6b2cc4565a4d94e519398222d85a443` | port selected docs taxonomy; refresh canonical runtime/PR grounding | `35911926414` |
| #134 | `25eea6fcc4fdbc9d24588599f849a5afe5b4e613` | final PR/evidence snapshot refresh | `35912925386` |
| #135 | `270caea33fec1b08493312c45b2ced19b21e07d1` | correct stale PR snapshot; constrain Deep Work smoke to RTX 4090 and $0.40/hour | `35916137793` |
| #136 | `86f124f9c1a5fa3cb0ccf1040ac392e26b09e365` | record 4090-only smoke landing and current grounding state | `35917484715` |
| #138 | `9638ac6fef4ad81a993a5bf1d73ffeffeb30a1ef` | make `stint perf` request visible output; verify the fix on RTX 4090 | `35923648426` |

Each listed exact landed-main run completed all five required Stint jobs. #124
Pull-request runs `35882783534`, `35894208985`, `35898993338`, `35900956163`,
and `35901794930` reported all five checks green on the pre-#132 synthetic
merge tree. They did not establish exact-head CI. #132 now checks exact PR heads
and separately reruns integration coverage on the synthetic merge tree. PR #115
and #114 are part of the starting main ancestry (`9634bf7`) and are not
grounding merges.

## Runtime decision and current design

Selected first immutable release tuple:

- NInfer source: `81b68a20a9a0d9ab47d7e5838887c6d636ab76e0`.
- Qwen model repository revision: `18dfc887423fa5aabf3cb56fac41490e462b3fab`.
- Model artifact SHA-256: `eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e`; 18,210,531,328 bytes.
- NInfer v2, CUDA 12.8, SM89/RTX 4090; native 262,144 context, E8 4-bit KV and MTP3. Stint's supported launch concurrency is one or two clients.

The source pin is the explicitly selected 4090 port build after upstream issue
#9's planner-accounting fix. The current upstream `rtx4090-port` head is
`aeeba414459d5d6989d57d8487c9d7a2f54bddd3`, five commits beyond Stint's pin.
The only code change in that range is
[`328d9aa`](https://github.com/sergiuszm/ninfer-4090/commit/328d9aa82d0c4a6d3540b65dfca7f41c8dec0cc4),
which fixes the vision per-item cap and includes a regression test. The other
four commits update documentation/ledger material. Do not fast-forward the
production pin until the full Stint acceptance gate passes. The old Stint pin
retains the repeated-screenshot vision per-item-cap issue; include repeated
vision inputs in candidate acceptance before changing the pin. Current
upstream README text claims MTP3, 262K context, and vision on 24 GB, but that is
upstream capability documentation, not Stint live or dual-lane evidence.

The pinned Hugging Face model revision remains v2:
[`18dfc887`](https://huggingface.co/neroued/Qwen3.8-27B-NInfer/tree/18dfc887423fa5aabf3cb56fac41490e462b3fab),
18,210,531,328 bytes with SHA-256
`eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e`. Current
Hugging Face `main` at the 2026-09-23 review was revision
[`1cbd84e`](https://huggingface.co/neroued/Qwen3.8-27B-NInfer/tree/1cbd84e7221e51186bd7f093a149912d2489625b),
last updated 2026-09-15: container v3, 20,437,521,664 bytes, SHA-256
`81f924d440c27261d820c19a9f8d45794c5aee410f8a68bd358133fa8c0375da`, and
minimum runtime `98dada0e…` with `sm_120a`. It is an RTX 5090/CUDA 13.1
artifact, not an SM89 candidate.

DFlash2 is technically supported with the v2 artifact. The
[pinned NInfer README](https://github.com/sergiuszm/ninfer-4090/blob/81b68a20a9a0d9ab47d7e5838887c6d636ab76e0/README.md)
lists DFlash2 v2 artifact revision
`dc370fb6295ae8b786e1af4f90d7142a16255c35`, 19.03 GiB, SHA-256
`0634abb07024221de141456cf04a42ab74b18bc38e1b781c6eb2e062a467eec3`. It
remains only an upstream candidate. Closed, unmerged upstream
[PR #7](https://github.com/sergiuszm/ninfer-4090/pull/7) reports RTX 4090D
48 GB / SM89 results at K=3 and 262K: DFlash2 133.3 code / 111 prose tok/s
versus same-build MTP3 122 / 97.5, with reported acceptance 78.6% / 60.1%
versus 68.8% / 48.3%. The report also records about 25 GiB NInfer process use
near 258K and explicitly says a second full 262K lane does not fit on a 24 GB
card. These are upstream/community measurements, not Stint results; the 48 GB
4090D result does not establish Stint's 24 GB dual-lane viability. The report
states deterministic output differs from MTP3. The maintainer closed the
remaining SM89 adaptation after identifying an unguarded shared-memory change
affecting the SM120 path, loss of split E8/packed-KV capacity, and an old
`post_mixer` signature; 77 of 78 PR commits were already upstream. DFlash2
requires Stint output-correctness evaluation. Keep MTP3 selected pending Stint
acceptance.

PR #124 keeps source-build as the default and explicit recovery route.
`--ninfer-deployment release-bundle` opts into a fail-closed, immutable
GitHub Release acquisition path with checked tag/manifest/archive/member/mode/
hash/binary provenance, atomic install/symlink, visible deployment status,
startup phase timing and overlapping model prefetch. No llama fallback is
allowed for bundle acquisition or validation failure. The release path remains
unpromoted until fresh RTX 4090 acceptance covers model loading, two lanes,
native context, correctness, Deep Work and teardown.

### First live RTX 4090 source-build run

On 2026-09-23, one RTX 4090 only (Vast offer `40583612`, instance `52296599`)
ran the pinned NInfer source-build path. The offer was `$0.4759259/hour`; the
one-off launch used a one-hour schedule capped at `$0.48` and a `$0.50` total
session cap. This was the user's explicit one-run exception to the normal
`$0.40/hour` target; no profile or repository price policy changed. The run
used one candidate attempt and measured `46.3 MB/s` transfer throughput, above
the 40 MB/s requirement. No 3090 was selected or used.

Startup telemetry measured SSH-ready at 58s after rental, network qualification
at 76s, source runtime acquisition/build at 23m51s, runtime verification at
0.12s, and model acquisition at 8m26s overlapping the build. READY arrived
26m17s after rental. The prorated cost is estimated at about `$0.31` (not a
reconciled provider invoice); scheduled exposure was at most `$0.48`. The
instance was destroyed after the run.

At native context `262144` with two configured clients, two concurrent chat
requests returned `42` and `56`. An 8K perf sample processed 7,413 actual
prompt tokens with TTFT 4.91s, total 5.18s, and 164.8 tok/s decode. A request
for 200,000 tokens processed 178,904 actual prompt tokens with TTFT 124.59s,
total 124.78s, and 549.0 tok/s decode; VRAM was 22.4/24.0 GB with no OOM.
This did not fill the full 262,144-token context or prove two simultaneous
full-context lanes.

The live perf run exposed that the synthetic benchmark prompt could end with
no generated token on a reasoning model. #138 adds an explicit one-word
completion instruction and regression test; the fixed binary passed both
live perf samples. The run did not exercise release-bundle deployment or a
Deep Work workflow. It establishes a source-build READY-time baseline, not a
release-bundle A/B result.

### Fresh RTX 4090 release-bundle transfer attempt

On 2026-09-23, a second fresh instance of the same RTX 4090 offer
(`40583612`, instance `52304590`, Taiwan) started the published immutable
release-bundle path. The hourly rate was `$0.4759259`; the one-hour schedule
was capped at `$0.48` with a `$0.50` session ceiling and one candidate. Stint's
network qualification measured `52.3 MB/s`, and the pinned 18.2 GB model was
already cached on the node. The selected bundle was
`ninfer-runtime-81b68a20-sm89`, SHA-256
`6725e60c8e3edb2982ad828898210868dd98ea1d4fe4d35f97bfa1e625414416`.

The GitHub Release archive is 939,381,613 bytes. Its single-stream asset
download reached about 428 MiB (48%) after 25 minutes; in the last sampled
minutes it advanced at roughly 16–17 KB/s. The run was deliberately stopped
at 26m41s because the remaining transfer could not fit the lease and leave
time for acceptance. The prorated cost is estimated at `$0.21` (not a
reconciled provider invoice); scheduled exposure was at most `$0.48`. Stint
archived the session as STOPPED, confirmed provider teardown, and reported no
active compute.

This attempt did not finish archive acquisition or verify its SHA. It never
reached runtime verification, model loading, READY, inference, perf sampling,
Deep Work, publication, or archive creation. It is not a release-bundle
acceptance pass and produces no Deep Work evidence PRs. The provider's general
network qualification is not representative of the GitHub asset route. The
observed single `curl` transfer points to a per-route or per-connection
bottleneck; the exact cause (GitHub CDN, path, or transport behavior) remains
unresolved. Keep `source-build` as default and `release-bundle` opt-in. Next,
improve and locally verify release asset delivery, then run another bounded
4090 attempt only after its expected transfer time leaves room for the full
acceptance gate.

A follow-up operator-workstation probe confirmed that the public GitHub asset
supports byte ranges through its `release-assets.githubusercontent.com`
redirect (`206`, total size 939,381,613 bytes). A single 2 MiB range took
0.36s (5.8 MB/s); eight concurrent 2 MiB ranges transferred 16 MiB in 1.30s
(12.3 MB/s aggregate). These samples use a different network path from the
Vast node, so they do not predict its range-download rate. The code change in
progress uses eight 16 MiB ranges, retries failed chunks, preserves completed
chunks for Stint resume, verifies the final pinned SHA-256 before installation,
and falls back to resumable `curl` if the server lacks range support. Local
fixtures cover concurrent ranges, transient retries, resume after interruption,
bad-digest rejection, and the no-range fallback. Full Go tests, race tests, vet, CLI build, Python bundle fixtures,
and shell/Python syntax checks pass locally; exact-head CI and another live
4090 measurement are still required.

Local deterministic evidence: `go test -count=1 ./...`,
`go test -race -count=1 ./...`, `go vet ./...`, build, four Python bundle
tests, shell syntax, `git diff --check`, exact pinned-source binary help/`ldd`,
and clean pinned-base bundle extraction all passed. #127 added a fifth bundle
unit test for the release-time hard-pin check; all five pass locally and in its
legacy PR run. Before #132, pull-request CI tested synthetic merge trees, not
exact heads. The earlier locally packaged archive SHA-256 was
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
$0.40/hour ceiling, and nine also failed the reliability threshold. A later
read-only interactive planner run at `19:06 UTC` selected an RTX 3090 at
$0.395/hour, which does not satisfy the selected SM89 runtime objective. Both
checks were performed under the existing caps; no instance was rented and no
GPU spend occurred. A fresh read-only one-hour plan at `2026-09-23 20:12 UTC`
queried 41 offers and reported 8 generic interactive qualifiers, selecting an
RTX 3090 at `$0.379/hour`. Its closest rejected offers were RTX 4090s beginning
at `$0.476/hour`, rejected by price. This was a generic interactive plan;
its RTX 3090 selection is excluded from the runtime/Deep Work mission. The
plan explicitly reported `mutating: false` and `computeRented: false`; no
compute was rented. Acceptance must use the explicit `--runtime ninfer` path,
which constrains the provider query to RTX 4090 only; never rent or test on a
3090.

The clean-base bundle extraction, CLI, and dynamic-library smoke is not a
runtime-readiness comparison. Source-build now has one live 4090 startup
baseline; release-bundle and historical GHCR have not measured provider
loading or rental-to-READY time on comparable fresh hosts. Compilation of 332
source targets and the release candidate archive hash establish build
provenance only, not live startup benefit.

### Historical runtime image decision

Historical [PR #32](https://github.com/Marguelgtz/Stint/pull/32) merged as
`20382ea96338fdb82a1bf6460fb9478743a36084`. It built
`ghcr.io/marguelgtz/stint-ninfer:981b685e-cuda12.8` from NInfer
`981b685ea2124fdaed023123d2e63fd29d529ab8` on top of
`vastai/base-image:cuda-12.8.1-cudnn-devel-ubuntu24.04-py310`, checked the
prebuilt server's dynamic libraries and `--help`, and bridged it into the
existing Stint runtime path. This demonstrated that an image build could avoid
per-rental compilation at that historical tuple.

The custom image is not in current startup configuration. It was later
excluded because image acquisition/provider loading and startup behavior had
not been revalidated against the current lifecycle, and it moved more startup
behavior inside the provider image path, reducing SSH-level observability.
The current release bundle keeps the standard Vast CUDA base and visible SSH
lifecycle. Source-build now has a live 4090 READY baseline, but the release
bundle and GHCR have not been measured on comparable fresh hosts. Keep #32 as
a historical control candidate; do not restore it as the default from
compilation evidence alone.

The normal interactive target remains `$0.40/hour` and `$2.50` per session.
The user authorized a bounded one-off price exception when a 4090 is required
and no qualifying offer fits the target. Each exception must set its GPU-only
objective, hourly/session ceiling, network floor and one-candidate maximum
before launch. Keep the RTX 4090 selector explicit, never use a 3090, and do
not change the permanent profile. Source-build smoke has completed; fresh
release-bundle qualification, full-context/two-lane coverage, Deep Work on the
bundle path, and teardown evidence remain.

## PR/evidence and history policy

- Keep #110–#113 open, unmerged and untouched; they are successful generated evidence for session `20260923-022052`.
- #48–#51 were closed on 2026-09-23 after #125 merged. Their unique bundle/timing/deployment ideas are replaced by #121–#130; the candidate build, exact pin, draft-to-immutable publisher and bundle SHA are verified.
- #73 was closed on 2026-09-23 after #125 merged. Its two unique CP1 reports are preserved verbatim under `docs/history/`; provenance points to source commit `e78ceef308d85c9cac7c71e7d172bed7c66c4182`.
- Successful generated Deep Work evidence #81–#84 (session `20260908-194552`) and failed attempts #86–#89 (sessions `20260909-012307` and `20260909-014522`) were closed unmerged on 2026-09-23 after checkpoint/handoff SHAs and accurate lessons were recorded in the ledger.
- Keep #85 parked as a separate high-authority maintenance experiment. PR #93 was closed after selected taxonomy and current pages landed in #133; its replacement and closure comment are recorded in the ledger.
- Keep #110–#113 open, unmerged and untouched as protected generated Deep Work evidence. The post-#138 snapshot still contains exactly #85 and #110–#113. Their legacy CI rollups were on synthetic merge trees; exact-head CI is not established. Current refs remain in the ledger.

## Checkpoints

- [x] Verify starting main and protect the dirty original checkout.
- [x] Ground lifecycle/provider safety (#116), ordinary dashboard/lane telemetry (#117), Deep Dashboard (#118), and Spark paths (#120).
- [x] Record full point-in-time open-PR semantics and Deep Work evidence (#119); protect #110–#113.
- [x] Port best-effort startup phase events (#121), select tuple/build workflow (#122), and repair safe pinned-source bundle build/publisher (#123).
- [x] Add opt-in release deployment provenance and fail-closed recovery rules (#124); legacy PR merge-tree and landed-main CI green.
- [x] Audit the pinned NInfer/Qwen tuple, current upstream port changes, v3 and DFlash2 candidates.
- [x] Verify local Go/Python/shell/build/bundle clean-base checks.
- [x] Merge hash-pin correction #126 after its PR CI; exact landed-main run `35887717054` passed all required jobs.
- [x] Replace repeated-build hash assumptions with candidate build and pin-gated promotion (#127); legacy PR merge-tree and landed-main CI passed.
- [x] Build and clean-base smoke candidate run `35894635094`, pin its uploaded archive via #128, and publish/verify immutable release via #129–#130 (`35902030339`).
- [x] Inspect the market read-only under existing caps at `18:37 UTC`, recheck the plan at `19:06 UTC`, and refresh it at `20:12 UTC`; no qualifying RTX 4090 was established and no compute was rented.
- [x] Complete the first fresh RTX 4090 source-build smoke with a one-off hourly exception; record READY time, throughput, chat correctness, perf, cost ceiling and teardown. Merge the visible-output fix in #138.
- [~] Continue qualifying the immutable release bundle. The first fresh RTX 4090 attempt stopped before runtime verification: GitHub asset transfer reached only about 48% after 25 minutes, despite 52.3 MB/s network qualification and a cached model. A parallel-range downloader is implemented and passes local tests; wait for exact-head CI, then repeat the full 4090 acceptance gate. Keep release-bundle opt-in and source-build default until live acceptance passes.
- [x] Merge #125 with the canonical docs and verbatim #73 history; then close only #48–#51 and #73 after their replacement/history records landed.
- [x] Merge #131 and record its landing SHA / main CI; inspect its pull-request run and identify the synthetic-merge checkout gap.
- [x] Repair CI in #132: required jobs check exact PR head SHA, then `unit-tests` runs again on GitHub's synthetic merge tree; exact-head, merge-tree, and landed-main checks passed.
- [x] Close old generated PRs #81–#84 (successful) and #86–#89 (failed) after preserving their session IDs, checkpoint/handoff SHAs, and outcomes.
- [x] Port selected #93 taxonomy to current docs; archive superseded content; add current index, operations, roadmap and architecture pages in #133.
- [x] After #133's exact-head, synthetic merge-tree and main push CI passed, comment on and close #93; verify the resulting five-PR open snapshot and record its refs/check evidence.
- [x] Merge #135 after exact-head and synthetic merge-tree verification; its smoke remains RTX 4090-only at the normal $0.40/hour target.
- [x] Merge #136 smoke-landing docs and #138 perf-prompt fix. #138 exact-head CI `35923353592` and landed-main CI `35923648426` passed all five required jobs.

## Final-state rules

The exact-main candidate workflow and immutable publication are verified.
PR #138 passed exact-head CI run `35923353592` and merged as
`9638ac6fef4ad81a993a5bf1d73ffeffeb30a1ef`; landed-main run `35923648426`
passed all five required jobs. PR #139 merged as
`bc368274ab5746a03eaa8c497b83023ddab3f6fe` after exact-head run
`35924946874` and landed-main run `35925100519` passed. A one-off source-build
run on RTX 4090 measured 26m17s rental-to-READY, with source runtime
acquisition at 23m51s and model transfer overlapping at 8m26s. It confirmed
46.3 MB/s, two concurrent short chat responses, 8K and 178,904-token perf, no
OOM, and verified teardown. It did not exercise release-bundle deployment,
the full 262,144-token context or
a Deep Work run. A fresh release-bundle attempt on the same 4090 offer reached
only about 48% of the immutable 895 MiB asset after 25 minutes and was stopped
before READY; the instance was verified destroyed. The exact asset-route
bottleneck is unknown, so release-bundle has no live acceptance evidence. A
parallel-range downloader now passes local range/retry/integrity fixtures;
its real Vast route performance remains unmeasured. Source-build remains the
default until the changed path reaches full live acceptance. The normal
`$0.40/hour`
target remains; bounded one-off price overrides are authorized when a 4090 is
necessary. Never use a 3090.
