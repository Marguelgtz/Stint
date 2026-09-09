# On-box Deep Work execution plan

This is the required topology for the first unattended CP1 run. The rented instance
must continue the mission after the operator machine disconnects or powers off. A
local Stint coordinator is a development fixture only.

After the launch handshake, the operator machine is limited to optional read-only
inspection. It must not coordinate tasks, upload run evidence, create commits, or
publish GitHub changes. The GPU instance owns those actions until landing.

**Live status (2026-09-09):** P6.1 local-on-box Hermes execution, P6.2 detached
supervisor/restart loop, P6.5 reconnectable sanitized heartbeat, GPU-side GitHub
checkpoint/PR publishing, and explicit R2 uploader provenance are implemented on
the publishing branch. This branch adds explicit engineering/maintenance policy,
GPU-side inventory and merge gating, and report-and-destroy ordering. The R2 credentials and object contract pass an isolated
bucket smoke. The live GPU handoff, disconnect proof, GitHub publish proof, and
full two-lane gate still require a real qualifying GPU run.

## Living checklist

- [x] Add a local Hermes executor with no SSH loopback.
- [x] Add a detached supervisor with PID/heartbeat and restart-from-state behavior.
- [x] Add a launch helper that exits after the remote `RUNNING` handshake.
- [x] Add sanitized heartbeat and final-state R2 upload hooks.
- [x] Make the GPU publish verified checkpoint commits and stackable GitHub PRs.
  The publisher creates one remote checkpoint branch per verified task, stacks each
  PR on the previous checkpoint branch, persists `publication.json`, and adds a
  final handoff layer at landing. Live GPU proof is still pending.
- [x] Pass a least-privilege GitHub publish credential to the GPU at launch and
  persist branch/PR URLs in the on-box state. Production launch fails closed when
  the token file, repository, base branch, origin match, or repository push access
  is unusable. `STINT_ONBOX_SKIP_GITHUB=1` is an explicit fixture-only bypass.
- [x] Add explicit `none`, `engineering`, and `maintenance` GitHub policies,
  persisted completion/phase metadata, compact inventory/context/reply tooling,
  deterministic merge gates, SHA-locked merge requests, and an append-only
  GPU-side action ledger. Maintenance excludes all `stint/deep-*` branches.
- [x] Add report-and-destroy completion ordering: final GitHub/R2 publication,
  handoff archive, provider teardown, disappearance state, and deadline-watchdog
  fallback. `bounded-replan` remains opt-in.
- [ ] Make NInfer bootstrap resumable after launch SSH loss (the model marker loop
  is now safe against a completed download and PID reuse; a dropped-SSH live proof
  is still required).
- [ ] Prove the supervisor survives launch-process termination and operator disconnect
  on a real GPU (the detached restart/landing fixture passes locally).
- [ ] Prove the on-box deadline watchdog destroys the instance after a provider/DNS retry.
- [ ] Verify R2 heartbeat and final archive objects on a live run, including
  `provenance.origin=gpu-instance`, the Vast instance id, GPU hostname, GitHub
  publication state, and the final `provenance.json` object.
- [ ] Repeat the two-lane xhigh/medium phase smoke through the on-box supervisor.
- [ ] Unlock the real CP1 mission only after all gates above pass.

## What is already complete

- Hermes can execute file and shell work on the GPU box through the existing remote
  executor.
- The coordinator loop, per-task verification, checkpoint commits, handoff, phase
  routes, compression observer, and two-lane preflight have been tested in the
  prototype topology.
- The dashboard renders local Deep Work state and sanitized GPU observations.
- A completed prototype smoke was archived to Cloudflare R2 manually.
- `stint deep onbox` runs the coordinator and Hermes as co-located local processes;
  `scripts/onbox-deep-supervisor.sh` detaches and restarts it from `deep.json`.
- `scripts/launch-onbox-deep.sh` transfers a pinned Stint runtime, mission, explicit
  action-plan input, and a standalone repository clone at the exact source `HEAD`.
  It works when the source is a linked git worktree and deliberately excludes dirty
  and untracked operator files from the GPU repository baseline. The action-plan
  input is transferred as an on-box seed and copied by the GPU coordinator to the
  requested worktree-relative path before the xhigh planning task starts.
- Sanitized heartbeat and final-state R2 helpers are available when an explicit,
  root-only R2 credential file is supplied.
- `scripts/onbox-github-publish.py` runs on the GPU only. It reads the root-only
  GitHub token file, pushes verified checkpoints without writing the token into git
  config or command arguments, creates draft stackable PRs, and records their
  branches, commit SHAs, numbers, and URLs in `publication.json`.
- Each verified task persists its exact accepted `checkpointCommit` in `deep.json`
  after checkpointing. The coordinator always creates a distinct acceptance marker
  commit, including when the worker already committed its changes or verification
  accepted a no-op. The publisher validates this SHA as an ancestor of the landing
  branch before pushing it; commit-subject discovery remains only for sessions
  created before this field existed.
- The supervisor retries publication while the run is active and requires the final
  landing publication to converge before it reports a clean landed supervisor exit.
- Heartbeats now include sanitized origin/publication fields. R2 heartbeat objects
  also carry object metadata identifying origin, instance id, hostname, uploader,
  and machine. The final archive includes `publication.json` when present plus a
  generated `provenance.json` object.

The `dashboard-smoke` prefix is from the legacy operator-side `cp1-upload.sh`
path: it retrieves local state first and records `machine: MGR-PC` in
`meta-upload.json`. That path is suitable for local dashboard/compression fixtures
only. It is not the Deep Work topology and must not be used after an on-box launch
handshake. The on-box launcher instead transfers `onbox-r2-sync.py`,
`onbox-r2-archive.py`, and the R2 credential file to the GPU; the detached
supervisor executes those helpers on the instance. Existing fixture objects are
retained until an operator explicitly authorizes deletion.

Verified checkpoint publication is now owned by the instance. For every task that
reaches coordinator-verified state, the publisher resolves its checkpoint commit,
pushes a stable session/task branch, and opens a draft PR. The first PR targets the
configured `STINT_GITHUB_BASE`; each later PR targets the previous checkpoint branch.
At landing, the generated handoff is pushed as a final stack layer. The laptop may
later inspect those PRs, but it is neither a publisher nor a fallback.

The failed phase/lane retry stopped before Deep Work because the foreground NInfer
bootstrap lost SSH at 83% model transfer. It produced no task evidence and did not
test the mission. The corrected runner then made a bounded five-candidate attempt:
four hosts failed measured throughput or SSH startup, and the final host measured
18.1 MB/s. All five were rejected before model execution and cleaned up.

The independent R2 smoke uploaded `latest.json`, one timestamped heartbeat, and the
safe `deep.json`, `mission.md`, `handoff.md`, and `incidents.jsonl` files to
`s3://deep-work/vanta/onbox/plan-r2-smoke-20260908T025928Z/`. A read-back listed all
six objects and confirmed that the heartbeat contained no prompt or raw-output
fields. This proves credential parsing, S3 signing, prefix isolation, and the
sanitized payload contract; it is not a substitute for a live supervisor archive.

The detached-supervisor fixture then exercised a failed first coordinator attempt,
restart from `deep.json`, landing, and EXIT cleanup. It produced repeated sanitized
heartbeats and a final four-file archive at
`s3://deep-work/vanta/onbox/fixture-supervisor-20260908T030515Z/`; read-back showed
the final phase `landed`, one verified task, and the `hermes-onbox` worker. This
proves the local supervisor/restart and R2-hook seams without claiming a GPU
disconnect proof. That historical fixture predates GitHub publication and the new
R2 provenance object contract.

An R2 audit on 2026-09-08 found that this fixture prefix contained 1,237 objects,
including 1,232 heartbeat snapshots uploaded between 04:05 and 05:26 BST. The
coordinator had already landed; the heartbeat child survived because the EXIT trap
could not see a function-local PID after `run_supervisor` returned. The supervisor
now keeps that PID at script scope, reaps it on EXIT, and routes TERM/INT through
the same cleanup path. The fixture objects are test data, not evidence of a live
GPU run, and are intentionally retained until an operator authorizes R2 cleanup.

## GPU GitHub credential contract

A production on-box launch requires these inputs before the `RUNNING` handshake:

```sh
export STINT_GITHUB_TOKEN_FILE=/path/to/root-readable-github-token
export STINT_GITHUB_REPOSITORY=Marguelgtz/Stint
export STINT_GITHUB_BASE=<remote-base-branch>
```

The credential should be least privilege for the target repository: repository
contents read/write and pull requests read/write. The launcher normalizes either a
single-token file or a `GITHUB_TOKEN=` / `GH_TOKEN=` env-style file to a root-only
one-line secret on the GPU. It then executes a GPU-side preflight that validates the
repository origin, token repository access, push permission when GitHub exposes it,
and existence of the configured base branch. The operator-side temporary normalized
file is removed when the launcher exits.

The token is not persisted into `.git/config`, a remote URL, a PR body, R2 state, or
a command argument. Git push uses an ephemeral askpass helper on the instance. The
only supported no-GitHub path is `STINT_ONBOX_SKIP_GITHUB=1`, which is explicitly a
fixture mode and is not valid evidence for CP1 readiness.

An optional living action plan uses two distinct inputs:

```sh
export STINT_ONBOX_ACTION_PLAN=/path/to/action-plan-seed.md
export STINT_ONBOX_ACTION_PLAN_PATH=deep-work/action-plan.md
```

The first is the operator-selected seed transferred during launch. The second must
stay inside the Deep Work worktree. The GPU coordinator copies the seed there before
creating `PLAN-001`; the laptop does not edit or commit the plan after handoff.

## P6 implementation gates

1. **Local-on-box worker.** **Implemented.** Add an on-box execution mode that uses the local Hermes
   process, local NInfer endpoint, local git worktree, and local verification. It must
   not create an SSH client pointed back at the same instance.
2. **Detached supervisor.** **Implemented in scripts; live proof pending.** Transfer a pinned Stint runtime, mission, action plan, and
   repository baseline. Start a detached supervisor with a lock, a heartbeat, and
   durable state under the instance. The launch connection may exit after the
   supervisor writes `RUNNING`.
3. **Resilient bootstrap.** **Launcher seam ready; runtime integration pending.** Build and model prefetch run as resumable remote jobs with
   progress files and sentinels. A dropped launch SSH connection must not kill the
   bootstrap or mark the mission failed. Reconnect reads the sentinel and resumes
   polling.
4. **Remote deadline authority.** **Watchdog handoff implemented; provider credential/TTL proof pending.** The instance owns the deadline watchdog and can
   destroy its own Vast instance after the landing window. Prefer a provider-native
   lease/TTL. If Vast requires an API call, use a narrowly scoped root-only credential
   or an equivalent remote control token; never depend on a laptop process.
5. **Remote evidence publishing.** **GitHub/R2 implementation complete; live proof pending.** Every heartbeat and state transition writes a
   sanitized snapshot containing session phase, active task, verified/blocked counts,
   last checkpoint commit, publication summary, origin provenance, model/phase
   counters, compression counters, error domain, and deadline. Upload snapshots and
   final artifacts to an R2 prefix owned by this session. GitHub checkpoint branches
   and stack PRs are created by the instance. Do not upload prompts, raw Hermes
   output, credentials, or arbitrary shell output.
6. **Reconnectable inspection.** **Supervisor status implemented; CLI/R2 reconnect test pending.** Add a read-only remote status command. When online,
   it reads the instance state over SSH; when the instance is gone, it reads the last
   R2 snapshot and final handoff. Inspection must never be required for execution.

## Readiness test before CP1

Use a one- or two-task synthetic mission and run the following gates in order:

1. Start the supervisor and verify `RUNNING` plus a remote heartbeat. Record the
   source `HEAD`, GitHub repository/base, instance id, R2 prefix, and deadline.
2. Terminate the launch process and close the operator SSH connection. Do not run a
   local coordinator, uploader, git push, or PR creator during the disconnected
   window.
3. Wait for one task to execute, verify, checkpoint, push from the GPU, and create
   its first draft PR.
4. Reconnect and confirm the same `deep.json`, `publication.json`, branch, task
   status, checkpoint SHA, and PR URL; verified work must not be replayed.
5. Read sanitized progress from R2 while the instance is still running. Confirm
   `provenance.origin` is `gpu-instance`, the instance id matches the rental, and
   object metadata names the on-box uploader/hostname rather than `MGR-PC`.
6. Let the supervisor land and confirm the handoff, final stacked handoff PR,
   `publication.json`, final archive, and `provenance.json` are in remote evidence.
7. Confirm the remote deadline watchdog destroys the instance after the configured
   deadline, including retry after a temporary Vast API/DNS failure.
8. Repeat the phase/lane smoke with two NInfer clients, concurrent xhigh/medium
   requests, and the action-plan task. Only a clean result unlocks CP1.

## Operator experience

The operator runs one launch command and waits for the `RUNNING` handshake. Before
launch, the operator provides the GPU-bound GitHub token file/repository/base and,
when R2 evidence is required, the R2 credential file. After `RUNNING`, the terminal
can close. Optional monitoring is either a reconnecting read-only status command or
an R2 poller; neither process is the coordinator or publisher. The first CP1 launch
must record the remote session id, R2 prefix, deadline, GitHub base, and reconnect
command before the operator disconnects.

The reconnect command is intentionally read-only:

```sh
ssh -i "$STINT_BOX_KEY" -p "$STINT_BOX_PORT" root@"$STINT_BOX_HOST" \
  "STINT_ONBOX_ROOT=/var/lib/stint-onbox /var/lib/stint-onbox/onbox-deep-supervisor.sh status"

ssh -i "$STINT_BOX_KEY" -p "$STINT_BOX_PORT" root@"$STINT_BOX_HOST" \
  "XDG_STATE_HOME=/var/lib/stint-onbox/state /var/lib/stint-onbox/bin/stint deep status --json"
```

When the instance is no longer reachable, the same sanitized heartbeat, publication
summary, and final archive are read from the recorded R2 prefix. The local dashboard
may later consume those objects, but it is never needed for the worker to continue.
