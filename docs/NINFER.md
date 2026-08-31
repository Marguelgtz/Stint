# NInfer runtime

Stint serves Qwen3.8-27B through a pinned NInfer build on RTX 4090 hosts. NInfer is an RTX 4090-optimized serving engine (speculative MTP decoding, compact KV cache) selected automatically for 4090 hosts; every other qualifying GPU runs the llama.cpp fallback so one command works across the marketplace.

## Runtime selection

`stint start interactive --runtime <name>`:

| Value | Behavior |
| --- | --- |
| `auto` (default) | RTX 4090 host → NInfer; any other qualifying GPU → llama.cpp. If the NInfer bootstrap is unavailable on the 4090 host, auto mode falls back to llama.cpp on the same host and reports the switch. |
| `ninfer` | NInfer only; the search is restricted to RTX 4090 offers and any non-4090 selection is an error. Bootstrap failure is fatal (no silent fallback). |
| `llama.cpp` (aliases `llama`, `llamacpp`, `llama-cpp`) | Always llama.cpp. |

Read-only surfaces stay consistent with paid starts:

- `stint plan interactive` prints `Runtime (auto)` for the selected offer, using the same selection rule as `stint start`.
- `stint status` prints `NInfer config` (derived from the persisted context) for NInfer sessions.
- `stint dash` shows the runtime and context in the session header.

## Host qualification

NInfer hosts are qualified at search time, before any rental:

- GPU: RTX 4090 (with `--runtime ninfer` the Vast query is restricted to it).
- CUDA: host `cuda_max_good >= 12.8` (`internal/provider/vast/cuda_policy.go`). The pinned NInfer image is built on `vastai/base-image:cuda-12.8.1-cudnn-devel-ubuntu24.04-py310`, so the runtime and the host driver floor move together.
- The llama.cpp fallback image (`vastai/llama-cpp:b10472-mix-4b653db-cuda-12.9`) requires `cuda_max_good >= 12.9` instead.
- Normal hard policy (price, reliability, VRAM, ports, storage, on-demand, session ceiling) and measured model-transfer throughput qualification apply to both runtimes.

## Config presets

`--ninfer-config <name>` selects the NInfer context/KV profile (default `coding`):

| Config | Aliases | Context tokens | KV dtype | Description |
| --- | --- | --- | --- | --- |
| `coding` (default) | `cline`, `agent` | 126,976 | `int8` | 126,976 context, INT8 KV, MTP3 |
| `precision` | `int8`, `max-precision` | 172,032 | `int8` | 172,032 context, INT8 KV, MTP3 |
| `native` | `full`, `max`, `262k` | 262,144 | `rk4v4-e8` | 262,144 native context, E8 4-bit KV, MTP3 |

All presets run MTP3 speculative decoding (`--spec mtp --draft-tokens 3 --lm-head-draft`) and preserve thinking output (`--preserve-thinking`).

NInfer context is owned by the config preset; `--context` is accepted for llama.cpp only (1024–131072, default 16384) and is rejected with an explanatory error when the selected runtime is NInfer. The launch command re-derives the same preset from the context size, so the on-host flags always match the preset chosen at start.

## Pinned Vast image

- Image: `ghcr.io/marguelgtz/stint-ninfer:981b685e-cuda12.8` (also tagged `:edge`).
- NInfer source: `https://github.com/sergiuszm/ninfer-4090.git` at pinned commit `981b685ea2124fdaed023123d2e63fd29d529ab8`, compiled for CUDA architecture 89 (RTX 4090) into `/opt/ninfer/bin/ninfer-serve` and `/opt/ninfer/bin/ninfer`.
- The image keeps Vast's base-image SSH/container lifecycle, so Stint's dedicated-key flow is unchanged.
- GHCR packages in a new personal account are **private by default**. Vast pulls the image anonymously, so the `marguelgtz/stint-ninfer` package must be set to **Public** in GitHub package settings; the publishing workflow verifies an anonymous pull and fails with instructions otherwise.
- Publishing: `.github/workflows/ninfer-image.yml` builds from `images/ninfer/Dockerfile` (two-stage: build on the CUDA 12.8.1 base, then a runtime stage that verifies the binary links and runs). Rerun via `workflow_dispatch` or a push to `images/ninfer/**`.

## Startup path

1. Stint rents the qualified offer with the runtime's Vast image and a startup hook that (a) repairs `/root/.ssh` ownership/modes for a bounded window (hosts are observed creating `authorized_keys` in modes OpenSSH `StrictModes` rejects) and (b) bridges the prebuilt binary: a wrapper at `/workspace/stint/ninfer/build/apps/ninfer-serve` that execs `/opt/ninfer/bin/ninfer-serve`, plus a `.stint-commit` marker with the pinned commit.
2. The bootstrap fast path validates the wrapper and marker, so the paid instance never compiles NInfer; it goes straight to the model transfer.
3. The model artifact is the pinned `qwen3_8_27b.ninfer` from `huggingface.co/neroued/Qwen3.8-27B-NInfer` (SHA-256 `eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e`), downloaded resumably and verified before launch.
4. `ninfer-serve` binds remote `127.0.0.1:8080` with `--max-context`/`--kv-capacity` set to the preset context, `--kv-dtype` from the preset, `--max-concurrency 1`, `--max-pending-requests 16`, `--pending-timeout-ms 600000`, and `--prefill-chunk 1024`. Stint tunnels it to `http://127.0.0.1:8409/v1` as model `qwen3.8-27b`.
5. If the runtime actually bootstrapped as llama.cpp (auto fallback), the persisted session records `llama.cpp` and resume/extend/down behave identically for both runtimes.

The llama.cpp fallback is pinned the same way: `vastai/llama-cpp:b10472-mix-4b653db-cuda-12.9` prebuilt binary, `Qwen3.8-27B-Q4_K_M.gguf` (SHA-256 `31629f53165ab6a7dad8c9847dcfd1fdf55829dac1e6e748f4a68581b0033d34`), launched with `-ngl all`, q8_0 K/V cache, flash attention, and `--metrics --slots` so Stint's live inference observation can poll its `/metrics` and `/slots` endpoints (llama.cpp serves them only with those flags; NInfer serves both by default).

## Updating the pin

1. Bump `NINFER_COMMIT` in `images/ninfer/Dockerfile`, the `TAG` in `.github/workflows/ninfer-image.yml`, and the `ninferVastImage`/`ninferSourceCommit`/marker constants in `cmd/stint/ninfer_vast.go` and `cmd/stint/runtime.go` together (the bridge writes the same commit marker the bootstrap checks).
2. Rerun the image workflow and let it verify the anonymous pull.
3. Re-qualify cold-start time and decode performance on a real 4090 (`stint perf`) before merging.

## See also

- [`docs/CLI.md`](CLI.md) — flag reference for `start`, `plan`, `status`, `dash`, `perf`, `resume`
- [`docs/DASHBOARD.md`](DASHBOARD.md) — live cockpit, including recoverable-session controls
- [`docs/TELEMETRY.md`](TELEMETRY.md) — status snapshot and benchmark telemetry contract