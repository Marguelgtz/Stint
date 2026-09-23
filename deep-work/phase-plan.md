# Phase/lane E2E — living action plan

Status: planning phase complete (STINT-PLAN-001, xhigh, attempt 1). Medium execution
has not started. This file is the living plan; it is updated with checkpoint and
verification evidence as each task completes. Checkpoints below are coordinator
assertions, not proof — the coordinator's per-task verify commands are the proof.

## Repository state at planning time

- Worktree: `/var/lib/stint-onbox/repo/.stint-deep/20260923-022052` (linked worktree,
  shared git state), branch `stint/deep-20260923-022052`, HEAD
  `bdd55c57b7fbdb7a2128c8ed14861cf3f527c04d` (merge of PR #109).
- `deep-work/phase-plan.md` (this file) and, after execution, the `phase-lane-smoke/`
  artifacts are untracked worktree files. Nothing is committed, pushed, or turned
  into a PR from this worktree.
- `phase-lane-smoke/` does not exist yet; PHASE-001 creates it.
- Mission definition: `deep-work/PHASE_LANE_E2E_MISSION.md` (task list, exact marker
  contents, per-task verify commands). Read-only seed:
  `deep-work/PHASE_LANE_E2E_PLAN_SEED.md` (never edited; this file replaced its content
  here).

## Objective

Run the plan phase (xhigh) plus two short execution phases (medium) on this fresh
two-lane Hermes/NInfer box and prove that each task leaves durable, independently
verifiable evidence:

- STINT-PLAN-001 (xhigh, this task): update this living plan. Acceptance:
  `test -s 'deep-work/phase-plan.md'`.
- PHASE-001 (medium): create `phase-lane-smoke/medium-ready.txt` containing exactly
  `medium execution ready`. Verify:
  `test -f phase-lane-smoke/medium-ready.txt && grep -Fqx 'medium execution ready' phase-lane-smoke/medium-ready.txt`
- PHASE-002 (medium): read `phase-lane-smoke/medium-ready.txt`, then create
  `phase-lane-smoke/final.txt` containing exactly `phase lane e2e complete`. Verify:
  `test -f phase-lane-smoke/final.txt && grep -Fqx 'phase lane e2e complete' phase-lane-smoke/final.txt`

Mission success: coordinator completes all three tasks; the dashboard observer records
xhigh and medium route traffic with no failures; each task is independently verified
and checkpointed.

## Decisions

1. Strict ordering: PHASE-001 → verify → checkpoint → PHASE-002 → verify →
   checkpoint. PHASE-002's whole point is that it reads PHASE-001's checkpoint, so it
   must not start until PHASE-001's verify command passes. No parallel execution of
   the two medium tasks.
2. Marker files: single line, exact content per the mission, written with
   `printf '<line>\n'` (trailing newline; the mission's `grep -Fqx` verification is
   satisfied either way).
3. `mkdir -p phase-lane-smoke` is part of PHASE-001's step, so directory creation is
   attributable to the first medium task, not to the plan phase.
4. This plan file lives at `deep-work/phase-plan.md` as required by the coordinator's
   plan task; all execution artifacts stay under `phase-lane-smoke/`. The file is kept
   untracked: no commits, pushes, or PRs from this worktree.
5. No network anywhere in this smoke. All steps are local file and shell commands;
   the mission's verify commands are already local-only.
6. Checkpoint format: after each task's verify command passes, the coordinator records
   a one-line checkpoint in the table below (task, UTC timestamp, verify result,
   evidence pointer). A failed task is recorded as a failed checkpoint with the reason
   and left explicit in the final handoff.
7. Expected observer traffic: exactly three routed tasks on this box — one xhigh
   (STINT-PLAN-001) and two medium (PHASE-001, PHASE-002). Any other route traffic or
   a failed task contradicts the "no failures" success criterion and must be recorded.

## Risks

1. Ordering violation: if PHASE-002 runs before PHASE-001's checkpoint is verified,
   `final.txt` no longer proves the dependency. Mitigation: coordinator gate (decision
   1); PHASE-002's first step is to read and check `medium-ready.txt`.
2. Exact-content drift: trailing whitespace or CRLF would break `grep -Fqx`.
   Mitigation: `printf` writes, verify command re-run from the worktree root after
   each task, not trusted to the writing tool's report.
3. Plan/seed confusion: `PHASE_LANE_E2E_PLAN_SEED.md` is the read-only seed; only this
   file is updated. Editing the seed would corrupt the baseline for comparison.
4. Worktree is linked and shares git state with the primary tree: stray commits or
   untracked-file side effects could leak into other runs. Mitigation: this smoke
   writes only `deep-work/phase-plan.md` and `phase-lane-smoke/*`; nothing is staged.
5. Prior live on-box smoke blocker (context, not on this smoke's critical path):
   `docs/DEEP_WORK_ONBOX_EXECUTION_PLAN.md` (commit `de3b9b8`) records that fresh-box
   validation failed before `RUNNING` — provider hosts died on the 30 MB/s download
   floor (11.8/13.8 MB/s), the missing `--default-max-tokens 262144` flag (fixed by
   PR #106/#107, landed at `2041498`), and a failed authenticated SSH probe on host
   `52149289`; PR #109 (`b88c817`, HEAD) then capped live smoke within remaining
   budget ($1.20 rental cap, $0.80/hour ceiling, one candidate, 1.5-hour run). That
   GPU/provider path is out of scope here; this smoke deliberately uses no network, so
   it tests the phase/lane machinery, not provider reliability.
6. Replacement-compute recovery (PR #101) does not restore state volumes; a
   durable-volume restore remains unproven and is out of scope for this local smoke —
   carry it as an open item in the handoff, not as a failure of this run.

## Next steps

- [x] STINT-PLAN-001 (xhigh): this living plan written; coordinator verifies with
      `test -s 'deep-work/phase-plan.md'` and checkpoints.
- [ ] PHASE-001 (medium): `mkdir -p phase-lane-smoke`, write `medium-ready.txt`
      (`printf 'medium execution ready\n'`), run its verify command, checkpoint.
- [ ] PHASE-002 (medium): read `phase-lane-smoke/medium-ready.txt` and confirm the
      exact line; write `final.txt` (`printf 'phase lane e2e complete\n'`), run its
      verify command, checkpoint.
- [ ] Observer: confirm xhigh + medium route traffic recorded, zero failed tasks.
- [ ] Final handoff: mark this checklist complete, keep any failed checkpoints and
      the external open items (live GPU blocker, volume-restore proof) explicit.

## Evidence and checkpoints

- Mission: `deep-work/PHASE_LANE_E2E_MISSION.md` (task list, exact contents, verify
  commands) — read at planning time.
- Repo context: HEAD `bdd55c57b7fbdb7a2128c8ed14861cf3f527c04d`; blocker record
  `docs/DEEP_WORK_ONBOX_EXECUTION_PLAN.md` at commit `de3b9b83196b9105e2642c02df5e5e77f3067e69`;
  PR #107 landing `2041498ed00724cef3bca3769ccaa9907e87efa2`; PR #109 budget cap
  `b88c817`. Local checks available via `Makefile` (`make test`, `make check`,
  `make build`) — not required for this file-only smoke.
- Worktree layout at planning time: `deep-work/` holds `COMPRESSION_SMOKE_MISSION.md`,
  `PHASE_LANE_E2E_MISSION.md`, `PHASE_LANE_E2E_PLAN_SEED.md`, this plan; no
  `phase-lane-smoke/` directory yet (checked with `ls`).

Checkpoints (filled by the coordinator after each verify command):

| Task | Lane | Verify result | Checkpoint (UTC) | Evidence pointer |
|---|---|---|---|---|
| STINT-PLAN-001 | xhigh | (pending coordinator) | — | `deep-work/phase-plan.md` non-empty; `test -s deep-work/phase-plan.md` |
| PHASE-001 | medium | (pending) | — | `phase-lane-smoke/medium-ready.txt` |
| PHASE-002 | medium | (pending) | — | `phase-lane-smoke/final.txt` |

## Safety boundaries

- Work only inside the supplied worktree.
- No network access from planning or task execution.
- Keep task artifacts below `phase-lane-smoke/`; no commits, pushes, or PRs.