# Deep Work phase and lane end-to-end smoke

## Objective

Exercise the living action-plan phase and two short execution phases on a fresh
two-lane Hermes/NInfer box, then verify that each task leaves durable evidence.

## Success

- The coordinator completes the plan task and both execution tasks.
- The dashboard observer records xhigh and medium route traffic with no failures.
- Each task is independently verified and checkpointed.

## Constraints

- Work only in the supplied smoke worktree.
- Use ordinary local file and shell commands; do not access the network.
- Keep all artifacts in `phase-lane-smoke/`.

## Tasks

- [ ] PHASE-001: Create `phase-lane-smoke/medium-ready.txt` containing exactly `medium execution ready`.
  - reasoning: medium
  - acceptance: the marker exists with the exact expected line.
  - verify: test -f phase-lane-smoke/medium-ready.txt && grep -Fqx 'medium execution ready' phase-lane-smoke/medium-ready.txt
- [ ] PHASE-002: Read `phase-lane-smoke/medium-ready.txt` and create `phase-lane-smoke/final.txt` containing exactly `phase lane e2e complete`.
  - reasoning: medium
  - acceptance: the final marker proves the second medium execution task saw the first checkpoint.
  - verify: test -f phase-lane-smoke/final.txt && grep -Fqx 'phase lane e2e complete' phase-lane-smoke/final.txt

## Verification

test -f phase-lane-smoke/final.txt && grep -Fqx 'phase lane e2e complete' phase-lane-smoke/final.txt
