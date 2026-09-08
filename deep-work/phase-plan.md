# Phase/lane E2E living plan

Living action plan for the Deep Work phase-and-lane end-to-end smoke on this box.
Author: PLAN-001 (xhigh planning lane). Updated in place as each phase checkpoints.
Do not treat this file as proof of execution results — repository files and the
verify commands are the evidence.

## Repository state at planning time

- Worktree: `/var/lib/stint-onbox/repo/.stint-deep/20260908-194552`
- Branch: `stint/deep-20260908-194552`
- Head: `85423b76d14c6bf702097c3bc3a7b475c513e8eb`
  (`fix: register staged GPU repository as safe`)
- Uncommitted: `?? deep-work/phase-plan.md` (this file, seeded by
  `cc10d8c fix: seed on-box action plan inside worktree`), plus a
  coordinator-owned prompt file (`.stint-hermes-prompt-*`) that this plan
  does not touch.
- Mission of record: `deep-work/PHASE_LANE_E2E_MISSION.md`
- Seed of record: `deep-work/PHASE_LANE_E2E_PLAN_SEED.md`
- No `phase-lane-smoke/` directory exists yet; PHASE-001 creates it.

## Objective

Complete the two bounded marker-file tasks from `PHASE_LANE_E2E_MISSION.md`,
verify each checkpoint with its verify command, and leave an evidence trail
suitable for the GPU-owned PR stack. Success requires the plan task
(PLAN-001, this document) plus both execution tasks to complete, with
xhigh and medium route traffic recorded and no failures.

## Decisions

1. **Order is strictly PHASE-001 → PHASE-002.** PHASE-002 must read
   `phase-lane-smoke/medium-ready.txt` before writing
   `phase-lane-smoke/final.txt`, so it is inherently dependent on PHASE-001's
   checkpoint. Running them out of order or in parallel would break the
   lane-sequencing evidence this smoke exists to prove.
2. **Exact file contents.** `medium-ready.txt` contains exactly
   `medium execution ready` and `final.txt` contains exactly
   `phase lane e2e complete`. Both verify commands use `grep -Fqx`, which
   matches a whole fixed-string line — an extra line or trailing junk fails
   verification. Write each file as a single line plus one trailing newline
   and nothing else.
3. **Artifact location.** All execution artifacts live under
   `phase-lane-smoke/` in the worktree root, per the mission constraint.
   This plan file is the coordinator's planning artifact and stays in
   `deep-work/` as the mission instructs.
4. **No network.** Tasks use ordinary local file and shell commands only
   (`mkdir`, `printf`/`echo`, `test`, `grep`). No downloads, remotes, or
   network access at any point.
5. **Scope.** All work stays inside this supplied worktree. No pushes, no
   pull requests, no destructive commands (the box registers a staged GPU
   repo as safe per `85423b7`; we do not invoke it).

## Risks and mitigations

- **Ordering / lane visibility:** if PHASE-002 runs before PHASE-001's
  marker exists, its read step fails. Mitigation: PHASE-002 is dispatched
  only after PHASE-001's verify command passes and its checkpoint is
  recorded in this file.
- **Exact-line mismatch:** `grep -Fqx` is strict about whole-line matches.
  Mitigation: write files with a single trailing newline only; re-run the
  exact verify command after writing rather than assuming success.
- **Missing directory:** `phase-lane-smoke/` does not exist yet; writing
  into it without `mkdir -p` fails. Mitigation: PHASE-001 creates the
  directory as part of its step.
- **Accidental scope drift:** editing outside the worktree or touching
  coordinator-owned files (`.stint-hermes-prompt-*`) would violate
  constraints. Mitigation: every command targets paths under the worktree
  root; the prompt file is left alone.
- **Plan vs. evidence drift:** this file could be edited (or go stale)
  without execution actually happening. Mitigation: each checkpoint records
  the verify command's exit status and the file state observed on disk; the
  final handoff re-runs the mission-level verification command.

## Next steps (execution plan)

1. **PHASE-001** (medium): `mkdir -p phase-lane-smoke` and write
   `phase-lane-smoke/medium-ready.txt` with exactly `medium execution ready`.
   Verify:
   `test -f phase-lane-smoke/medium-ready.txt && grep -Fqx 'medium execution ready' phase-lane-smoke/medium-ready.txt`
   Checkpoint: record verify result below.
2. **PHASE-002** (medium): read `phase-lane-smoke/medium-ready.txt` to prove
   it saw PHASE-001's checkpoint, then write
   `phase-lane-smoke/final.txt` with exactly `phase lane e2e complete`.
   Verify:
   `test -f phase-lane-smoke/final.txt && grep -Fqx 'phase lane e2e complete' phase-lane-smoke/final.txt`
   Checkpoint: record verify result below.
3. **Final verification** (mission-level):
   `test -f phase-lane-smoke/final.txt && grep -Fqx 'phase lane e2e complete' phase-lane-smoke/final.txt`
4. **Handoff:** confirm no unresolved issues remain; state the evidence
   pointers below in the final report.

## Checkpoint log (filled in as work proceeds)

- [x] PLAN-001 (xhigh, planning): this living plan updated at
      `deep-work/phase-plan.md` with decisions, risks, next steps, and
      evidence pointers. Verify: `test -s 'deep-work/phase-plan.md'` —
      exit 0 (recorded below once the coordinator verifies).
- [x] PHASE-001 (medium): marker written and verify command passed.
      Evidence: `phase-lane-smoke/medium-ready.txt` on disk, exactly
      `medium execution ready` + one trailing newline (23 bytes, confirmed
      via `od -c`). Verify command
      `test -f phase-lane-smoke/medium-ready.txt && grep -Fqx 'medium execution ready' phase-lane-smoke/medium-ready.txt`
      exit 0 (re-run by the executor). PHASE-001 may now be checkpointed;
      PHASE-002 is cleared to read this marker before writing `final.txt`.
- [x] PHASE-002 (medium): completed (attempt 1). Read
      `phase-lane-smoke/medium-ready.txt` first (confirmed exactly
      `medium execution ready` on disk from PHASE-001), then wrote
      `phase-lane-smoke/final.txt` with exactly `phase lane e2e complete`
      + one trailing newline (24 bytes, confirmed via `od -c`).
      Verify command
      `test -f phase-lane-smoke/final.txt && grep -Fqx 'phase lane e2e complete' phase-lane-smoke/final.txt`
      exit 0 (re-run by the executor).

## Unresolved issues

None at planning time. Any issue found during execution is appended here
with the task, the symptom, and the mitigation before the final handoff.

## Safety boundaries

- Work only inside the supplied worktree.
- Do not use network access from task execution.
- Keep task artifacts below `phase-lane-smoke/` (this plan lives in
  `deep-work/` per the coordinator's tasking).
- No pushes, no pull requests, no destructive commands.