# NInfer runtime-bundle startup experiment

## Goal

Reduce opaque Vast `loading` time by removing the Stint-owned full NInfer container from the critical startup path.

Current path:

```text
rent
  -> Vast resolves/pulls ghcr.io/marguelgtz/stint-ninfer:<pin>
  -> SSH
  -> model transfer
  -> NInfer model load
  -> READY
```

Target path:

```text
rent
  -> Vast starts its commonly cached CUDA 12.8 base image
  -> SSH
  -> download a small pinned NInfer runtime bundle
       || download Qwen model in parallel
  -> NInfer model load
  -> READY
```

The optimization target is rental-to-READY, especially p90 provider loading time. A faster post-SSH step does not count as a win if total READY time regresses.

## Hard invariants

- Keep NInfer pinned to `981b685ea2124fdaed023123d2e63fd29d529ab8`.
- Keep the RTX 4090 / SM89 build target.
- Normal paid startup must not compile NInfer.
- Normal paid startup must not run `apt-get` to prepare NInfer.
- The bundle must execute on the pristine target Vast CUDA base image.
- The bundle must be checksum-pinned before it becomes a Stint runtime input.
- Model transfer should overlap runtime preparation rather than wait behind it.
- The existing full-image path stays available until live A/B evidence shows the bundle path wins.

## Proof artifact

`images/ninfer/Bundle.Dockerfile` builds NInfer from the same source pin as the current image and creates:

```text
bundle/
  bin/
    ninfer
    ninfer-serve
  lib/
    <non-core ELF runtime dependencies>
  manifest.json
  commit
```

The compressed artifact is named:

```text
stint-ninfer-981b685e-sm89-linux-amd64.tar.gz
```

The bundle deliberately does not carry glibc, the ELF loader, or CUDA/driver libraries. Those remain supplied by the exact Vast base image used by both the build and target smoke stage. Other ELF dependencies discovered from `lddtree` are copied beside the binaries and resolved through `LD_LIBRARY_PATH`.

## CI proof gate

`.github/workflows/ninfer-bundle.yml` performs three checks before the bundle can be considered usable:

1. Build NInfer at the exact source pin.
2. Copy the runtime directory into a pristine `vastai/base-image:cuda-12.8.1-cudnn-devel-ubuntu24.04-py310` stage and run `ninfer-serve --help` with no package installation in that target stage.
3. Extract the actual `.tar.gz` artifact into another pristine target stage and repeat the binary/dependency/commit checks.

The workflow then exports the archive plus SHA-256 as a temporary GitHub Actions artifact. This PR intentionally does not make that Actions artifact a production download source; Stint needs a stable anonymous immutable publication URL before bundle deployment can become a runtime option.

## Action plan

```text
[DONE] T01 Define the rental-to-READY optimization target and preserve the image path as control.
[DONE] T02 Build NInfer at the exact pinned source revision and SM89 target.
[DONE] T03 Derive the non-core ELF dependency closure into a relocatable lib/ directory.
[DONE] T04 Package bin/, lib/, manifest.json, and commit into a compressed runtime artifact.
[DONE] T05 Add a pristine Vast-base smoke target with zero apt-get/compile in the target stage.
[DONE] T06 Add a second smoke target that extracts and executes the actual compressed artifact.
[DONE] T07 Emit SHA-256 next to the proof bundle and upload both as CI artifacts.

[TODO] T08 Record startup phase timestamps: contract, loading, running, SSH, runtime-ready, model-ready, endpoint-ready.
[TODO] T09 Choose a stable immutable anonymous publication mechanism for the runtime bundle.
[TODO] T10 Pin the production bundle URL + SHA-256 in Stint.
[TODO] T11 Add an experimental NInfer deployment strategy using the plain Vast CUDA base image.
[TODO] T12 Download/verify/extract the runtime bundle immediately after SSH.
[TODO] T13 Start runtime-bundle and Qwen transfers concurrently.
[TODO] T14 Cache the extracted runtime by NInfer commit and reuse it during resume.
[TODO] T15 Keep source compilation as recovery-only fallback, not normal startup.
[TODO] T16 Add corrupt archive, wrong commit, checksum, missing-library and transfer-failure tests.
[TODO] T17 Run matched full-image vs runtime-bundle Vast starts across multiple machine IDs.
[TODO] T18 Compare p50/p90 provider loading, SSH-ready, runtime prepare and total READY.
[TODO] T19 Make bundle deployment default only if total startup evidence wins without reliability regression.
[DEFER] T20 Remove the full GHCR NInfer image after at least one release cycle of successful bundle operation.
```

## Decision gate

Do not replace the existing image merely because the bundle is smaller. Promote the bundle path when either of these is demonstrated without a meaningful startup-failure regression:

```text
p50 total READY improves >= 15%
OR
p90 total READY improves >= 25%
```

The p90 criterion is intentionally stronger because this experiment exists primarily to reduce long `Vast loading` tails.

## Publication follow-up

The next implementation should not use a short-lived authenticated Actions artifact URL from paid instances. The production artifact needs all of:

- immutable version derived from the NInfer source pin,
- anonymous HTTPS download from Vast hosts,
- stable URL suitable for Stint source constants,
- published SHA-256,
- no registry credentials on rented compute.

A commit-specific GitHub Release asset is one viable option. Whichever mechanism is selected, Stint should treat the URL and digest as one pinned runtime contract.
