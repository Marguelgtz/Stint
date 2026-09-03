# Synthetic dry-run mission for `--worker hermes` (P4)

## Objective

Exercise the full Deep Work coordinator path against the real remote stack —
headless Hermes on the compute box, git/verify over the Stint SSH channel,
per-task verification, on-box checkpoints, and the on-box landing handoff —
using a trivial mission. No Vanta work: everything stays inside `dryrun/`.

Run against the Vanta baseline (which provides `scripts/verify-cp1` as a
known-good mission-level command). Two supported topologies:

### Mode A — two-box dry run (coordinator stays on the agent's box)

The machine running the agent (box A) hosts the Stint coordinator and rents a
second GPU box (box B) for the Hermes worker. Box A's own Stint session keeps
running untouched; the second session lives in an isolated XDG profile and uses
a different tunnel port (`--tunnel-port`, since 8409 is taken by box A's own
tunnel). The coordinator never needs box B's model endpoint — the worker talks
to `127.0.0.1:8080/v1` *on box B* — so the profile isolation only separates
state/config/tunnel:

    export XDG_CONFIG_HOME=$HOME/.config/stint-dryrun
    export XDG_STATE_HOME=$HOME/.local/state/stint-dryrun
    mkdir -p $XDG_CONFIG_HOME $XDG_STATE_HOME
    # Vast API key for this profile: copy box A's, or run `stint auth vast` here
    cp ~/.config/stint/credentials.json $XDG_CONFIG_HOME/credentials.json
    # rent box B (tunnel to 8410 so it cannot collide with box A's 8409):
    # the profile gets its own Stint SSH keypair, which stint auto-attaches to
    # the rented box — box B's SSH key is therefore the profile's key:
    ./stint start interactive --hours 2 --tunnel-port 8410 \
      --runtime ninfer --ninfer-config coding --yes
    KEY=$XDG_CONFIG_HOME/stint/ssh/id_ed25519
    # provision + transfer (over box B's SSH; host/port from `stint status --json`):
    ssh -i $KEY -p <B-port> root@<B-host> 'bash -s' < scripts/provision-box.sh
    ssh -i $KEY -p <B-port> root@<B-host> 'bash -s' < scripts/box-smoke.sh
    rsync -a -e "ssh -i $KEY -p <B-port>" Vanta/ root@<B-host>:/root/stint-vanta/
    # launch the dry run (same environment, so the coordinator reads box B's session):
    ./stint deep start \
      --mission <this file> --repo /root/stint-vanta \
      --worker hermes --provider custom --model qwen3.8-27b \
      --task-timeout 10m --max-attempts 2 --hours 1
    # teardown: ./stint down (profile-isolated) — only box B is destroyed

Box A's own session (the one running the agent) is never touched: different
state dir, different port, different pid lock.

### Mode B — single box, dedicated (production shape)

The launch box is the only box: `stint start interactive` on the operator
machine, then the same `stint deep start --worker hermes …` line as above
(default 8409 tunnel; no profile tricks needed). This is the shape the real
CP1 run takes.

Gate (all must hold before the real CP1 launch):
1. each task VERIFIED by its own per-task `verify:` over the box channel;
2. a checkpoint commit per verified task on the `stint/deep-*` branch on the box;
3. `DEEP_WORK_HANDOFF.md` present in the on-box worktree and included in the
   handoff commit;
4. a truthful local `handoff.md`; no hang on any approval gate.

## Success
- Both tasks complete and are marked verified by the coordinator from repository evidence
- The on-box branch contains one checkpoint commit per verified task plus the handoff commit

## Constraints
- Work only inside `dryrun/`; do not touch any other file in the repository
- Do not run network commands, installs, or destructive commands
- Keep each artifact under 20 lines

## Verification
bash scripts/verify-cp1

## Tasks
- [ ] DRY-001: Create the dryrun marker artifact
  - acceptance: dryrun/hello.txt exists, is non-empty, and contains the line DRYRUN_OK; nothing outside dryrun/ is modified
  - verify: test -s dryrun/hello.txt && grep -q DRYRUN_OK dryrun/hello.txt
- [ ] DRY-002: Add a checksum script and its digest
  - acceptance: dryrun/checksum is an executable shell script that prints the sha256 hex of dryrun/hello.txt; running it produces a 64-char hex line written to dryrun/digest.txt; nothing outside dryrun/ is modified
  - verify: test -x dryrun/checksum && test -s dryrun/digest.txt && test "$(wc -c < dryrun/digest.txt)" -ge 64

## Notes for human reviewers (ignored by the Stint parser)
- DRY-001 and DRY-002 are deliberately tiny so one Hermes turn per task is enough;
  the mission exercises the plumbing, not the model. The mission-level command is the
  real Vanta lenient verifier: on this synthetic tree it passes (nothing CP1-related
  exists), so the per-task commands are what carry the acceptance signal — exactly the
  property the CP1 mission now relies on.
- If DRY-002 ever parks, its per-task verify pins the missing piece (the executable
  or the digest), which names the blocker in the handoff.