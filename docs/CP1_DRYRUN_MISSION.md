# Synthetic dry-run mission for `--worker hermes` (P4)

## Objective

Exercise the full Deep Work coordinator path against the real remote stack —
headless Hermes on the compute box, git/verify over the Stint SSH channel,
per-task verification, on-box checkpoints, and the on-box landing handoff —
using a trivial mission. No Vanta work: everything stays inside `dryrun/`.

Run against the Vanta baseline (which provides `scripts/verify-cp1` as a
known-good mission-level command) on a dedicated box:

    ssh -i <stint-key> -p <port> root@<host> 'bash -s' < scripts/provision-box.sh
    ssh -i <stint-key> -p <port> root@<host> 'bash -s' < scripts/box-smoke.sh
    rsync -a -e "ssh -i <stint-key> -p <port>" Vanta/ root@<host>:/root/stint-vanta/
    stint start interactive --yes   # session on that box
    stint deep start \
      --mission <this file> --repo /root/stint-vanta \
      --worker hermes --provider custom --model qwen3.8-27b \
      --task-timeout 10m --max-attempts 2 --hours 1

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