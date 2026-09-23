# Dry Run Findings (2026-09-05 re-run) — P4 gate result

**Status:** run 2 (session `20260905-180320`) **landed 2026-09-05 19:05 BST
in 2m01s of actual agent work**. Two new defects found and fixed inline; one
CP1 blocker remains (git identity on the box). See
`CP1_DRYRUN1_INCIDENT.md` for the prior failed run.

## What ran

Mode A, two-box, `stint-dryrun` profile:
- **Box A** (instance 49980832, operator machine's own box): hosts the Stint
  coordinator; untouched all night.
- **Box B** (instance 49982899, RTX 4090, $0.387/hr, tunnel 8410): the
  headless-Hermes worker, running box-local `qwen3.8-27b` at
  `127.0.0.1:8080/v1` (no tunnel hop for inference).
- Mission: the synthetic P4 dry run (DRY-001 write+verify a file; DRY-002
  checksum script + digest), run against the Vanta baseline repo purely for
  its known-good `scripts/verify-cp1` mission command.

## Result

| Gate (per `CP1_DRYRUN_MISSION.md`) | Result |
| --- | --- |
| DRY-001 verified by its own `verify:` over the box channel | **PASS** (1 attempt, 22 s) |
| DRY-002 verified by its own `verify:` over the box channel | **PASS** (1 attempt, 96 s headless run) |
| Mission-level `scripts/verify-cp1` | **PASS (LENIENT)** |
| Truthful on-box landing + `DEEP_WORK_HANDOFF.md` + local `handoff.md` | **PASS** |
| No hang at any approval gate (headless deny-by-default policy) | **PASS** — the run never blocked waiting for a human |
| Per-task checkpoint commits on the box's `stint/deep-*` branch | **FAIL** — see finding F1 |
| On-box handoff commit | **FAIL** — same root cause (F1) |

## Findings

### F1 (CP1 blocker): box has no git committer identity

Every `git commit` on box B failed with `Committer identity unknown` — the
fresh Vast image's root has no `user.name`/`user.email`, and
`scripts/provision-box.sh` only checks that `git` *exists* (it never configures
an identity). Coordinator behavior under the failure was correct: it logged
`checkpoint-failed` incidents, still marked both tasks **verified** (verify is
the truth, not the commit), and landed honestly with the failures recorded.
But **CP1's real gates include checkpoint commits** (each Vanta task lands as
a commit on the box), so this must be fixed before the real run.

**Fix (one line in `scripts/provision-box.sh`, not yet applied/committed):**
```sh
git config --global user.name "Stint Deep Work"
git config --global user.email "deepwork@stint.local"
```

### F2 (fixed inline): rsynced repo hits git's `safe.directory` check

The coordinator's preflight died on `detected dubious ownership in repository
at /root/stint-vanta` — `rsync` copies the operator's Vanta repo preserving
uids, and root's git refuses a repo owned by an unknown uid. Fixed live on
the box: `git config --global --add safe.directory /root/stint-vanta`, then
relaunched the coordinator (session id unchanged, 18:03 → landed 18:05).
The same fix belongs in the launch procedure between rsync and `deep start`.

### F3 (cosmetic, by design): M6 keeps state after an unconfirmed destroy

After the run, my teardown watcher called `stint down` **without `--yes`**;
the new type-to-confirm gate (PR #67 `af1bc0f`) is correct for an
interactive operator but **no-ops in a non-interactive script** — the box
was destroyed by Vast's auto-destroy deadline instead (SSH confirmed gone),
but because the destroy wasn't *confirmed through Stint*, M6 correctly kept
`session.json` as an unconfirmed-destroy record. Consequences for automation:
- automated teardown **must** pass `--yes` (or the equivalent programmatic
  confirm), or the state record lingers;
- Vast's instance list lagged the real destroy (still listed the instance
  minutes after teardown; SSH was already refused) — the confirmation poll
  needs to tolerate provider-side list lag, which is exactly what the
  bounded-retry design covers, but an *operator script* that polls only once
  will see a false "still alive".

### F4 (process): the profile cost ceiling is not configurable

The `stint-dryrun` profile's $2.50 `MaxCostUSD` ceiling (built-in default in
`internal/core/plan.go`, no flag/env/config override) rejected a 16 h box B
rental ($5.98 estimate). Worked around by renting 4 h (~$1.50) with a 3 h
deep-session cap. For the real CP1 run (a 6–8 h box), the profile needs an
explicit higher ceiling — needs a config override, which does not exist yet.

## What was proven (the point of P4)

- Headless Hermes on a rented box does real shell/file work through the
  box-local model endpoint, with deny-by-default command policy, **with no
  human and no hangs**.
- Per-task verify-over-SSH, mission-level verify, and the two-document
  landing (on-box + operator-side) all work end to end.
- The run-1 safety stack held up live: the rejected-instance path archived
  its final state (box 49982506, see `archive/sessions/`), and the watchdog
  ran detached for the whole session.
- Failure containment works: two operator-side defects (F1, F2) both failed
  *fast, loudly, and without orphaning anything*.

## Open items before the real CP1 run (P5)

1. **F1** — add git identity to `scripts/provision-box.sh` (CP1 blocker).
2. **F2** — `safe.directory` for the rsynced repo in the launch procedure.
3. **F3** — automated teardown must pass `--yes`; document the provider
   list-lag behavior.
4. **F4** — cost-ceiling override for long CP1 boxes (or keep boxes ≤ ~6 h).
5. **P4 gate re-run** — one clean dry run with F1+F2 in place (both tasks
   verify *and* checkpoint) to flip P4 to unambiguous green. ~40 min, ~$1.

## Run-2 evidence

- `~/.local/state/stint-dryrun/stint/deep/20260905-180320/` — `handoff.md`,
  `deep.json` (task table: both `verified`, 1 attempt each),
  `coordinator.log`, `incidents.jsonl` (checkpoint-failed events for F1).
- `~/.local/state/stint-dryrun/stint/archive/sessions/49982506.*.json` —
  the rejected first candidate (US, 10.9 MB/s < 40 MB/s network gate).
- `~/dryrun-launch.log` — full launch transcript (rent → boot → smoke →
  rsync → the F2 preflight failure → relaunch → landing).
- Box B destroyed (SSH connection refused; Vast instance gone); billing
  stopped. Operator teardown used the auto-destroy deadline (see F3).