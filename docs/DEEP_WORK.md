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

The supported production entry point is [`scripts/launch-onbox-deep.sh`](../scripts/launch-onbox-deep.sh). `stint deep onbox` is the supervisor's box-side command. It expects the launcher to have prepared Hermes providers, phase routing, the verification toolchain, and durable compute identity first.

## Before launch

1. Build Stint with `make build`.
2. Start a Stint compute session with the NInfer runtime and wait for `READY`.
3. Commit the repository state you want copied to the box. The launcher stages the exact `HEAD`; it does not transfer local uncommitted files.
4. Prepare a mission with an independent verification command for every task or at mission level.
5. Provide a GitHub token file with the least authority the configured publisher needs, the target repository and base branch, and the Vast credentials file used by the deadline watchdog.

## Production launch

```sh
STINT_BOX_HOST=203.0.113.10 \
STINT_BOX_PORT=22 \
STINT_BOX_KEY="$HOME/.config/stint/ssh/id_ed25519" \
STINT_MISSION="$PWD/mission.md" \
STINT_REPO="$PWD" \
STINT_GITHUB_TOKEN_FILE="$HOME/.config/stint/github-token" \
STINT_GITHUB_REPOSITORY=owner/repository \
STINT_GITHUB_BASE=main \
STINT_VAST_CREDENTIALS="$HOME/.config/stint/credentials.json" \
STINT_ONBOX_CLIENTS=2 \
scripts/launch-onbox-deep.sh
```

The launcher obtains the instance ID and deadline from the READY Stint state file. If using a non-default state directory, set `STINT_SESSION_JSON` or set `STINT_INSTANCE_ID` and `STINT_DEADLINE` to the same values recorded in that READY state. It refuses an expired session or an identity mismatch.

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

A successful Hermes exit is a worker claim. A task becomes `verified` only after its independent verifier succeeds, a checkpoint commit is created, its exact commit SHA is read, and the verified state is persisted. A task without a verifier remains `needs_human`.

`--allow-command` adds advisory prompt text only. Stint does not enforce a command allow-list at the Hermes process boundary. Do not treat this setting as a security boundary.

Hermes prompt files are created in protected temporary storage outside the target repository and removed after each invocation. Phase and compression counts shown by the worker view come from shared host logs. They are explicitly not attributed to a particular Deep Work session and are not task acceptance evidence.

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
