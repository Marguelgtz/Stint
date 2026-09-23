# Current architecture

## Control plane and interactive sessions

Stint is a local Go control plane. It owns provider selection, paid-session
state, deadlines, SSH access, runtime bootstrap, telemetry, recovery, and
teardown. The client-facing endpoint remains local while Vast compute can
change underneath it:

```text
coding client
     |
     v
127.0.0.1:8409/v1
     |
     v
Stint lifecycle, watchdog, SSH tunnel, and telemetry
     |
     v
Vast GPU -> Qwen3.8-27B -> NInfer or llama.cpp
```

`session.json` is the interactive lifecycle authority. A paid session is
protected by its deadline watchdog; failed destroy verification retains
recoverable local state rather than silently clearing ownership. `stint dash`
observes this lifecycle and its live runtime telemetry.

## NInfer runtime

The current source pin targets the NInfer RTX 4090 port on CUDA 12.8 / SM89,
with the pinned Qwen NInfer v2 artifact, 262,144-token native context, E8
4-bit KV, and MTP3. The exact tuple is recorded in the
[grounding plan](../STINT_REPOSITORY_GROUNDING_PLAN.md) and `cmd/stint/runtime.go`.

Source-build remains the default and explicit recovery path. The immutable
GitHub Release bundle is opt-in and fails closed on acquisition or integrity
errors. Its build, archive, and clean-base extraction/CLI/`ldd` smoke are
verified; fresh-host model-load and `READY` time comparisons are not.

The two NInfer lanes are execution lanes over a shared runtime pool, not stable
client identities. Retained context is not active processing; shared telemetry
is not attributed to a particular task without evidence.

## Deep Work

Deep Work uses Hermes on-box with a detached supervisor, durable coordinator
state, compute identity binding, explicit audited rebind, independent task
verification, durable checkpoint SHA, policy-bound publication, resumable
landing, final handoff/archive, and verified provider teardown. `stint deep
dash` presents coordinator/task state and safe controls. It is a separate
surface from the ordinary compute/runtime `stint dash`.

Generated Deep Work plans, checkpoints, and handoffs remain external evidence.
The successful session `20260923-022052` is preserved in open PRs #110–#113;
they are not product changes and must remain unmerged.

## Spark boundary

Spark observes repository changes and checks through `.spark/profile.yml`.
Stint remains the owner of provider and runtime lifecycle. Spark integration
should consume measured bootstrap/runtime evidence after the live acceptance
decision rather than duplicate provider control.

See the retained [early architecture snapshot](../history/pre-v0-architecture.md)
only for historical context.
