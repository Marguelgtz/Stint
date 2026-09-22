# Deep Work setup

This is the operator runbook for the Hermes-on-box Deep Work path.

## Local prerequisites

- Linux or macOS with Bash, OpenSSH, `rsync`, Python 3, and Git.
- A built Stint binary at `bin/stint` (`make build`).
- A Stint SSH key, Vast credentials, and a READY compute session.
- A mission file and a clean target repository at the commit to stage.
- A GitHub token file, repository, and base branch for production publication.

The launcher requires the compute session to be `READY` and its deadline to be in the future. It reads the instance ID and deadline from `${XDG_STATE_HOME:-$HOME/.local/state}/stint/session.json` by default. Set `STINT_SESSION_JSON` to use another state file.

## Prepare the compute session

Start Stint with the NInfer runtime. Use two clients if the mission needs a concurrent phase-routing check:

```sh
./bin/stint start interactive --runtime ninfer --ninfer-config native --clients 2 --hours 2
./bin/stint status
```

Wait until the status is `READY`. Do not pass a stale instance ID or deadline to Deep Work. If explicit `STINT_INSTANCE_ID` or `STINT_DEADLINE` values are supplied, the launcher verifies they match the READY session file.

## Launch Deep Work

Set the remote SSH coordinates from the READY session and provide the local target repository and mission:

```sh
STINT_BOX_HOST=<ssh-host> \
STINT_BOX_PORT=<ssh-port> \
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

The launcher stages the committed repo `HEAD`, transfers the mission and bootstrap bundle, then:

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

The smoke uses an isolated Stint state/config root, an RTX 4090 session capped at $2, two clients, and the phase-lane mission. It requires the local Vast credentials and R2 environment files plus GitHub publication settings. It tears down the paid compute session on exit.

## Recovery and inspection

The detached supervisor owns restart, state recovery, publication retries, evidence snapshots, and the deadline watchdog. The operator machine may disconnect after the launcher prints `ONBOX_SUPERVISOR_RUNNING` with a `RUNNING` durable-state record.

Inspect from the operator host:

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

### Resume durable state after compute replacement

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
