# Dry Run 1 (2026-09-04) — Incident Report

**Status:** closed — all failure modes fixed on `feat/deep-work-hermes-worker`
(PRs #67/#69); incident cost borne, no recurrence possible in the same form.

## Summary

The first P4 synthetic dry run of `stint deep --worker hermes` (Mode A,
two-box, `stint-dryrun` profile) ran at **2026-09-04 ~01:12 BST** and failed
at the bootstrap of box B. The failure chain orphaned the rented Vast
instance (**49805324**) and it **billed ~15 h past its auto-destroy deadline**
before Vast's own deadline finally tore it down. No coordinator state was
ever written (`deep/` was empty), which is why the failure initially looked
like "the run never happened."

## Timeline (all 2026-09-04, BST unless noted)

| Time | Event |
| --- | --- |
| ~01:12 | Dry run launched via `stint-dryrun` profile (Mode A: coordinator on the operator machine, box B rented for the headless-Hermes worker). Box B = instance **49805324**. |
| ~01:12 | Profile bootstrap fails: the committed `docs/CP1_DRYRUN_MISSION.md` told the operator to copy `credentials.json` to `$XDG_CONFIG_HOME/credentials.json`, but the profile reads it from `$XDG_CONFIG_HOME/stint/credentials.json`. The dryrun profile could not complete its Vast bootstrap. |
| ~01:12–01:23 | The session's watchdog attempts to destroy box B. The destroy call **no-ops on a DNS timeout for `console.vast.ai`** — the request fails, is not retried to completion, and nothing is recorded locally as a failed destroy. |
| ~01:23 | The operator terminal closes. `spawnWatchdog` did not set
`SysProcAttr{Setsid: true}`, so the watchdog dies with the controlling
terminal (SIGHUP). No local deadline enforcer remains. |
| ~01:23 → +~15 h | Box B is **orphaned**: no watchdog, no local state
pointing at it (the bootstrap failure meant no usable `session.json`), and
**Vast exposes no `auto_destroy`/`end_time` field** on `create_instance`, so
nothing on the provider side could recover the situation either. The box
billed ~15 h past its intended auto-destroy window. |
| 2026-09-05 | Incident investigated; fix stack (M5/M6 + watchdog hardening)
landed as PR #69 (`a7a423b`) on top of PR #67 (M6 destroy-verify). P4 re-run
launched 2026-09-05 18:32 BST — see `CP1_DRYRUN_FINDINGS.md`. |

## Failure chain (root causes)

1. **Operator-facing doc bug** — the committed Mode-A bootstrap instructions
   placed `credentials.json` in the wrong path for the profile, so the
   dryrun profile's bootstrap failed. (Doc fix: corrected path
   `$XDG_CONFIG_HOME/stint/credentials.json` — landed in the working tree,
   pending commit; see findings doc §"Open items".)
2. **Destroy without confirmation** — `DestroyInstance` treated a failed
   request as a no-result; a transient DNS timeout on `console.vast.ai`
   meant the box was *never destroyed*, with no `LastError` persisted and no
   record in `watchdog.log` that the destroy failed. A paid instance can
   outlive its local bookkeeping silently.
3. **Watchdog dies with its terminal** — no `Setsid`, so SIGHUP on terminal
   close killed the only local deadline enforcer. Combined with (2), nothing
   local could ever destroy the box again.
4. **No provider-side ceiling exists** — verified against the authoritative
   Vast `create_instance` OpenAPI spec (`vast-ai/docs`,
   `create_instance.yaml`, cross-checked with `vast-cli`
   `instances.py::create_instance`): the accepted body has **no
   `auto_destroy`/`end_time`/duration field**. The local mechanisms are the
   ceiling; an on-box `onstart` self-destruct was rejected because it would
   put the Vast API key on a transient GPU host.

## Cost impact

- One RTX-4090-class box billed ~15 h past its intended destroy window
  (roughly the difference between the auto-destroy deadline and the actual
  Vast-side teardown; operator paid the full orphaned duration).
- Secondary cost: the failure was invisible for ~24 h (no state, no log
  signal), which delayed the fix by a full day.

## Fixes landed (all on `feat/deep-work-hermes-worker`, PRs #67/#69)

| Fix | Commit | What it guarantees now |
| --- | --- | --- |
| **M6 — destroy-verify-until-gone** (PR #67, `af1bc0f`, `2acc198`) | `runDown` + `destroyExpiredSession` | After a destroy, Stint polls `ShowInstance` (2 s interval, bounded) until the instance 404s/reports terminal; if still alive, `LastError` is persisted and state is **never cleared while unconfirmed**. |
| **M5 — per-session archive** (`a7a423b`) | all eight teardown sites | Before every `sessionstate.Clear`, the final `session.json` is copied (0600, atomic) to `<state>/archive/sessions/<instanceId>.<destroyedAt>.json` — an orphaned-box incident is provable from the operator machine alone. |
| **Watchdog `setsid`** (`a7a423b`) | `spawnWatchdog` | Watchdog leaves the terminal's process group; survives SIGHUP on terminal close. |
| **RECOVERABLE-preserve watchdog ensure** (`a7a423b`) | `runStartResumable` | A start failure that preserves a paid instance now guarantees a live watchdog (re-spawn if the recorded PID is dead) before the preserved state is written. |

Verified live during the 2026-09-05 re-run: the rejected-instance path
archived its final session state correctly (box 49982506), and the watchdog
ran detached (process-group leader) for the whole session.

## Lessons

1. For any paid external resource, **a destroy is not done until the
   resource is observed gone** — confirmation is part of the operation.
2. **Local state is the only durable link** between an operator and a paid
   box; if state is lost or the watcher process dies, the box is gone from
   the operator's world. Archive-before-clear + detached watchdog is the
   cheapest possible insurance.
3. Provider APIs are a ceiling, not a safety net: Vast has no auto-destroy
   field, so *local* mechanisms are the entire safety story for unattended
   runs. (Documented so future operators stop looking for a provider flag.)
4. Operator-facing launch docs are part of the safety system: a one-line
   path bug in the bootstrap instructions is enough to strand a paid
   instance. The fixed `CP1_DRYRUN_MISSION.md` is now the reference.

## Sources

- Commit `a7a423b` (M5 archive + watchdog hardening) — incident description
  and fix rationale.
- `investigations/2026-09-03-stint-live/PENDING_PLANS.md` § P-TEARDOWN
  (instance ID, billing detail, provider-spec verification) — local durable
  record (untracked by convention).
- `~/.local/state/stint-dryrun/stint/` — run-1 residue: `known_hosts`
  (box B key), `watchdog.log` (DNS-timeout destroy), `lifecycle.lock`.
  (Note: run 2 overwrote most of this state; the evidence above is preserved
  in the two committed/recorded sources.)
- `docs/CP1_DRYRUN_MISSION.md` — the launch procedure whose bootstrap bug
  triggered run 1.