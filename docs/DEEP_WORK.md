# Deep Work

Deep Work runs bounded engineering missions on a rented GPU box. Stint prepares and verifies the box, then starts a detached on-box supervisor. The coordinator, Hermes, repository verification, checkpoint commits, publication, and final handoff all run on that box.

```text
Stint compute session
  → fresh-box bootstrap and qualification
  → detached on-box supervisor
  → durable coordinator state
  → Hermes task invocation
  → independent verification
  → checkpoint commit and SHA
  → publication
  → resumable landing and handoff
```

The supported operator entry point is `stint deep start`. It uses the existing READY Stint compute session and calls [`scripts/launch-onbox-deep.sh`](../scripts/launch-onbox-deep.sh) internally. The shell launcher remains the production bootstrap implementation; `stint deep onbox` remains its box-side coordinator command.

## Before launch

1. Build Stint with `make build`.
2. Start a Stint compute session with the NInfer runtime and wait for `READY`.
3. Commit the repository state you want copied to the box. `stint deep start` refuses tracked or untracked changes and the launcher stages the exact committed `HEAD`.
4. Prepare a mission with an independent verification command for every task or at mission level, plus the production `## GitHub` policy.
5. Keep Stint's Vast credentials at `~/.config/stint/credentials.json` and the least-privilege GitHub token at `~/.config/stint/github-token`. An existing `~/.config/vanta-r2.env` is used for optional R2 evidence; use `--r2-env-file` for a different location.

## Production launch

The normal Deep Work path is one command. When no compute session exists, an explicit `--hours` authorizes `stint deep start` to call Stint's existing paid rental/provisioning lifecycle first, then continue directly into detached Deep Work:

```sh
stint deep start \
  --hours 3 \
  --repo ~/Documents/projects/spark \
  --mission ~/Documents/projects/Stint/docs/missions/spark-mcp-graduation.md \
  --runtime ninfer \
  --ninfer-deployment release-bundle \
  --ninfer-config native \
  --clients 2 \
  --max-hourly-usd 0.45 \
  --max-cost-usd 1.35

stint deep dash
```

The compute portion still uses the standard `stint start interactive` lifecycle internally: Vast selection, cost policy, SSH qualification, NInfer/model startup, watchdog, tunnel, and READY state remain one implementation rather than a second Deep Work rental stack. After READY, the same command continues through the production launcher and detached on-box supervisor. The word `interactive` is therefore an internal provisioning profile in this path; Hermes inference and Deep Work execution remain GPU-local after handoff.

`--hours` is the explicit paid-compute opt-in. Without an active session and without `--hours`, `stint deep start` fails rather than renting unexpectedly. For a new session, production Deep Work defaults to NInfer with the native 262144-token context and rejects incompatible runtime/context choices before renting. A READY session must already be NInfer/native. If a READY Stint session already exists, omit compute-provisioning flags and the command reuses that session. Supplying new-session cost/runtime flags while a READY session exists fails instead of silently ignoring them.

Before any new rental is requested, Deep Work validates the mission and repository and checks the publication/R2 configuration needed after provisioning. The standard lifecycle then validates the Stint-managed SSH and Vast credentials while qualifying the compute session. Before staging, `stint deep start` prints the repository, exact source commit and origin, clean-tree status, mission, compute identity, GPU/runtime, remaining time/deadline, clients, model, task timeout, max attempts, GitHub policy, R2 setting, and cost estimates when recorded. The CLI resolves the instance ID, SSH endpoint, key, deadline, and client count from the resulting READY session state.

If compute reaches READY but the later Deep Work bootstrap fails before the durable RUNNING handshake, the paid READY session is preserved under the existing lifecycle/watchdog rules for diagnosis or retry; Stint does not hide the failure by starting a second rental.

The command snapshots the validated mission and pins the source HEAD/origin between its preflight and staging. It rejects uncommitted tracked and untracked files; only the clean committed HEAD is staged, and the operator checkout is not mutated. The launcher transfers the required Vast credential and the single GitHub token file, not the broad operator configuration directory.

`stint deep start` returns success only when the launcher reports both an `ONBOX_SUPERVISOR_RUNNING` process and a durable `RUNNING` record for the same Stint deadline. It records the on-box connection locally so `stint deep dash` opens the existing remote Deep Dashboard. Closing that SSH dashboard does not stop the detached supervisor or its deadline watchdog.

The default task timeout is 15 minutes, maximum attempts is 2, provider is `custom:qwen-stint-{reasoning}`, model is `qwen3.8-27b`, and reasoning is `medium`. Use `--task-timeout`, `--max-attempts`, `--provider`, `--model`, `--reasoning`, `--action-plan`, or repeatable `--allow-command` only when the mission needs an override. The mission's explicit `## GitHub` policy supplies mode, repository, base, allowed authors, and approval; the current publisher supports `engineering` mode.

For launcher diagnosis, [`scripts/launch-onbox-deep.sh`](../scripts/launch-onbox-deep.sh) remains callable directly, but the normal operator workflow should use `stint deep start`.

Before it reports `RUNNING`, the launcher transfers and runs the same bootstrap used by the live smoke. It installs missing Hermes/Node and a compatible Go toolchain when the target repo has `go.mod`, verifies NInfer and the selected model, runs `go test ./...` for Stint itself, installs phase proxy and observer scripts, configures xhigh/medium providers and compression, and makes real Hermes calls through both routes. When `STINT_ONBOX_CLIENTS=2`, it also verifies concurrent xhigh and medium traffic. Any failed step stops launch before the supervisor starts.

A Vast credentials file is required because the on-box watchdog enforces the compute deadline. `STINT_ONBOX_SKIP_WATCHDOG=1` is reserved for disposable fixtures and disables automatic teardown. The GitHub publisher can also be disabled with `STINT_ONBOX_SKIP_GITHUB=1` for fixtures; production sessions require publication setup.

## Mission format

```markdown
# Example repair

## Objective
Fix the retry bug in the parser.

## Verification
go test ./...

## Tasks
- [ ] PARSE-001: Reject the malformed retry header.
  - acceptance: the parser returns a clear error and keeps valid headers working.
  - verify: go test ./internal/parser
- [ ] PARSE-REVIEW-001: Review the parser change after implementation.
  - depends-on: PARSE-001
  - verify: go test ./internal/parser

## GitHub
- mode: engineering
- repository: owner/repository
- base: main
- approval: internal
- allowed-authors: alice, bob
```

Production on-box missions must persist an explicit GitHub policy. The launcher
configuration (mode, repository, base, allowed authors, and approval) must match
that mission section exactly or launch fails. The current on-box publisher only
supports `engineering`; maintenance merge authority remains a separately gated
feature. The fixture bypass can use `mode: none` with GitHub publishing disabled.

The box preflight checks that declared verifier executables are available. For
the Stint Go repository, bootstrap runs `go test ./...` before `RUNNING`. Other
repositories must provide their own reproducible dependency setup; a present
executable does not prove project dependencies are installed.

Coordinator task IDs beginning `STINT-PLAN-` and `STINT-CLOSE-` are reserved. `--action-plan` adds a coordinator-owned xhigh planning task using the reserved namespace.

## Verification and command guidance

A successful Hermes exit and a passing repository verification command are separate evidence. For a new task attempt, Stint captures a verification subject immediately after the executor returns and before running the task verifier. The subject records the observed `HEAD` SHA and a Git tree assembled from the worktree through a temporary index. It covers tracked content and deletions, non-ignored untracked files, file modes, symlinks, and initialized clean submodules. It starts from current worktree bytes (then applies Git's configured clean filters), rather than stale staged blobs. Uninitialized or dirty submodule worktrees fail closed because a superproject tree cannot represent their uncommitted contents.

After verification, Stint captures the subject again and checks it while preparing and recording the checkpoint. A mutation invalidates that verification evidence and leaves the task unaccepted. The checkpoint tree must equal the verified tree. If `HEAD` already has that tree, Stint reuses the existing commit; otherwise it creates a commit for the actual tree change, with a subject derived from the task objective. No empty task-marker commit is created. The task state persists both the verification subject and checkpoint tree SHA.

Git-ignored files and external runtime/toolchain state are outside this Git-tree identity; it is an exact Git-visible product-tree identity, not a hermetic filesystem or runtime snapshot. Local Hermes and local verifier commands run in Stint-owned process groups, and the coordinator terminates remaining group members before returning to subject capture. Remote invocations use a `setsid --wait` supervisor and bounded `timeout`; setup or cleanup failure is an execution error, not stable verification evidence. This boundary covers ordinary descendants that remain in the owned process group, not processes that deliberately detach into another session. If a remote transport failure prevents Stint from confirming quiescence, the task is marked `needs_human`, the run records the unresolved writer boundary, and verification, checkpointing, and further tasks stop.

Configured action-plan files and `DEEP_WORK_HANDOFF.md` are treated separately when they are untracked Stint bookkeeping: Stint records a Git blob identity (or absence) in `verificationBookkeeping` and excludes them from product checkpoints. If such a file is tracked or staged, it is Git-visible and participates in the product tree. Landing will not rewrite a Git-visible handoff input after final verification; the generated handoff remains durable under the Stint state directory. For an untracked handoff copy, changes to its separate metadata do not change the product-tree subject.

Mission-level final verification uses the same subject boundary as task verification: Stint captures the product subject, runs the configured verifier, captures it again, and persists the result only if the subject remained unchanged. An interrupted landing may reuse that result only when the persisted subject and any relevant untracked action-plan identity still match. Otherwise it clears the stale evidence and reruns the verifier. The landing checkpoint tree SHA must equal the final verification tree SHA. Legacy `LandingVerifyDone` values without a subject are not reused when a mission verifier is configured. A stop request received during an active task defers landing verification until the executor owner confirms it returned; if the owner never returns, Stint leaves an explicit quiescence block rather than verifying concurrently.

`landed` is an operational phase: Stint reached a recoverable stopping and handoff boundary. It does not by itself mean the mission succeeded. The separate `missionOutcome` is `succeeded` only when every task has verified evidence bound to its checkpoint and any configured final mission verifier passed on the landing checkpoint tree. An explicit non-zero final mission verifier on that exact checkpoint produces `failed` without changing previously verified task evidence. Remaining unaccepted tasks produce `incomplete`; when task evidence is otherwise complete, missing final-verifier provenance or a timeout, cancellation, execution error, or absent required proof produces `unresolved`. A2 state without the typed final-verifier outcome is rechecked on resume rather than promoted from its prose summary. Legacy landed state without a recorded mission outcome remains unknown; Stint does not synthesize a historical outcome. The handoff and `stint deep status` show phase and mission outcome separately, and `deep start`/`deep resume` returns an error after safely landing a mission whose required final verifier failed.

Verification can run after an executor failure for diagnostics, but that result cannot accept the task. Durable state and the dashboard retain executor and verification outcomes separately. A task without a verifier remains `needs_human`. Legacy task records remain readable; absent subject fields stay absent and do not gain synthetic historical evidence.

Mission and task verification values are trusted shell input and run through `sh -c`; Stint does not sandbox them. A mission may express a command in a supported shell fenced block, which the mission parser stores as raw command data. Persisted commands must remain raw: inline backtick wrappers are rejected during mission parsing, state preflight, and immediately before local or remote execution. Shell syntax inside a raw command, including command substitution such as ``echo `date` ``, remains shell syntax and is preserved. Verifier outcomes distinguish pass, nonzero exit, timeout, cancellation, invalid command, and execution or transport error, with the exit status and bounded output retained where available.

Review tasks may use `depends-on: IMPLEMENT-001, TEST-001` to name prerequisites declared earlier in the mission. The coordinator runs the review only after each prerequisite reaches `verified`; if a prerequisite ends blocked or needs human input, the review is recorded as blocked without invoking Hermes. The Tasks view shows a task's configured reasoning level, or `inherit` when it uses the session default.

`--task-timeout` is a per-invocation maximum. When less time remains before the landing cutoff, the coordinator shortens an invocation only if it can still reserve the task-verification bound and checkpoint overhead. It defers work when the remaining executor window is below five minutes (or below the configured maximum when that maximum is shorter). The chosen timeout and its reason are persisted with the task attempt.

Timing controls remain separate: the Vast `--hours` rental cap establishes the paid compute deadline; the Deep Work `--deadline` follows that deadline in the on-box launcher unless explicitly set; `LandBefore` protects the final landing window (normally 10 minutes, or one quarter of sessions shorter than 40 minutes, with a 2-minute minimum); `--task-timeout` is the maximum for each Hermes invocation (15 minutes in the production on-box launcher by default); `--max-attempts` limits retries (2 in that launcher); a configured task-verification command has a 3-minute bound, while a task with no verifier reserves no separate verifier timeout; NInfer's pending-request timeout is 600,000 ms; the on-box NInfer observer samples every 10 seconds; sanitized R2 heartbeats run every 20 seconds; and transient final-publication retries default to 12 attempts with 5 seconds between attempts. Policy and immutable identity conflicts stop publication immediately. Verification's 3-minute bound and the coordinator/checkpoint reserve are not coding-task timeout recommendations. A shortened coding invocation is still required to meet the five-minute useful-work floor when the configured maximum is longer.

`--allow-command` adds advisory prompt text only. Stint does not enforce a command allow-list at the Hermes process boundary. Do not treat this setting as a security boundary.

Hermes prompt files are created in protected temporary storage outside the target repository and removed after each invocation. Phase and compression counts shown by the worker view come from shared host logs. They are explicitly not attributed to a particular Deep Work session and are not task acceptance evidence. The Worker view also fetches a redacted tail of `/root/.hermes/logs/agent.log` over SSH (at most 80 lines/16 KiB); the remote tail command redacts common token and secret fields before returning data. Missing SSH/log access is shown as an unavailable observation and does not change durable run status. This passive read uses the session's persisted matching instance endpoint and does not require the local inference tunnel or READY lifecycle state.

For the production `hermes-onbox` worker, Deep Dash runs the same bounded observer and log-tail helpers co-located on the GPU host. It does not require synthetic session state to contain a separate SSH endpoint. Unavailable local observation is reported on the Worker view and does not change the durable Deep Work phase.

The detached supervisor samples NInfer `/metrics` and `/slots` every 10 seconds into a mode-0600, bounded `ninfer-runtime.jsonl` (up to 1,200 rows/6 MiB). Each row records configured NInfer clients separately from exposed engine slot rows, slot processing/retained/context/prompt depth, allow-listed request/cache/speculative counters, and counter-derived prefill and live-engine decode rates where consecutive monotonic samples exist. `session_digest`, prompts, and caller identity are omitted. Live-engine decode remains separate from benchmark decode.

R2 remains a bounded evidence sink. The supervisor's sanitized snapshot is uploaded as `latest.json` and a timestamped `heartbeats/<UTC stamp>.json` using credentials from the configured R2 environment file (default `/var/lib/stint-onbox/config/r2.env`). Heartbeats exclude Hermes logs, prompts, source files, and credentials. The final allow-list is `deep.json`, `mission.md`, `handoff.md`, `incidents.jsonl`, `publication.json`, and the bounded `ninfer-runtime.jsonl`, plus `provenance.json`; raw Hermes logs are not archived to R2.

## State and recovery

Deep Work state is stored on the GPU under the supervisor's state root (default `/var/lib/stint-onbox/state/stint/deep`). It records the owning Vast instance. The dashboard only attaches live telemetry and enables landing when the active READY compute instance matches that binding. Resuming on replacement compute requires an explicit audited rebind, and Stint first checks that the saved branch and worktree can be recovered there.

`deep.json` remains the readable current-state projection. Journal-aware runs also append versioned facts to `run-events.jsonl` in the run's state directory. The session ID is the stable run ID; each new or resumed execution period gets a separate epoch ID, while event sequence numbers continue monotonically across the whole run. Schema version 1 identifies the event envelope and field encoding; additive event types may be introduced within v1. Readers explicitly recognize supported types and fail closed on an unknown or malformed complete event. Older binaries are not guaranteed to read journals containing event types they do not know. Existing B1 lifecycle journals remain readable, and no executor history is synthesized for them. Executor records include task and attempt identity, configured/effective timeout and remaining-deadline context, bounded worker/provider/model identity (including the resolved provider actually passed to Hermes), compute binding, and Git-visible repository identity before and after execution. Prompts, environment dumps, full logs, and secrets are excluded; no durable executor artifact is currently produced, so artifact references are empty. Verifier invocations are not yet journaled.

For journal-aware runs, `executor.started` and its projected active-task attempt are synced immediately before Hermes is launched. This is the durable invocation boundary; by itself it does not prove the Hermes process was reached. A normal `executor.result` is synced after Hermes returns and writer quiescence is confirmed, before task verification starts. It records the observed exit/outcome and the resulting Git-visible product-tree identity. A timeout or known execution failure remains an executor result; an unconfirmed process group records that uncertainty and sets the existing hard quiescence block. If resume finds a start without a result, it appends `executor.recovery_required`, marks the outcome and quiescence unknown, and stops before verification, retry, or further execution. The recovery event's `occurredAt` is when Stint observed the missing result; the executor record keeps `endedAt` zero and has no fabricated duration, exit, or completion fact. This implementation cannot prove that a process from a crashed coordinator has stopped, so it does not automatically relaunch that task.

When an executor result is durable but its projection write failed, `deep.json` replay applies the result once and resume continues from the recorded result without launching Hermes again. If the process stopped before a result was durable, the unmatched-start recovery block is the truthful state. The current slice has no persisted process-group registry or explicit operator reconciliation command, so that hard block remains sticky; clearing it safely is a later recovery capability. A resumed verification may reuse a completed executor result only while the product tree SHA still matches the recorded post-executor tree; a checkpoint-only HEAD advance with an unchanged tree is allowed, and verification is rerun against the current exact subject before checkpoint reuse. A changed product tree leaves the task `needs_human` instead of attributing that state to the old invocation. `SaveDir` checks journal-owned executor fields against the current watermark event and refuses contradictory projection writes.

Journal appends and projection writes share a per-run file lock. Every durable `deep.json` write advances `projectionRevision`; normal state saves and journal transitions compare the revision loaded by the caller with the current durable revision and reject stale writers. This prevents a lifecycle transition from replacing newer task or verification/checkpoint evidence. For a resume, bounded context changes such as executor settings and compute binding are intentionally persisted at the previous event watermark; the old deadline remains in place until the epoch event records its replacement. Stint then appends and syncs the event before atomically projecting the lifecycle transition and advancing the watermark. Loading state replays complete events after that watermark. A projection watermark ahead of the journal, malformed complete event, or invalid event order fails closed. Only a final unterminated append is quarantined as `run-events.incomplete-tail` and truncated; earlier complete events are left untouched. If a Git checkpoint exists but the landing event was not recorded, a resumed landing reuses the matching checkpoint rather than creating another commit.

Journal validation also keeps landing-reason identity continuous: resuming an interrupted landing preserves its recorded reason, and the landed event must carry that same reason. An inconsistent complete history fails before projection replay or persistence.

Legacy `deep.json` remains readable without fabricated history. Its first journal-aware resume begins at sequence 1 with an explicit `legacy_resume` epoch boundary; pre-journal task attempts do not gain synthetic executor events. In B1, A3 `missionOutcome` remains a derived `deep.json` projection: `run.landed` records the operational landing and checkpoint, not mission success. This executor-record slice still does not journal final-verifier results or their provenance; a later B2 boundary must establish those facts. This is separate from Objective C task acceptance, and no legacy `verified` task is promoted into a new `accepted` claim.

`stint deep dash` provides Run, Tasks, Activity, Worker, and Phase views. Task and Phase views expose independently verified status, verification time, and exact checkpoint SHA. Phase also shows the saved base/compute binding, final landing verification, landing commit, and whether the handoff is written. Selecting a historical session labels it as such and disables landing; the confirmation path also requires the latest session to match its READY bound compute.

The on-box supervisor resumes persisted settings after a process failure. It does not take omitted command-line defaults as new policy. Landing is a durable transaction; an interrupted landing is resumed from its saved phase, and the handoff records the exact landing commit.

To inspect from the GPU host:

```sh
XDG_STATE_HOME=/var/lib/stint-onbox/state /var/lib/stint-onbox/bin/stint deep status
XDG_STATE_HOME=/var/lib/stint-onbox/state /var/lib/stint-onbox/bin/stint deep dash
```

To inspect supervisor readiness from the operator host:

```sh
ssh -i "$STINT_BOX_KEY" -p "$STINT_BOX_PORT" "root@$STINT_BOX_HOST" \
  "STINT_ONBOX_ROOT=/var/lib/stint-onbox /var/lib/stint-onbox/onbox-deep-supervisor.sh status"
```

The supported live smoke is [`scripts/run-onbox-deep-smoke.sh`](../scripts/run-onbox-deep-smoke.sh). It rents a bounded fixture session and calls the same launcher and bootstrap path as production.

## Current limits

- Phase and compression logs are host-wide. Session/task/invocation correlation is not yet available.
- Preflight detects missing executables. Outside the Stint Go project, it does not install arbitrary language dependencies or prove a project's full test suite.
- A passing bootstrap is not a completed mission. Completion still depends on coordinator verification, durable publication, final handoff, and the deadline watchdog.

See the canonical [Deep Work integration action plan](DEEP_WORK_ONBOX_EXECUTION_PLAN.md) for current branch/PR status, evidence, and remaining work.

See the [2026-09-24 stabilization evidence report](DEEP_WORK_STABILIZATION_2026-09-24.md) for the grounded PR review, latest forensic findings, and stale session-checkpoint dispositions.
