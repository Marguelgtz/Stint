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

A successful Hermes exit and a passing repository verification command are separate evidence. A task becomes `verified` only when the Hermes executor completed successfully, its independent verifier succeeds, a checkpoint commit is created, its exact commit SHA is read, and the verified state is persisted. Verification can run after an executor failure for diagnostics, but that result cannot accept the task. Durable state and the dashboard retain executor and verification outcomes separately. A task without a verifier remains `needs_human`.

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
