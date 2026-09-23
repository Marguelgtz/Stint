# Stint repository grounding handoff

**Status:** one authoritative `main` is verified through CI, source/runtime and
historical PR semantics are grounded, and the immutable runtime bundle remains
opt-in. One fresh RTX 4090 source-build run reached READY and supplied a live
startup/perf baseline. A second fresh 4090 run stopped during release-asset
download at about 48%; release-bundle and Deep Work qualification remain open.

## Authoritative repository and verification

- Product `main` before this evidence-documentation PR:
  `bc368274ab5746a03eaa8c497b83023ddab3f6fe` (after #139).
- PR #139 exact-head run `35924946874` and landed-main run `35925100519`
  passed all five required CI jobs.
- Exact landed-main push run `35916137793` passed all five required jobs.
- PR #131 merged as `2b5f7342093c520175b608a1a64bbce0b9443f31`. Its PR run
  `35905726077` appeared green but physically tested synthetic merge tree
  `45644d2`, not its head tree. That gap is fixed by #132.
- PR #132 merged as `4ff7159…`. PR run `35908857053` checked exact head
  `a90b059c81d7a7bfb1fdbc17aff053e40c217429`, then reran Go/Python/shell
  coverage on synthetic merge tree `1a1b01ba429d8953df2f8519ad31b2ea7bb416cf`;
  all five required checks passed. Main push run `35909255732` passed.
- PR #133 merged as `3586022…`. PR run `35911740797` checked exact head
  `58e3a5832dbffdcf683665598e8c0d5335c34760` and synthetic merge tree
  `2f349cf2c57e4cdc5e9809019de963558d0b3069`; all five required checks passed.
  Main push run `35911926414` passed all five required jobs.
- PR #134 merged as `25eea6fcc4fdbc9d24588599f849a5afe5b4e613`. Its exact-head
  PR run `35912743074` passed all five required jobs and separately passed its
  synthetic merge-tree test; main push run `35912925386` passed all five jobs.
- PR #135 merged as `270caea33fec1b08493312c45b2ced19b21e07d1`. Its exact-head
  PR run `35915946946` and synthetic merge-tree test passed; main push run
  `35916137793` passed all five required jobs. The Deep Work smoke explicitly
  uses `--runtime ninfer`, a one-candidate limit, and the fixed `$0.40/hour`
  cap, so acceptance selects only an RTX 4090 and has no 3090 fallback.
- PR #136 merged as `86f124f9c1a5fa3cb0ccf1040ac392e26b09e365`; PR run
  `35917286961` and landed-main run `35917484715` passed all five jobs.
- PR #138 fixed a live `stint perf` no-output failure by appending an explicit
  short completion instruction to its synthetic prompt. Exact-head run
  `35923353592` and landed-main run `35923648426` passed all five required
  jobs; merge SHA is the current `main` above.
- PR #93 closed unmerged at `2026-09-23T19:51:25Z` after #133 landed. Its
  [replacement comment](https://github.com/Marguelgtz/Stint/pull/93#issuecomment-5801853350)
  records the new docs index and paths. The final open PR set is #85 and
  protected generated evidence #110–#113.
- The current open PR set remains #85 and protected evidence #110–#113; the
  post-#138 refs and base SHAs are recorded in the ledger.
- The original dirty checkout at `792bb508dfcd7d64e293359dfbed7b497b10dafa`
  and all its untracked user files and local binary remain untouched.

## Product surfaces on `main`

- Interactive Vast sessions retain paid-resource watchdogs, durable/recoverable
  lifecycle state, safe teardown, `stint dash`, and truthful NInfer lane and
  telemetry semantics.
- Hermes-on-box Deep Work retains detached supervision, durable coordinator
  state, bound compute, independent verification, checkpoint SHA, resumable
  landing, final handoff/archive, and verified teardown. `stint deep dash` is
  separate from the runtime dashboard.
- Spark owns repository/evidence observation; Stint owns provider and runtime
  lifecycle. The exact semantics and superseded PR dispositions are in the
  [semantic ledger](STINT_OPEN_PR_GROUNDING_LEDGER.md).

## Runtime tuple and promotion decision

- Selected tuple remains NInfer `81b68a20a9a0d9ab47d7e5838887c6d636ab76e0`,
  CUDA 12.8 / SM89, Qwen v2 revision
  `18dfc887423fa5aabf3cb56fac41490e462b3fab`, artifact SHA-256
  `eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e`, native
  262144 context, E8 4-bit KV, and MTP3.
- NInfer `rtx4090-port` is now at `aeeba414459d5d6989d57d8487c9d7a2f54bddd3`;
  code commit `328d9aa…` fixes vision per-item caps. Evaluate it as a separate
  candidate after live acceptance; do not advance the pin by recency.
- Current Qwen v3 is a CUDA 13.1 / `sm_120a` container and is not an SM89
  candidate. Upstream DFlash2 data is from 4090D 48 GB and does not establish
  24 GB dual-lane or Stint correctness; keep MTP3 selected.
- Immutable release
  [`ninfer-runtime-81b68a20-sm89`](https://github.com/Marguelgtz/Stint/releases/tag/ninfer-runtime-81b68a20-sm89)
  has archive SHA-256
  `6725e60c8e3edb2982ad828898210868dd98ea1d4fe4d35f97bfa1e625414416` and
  passed candidate build plus clean standard-base extraction/CLI/`ldd` smoke.
  `source-build` remains default; `release-bundle` remains explicit and
  fail-closed.
- Historical #32 built
  `ghcr.io/marguelgtz/stint-ninfer:981b685e-cuda12.8` from the older
  `981b685…` tuple. The image is not current startup architecture because
  provider image acquisition and lifecycle observability were not revalidated.
  There is no comparable Stint source-build / release-bundle / GHCR
  rental-to-READY measurement; build targets and archive smoke do not prove
  startup savings.

## Live acceptance gate

The normal interactive target remains $0.40/hour and $2.50 per session. The
user authorized one-off bounded price overrides when an RTX 4090 is necessary
and no qualifying offer fits the target. Never use a 3090. Set the one-run
objective, hourly/session ceilings, 500 Mbps advertised floor, 40 MB/s measured
floor, and one-candidate limit before rental.

On 2026-09-23, one fresh RTX 4090 source-build run (offer `40583612`, instance
`52296599`, rate `$0.4759259/hour`) used one candidate attempt, a one-hour
schedule capped at `$0.48`, and `$0.50` total session limit. Measured transfer
was 46.3 MB/s. SSH was ready at 58s, network qualification at 76s, source
runtime acquisition/build took 23m51s, verification 0.12s, and model transfer
8m26s overlapped the build. READY arrived 26m17s after rental. Approximate
prorated cost was `$0.31` (not a reconciled invoice); instance teardown was
verified.

With native context 262144 and two configured clients, concurrent chat requests
returned 42 and 56. Live perf passed at 7,413 actual prompt tokens (TTFT 4.91s,
164.8 tok/s decode) and 178,904 actual prompt tokens (TTFT 124.59s, 549.0 tok/s
decode; 22.4/24 GB VRAM, no OOM). The full 262144 context was not filled, the
two clients were not tested at full context, no release-bundle deployment was
used, and this run did not execute Deep Work. Source-build now has a measured
READY baseline.

A fresh RTX 4090 run then selected the immutable release bundle on the same
Vast offer, at `$0.4759259/hour`, with a one-hour `$0.48` schedule cap and
`$0.50` session ceiling. Stint measured `52.3 MB/s` in its network
qualification, and the pinned 18.2 GB model was cached. The 939,381,613-byte
GitHub Release asset reached about 428 MiB (48%) after 25 minutes; its last
sampled minutes were around 16–17 KB/s. The run was stopped at 26m41s to avoid
spending the remaining lease on an incomplete transfer. Estimated prorated
cost was `$0.21` (not provider-invoice reconciled). Stint archived STOPPED,
confirmed provider teardown, and reported no active compute.

The release archive was not completed or SHA-verified. This run did not reach
runtime verification, model load, READY, inference, perf, Deep Work,
publication, or final archive. The exact cause of the GitHub-route slowdown
is unknown; general network qualification does not measure the release asset
path. Keep `release-bundle` opt-in and `source-build` default. Improve and
locally verify release asset delivery before another bounded 4090 attempt.

Keep release deployment opt-in until a fresh RTX 4090 run covers the immutable
bundle, short two-lane requests, full native-context stability, correctness,
Hermes xhigh/medium routing, independent verification, checkpoint publication,
handoff/archive, and teardown. First improve and locally verify asset delivery;
then compare READY time with the source-build baseline. See the
[grounding plan](STINT_REPOSITORY_GROUNDING_PLAN.md) for the current gate.

The generic read-only Vast plan at `2026-09-23 20:12 UTC` selected an excluded
RTX 3090 at `$0.379/hour`; it did not authorize or trigger a rental. The actual
acceptance run used explicit `--runtime ninfer` and selected only the RTX 4090.

The successful current Deep Work session is `20260923-022052`, protected in
open PRs #110–#113 and not merged. Older #81–#84 are successful 2026-09-08
evidence; #86–#89 are failed 2026-09-09 attempts. All eight are closed
unmerged with checkpoint/handoff SHAs and lessons in the ledger. Legacy CI
rollups for #85 and #110–#113 were attached to PR heads but ran on synthetic
merge trees; exact-head CI is not established, and those protected/parked
branches were not modified to rerun them.

## Next work

1. Improve release asset delivery and verify it locally, including retry and
   integrity behavior on a throttled/range-capable fixture.
2. After the transfer path can leave time for acceptance, run the immutable
   release bundle on one fresh RTX 4090 and complete the remaining runtime and
   Deep Work gates within explicit price and candidate limits.
3. Compare release-bundle startup with the source-build baseline before choosing
   a production default. Keep source-build as default until qualification passes.
4. Refresh the open-PR ledger after acceptance; keep #85 parked and #110–#113
   open, unmerged, and untouched.
