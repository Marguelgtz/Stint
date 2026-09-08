# On-box Deep Work execution plan

This is the required topology for the first unattended CP1 run. The rented instance
must continue the mission after the operator machine disconnects or powers off. A
local Stint coordinator is a development fixture only.

**Live status (2026-09-08):** P6.1 local-on-box Hermes execution, P6.2 detached
supervisor/restart loop, and P6.5 reconnectable sanitized heartbeat are implemented
on the dashboard branch. P6.3 disconnect proof, P6.4 production R2 credentials and
archive proof, and the full two-lane gate remain to be run on a fresh GPU instance.

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
- `scripts/launch-onbox-deep.sh` transfers a pinned binary/repository and exits after
  the remote supervisor reports `RUNNING`.
- Sanitized heartbeat and final-state R2 helpers are available when an explicit,
  root-only R2 credential file is supplied.

The failed phase/lane retry stopped before Deep Work because the foreground NInfer
bootstrap lost SSH at 83% model transfer. It produced no task evidence and did not
test the mission.

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
5. **Remote evidence publishing.** **Helpers implemented; live R2 proof pending.** Every heartbeat and state transition writes a
   sanitized snapshot containing session phase, active task, verified/blocked counts,
   last checkpoint commit, model/phase counters, compression counters, error domain,
   and deadline. Upload snapshots and final artifacts to an R2 prefix owned by this
   session. Do not upload prompts, raw Hermes output, credentials, or arbitrary shell
   output.
6. **Reconnectable inspection.** **Supervisor status implemented; CLI/R2 reconnect test pending.** Add a read-only remote status command. When online,
   it reads the instance state over SSH; when the instance is gone, it reads the last
   R2 snapshot and final handoff. Inspection must never be required for execution.

## Readiness test before CP1

Use a one- or two-task synthetic mission and run the following gates in order:

1. Start the supervisor and verify `RUNNING` plus a remote heartbeat.
2. Terminate the launch process and block or close the operator SSH connection.
3. Wait for one task to execute, verify, and checkpoint on the instance.
4. Reconnect and confirm the same `deep.json`, branch, task status, and checkpoint;
   verified work must not be replayed.
5. Read the sanitized progress from R2 while the instance is still running.
6. Let the supervisor land and confirm the handoff and final artifact are in R2.
7. Confirm the remote deadline watchdog destroys the instance after the configured
   deadline, including retry after a temporary Vast API/DNS failure.
8. Repeat the phase/lane smoke with two NInfer clients, concurrent xhigh/medium
   requests, and the action-plan task. Only a clean result unlocks CP1.

## Operator experience

The operator runs one launch command and waits for the `RUNNING` handshake. After that,
the terminal can close. Optional monitoring is either a reconnecting read-only status
command or an R2 poller; neither process is the coordinator. The first CP1 launch must
record the remote session id, R2 prefix, deadline, and reconnect command before the
operator disconnects.
