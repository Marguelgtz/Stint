# On-box Deep Work execution plan

This is the required topology for the first unattended CP1 run. The rented instance
must continue the mission after the operator machine disconnects or powers off. A
local Stint coordinator is a development fixture only.

**Live status (2026-09-08):** P6.1 local-on-box Hermes execution, P6.2 detached
supervisor/restart loop, and P6.5 reconnectable sanitized heartbeat are implemented
on the dashboard branch. The R2 credentials and object contract pass an isolated
bucket smoke. The live GPU handoff, disconnect proof, and full two-lane gate remain
blocked on obtaining a host with both reachable SSH and at least 30 MB/s measured
model-transfer throughput.

## Living checklist

- [x] Add a local Hermes executor with no SSH loopback.
- [x] Add a detached supervisor with PID/heartbeat and restart-from-state behavior.
- [x] Add a launch helper that exits after the remote `RUNNING` handshake.
- [x] Add sanitized heartbeat and final-state R2 upload hooks.
- [ ] Make NInfer bootstrap resumable after launch SSH loss (the model marker loop
  is now safe against a completed download and PID reuse; a dropped-SSH live proof
  is still required).
- [ ] Prove the supervisor survives launch-process termination and operator disconnect
  on a real GPU (the detached restart/landing fixture passes locally).
- [ ] Prove the on-box deadline watchdog destroys the instance after a provider/DNS retry.
- [ ] Verify R2 heartbeat and final archive objects on a live run (the helpers,
  bucket access, and detached fixture pass; a real GPU archive is still required).
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
- `scripts/launch-onbox-deep.sh` transfers a pinned binary/repository and exits after
  the remote supervisor reports `RUNNING`.
- Sanitized heartbeat and final-state R2 helpers are available when an explicit,
  root-only R2 credential file is supplied.

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
disconnect proof.

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

The reconnect command is intentionally read-only:

```sh
ssh -i "$STINT_BOX_KEY" -p "$STINT_BOX_PORT" root@"$STINT_BOX_HOST" \
  "STINT_ONBOX_ROOT=/var/lib/stint-onbox /var/lib/stint-onbox/onbox-deep-supervisor.sh status"

ssh -i "$STINT_BOX_KEY" -p "$STINT_BOX_PORT" root@"$STINT_BOX_HOST" \
  "XDG_STATE_HOME=/var/lib/stint-onbox/state /var/lib/stint-onbox/bin/stint deep status --json"
```

When the instance is no longer reachable, the same sanitized heartbeat and final
archive are read from the recorded R2 prefix. The local dashboard may later consume
those objects, but it is never needed for the worker to continue.
