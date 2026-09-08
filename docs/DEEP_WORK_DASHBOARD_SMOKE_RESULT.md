# Deep Work dashboard compression smoke result

**Run:** `20260908-001117`  
**Deep Work session:** `20260908-002648`  
**Date:** 2026-09-08

The isolated GPU smoke completed the normal phased box smoke and one Hermes
compression task. The coordinator landed after `12m55s`; `COMPRESS-001` was
verified in one attempt and checkpointed on the disposable remote branch.

The sanitized worker observation at teardown reported:

| Signal | Result |
| --- | --- |
| NInfer context / KV / completion | 262,144 / 262,144 / 262,144 |
| xhigh / medium requests | 9 / 20 |
| Compression | 4 completed, 0 failed, 0 truncated |
| Task verification | passed |

The first acceptance wrapper returned non-zero because its collector looked for
`compression-smoke.ok` in the base repository. Deep Work correctly writes the
checkpoint artifact in its session worktree; the handoff and coordinator state
confirm the artifact and verification. The collector now resolves the recorded
Deep Work worktree before reading the artifact.

Evidence remains outside the repository in the isolated run artifact directory:
`~/Documents/projects/Stint/deep-compression-smoke-20260908-001117/`.
