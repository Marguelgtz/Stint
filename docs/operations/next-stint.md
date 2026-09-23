# Next Stint: live runtime acceptance

## Current gate

The source-build path remains the default. The immutable NInfer release bundle
is available as an explicit, fail-closed option, but neither path has a
comparable fresh-host `READY` measurement. Do not promote the bundle based on
its successful build or clean-base extraction smoke alone.

A generic read-only interactive plan at 2026-09-23 20:12 UTC selected an RTX
3090 at $0.379/hour. That result is explicitly excluded from runtime
qualification: acceptance runs only on RTX 4090, never on RTX 3090 or another
GPU. The closest rejected RTX 4090 offers shown by that query exceeded the
$0.40/hour ceiling. An earlier scan at 18:37 UTC returned 39 records, including
24 RTX 4090 offers; none passed Stint's unchanged price and reliability policy.
No compute was rented.

Keep the acceptance limits fixed:

- $0.40/hour maximum and $2.50 total session ceiling
- at most three distinct candidates
- 500 Mbps advertised bandwidth and 40 MB/s measured transfer floor
- no cap increase to obtain an RTX 4090

## Acceptance sequence

When a policy-qualified RTX 4090 is available, use a fresh host and record the
exact runtime and model tuple. Start with `--runtime ninfer`, which constrains
the Vast search to RTX 4090 offers; never rent a 3090 for this qualification.
Compare the current source-build default with the pinned release bundle under
the same conditions. Include the historical
GHCR path only if provider image loading can be revalidated without losing
SSH-level lifecycle visibility.

Record:

1. rental-to-SSH, runtime acquisition/preparation, model load, and rental-to-
   `READY` times;
2. one- and two-lane operation, native 262,144 context stability, and response
   correctness;
3. Deep Work Hermes behavior, teardown verification, total cost, and any
   recovery path used;
4. prefill/decode measurements and prefix-cache reuse, with workload and
   attribution details;
5. the exact Stint source SHA, NInfer source SHA, artifact revision/SHA,
   runtime deployment mode, and host GPU/CUDA details.

Upstream benchmark results do not substitute for measurements of the Stint
workload. Do not change the NInfer pin or enable DFlash2 based only on a newer
commit or higher upstream decode rate.

## After acceptance

- Keep or change the runtime default based on the recorded comparison and the
  complete two-lane, context, correctness, Deep Work, and teardown gates.
- Evaluate upstream commit `328d9aa82d0c4a6d3540b65dfca7f41c8dec0cc4`'s vision
  per-item cap fix as a separate candidate; it is not promoted by this plan.
- Recheck Spark's path-aware profile and runtime/bootstrap evidence boundary
  after the Stint runtime decision.

See the [canonical grounding plan](../STINT_REPOSITORY_GROUNDING_PLAN.md) for
the detailed evidence, fixed limits, and current acceptance status.
