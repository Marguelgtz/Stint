# Stint P0 Session Reliability & Doctor

**Status:** P0, active living plan  
**Integration branch:** `fix/p0-ninfer-session-safety`  
**Integration PR:** #76, based on the current NInfer artifact-fix line (#74)  
**Primary objective:** A paid Stint session must always be stoppable, attributable, diagnosable, and recoverable without requiring a second rental.

## Reliability invariants

1. **Teardown has priority over startup.** An in-flight `start` or `resume` must never make `stint down` unavailable for a paid session.
2. **Paid compute is never assumed destroyed.** Local paid-resource state is cleared only after Vast confirms the instance no longer exists.
3. **Deadline protection survives transient provider failures.** A watchdog destroy error is recorded and retried rather than silently abandoning the paid resource.
4. **One session, one evidence namespace.** Runtime/tunnel/watchdog evidence must be attributable to one Vast instance.
5. **Doctor reports session reality.** When a paid session exists, `stint doctor` diagnoses that session rather than printing only preflight readiness.
6. **Doctor is read-only.** Diagnosis must not rent, resume, destroy, or otherwise mutate provider compute.
7. **Recover before rerenting.** A recoverable existing instance is preferred over a second paid rental.

## Current incident: 2026-09-06

Observed on the current NInfer/dashboard stack while starting a one-hour RTX 4090 NInfer-native, two-client session.

Recorded session:

- Vast instance: `50099090`
- GPU: RTX 4090
- Runtime: NInfer native
- Context: 262,144 tokens
- Rate: $0.354/hour
- Recorded state: `BOOTING`
- Checkpoint: `INSTANCE_CREATED`
- Tunnel: not running
- Watchdog: running

Operator-visible failure:

- `stint start`/`resume` correctly rejected concurrent lifecycle mutation.
- `stint down` and `stint down --yes` then waited behind that same lifecycle lock, making teardown effectively lower priority than the in-flight startup.
- `stint doctor` simultaneously reported only preflight checks and concluded that Stint was ready for paid start, despite the recorded paid BOOTING instance.

Historical evidence in the same shared logs also included repeated tunnel forward connection refusals, SSH network failures from several hosts, and a watchdog Vast DELETE that failed on a DNS timeout. Because those logs were append-only and shared across rentals, not every line can be safely attributed to one instance.

## What the current stack already had

The current dashboard/NInfer stack was ahead of the old main-based P0 branch in several important areas:

- `stint down` sends the Vast destroy request and then polls authoritative provider state until the instance is confirmed absent before clearing local state.
- If disappearance cannot be confirmed, local session tracking is retained and teardown is safe to retry.
- The dynamic deadline watchdog reloads session state while waiting, rechecks under the lifecycle lock at deadline, and retries failed provider destruction rather than exiting permanently.
- Session JSON is archived before confirmed teardown clears active state.
- `stint status --refresh` already has a shared read-only snapshot/telemetry layer for tunnel, watchdog, endpoint, SSH/runtime, GPU, inference, performance, and state freshness.

These existing mechanisms supersede the main-based #75 teardown rewrite. The P0 integration should extend them, not replace them.

## Execution status

Legend: `[ ]` planned, `[~]` in progress, `[x]` verified, `[!]` blocked, `[-]` superseded/deferred.

### P0-A — Paid-resource control and teardown

- [x] Confirm instance disappearance before clearing state (existing current stack).
- [x] Preserve session state when destruction is unconfirmed (existing current stack).
- [x] Dynamic watchdog retries provider/API destruction failures (existing current stack).
- [x] Authoritative Vast v1 instance inventory lookup with exact instance-ID filtering integrated on #76.
- [x] Lifecycle lock records owner PID, operation, start time, and executable.
- [x] `stint down` can gracefully preempt a verified in-flight `start`/`resume` owner.
- [x] `stint down --yes` can preempt only the exact recorded watchdog when executable and lock-file ownership are verified.
- [x] Unknown/legacy lock owners are never blindly signaled; teardown waits with a bounded timeout.
- [x] No SIGKILL fallback.
- [x] Deterministic lock/watchdog/provider tests.
- [x] CI verification for lifecycle/provider integration: run #324 passed.

### P0-B — Durable, attributable evidence

- [x] Archive final `session.json` before confirmed teardown (existing current stack).
- [ ] Move tunnel logs from shared `tunnel.log` to an instance-scoped evidence path.
- [ ] Move watchdog logs from shared `watchdog.log` to an instance-scoped evidence path.
- [ ] Preserve a bounded remote runtime/model log tail before teardown when SSH is available.
- [ ] Make `doctor --last` surface relevant retained evidence, not only archived state.
- [ ] Add retention/rotation policy so evidence remains bounded.

### P0-C — Active Doctor

- [x] Reuse the existing `sessionSnapshot`/telemetry model instead of introducing a duplicate probe stack.
- [x] Exact provider-instance probe.
- [x] Lifecycle-lock owner probe.
- [x] SSH/runtime probe through existing telemetry.
- [x] Tunnel probe.
- [x] Local `/v1/models` probe.
- [x] Watchdog probe.
- [x] Root-cause classification for provider failure, missing instance, provider boot, active startup, anonymous/legacy lifecycle owner, stalled startup, SSH failure, dead runtime, missing tunnel, unavailable endpoint, and missing watchdog.
- [x] Safety ordering: missing watchdog outranks apparent startup progress.
- [x] `stint doctor --json`.
- [x] `stint doctor --last` using current-stack session archives.
- [x] Active Doctor implementation CI verification: run #328 passed after fixing a diagnostic-name collision.
- [ ] Add explicit remote `:8080` readiness observation.
- [ ] Add NInfer model-acquisition phase observation so active download/verification/load is distinguishable from a stalled launcher.
- [ ] Add transport-specific SSH diagnosis (`refused`, `unreachable/timeout`, `auth`).

### P0-D — Status / operator surface

- [x] `status --refresh` already exists on current stack.
- [x] Active Doctor now uses the same underlying telemetry model.
- [ ] Surface lifecycle-owner identity in refreshed status/dashboard when a lifecycle command is active.
- [ ] Surface a prominent paid-session safety warning when provider state is unknown or deadline protection is missing.

### P0-E — Fault-injection / regression tests

- [x] Concurrent lifecycle mutation rejected.
- [x] Owner metadata written and cleared under flock.
- [x] Safe wait/reacquire after owner release.
- [x] Watchdog preemption requires exact recorded PID + executable + lock ownership.
- [x] Provider inventory exact-ID lookup and missing-instance behavior.
- [x] Active-start Doctor classification.
- [x] Anonymous lifecycle owner classified as safety issue.
- [x] Startup without a verified owner classified as stalled/recoverable.
- [x] Missing tracked provider instance classified as safety issue.
- [x] Missing tunnel and watchdog root-cause ordering.
- [ ] Tunnel-forward-refused while NInfer model acquisition is still progressing.
- [ ] Transient provider DNS failure during deadline teardown followed by successful retry, exercised end-to-end with fake provider dependencies.

### P0-F — Controlled live validation

- [ ] Build the #76 integration binary locally.
- [ ] Use the shortest practical paid session.
- [ ] Verify active Doctor during provider boot.
- [ ] Verify `stint down --yes` can preempt an in-flight start and obtains the lifecycle lock.
- [ ] Verify provider disappearance before local state clear.
- [ ] Verify no second rental is created during recovery.
- [ ] Record instance ID, runtime/config, timeline, diagnosis, cost, and final destruction confirmation here.

**Paid live validation requires an explicit operator decision. Do not initiate a new rental merely to test this P0.**

## Current integration topology

The safety work had become fragmented across sibling PRs:

- #71: lifecycle owner metadata and start/resume recovery.
- #72: watchdog-aware `down` recovery plus authoritative Vast instance lookup.
- #74: current NInfer artifact revision/transfer-size fix used by the incident binary.
- #75: main-based P0 Doctor/teardown experiment, now architecturally stale relative to the current dashboard/NInfer stack.

PR #76 is the consolidation line. It is based on #74 and semantically transplants the compatible #71/#72 safety work, then implements active Doctor on top of the current `sessionSnapshot` telemetry architecture.

## Superseded assumptions

### “The watchdog needs an entirely new retry architecture”

**Superseded.** The current dynamic watchdog already records a failed deadline destroy and retries at a fixed interval until teardown succeeds or session state is removed/replaced. The historical DNS timeout remains useful fault evidence, but the current implementation already addresses the central persistence problem.

### “The main-based #75 teardown implementation should be merged wholesale”

**Rejected.** The current NInfer/dashboard line already has confirmed-disappearance teardown, dynamic watchdog retry, richer status telemetry, and session archives. Wholesale merging #75 would duplicate or regress those mechanisms. #76 integrates only the missing invariants into the current stack.

### “Connection refused in shared tunnel.log proves one specific rental’s tunnel failed”

**Rejected.** The historical file contains output from multiple Vast hosts and sessions. A forward refusal does show that an SSH channel could not connect to its remote target at that moment, but the shared append-only file is insufficient to attribute every refusal to the latest rental. Instance-scoped logs are required before using these lines as per-session forensic proof.

## Exit criteria

P0 is complete only when all are true:

- [x] A paid start/resume cannot permanently block operator teardown through the lifecycle lock.
- [x] State is not cleared until provider disappearance is confirmed.
- [x] Deadline teardown survives transient provider/API failure through retries.
- [x] Active `stint doctor` diagnoses paid-session reality instead of preflight state.
- [ ] Every session has isolated tunnel/watchdog/runtime evidence.
- [ ] Doctor can distinguish active NInfer acquisition/load from a dead/stalled runtime.
- [ ] Deterministic tests cover the observed remote-forward-refused-during-startup shape.
- [ ] One controlled live validation proves start -> diagnose -> preempt/down -> confirmed-gone without a second rental.

## Living-plan protocol

Do not erase disproven hypotheses. Move them into **Superseded assumptions** with the evidence that changed the decision.

Each controlled live validation must append:

- date/time
- branch + commit
- Vast instance ID
- GPU/runtime/config/client count/context
- observed lifecycle timeline
- Doctor diagnosis at each interesting phase
- recovery/teardown actions
- provider destruction confirmation
- estimated cost
- unexpected behavior
