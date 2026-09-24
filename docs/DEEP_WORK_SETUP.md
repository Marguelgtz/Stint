# Deep Work setup

This is the operator runbook for the Hermes-on-box Deep Work path. Normal startup uses `stint deep start`; the production shell launcher remains the implementation behind that command.

## Local prerequisites

- Linux or macOS with Bash, OpenSSH, `rsync`, Python 3, and Git.
- A built Stint binary at `bin/stint` (`make build`).
- A Stint SSH key and Vast credentials. A READY compute session is optional: `stint deep start --hours ...` can create it.
- A mission file and a clean target repository at the commit to stage.
- The mission's explicit GitHub policy and a least-privilege GitHub token at `~/.config/stint/github-token`.

Stint credentials are read from `~/.config/stint/credentials.json`, and the managed SSH key from `~/.config/stint/ssh/id_ed25519`. The CLI resolves host, port, instance ID, deadline, runtime, and client count from the READY session state. It refuses an expired session or missing identity fields before connecting to the box.

## Launch Deep Work

For normal use, let Deep Work own the transition from no compute to detached execution:

```sh
./bin/stint deep start \
  --hours 3 \
  --repo ~/Documents/projects/spark \
  --mission ~/Documents/projects/Stint/docs/missions/spark-mcp-graduation.md \
  --runtime ninfer \
  --ninfer-deployment release-bundle \
  --ninfer-config native \
  --clients 2 \
  --max-hourly-usd 0.45 \
  --max-cost-usd 1.35

./bin/stint deep dash
```

When no session exists, the explicit `--hours` value authorizes the paid rental. The command forwards the compute flags into the existing `stint start interactive` lifecycle, waits for READY, then continues automatically into the production Deep Work launcher and detached supervisor. You do not need to run `stint start interactive`, poll `stint status`, and issue a second launch command yourself.

The existing lifecycle remains available independently for interactive inference or diagnostics. If you already have a READY session that you intentionally want Deep Work to reuse, omit the compute-provisioning flags:

```sh
./bin/stint deep start \
  --repo ~/Documents/projects/spark \
  --mission ~/Documents/projects/Stint/docs/missions/spark-mcp-graduation.md
```

Supplying compute-provisioning flags while a READY session already exists fails closed instead of silently ignoring the requested cost/runtime contract. Direct calls to the shell launcher remain reserved for advanced diagnostics and recovery.

Before a new rental is requested, `stint deep start` validates the repo, mission, GitHub policy/token, and optional R2 configuration. After compute reaches READY and before transfer, it prints the repo `HEAD`, origin and clean-tree state; mission and GitHub policy; compute identity, GPU/runtime, remaining time and deadline; clients, model, task timeout, maximum attempts, R2 status, and estimated costs when available. The standard compute lifecycle validates the Stint SSH and Vast credentials. Deep Work stages only the immutable committed `HEAD` and a validated mission snapshot, without mutating the operator checkout.

The CLI passes the Stint-managed host, port, key and watchdog credential path to the existing launcher. It returns only after both the detached supervisor process marker and durable `RUNNING` record match the READY session deadline. The supervisor continues after the launcher and dashboard SSH connection close. `stint deep dash` routes to the GPU's existing Deep Dashboard using the saved SSH identity.

Production defaults are a 15-minute task timeout, 2 attempts, `custom:qwen-stint-{reasoning}`, `qwen3.8-27b`, and `medium` reasoning. The mission's explicit `## GitHub` policy controls publication mode, repository, base, authors, and approval. Put the token in `~/.config/stint/github-token`; Stint uses `~/.config/vanta-r2.env` if present for optional R2 evidence, or pass `--r2-env-file` for another file.

The existing launcher stages the committed repo `HEAD`, transfers the mission and bootstrap bundle, then:

1. Installs missing Hermes and Node.js.
2. Installs a current stable Go release with checksum verification when the target repository declares `go.mod` and its current compiler is too old.
3. Runs `go test ./...` before startup when the staged repository is Stint itself.
4. Verifies NInfer, its native context/completion configuration, and the requested model.
5. Installs the phase proxy and content-filtered worker observer.
6. Configures xhigh and medium Hermes providers plus compression routing.
7. Runs real requests over xhigh and medium; with two clients it also checks concurrent routing.
8. Starts the detached supervisor only after those checks succeed.

If any check fails, the launcher exits with an error and does not start Deep Work. The current remote log path is `/var/lib/stint-onbox/supervisor.log`.

The live smoke calls this exact launcher, rather than maintaining a separate provisioning path:

```sh
scripts/run-onbox-deep-smoke.sh
```

The smoke uses an isolated Stint state/config root, an RTX 4090-only NInfer query, and one candidate attempt. Its one-hour rental is capped at $0.40/hour and $0.40 total, with two clients and the phase-lane mission. The explicit NInfer selection filters the provider query to RTX 4090; do not use a 3090 or another GPU/runtime for this qualification. It requires the local Vast credentials and R2 environment files plus GitHub publication settings. It tears down the paid compute session on exit.

## Recovery and inspection

The detached supervisor owns restart, state recovery, publication retries, evidence snapshots, and the deadline watchdog. The operator machine may disconnect after the launcher prints `ONBOX_SUPERVISOR_RUNNING` with a `RUNNING` durable-state record.

Use `stint deep dash` for normal monitoring. Raw SSH inspection is available for diagnostics; the command uses the SSH host, port, and Stint key already saved in local Stint configuration.

Inspect the supervisor directly from the operator host only when diagnosing transport or dashboard problems:

```sh
ssh -i "$STINT_BOX_KEY" -p "$STINT_BOX_PORT" "root@$STINT_BOX_HOST" \
  "STINT_ONBOX_ROOT=/var/lib/stint-onbox /var/lib/stint-onbox/onbox-deep-supervisor.sh status"
```

Inspect durable task state on the GPU host:

```sh
ssh -t -i "$STINT_BOX_KEY" -p "$STINT_BOX_PORT" "root@$STINT_BOX_HOST" \
  "XDG_STATE_HOME=/var/lib/stint-onbox/state /var/lib/stint-onbox/bin/stint deep dash"
```

The supervisor restarts the coordinator with `stint deep onbox --resume`. Persisted provider, model, reasoning, task timeout, command guidance, and action-plan path remain authoritative unless a supported override is explicitly provided. A different compute instance cannot claim the saved session without an audited rebind.

When R2 final archiving is configured, the supervisor reports successful
completion only after that archive succeeds. An archive failure leaves the
on-box state available for recovery and is recorded as an incomplete supervisor
exit; the deadline watchdog remains the hard upper bound on paid compute.

Every production mission must declare its publisher policy in `## GitHub` with
`mode: engineering`, the exact owner/repository and base branch, and an approval
policy (`internal` by default). The launcher values must match the mission's
persisted mode, repository, base, allowed authors, and approval policy. The
checkpoint publisher currently rejects maintenance mode; use the separate
maintenance workstream only after its merge and pagination gates are repaired.

### Advanced: resume durable state after compute replacement

The operator launcher can qualify a replacement compute instance and resume a
previous on-box session, but the saved state and repository must already be
restored at the same remote root (`/var/lib/stint-onbox/state` and
`/var/lib/stint-onbox/repo`, or the configured `STINT_REMOTE_ROOT`). The launcher
checks the session pointer, saved branch, repository/worktree paths, persisted
model, and current compute ID before provisioning. It does not copy or recreate
lost state or checkpoint branches; if the prior disk was ephemeral, restore the
state and repository volume first. A missing saved branch fails closed.

With the restored volume mounted on the replacement instance, set the new
instance's SSH details and READY session identity, then run:

```sh
STINT_BOX_HOST=<replacement-ssh-host> \
STINT_BOX_PORT=<replacement-ssh-port> \
STINT_BOX_KEY="$HOME/.config/stint/ssh/id_ed25519" \
STINT_ONBOX_RESUME=1 \
STINT_ONBOX_REBIND_COMPUTE=1 \
STINT_ONBOX_REBIND_REASON="restored durable state volume after instance replacement" \
STINT_GITHUB_TOKEN_FILE="$HOME/.config/stint/github-token" \
STINT_GITHUB_REPOSITORY=owner/repository \
STINT_GITHUB_BASE=main \
STINT_VAST_CREDENTIALS="$HOME/.config/stint/credentials.json" \
scripts/launch-onbox-deep.sh
```

Omit `STINT_MISSION` and `STINT_REPO` in resume mode. If the saved compute ID
still matches the READY session, leave out the two rebind variables; requesting
a redundant rebind is rejected. Provider, model, reasoning, task timeout, max
attempts, action-plan path, and command guidance are inherited from durable
state. Set an individual `STINT_ONBOX_*` variable only to override that value.
`STINT_ONBOX_ACTION_PLAN_PATH` changes the worktree-relative plan destination;
`STINT_ONBOX_ALLOW_COMMANDS` accepts newline-separated advisory prefixes, and an
explicitly empty value clears the persisted list. The resumed coordinator
rewrites `RUNNING.json` only after it has checked and saved the recovered state.

The watchdog needs the Vast credential at `/root/.config/stint/credentials.json`. The launcher transfers it from `STINT_VAST_CREDENTIALS`. Do not set `STINT_ONBOX_SKIP_WATCHDOG=1` for a production run.

For a detailed operator description of mission files, verification, durable task state, and known telemetry limits, see [Deep Work](DEEP_WORK.md). Current implementation evidence and unresolved items are in the [canonical integration action plan](DEEP_WORK_ONBOX_EXECUTION_PLAN.md).
