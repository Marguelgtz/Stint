# Stint Deep Work

Deep Work hands Stint an **engineering mission** and a **bounded amount of time**. Stint
then supervises execution continuity: a coding agent (the [Cline CLI](https://github.com/cline/cline),
pointed at your Stint endpoint) works in an isolated git worktree, and every time an
invocation ends, **Stint — not the worker — decides what happened next**, using
repository evidence. A partially completed mission that lands with an honest handoff is a
successful Deep Work run.

```
You            Stint (coordinator)                    Cline CLI (worker)
 │  stint deep start ──────────► │
 │                               │  mission parse, preflight checks
 │                               │  git worktree (stint/deep-<id>)
 │                               │  durable state (deep.json)
 │                               │ ── reconstructed prompt ──────►│  works in worktree
 │                               │ ◄── JSONL events + exit code ──│
 │                               │  runs YOUR verify command
 │                               │  VERIFIED → checkpoint, next task
 │                               │  INCOMPLETE → reconstruct context, re-invoke
 │                               │  BLOCKED → park, continue with next task
 │                               │  … until no safe useful work remains
 │  handoff.md ◄── truthful landing report, worktree left for review
```

**Stint orchestrates; Cline executes.** Stint owns the mission, task state, deadline-aware
loop, workspace, verification, and handoff. Cline owns model interaction and coding-agent
mechanics. One logical Deep Work worker spans **multiple fresh Cline invocations** —
continuity comes from durable state + git, never from a long-lived conversation.

---

## 1. Prerequisites

| Requirement | Check |
| --- | --- |
| `stint` built with Deep Work (this branch) | `stint deep` → `deep requires a subcommand: start, status, stop, or resume` |
| A **READY** compute session | `stint status` (or `stint start interactive --hours 2` first) |
| The Stint endpoint answering | `curl -s http://127.0.0.1:8409/v1/models` lists a model |
| Cline CLI on PATH | `cline --version` (install: `npm i -g cline`) |
| Cline configured once against the endpoint | provider `openai-compatible` with base URL `http://127.0.0.1:8409/v1` (or pass `--provider/--model/--api-key` per run) |
| A local git repository | `git -C <repo> status` works; **no uncommitted tracked changes** (untracked files are fine) |

Deep Work **rides your existing compute session**: it never rents, extends, or destroys
compute, and never outlives the session deadline. If the session dies mid-run, re-establish
compute with the usual `stint resume` / `stint start interactive` and continue with
`stint deep resume` (§4) — or start a fresh session with `stint deep start`.

**Keep the machine awake.** The coordinator runs in the foreground process you launched.
Wall-clock deadlines keep advancing while the host sleeps. A sleep that lapses the
deadline no longer loses the session — `stint deep resume` re-anchors the budget to the
compute session you restore — but it does consume your compute budget, so `caffeinate`
(or equivalent) is still the cheap option for long missions.

---

## 2. Quick start

```sh
# 1. Compute (existing machinery)
stint start interactive --hours 4

# 2. Write a mission (see §3)
$EDITOR mission.md

# 3. Launch — rides the READY session, runs until it lands
stint deep start --mission mission.md --repo /path/to/repo

# 4. While it runs (optional): inspect progress
stint deep status
stint deep status --json

# 5. Intervene if needed: land now, honestly, from durable state
stint deep stop

# 6. After a crash, a lapsed machine, or a deadline landing
stint resume            # or: stint start interactive --hours 2
stint deep resume       # continues the session in the same worktree and branch
```

When the session lands, Stint prints the handoff path. Read
`~/.local/state/stint/deep/<session-id>/handoff.md` — it is the deliverable:
verified vs partial vs blocked work, evidence, and the exact next action.
---

## 3. The mission file

A mission is a small Markdown document. Only `## Objective` and a non-empty `## Tasks`
list are required; everything else is optional and unknown sections are ignored, so the
format can grow.

```markdown
# Fix the flaky session-resume path

## Objective
Make `stint resume` reliably re-attach to a running compute session, and prove it
with a focused test.

## Success
- `go test ./internal/session/ -run TestResume` passes
- no new vet or build failures in the touched packages

## Constraints
- do not change the Vast API surface
- keep the change under ~300 lines
- no new dependencies

## Verification
go test ./internal/session/ -count=1

## Tasks
- [ ] T-001: reproduce the resume flake with a failing test
  - acceptance: a test that fails on the current code exists and is committed
- [ ] T-002: fix the root cause in the resume path
  - acceptance: the reproduction test passes and the package suite is green
- [ ] T-003: document the resume contract in docs/
  - acceptance: a short section exists and matches the implemented behavior
```

### Format rules

| Element | Syntax | Notes |
| --- | --- | --- |
| Name | first `# heading` | optional; used in reports |
| Objective | `## Objective` + lines | **required** |
| Success criteria | `## Success` bullets | folded into every prompt |
| Constraints | `## Constraints` bullets | folded into every prompt |
| Verification | `## Verification` + first line | a shell command Stint runs **in the worktree** after every task attempt and at landing; 3-minute cap |
| Tasks | `- [ ] ID: objective` | **required, ≥ 1**; `*` or plain `-` also work; `[x]` marks pre-done |
| Per-task acceptance | indented `- acceptance: …` | human-readable evidence description; included in the prompt |

Task IDs are 1–32 chars of letters, digits, `_`, `-`, and must be unique.

### Writing good missions

1. **Small, independently verifiable tasks.** Each task is a *fresh* agent invocation with
   no memory of the last one. "Refactor the auth module" is a bad task; "move token
   validation from `check.go` to `token.go` and keep tests green" is a good one.
2. **Acceptance you can check.** The coordinator runs your `Verification` command after
   each attempt. If the command can't tell success from failure, neither can Stint — it
   will honestly report *unverified* and eventually park the task.
3. **Scope one verification command per mission** (MVP behavior). If different tasks need
   different checks, write a small script the worker can run and make `Verification`
   invoke it. (Per-task acceptance commands are the next precision step, not yet done.)
4. **Keep the list short** (3–8 tasks), ordered by dependency: earlier tasks' work is
   committed before later tasks start, and later invocations see it in the repository state.
5. **Constraints are guidance; policy is the rail.** The worktree, local-only commits, and
   the landing deadline are hard; your constraints text guides the worker's judgment.
---

## 4. How a session works

### Preflight (fails fast, before any side effects)

1. The mission file parses and has at least one task.
2. A **READY** compute session exists and its deadline is in the future.
3. `cline` is on PATH; the model is known (your `--model`, else the first model the
   Stint endpoint serves).
4. The target repo is a git repository with no uncommitted **tracked** changes.

Then Stint creates the session: a Stint-owned **git worktree** at
`<repo>/.stint-deep/<session-id>` on branch `stint/deep-<session-id>`, cut from the repo's
current HEAD, and initializes durable state. Your active checkout is never touched.

### The loop

Each iteration the coordinator:

1. Re-reads the on-disk state (so `stint deep stop` from another terminal is honored
   immediately).
2. If the clock has reached the **landing window** (default: 10 min before the deadline;
   a quarter of shorter windows), it lands instead of starting new work.
3. Selects the first task that is not terminal: `queued`, or `incomplete` (retryable).
4. If even one invocation (`--task-timeout`) would overrun the landing window, a
   `queued` task is parked as `blocked` ("not started: insufficient time") and it lands.
5. Otherwise it invokes Cline in the worktree with a **reconstructed prompt**:
   mission, task ID/objective/acceptance, previous attempt result, findings, and the
   current repository state (branch, HEAD, recent commits, diff vs base, uncommitted
   changes). A fresh process reading only that prompt can continue the work.

### Acceptance (the part that makes it Deep *Work*)

When the invocation ends, Stint does **not** ask the worker "did you finish?". It runs
your `Verification` command in the worktree (if the mission defines one) and reads the
result. Only then does it transition the task:

| Outcome | Transition |
| --- | --- |
| verification passes | `verified` → checkpoint commit → next task |
| verification fails, attempts remain, time remains | `incomplete` → **continuation**: next fresh invocation with reconstructed context + the prior attempt's result |
| attempts exhausted (default cap 3) | `blocked` with a blocker string → park, continue with the next task |
| worker invocation itself failed (nonzero exit / no completion event) | same as above — the failure is recorded as the task's blocker evidence |

Without a `## Verification` command, a cleanly completed invocation is accepted but the
handoff marks the result **unverified (worker report)** — truthfulness over optimism.

### Landing

When no safe useful work remains (or the window is reached), the coordinator: parks any
`active` task as `incomplete`, runs the final verification, checkpoints the worktree,
writes `handoff.md` (state dir **and** worktree root as `DEEP_WORK_HANDOFF.md`), and
persists the final state atomically. The handoff distinguishes *verified* evidence,
*worker claims*, and *unresolved uncertainty*, and ends with the exact next action:
review the branch, merge or discard.

### Resuming

`stint deep resume` restarts the coordinator for a session whose process is gone —
after a crash, a killed process, a lapsed machine, or a deadline landing. It requires
compute to be READY first (`stint resume` / `stint start interactive`) and then
reconstructs everything else from durable state:

1. **Single coordinator.** The coordinator writes `coordinator.pid`; `start` and
   `resume` refuse to run if that process is still alive. A stale pid file (crash,
   power loss) is cleared and tolerated — a signal-0 probe decides.
2. **Deadline re-anchoring.** While the original deadline is still in the future it can
   only be tightened: `min(deep deadline, compute deadline)`. If it lapsed while the
   machine slept, the fresh compute deadline becomes the budget — you deliberately
   re-provisioned compute to continue.
3. **Worktree re-attachment.** If a crash or cleanup lost the worktree directory, the
   branch still holds every checkpoint commit, so resume re-attaches the worktree over
   the existing branch. Only a missing *branch* is unrecoverable (that is a fresh
   `stint deep start`).
4. **Phase revival.** A `landed` or `stopped` session is a pause, not a verdict: resume
   sets it back to `executing` and the loop continues with the remaining tasks.
   `verified` tasks are never redone.

Executor settings (auto-approve, provider, model, timeouts) are persisted at start and
come back on resume; `--auto-approve`, `--provider`, `--model`, `--api-key`,
`--task-timeout`, and `--cline-config` override them per resume.

---

## 5. Where things live

| Path | What |
| --- | --- |
| `~/.local/state/stint/deep/latest` | one line: the most recent session id |
| `~/.local/state/stint/deep/<session-id>/deep.json` | durable state (tasks, statuses, deadlines, executor settings, handoff path) — atomic, 0600 |
| `…/<session-id>/coordinator.pid` | live coordinator pid (signal-0 probed by `start`/`resume`; cleared on exit, tolerated when stale) |
| `…/<session-id>/mission.md` | copy of the mission as parsed |
| `…/<session-id>/coordinator.log` | append-only coordinator decisions (invocations, transitions, landing reason) |
| `…/<session-id>/handoff.md` | the landing report |
| `<repo>/.stint-deep/<session-id>/` | the Stint-owned worktree (left in place for your review) |
| branch `stint/deep-<session-id>` | local commits: task checkpoints + handoff commit. Never pushed |

Session IDs are `YYYYMMDD-HHMMSS` (UTC), so they sort chronologically and read well in
branches and logs.

---

## 6. Operations

| Command | What it does |
| --- | --- |
| `stint deep start --mission m.md --repo <repo>` | preflight → worktree → loop → landing (foreground) |
| `stint deep status` | latest session: phase, deadline remaining, worktree, task table (+ blockers) |
| `stint deep status --json` | the durable state, verbatim, for scripting |
| `stint deep status --session <id>` | inspect a specific past session |
| `stint deep stop` | land the latest session **now**, from durable state. Works whether or not the start process is alive; the running coordinator observes the phase change and exits on its next iteration. Stopping never touches compute. |
| `stint deep resume` | continue the latest (or `--session <id>`) session after a crash, a lapsed machine, or a deadline landing. Compute must be READY first. Deadline re-anchored to the current compute session, worktree re-attached if lost, remaining tasks continue in the same branch. |

**Recovering a crashed or lapsed session.** Restore compute (`stint resume` /
`stint start interactive`), then `stint deep resume`: the coordinator restarts from
`deep.json`, re-anchors the deadline to your restored session, and continues in the same
worktree and branch — verified and checkpointed work is never redone. If the *branch*
itself is gone (deleted, not just the worktree directory), the session is unrecoverable:
review any remaining evidence in `deep.json`/`coordinator.log` and start fresh with
`stint deep start`.

**Continuing a landed session.** Two honest paths: read the handoff's "Remaining work"
section, adjust the mission (or add tasks), and start a new session; or run
`stint deep resume` to pick the same session back up where the handoff left off — the
loop simply keeps working the tasks that were not yet verified.
---

## 7. Safety boundaries (hard rails)

- **Isolation**: work happens only in the Stint-owned worktree; your active checkout is
  never mutated by the coordinator.
- **Local only**: the coordinator commits to the `stint/deep-<session-id>` branch. It
  never pushes, opens PRs, merges, or fetches. Landing leaves the worktree in place so
  *you* decide merge-or-discard.
- **Bounded spend**: one invocation is capped by `--task-timeout` (hard process-group
  kill); the session is capped by the compute deadline, and the coordinator lands before
  it. Deep Work never extends or rents compute — that stays with `stint start/extend`,
  where you see the price.
- **Approval policy**: `--auto-approve` (default on) lets the worker use tools inside the
  worktree. Because the workspace is isolated and git-rollbackable, this is the
  productive default; pass `--auto-approve=false` for review-everything runs.
- **One coordinator per session**: `start` and `resume` write/probe a pid file and
  refuse to run a second coordinator for a session that has one alive. Durable state
  always wins over in-memory state: an external `stop` that lands a session cannot be
  resurrected by the running process's later saves.
- **Truthful reporting**: handoffs label worker claims as such; only coordinator-checked
  verification is reported as verified. A "completed" self-report that fails verification
  is reported as failed.

## 8. Tuning

| Flag | Default | When to change it |
| --- | --- | --- |
| `--hours <n>` | session deadline | give Deep Work a shorter budget than your session |
| `--task-timeout <dur>` | 10m | live runs of small tasks took 20–35 s; for large repo work 10–15 m is a sane bound. Never let it approach the landing window. |
| `--max-attempts <n>` | 3 | lower (2) if your verify command is expensive; raise if tasks legitimately need more fresh starts |
| `--auto-approve` | true | false for full review |
| `--provider` / `--model` | `openai-compatible` / first model the endpoint serves | pin a specific model |
| `--api-key` | from Cline config | per-run override |
| `--cline-config` | `~/.cline` | isolated Cline profile per machine/project |

Calibration notes from the live MVP run (2026-09-02): small fixture tasks completed in
22–33 s per invocation; the 10-minute task timeout was never close to binding; the
landing window (10 m) was never needed. Mission-level verification parked a correctly
scoped task whose acceptance depended on a *later* task — order tasks so each task's
verification can pass on its own, or use a verify command that checks cumulative progress.

## 9. Troubleshooting

| Symptom | Cause / fix |
| --- | --- |
| `no active compute session …` | no READY session: `stint start interactive` (or `stint resume`) first |
| `compute session is …, not READY` / deadline passed | `stint resume` or `stint start interactive` |
| `the cline CLI was not found on PATH` | `npm i -g cline` |
| `resolve model from the Stint endpoint` | endpoint down or no models: check `curl http://127.0.0.1:8409/v1/models`, `stint status` |
| `has uncommitted tracked changes` | commit or stash in the repo before starting |
| every task lands `blocked: verification did not pass` | the verify command is stricter than any single task's scope — split it, or make tasks cumulative (see §8) |
| task `blocked: invocation did not complete` | read the blocker's stderr tail in `deep.json`/handoff: auth, model, or Cline CLI failure — fix the endpoint/Cline config and re-launch |
| session landed early with tasks `queued` | the landing window closed first; extend your session and re-launch the same mission |
| Cline invocations seem slow | that's the model, not the coordinator: `run_result` durations are in `coordinator.log`; a bigger model or more context = slower |
| state looks stale | `stint deep status --json` reads `deep.json` directly; the on-disk file is the truth the coordinator itself trusts |
| `a coordinator for <id> is already running (pid N)` | a `start`/`resume` is in flight: wait for it to land, or `stint deep stop` first |
| `is no longer a git repository` (resume) | the repo path in `deep.json` no longer exists (moved/deleted): restore it or start fresh |
| `worktree … is not a usable git worktree` (resume) | the repository moved and the worktree's links are stale: `git -C <repo> worktree repair`, then resume again |
| `the worktree (…) and its branch (…) are gone` (resume) | the branch was deleted: the session is unrecoverable — `stint deep start` with the same mission |
| `both the Deep Work deadline and the compute session deadline have passed` | the machine lapsed past both budgets: `stint deep start` a fresh session |
| resume lands immediately with `not started: insufficient time` | the re-anchored window was too short: restore more compute time first (`stint extend` / `stint start interactive --hours N`), then resume |

## 10. Going further

- The `deep` profile opening its own compute rental (today Deep Work rides an existing
  session on purpose).
- Live `stint deep plan` (mission drafting against a repo), cross-review of landed
  branches, multiple workers.
- Engineering detail, findings, and live-run evidence: `docs/DEEP_WORK_MVP_EXECUTION.md`.
- Product direction: `docs/STINT_DEEP_WORK_VISION.md`.
