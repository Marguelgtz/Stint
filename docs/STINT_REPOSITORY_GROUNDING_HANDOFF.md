# Stint repository grounding handoff

**Status:** authoritative main is `db993e40da21ab0a4bf0895bde7e0969fa293785`
after docs-only PR #144; landed-main CI run `35939026936` passed all five
required jobs. The latest product-code change is PR #143 at
`089607965715bd9435cc0e3b671b4f34c594e446`; #144 changed only grounding docs.
The live parallel-range run on one RTX 4090 completed the immutable bundle
transfer, SHA verification, installation, model load, READY and teardown. PR
#143 fixed Stint's invalid decode-rate reporting. Full release-bundle
qualification and Deep Work remain open; source-build stays default and
release-bundle stays opt-in.

## Authoritative repository and verification

- Product code main after #143: `089607965715bd9435cc0e3b671b4f34c594e446`.
- Current main after docs-only #144: `db993e40da21ab0a4bf0895bde7e0969fa293785`; landed-main CI `35939026936` passed all five required jobs.
- PR #139 exact-head run 35924946874 and landed-main run 35925100519 passed all five required jobs; it records the source-build RTX 4090 performance baseline.
- PR #140 exact-head run `35928576565` and landed-main run `35928870944`
  passed all five required CI jobs. PR #140 records the incomplete RTX 4090
  release-bundle transfer; it did not claim runtime acceptance.
- PR #141 exact-head run 35931174559 and landed-main run 35931745106 passed all five required jobs; its live 4090 result is recorded below.
- PR #142 records that live result and its limitations; exact-head run `35936860289` and landed-main run `35937034112` passed all five required jobs. Merge SHA: `59130984deb751034921fac262cdacc073e3d75b`.
- PR #143 fixes invalid decode-rate reporting for buffered/reasoning-only output; exact-head run `35938070090` and landed-main run `35938220186` passed all five required jobs. Merge SHA: `089607965715bd9435cc0e3b671b4f34c594e446`.
- PR #144 refreshed the canonical plan, handoff and ledger; head `4cce22805fc909e986d188557d9d446e8b9a137f`, base `089607965715bd9435cc0e3b671b4f34c594e446`, merge `db993e40da21ab0a4bf0895bde7e0969fa293785`. Exact-head run `35938902247` and landed-main run `35939026936` passed all five required jobs.
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
  jobs; merge SHA is `9638ac6fef4ad81a993a5bf1d73ffeffeb30a1ef`.
- PR #93 closed unmerged at `2026-09-23T19:51:25Z` after #133 landed. Its
  [replacement comment](https://github.com/Marguelgtz/Stint/pull/93#issuecomment-5801853350)
  records the new docs index and paths. The final open PR set is #85 and
  protected generated evidence #110–#113.
- The current open PR set after #144 remains #85 and protected evidence
  #110–#113; their exact heads and observed base SHAs are recorded in the
  refreshed ledger snapshot.
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

### Earlier failed release-bundle transfer (2026-09-23)

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
path. An operator-workstation probe confirmed GitHub's redirected asset
supports HTTP ranges; eight concurrent 2 MiB range requests reached 12.3 MB/s
aggregate locally, versus 5.8 MB/s for one 2 MiB request. A parallel range
downloader with retry, resume cache, final SHA check, and a resumable-curl
fallback for servers without range support now passes local fixtures. The Vast
route result is not yet measured.
Keep `release-bundle` opt-in and `source-build` default until a bounded 4090
run passes the complete gate.

### Verified range transfer and partial live acceptance (2026-09-24)

On 2026-09-24, PR #141's range downloader was exercised on one fresh RTX
4090 (instance 52313848, offer 48882869, California, $0.5037037/hour, 778 Mbps
advertised). The run used one candidate, native 262144 context, two configured
clients, and a $0.57/hour / $0.57 session cap. The release archive is
939,381,613 bytes; all 56 ranges completed, the pinned SHA-256
6725e60c8e3edb2982ad828898210868dd98ea1d4fe4d35f97bfa1e625414416 passed, and
the runtime installed. Runtime acquisition took 2m41s. The Qwen model is a
separate 18,210,531,328-byte artifact (SHA-256
eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e); its
transfer took 26m21s. Rental-to-READY was 31m20s. The engine reported native
262144 capacity and two configured lanes; the 8K perf sample used 22.4/48 GiB
VRAM. Stint down confirmed teardown.

An earlier 4090 offer at $0.4524074/hour measured 22.6 MB/s and was destroyed
because it missed the original 40 MB/s floor. The next run relaxed the floor
to 20 MB/s and price cap to $0.57, but its start process was interrupted before
the measurement step. Resuming the INSTANCE_CREATED session bypassed network
qualification, so the second host has no measured-throughput result. Estimated
prorated cost from the two archive timestamps is about $0.36 total, not a
provider invoice.

The runtime bundle passed live acquisition and integrity but full acceptance
did not. Two simultaneous 32-token chat calls returned empty visible content
with every completion token marked as reasoning. At 8K, the benchmark processed
7,413 actual prompt tokens (TTFT 6.05s, total 6.05s, 45 output tokens); its
706,044.7 tok/s decode figure is invalid because visible text arrived at the
end of the stream. The 200K-target benchmark returned no visible token after
three retries. Full 262144 prompt depth and Hermes Deep Work were not tested.
Overall READY was 31m20s, including model transfer; source-build's earlier
26m17s result used different network conditions, so this is not an A/B result.
Keep source-build default and release-bundle opt-in pending visible correctness,
valid performance, full-context, measured-network and Deep Work acceptance. See
the grounding plan for the current gate.


PR #143 corrected the benchmark's timing logic: it requires nonempty
content/reasoning SSE update counts to match endpoint completion usage and a
measurable interval before reporting decode tok/s. The old 8K number remains
evidence of the defect, not a performance result. Exact-head and landed-main
CI passed; this deterministic parser correction was not rerun on a GPU.

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

1. When a complete acceptance run fits the authorized budget, test measured network qualification, visible two-lane correctness, valid 8K/long-context performance, full native-context stability and Hermes Deep Work on one explicit RTX 4090. Never use a 3090.
2. The two 2026-09-24 offers cost an estimated $0.36 total, not a provider invoice. The recorded one-off ceiling was $0.60; another full run does not fit the remaining ~$0.24 estimate because model acquisition alone took 26m21s. Do not start a partial acceptance run that cannot reach the remaining gates.
3. Keep source-build default and release-bundle opt-in until measured network qualification, full native context, two-lane correctness, performance and Deep Work pass.
4. Keep #85 parked and #110–#113 open, unmerged, and untouched; current exact refs and legacy-check caveats are in the refreshed open-PR snapshot.
5. Next Spark ↔ Stint slice: define a read-only evidence contract for selected runtime/bootstrap provenance after live qualification chooses a deployment path. Keep repository/evidence observation in Spark and provider lifecycle authority in Stint.
