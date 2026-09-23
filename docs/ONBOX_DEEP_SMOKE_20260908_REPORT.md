# On-box Deep Work smoke report — 2026-09-08

The full GPU-owned smoke completed successfully in session `20260908-194552`.
The operator machine disconnected after the remote supervisor reported
`RUNNING`; the remote supervisor then completed the work, published the stack,
and landed the session without local coordination.

## Runtime

- GPU instance: `50305484` (RTX 4090, Malaysia)
- Model: `qwen3.8-27b` through native NInfer
- Context/KV: `262144` / `262144`
- Supervisor: `hermes-onbox` via `onbox-deep-supervisor`
- Provenance: `origin=gpu-instance`
- Deadline watchdog: configured for the GPU instance

## Checks passed

- xhigh and medium routes reached NInfer concurrently.
- Compression configuration was installed with the medium summary route,
  180,000-token trigger, 48,000-token proactive prune, and in-place mode.
- Headless ordinary command execution passed.
- The destructive-command approval check denied the command while preserving
  its non-empty sentinel.
- The laptop tunnel and watchdog were stopped after the `RUNNING` handshake;
  remote execution continued and completed.
- The GPU uploaded heartbeat evidence to
  `s3://deep-work/vanta/onbox/20260908-194552/latest.json`.

## Verified task stack

| Task | Checkpoint commit | Draft PR |
| --- | --- | --- |
| `PLAN-001` | `b7280a22d1e48c976c3bbaa4617eb1e104fb92aa` | [#81](https://github.com/Marguelgtz/Stint/pull/81) |
| `PHASE-001` | `aae3dce18010bd8f63fb0864a1fdb8b722af1d1a` | [#82](https://github.com/Marguelgtz/Stint/pull/82) |
| `PHASE-002` | `2beeef207af2fa7f5d0809dbd323a27bc76b6f27` | [#83](https://github.com/Marguelgtz/Stint/pull/83) |
| final handoff | `df69d4a9c736790a20c29b9c2c743df5127af823` | [#84](https://github.com/Marguelgtz/Stint/pull/84) |

All four PRs are open drafts, stack cleanly on
`fix/deep-work-onbox-publishing`, and passed the repository CI checks.

## Limitation

The three short tasks completed before the 180,000-token compression threshold
was reached. The observer therefore recorded compression as `not_observed`.
This run proves the compression settings and medium routing are wired into the
GPU worker, but a separate long-context test is still needed to prove an actual
truncate/summary event and its recovery behavior.

## Cleanup

The supervisor reached `landed`, the local teardown destroyed instance
`50305484`, and the NInfer endpoint went offline. No active instance or local
run process remains.
