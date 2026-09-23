# Stint repository grounding handoff

**Status:** source/runtime convergence landed; exact-main runtime bundle build,
release publication, live RTX 4090 qualification, and final PR cleanup remain
in progress. This is a recovery point, not a claim of release qualification.

## Repository and landed work

- Starting main: `9634bf762a2dc9021747eb786db7fd23ccab84e9`.
- Current main: `706c78ae60f996c957d5f9ae86dc10bb529b604e`.
- Grounding merges: #116–#124, including lifecycle safety, both dashboards,
  Spark path profile, startup timing, runtime build/release workflow and
  fail-closed opt-in deployment. Exact landed CI run IDs and semantics are in
  [`STINT_REPOSITORY_GROUNDING_PLAN.md`](STINT_REPOSITORY_GROUNDING_PLAN.md).
- Original dirty checkout `792bb508dfcd7d64e293359dfbed7b497b10dafa` and its
  untracked user artifacts remain untouched.

## Current architecture and NInfer tuple

- `stint dash` preserves lane truth, active versus retained context, last-good
  telemetry and staleness/error attribution. `stint deep dash` exposes current
  Hermes coordinator/run/phase/checkpoint/landing evidence.
- Paid-resource teardown is verified before local session state is cleared.
  Current Deep Work remains Hermes-on-box with detached supervision, durable
  coordination, independent verification, bound compute, resumable landing
  and provider teardown.
- Selected first SM89 release tuple: NInfer
  `81b68a20a9a0d9ab47d7e5838887c6d636ab76e0`; Qwen revision
  `18dfc887423fa5aabf3cb56fac41490e462b3fab`; artifact SHA-256
  `eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e`;
  CUDA 12.8, RTX 4090, native 262144 context, E8 4-bit KV, MTP3. The audit
  rejected v3 for SM89 and DFlash2 for native-context/behavior reasons; a newer
  upstream 4090 vision cap fix remains a follow-up candidate.
- Previous fresh startup builds the pinned NInfer source at boot (332 Ninja
  build edges in the pinned workflow). The new release bundle puts those two
  prebuilt binaries in an immutable archive; expected benefit is removing that
  compile from paid startup. Neither source-build nor release-bundle end-to-end
  READY time is measured yet, and no GHCR comparison was made.
- Source-build remains default/recovery. Explicit `release-bundle` mode is
  fail-closed and reports deployment provenance. It is not promoted until
  fresh GPU and Deep Work acceptance passes.

## Exact build and release state

- Exact-main build workflow: run `35883203990`, head
  `706c78ae60f996c957d5f9ae86dc10bb529b604e`; runner validation and archive
  safety checks passed; build/package/clean-image smoke was running at last
  update. If successful, the publisher must run on the same main SHA before
  any further main commit. Intended tag:
  `ninfer-runtime-81b68a20-sm89`; expected archive SHA-256
  `f58ee66d05e5d1932b030a05cfd9a7e1e8570579a47b78476cf845c3abcde6e0`.
- Local Go tests/race/vet/build, Python bundle tests, shell syntax and local
  clean pinned-base binary/extraction checks passed. The previous workflow's
  missing `pkg-config` issue was repaired in #123.
- No fresh RTX 4090 bundle-vs-source READY comparison or new release-path Deep
  Work acceptance is recorded yet; no performance claims are made. The existing
  interactive defaults are $0.40/hour, $2.50/session, three candidates,
  500 Mbps advertised-network floor and 40 MB/s measured floor. Keep those
  caps fixed. If no suitable offer exists, document the blocker and preserve
  opt-in deployment.

## PR and history state

- Keep successful Deep Work evidence #110–#113 open, unmerged, untouched
  (session `20260923-022052`; checkpoint/handoff SHAs and per-PR records live
  in the ledger).
- #48–#51 are superseded by #121–#124; #73's incident/findings files are copied
  verbatim under `docs/history/`. Close only after the canonical docs change
  merges. Keep #85 parked and #93 for separate docs rework.
- PR #73 records run-1 orphaned instance 49805324 and run-2 session
  `20260905-180320`, including the task verification/landing results and
  remaining checkpoint commit identity blocker. The historical index names the
  source PR and commit.
- Full semantic ledger: [`STINT_OPEN_PR_GROUNDING_LEDGER.md`](STINT_OPEN_PR_GROUNDING_LEDGER.md).

## Next actions

1. Complete/inspect run `35883203990`; if it succeeds, immediately publish on
   the same SHA, verify tag/assets/immutability/hash, then update the canonical
   docs and README instructions on a topic branch.
2. Inspect offers under current caps. Run only bounded runtime/Deep Work
   acceptance within the user's existing limits; do not raise caps to find a
   host. Keep release mode opt-in unless all acceptance gates pass.
3. Merge the final docs change, close #48–#51 and #73 unmerged after recording
   exact replacement/history evidence, and refresh the final open-PR table.
4. Run final whole-repository CI/source/docs/Spark/profile audit and update this
   handoff with final main SHA, release outcome and any live-test blocker.

**Next Stint task:** qualify the immutable runtime on a fresh RTX 4090 instance,
then evaluate the newer upstream vision fix before changing the pin.
**Next Spark ↔ Stint task:** verify its path-aware profile against the final
runtime/bootstrap design after release and live acceptance decisions.
