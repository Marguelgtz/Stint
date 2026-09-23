# Stint repository grounding handoff

**Status:** one authoritative `main` is verified through CI, source/runtime and
historical PR semantics are grounded, the #93 taxonomy proposal was selectively
ported and closed, and the runtime bundle is immutable and opt-in. The only
remaining acceptance gate is fresh RTX 4090 runtime/Deep Work qualification
under unchanged limits.

## Authoritative repository and verification

- Current `main` before this final ledger refresh: `3586022bd6b2cc4565a4d94e519398222d85a443`.
- Exact landed-main push run `35911926414` passed all five required jobs.
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
- PR #93 closed unmerged at `2026-09-23T19:51:25Z` after #133 landed. Its
  [replacement comment](https://github.com/Marguelgtz/Stint/pull/93#issuecomment-5801853350)
  records the new docs index and paths. The final open PR set is #85 and
  protected generated evidence #110–#113.
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

The exact Vast offer scan at 2026-09-23 18:37 UTC found 39 offers, including
24 RTX 4090s; none met the current policy. A 19:06 UTC read-only plan selected
an RTX 3090 at $0.395/hour. No instance was rented and no GPU spend occurred.
Keep the limits fixed at $0.40/hour, $2.50 per session, at most three distinct
candidates, 500 Mbps advertised bandwidth, and 40 MB/s measured transfer.

On a fresh qualifying RTX 4090, compare source-build and the release bundle on
the same conditions. Record startup/READY time, model loading, single/two lanes,
262144 context stability, correctness, prefill/decode, cache reuse, Hermes
Deep Work, teardown, cost, exact tuple, and recovery behavior. Keep release
deployment opt-in until the complete gate passes. See the
[grounding plan](STINT_REPOSITORY_GROUNDING_PLAN.md) for details.

The successful current Deep Work session is `20260923-022052`, protected in
open PRs #110–#113 and not merged. Older #81–#84 are successful 2026-09-08
evidence; #86–#89 are failed 2026-09-09 attempts. All eight are closed
unmerged with checkpoint/handoff SHAs and lessons in the ledger. Legacy CI
rollups for #85 and #110–#113 were attached to PR heads but ran on synthetic
merge trees; exact-head CI is not established, and those protected/parked
branches were not modified to rerun them.

## Next work

1. Recheck Vast under the same limits and run fresh-host RTX 4090 acceptance
   only when a qualifying offer is available.
2. Select the startup default from comparable evidence; then reconcile Spark's
   path-aware profile against the measured runtime/bootstrap design.
3. Keep #85 parked and #110–#113 open, unmerged, and untouched.
