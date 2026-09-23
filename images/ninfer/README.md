# Stint NInfer runtime bundle

The selected candidate is `sergiuszm/ninfer-4090` source
`81b68a20a9a0d9ab47d7e5838887c6d636ab76e0`, built for Linux x86-64, CUDA
12.8+, and SM89. It serves the pinned NInfer v2 Qwen artifact at revision
`18dfc887423fa5aabf3cb56fac41490e462b3fab` with SHA-256
`eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e`
(18,210,531,328 bytes / 16.96 GiB, NInfer container v2).

Run `scripts/build_ninfer_runtime_bundle.sh` on a disposable Linux runner with
Docker, at least 35 GiB free space, and network access. It builds the exact
source in Vast's base image pinned by digest, packages only the `ninfer` and
`ninfer-serve` binaries plus the manifest, and smoke-tests safe
extraction, both `--help` commands, and dynamic library resolution in a fresh
container of that same base image.

The bundle workflow uses a trusted ephemeral self-hosted runner because the
base image is larger than the disk available on GitHub's standard Linux
runners. It does not need a GPU. The separate publish workflow consumes only a
successful bundle artifact from that exact `main` commit, creates a draft with
all three assets attached, verifies the draft, then publishes once. Never
replace or re-upload assets under the same immutable release tag.

Publishing this bundle does not promote it to the default startup path.
Source-build remains the recovery strategy until fresh RTX 4090 model-loading
and quality/performance acceptance is recorded.
