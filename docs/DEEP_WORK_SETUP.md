# Setting Up Stint Deep Work — In-Depth Guide

*Audience: an engineer who has Stint on their machine and wants to run their first
supervised, unattended engineering mission — and to be able to run many more after.
Includes one complete worked example, from empty environment to a reviewable branch.*

**End state when this guide is done:**

- a `stint` CLI on your PATH that knows `stint deep start|status|stop`
- a rented GPU session running an OpenAI-compatible endpoint at `http://127.0.0.1:8409/v1`
- the Cline CLI installed and configured *once* against that endpoint
- one Deep Work session run to landing: a git branch with verified work + a truthful
  `handoff.md` that tells you exactly what to do next

---

## 1. The architecture you are wiring together

```
 Your machine (never sleeps? it must not)                  Rented GPU (Vast)
 ┌────────────────────────────────────────────┐            ┌──────────────────────┐
 │  stint deep start (coordinator, foreground)│   SSH      │  NInfer / llama.cpp  │
 │   • parses mission, durable state          │   tunnel   │  serves Qwen3.8-27B  │
 │   • runs YOUR verification command         │ ◄────────► │  on 8409 (remote)    │
 │   • owns the worktree + branch             │            └──────────────────────┘
 │  Cline CLI (worker, fresh process/task)    │
 │   └─ talks to http://127.0.0.1:8409/v1 ────┼───────────► (OpenAI-compatible)   │
 └────────────────────────────────────────────┘
```

- **Stint** rents the machine, boots the runtime, tunnels the endpoint to a *stable local
  URL*, and enforces a hard paid deadline via a detached watchdog.
- **Cline** is a headless coding agent: prompt in, bounded autonomous execution out,
  JSONL event stream. It never touches Vast or the rental — it only talks to the local
  endpoint.
- **Deep Work** (the coordinator) is Stint's supervisory loop: it launches *fresh* Cline
  invocations per task in an isolated git worktree, and after each one it runs **your**
  verification command — not the worker's word — to decide: continue, continue with
  reconstructed context, park, or land.

One Cline process = one invocation = at most one task's worth of work. Continuity across
invocations comes from durable state + git, which is what makes a partially completed
mission resumable and honest.

---

## 2. One-time setup

### 2.1 The `stint` CLI

Build it (any branch that includes Deep Work; the dashboard branch additionally
carries the context bar):

```sh
cd <stint-repo>
make build                 # → bin/stint
ln -sfn $PWD/bin/stint ~/.local/bin/stint   # if ~/.local/bin is on PATH
stint version              # → 0.1.0
```

`make build` rewrites `bin/stint` in place; the running process keeps the old inode, new
invocations pick up the new binary.

### 2.2 Vast + SSH key

Stint needs a funded Vast account and an SSH key for machine qualification (both are part
of ordinary Stint onboarding — you don't configure them here). Verify with:

```sh
stint status
# Vast provider     configured
# Stint SSH key     configured
```

### 2.3 Node.js + the Cline CLI

```sh
node --version             # v20+ (v24 verified in the live run)
npm i -g cline
cline --version            # e.g. 3.0.61
```

### 2.4 Configure Cline against the Stint endpoint (once)

After your first `stint start interactive` (step 2.5) the endpoint is live, then:

```sh
cline auth -p openai-compatible \
          -k stint \
          -m qwen3.8-27b \
          -b http://127.0.0.1:8409/v1
```

- `-k stint` is a placeholder key — the local endpoint doesn't check it, but the CLI
  requires one. (Or pass `--api-key` per run instead.)
- Re-run this only if you switch models or endpoints. Cline's config lives in `~/.cline`
  (override per project with `stint deep start --cline-config <dir>`).

### 2.5 Rent your first compute session

```sh
stint start interactive --hours 4 --yes
```

What happens: Stint picks a Vast offer, **qualifies the host by actually measuring
model-transfer throughput** (it will happily re-rent a few candidates until one passes),
boots the runtime — **NInfer on RTX 4090 hosts, llama.cpp elsewhere** — and tunnels the
inference server to `http://127.0.0.1:8409/v1`. A detached watchdog destroys the instance
at the paid deadline even if your laptop closes.

Flags that matter for Deep Work missions:

| Flag | Meaning |
| --- | --- |
| `--hours <float>` | maximum *paid* session duration (default 1) — give Deep Work a real budget |
| `--yes` | skip the rental confirmation prompt |
| `--runtime auto\|ninfer\|llama.cpp` | inference runtime (default auto: NInfer on 4090s) |
| `--ninfer-config coding\|precision\|native` | NInfer profile (default coding) |
| `--clients 1\|2` | NInfer generation lanes sharing one KV pool (2 is NInfer-only) |
| `--location <text>` | prefer a Vast offer location |
| `--min-measured-download-mbps <n>` | throughput floor during qualification (default 40) |

### 2.6 The verification ritual (run before every mission)

```sh
stint status                                   # READY? deadline in the future?
curl -s http://127.0.0.1:8409/v1/models | head # model id + context_window
cline --version                                # CLI reachable
git -C <your-repo> status --short              # no uncommitted *tracked* changes
```

All four pass → you can launch. Any fail → fix that layer first; Deep Work preflight
checks exactly these and fails fast, but checking yourself costs five seconds.
---

## 3. How a Deep Work session works (60-second version)

**Preflight** (before any side effects): mission parses with ≥1 task → a compute session
is **READY** with a future deadline → `cline` on PATH → model known → target repo is a
git repo with a clean tracked tree.

**Workspace**: Stint creates its own git worktree at `<repo>/.stint-deep/<session-id>` on
branch `stint/deep-<session-id>`, cut from the repo's current HEAD. Your active checkout
is never touched. Session ids are `YYYYMMDD-HHMMSS` (UTC).

**Loop**, per task:

1. Fresh Cline invocation in the worktree, prompt = mission + task + acceptance +
   previous attempt result + current git state (branch, HEAD, recent commits, diff vs
   base). A process with *zero* memory of the last invocation can continue the work.
2. When it ends, Stint runs your `## Verification` command in the worktree.
3. Transition on **evidence**:

| Outcome | What happens |
| --- | --- |
| verification passes | task `verified` → checkpoint commit → next task |
| fails, attempts remain (cap 3), time remains | task `incomplete` → **continuation**: new invocation with reconstructed context |
| attempts exhausted | task `blocked` with the blocker string → mission continues with the next task |
| no `## Verification` in mission | completion is accepted but reported **unverified (worker report)** |

**Landing**: when no safe useful work remains — or the *landing window* opens (10 min
before the deadline; a quarter of shorter windows) — the coordinator parks in-flight
work, writes `handoff.md`, and exits. If the window opens mid-task, the task is parked
`incomplete` rather than started/finished recklessly. `stint deep stop` lands immediately
from durable state, from any terminal, whether or not the start process is alive.

**Where everything lives**:

```
~/.local/state/stint/deep/latest                  → newest session id
~/.local/state/stint/deep/<session-id>/
    deep.json         durable state (tasks, statuses, deadlines) — the coordinator's own source of truth
    mission.md         copy of the mission as parsed
    coordinator.log    append-only decision stream (invocations, transitions, landing)
    handoff.md         the landing report
<repo>/.stint-deep/<session-id>/                  the worktree (left for your review)
<repo>/.stint-deep/<session-id>/DEEP_WORK_HANDOFF.md
branch stint/deep-<session-id>                    local commits only — never pushed
```

---

## 4. Writing a mission

The format reference and all rules live in `docs/DEEP_WORK.md` §3. The five rules that
matter in practice:

1. **Each task = one fresh-agent-sized chunk.** "Refactor the module" fails; "move X
   from a.go to b.go, keep tests green" works.
2. **Your `## Verification` command must be able to tell success from failure.** If it
   can't, neither can Stint — tasks will park as blocked.
3. **One verification command per mission** (MVP). Different needs? Write a small script
   and verify *that*.
4. **3–8 tasks, ordered by dependency.** Earlier tasks' commits are visible to later
   invocations.
5. **Constraints steer the model; policy is the rail.** Worktree isolation, local-only
   commits, and the landing deadline are enforced by Stint, not by the model.

---

## 5. Worked example: `stint doctor`, start to finish

**The case.** You want a `stint doctor` command: one invocation that health-checks your
Stint setup (config paths, local endpoint, active session) and prints a pass/fail table,
plus a test that runs without a live endpoint. It's the ideal first mission: real value,
bounded scope, verifiable, and it runs *against your own toolchain*.

### 5.1 Write the mission (`mission.md`)

```markdown
# Add a stint doctor command

## Objective
Add `stint doctor` that checks config paths, the local endpoint, and the active
compute session, printing a compact pass/fail table and exiting nonzero when any
check fails.

## Success
- `stint doctor` runs against a live environment and reports each check
- `go test ./cmd/stint/ -run TestDoctor` passes without a live endpoint

## Constraints
- no new dependencies
- read-only: do not mutate compute session state
- keep the diff under ~200 lines

## Verification
go test ./cmd/stint/ -run TestDoctor -count=1

## Tasks
- [ ] T-001: implement `stint doctor` with the three checks
  - acceptance: `stint doctor` prints a pass/fail row for config, endpoint, and session
- [ ] T-002: add TestDoctor with a fake environment
  - acceptance: the test passes with no live endpoint and no active session
```

Note what makes this mission *Deep Work-shaped*: each task is one fresh invocation's
worth of work, T-002 doesn't depend on a live endpoint, and the verification command
(`go test … -run TestDoctor`) passes only when **both** tasks are genuinely done — so a
worker that "completed" T-001 without a test will fail verification and get a
continuation, not a credit.

### 5.2 Before you launch

```sh
stint status
# Active compute   instance 49674488 (READY)   ← must be READY
# Remaining        3h47m                          ← must cover your mission
curl -s http://127.0.0.1:8409/v1/models | head -3   # model is served
cline --version
git -C ~/Documents/projects/Stint status --short     # clean tracked tree
```

### 5.3 Launch

```sh
stint deep start --mission mission.md --repo ~/Documents/projects/Stint
```

Expected output, then silence:

```
Deep Work session 20260902-210500 started.
  mission:   Add a stint doctor command (2 tasks)
  model:     qwen3.8-27b via http://127.0.0.1:8409/v1
  worktree:  /home/you/Documents/projects/Stint/.stint-deep/20260902-210500 (branch stint/deep-20260902-210500)
  deadline:  2026-09-02T01:22:10Z  (lands from 2026-09-02T01:12:10Z)
  the coordinator runs in this process; keep this machine awake.
```

The silence is correct: decisions go to `coordinator.log`, not your terminal.
### 5.4 While it runs (second terminal, optional but fun)

```sh
stint deep status
```
```
Deep Work 20260902-210500 — Add a stint doctor command (active)
  deadline: 2026-09-02T01:22:10Z (3h21m remaining)
  worktree: /home/you/.../.stint-deep/20260902-210500 (branch stint/deep-20260902-210500)

  TASK       STATUS     ATT OBJECTIVE
  T-001      incomplete 1   implement `stint doctor` with the three checks
  T-002      queued     0   add TestDoctor with a fake environment
```

or the raw decision stream:

```sh
tail -f ~/.local/state/stint/deep/20260902-210500/coordinator.log
```
```
task T-001 attempt 1: invoking executor (timeout 10m0s)
task T-001 attempt 1 result: exit=0 finish="completed" iterations=11 tokens_in=61204 tokens_out=3877 in 96s | Added `stint doctor`…
task T-001 INCOMPLETE (attempt 1/3): will reconstruct context and continue
task T-001 attempt 2: invoking executor (timeout 10m0s)
…
```

What you're watching: T-001's first worker writes the command but not the test, so the
verification command (`go test -run TestDoctor`) fails — the coordinator **rejects the
worker's claim of completion** and continues T-001 in a fresh invocation that receives
the previous attempt's result plus the current git state. T-002's worker then writes the
test; verification passes; `T-002 VERIFIED`; the mission lands.

Timing calibration (from live runs on a 4090 + `qwen3.8-27b`): small fixture tasks took
22–33 s per invocation; a mission of this size lands in roughly **5–20 minutes**
depending on how many continuations the tasks need.

### 5.5 Landing

When the loop ends, the foreground process prints:

```
Deep Work session landed.
  handoff:  /home/you/.local/state/stint/deep/20260902-210500/handoff.md
  inspect:  stint deep status
```

…and returns you to your shell. The worktree and branch are left in place on purpose —
**you** decide what happens to the work.

### 5.6 Read the handoff

```markdown
# Deep Work Handoff — Add a stint doctor command

| | |
| --- | --- |
| Session | `20260902-210500` |
| Phase | landing (no safe useful work remaining) |
| Actual duration | 14m12s |

## Tasks

| ID | Objective | Status | Attempts | Evidence |
| --- | --- | --- | --- | --- |
| T-001 | implement `stint doctor` with the three checks | verified | 2 | mission verify passed |
| T-002 | add TestDoctor with a fake environment | verified | 1 | mission verify passed |

## Verification
Mission verification passed: go test ./cmd/stint/ -run TestDoctor -count=1

## Workspace
- worktree: /home/you/.../.stint-deep/20260902-210500 (left in place for review)
- branch: stint/deep-20260902-210500

## Remaining work & next action
None — all tasks verified.
Recommended next action: review branch `stint/deep-20260902-210500`, inspect the
worktree above, and merge or discard.
```

Read it top to bottom: **Status** tells you what was proven; **Evidence** separates
coordinator-checked results (verification passing) from worker reports; **Remaining
work** tells you exactly what a next session (or you) must finish.

### 5.7 What you do with the branch

```sh
cd ~/Documents/projects/Stint
git log --oneline stint/deep-20260902-210500            # one checkpoint per verified task
git diff HEAD...stint/deep-20260902-210500 --stat       # what it touched
git show stint/deep-20260902-210500 -- cmd/stint/doctor.go   # spot-check the core change
```

Then one of:

- **Accept**: `git merge stint/deep-20260902-210500` (or cherry-pick), run your normal
  CI/tests on the merged tree, then clean up:
  ```sh
  git worktree remove .stint-deep/20260902-210500 && git branch -D stint/deep-20260902-210500
  ```
- **Discard**: same two cleanup commands, no merge. The rental never saw any of it.
- **Re-run**: fix the mission (e.g. a too-strict verify command), `stint deep start`
  again with the same `--repo` — a new session id, a new worktree from your (now
  updated) HEAD.

### 5.8 How this example can go sideways — and what you'd do

| What happens | What the system does | What you do |
| --- | --- | --- |
| T-001's verify keeps failing (test too strict, or the command can't pass) | 3 attempts → `blocked: verification did not pass`; **T-002 still runs**; landing reports the blocker | Read the blocker, fix the mission or the test, re-run |
| The worker hallucinates "done" on T-001 | Verification fails → continuation with the failure attached (this is the product's core behavior, not an error) | Nothing — that's the loop working |
| Session deadline lands first (you rented too little) | In-flight task parked `incomplete`; honest handoff lists remaining tasks | `stint extend 2h --yes`, then `stint deep start` with the same mission |
| Compute dies mid-run (host lost) | Next invocation fails; coordinator logs it and the session lands or errors out | `stint resume` (re-qualifies a host), then a new `stint deep start` — verified checkpoints survive on the branch |
| Your laptop sleeps | Wall-clock deadlines still advance; the session may land early | Keep it awake; for long missions, run under a manager that re-launches |
| You change your mind mid-run | `stint deep stop` from any terminal | It lands now, from durable state, with a truthful partial handoff |
---

## 6. Day-to-day operations

| Command | Use it when |
| --- | --- |
| `stint deep start --mission m.md --repo <path>` | begin a mission (foreground, runs to landing) |
| `stint deep status` / `--json` / `--session <id>` | inspect the latest / a specific session; `--json` for scripts |
| `stint deep stop` | land now — safe from any terminal, even if the start process is gone |
| `stint extend 2h --yes` | buy more runway *before* re-launching a mission that ran out of time |
| `stint resume` / `stint start interactive` | restore compute after a death |
| `stint down` | destroy the session when the day is done |

Recipes:

```sh
# a long mission on a pinned model
stint deep start --mission big-refactor.md --repo ~/code/app \
  --hours 6 --model qwen3.8-27b --task-timeout 15m

# strict review-everything run in an isolated Cline profile
stint deep start --mission m.md --repo ~/code/app \
  --auto-approve=false --cline-config ~/.cline-strict

# machine-readable progress for a dashboard or script
stint deep status --json | jq '.tasks[] | {id, status, attempts, blocker}'
```

---

## 7. Troubleshooting

| Symptom | Cause → fix |
| --- | --- |
| `no active compute session` at `deep start` | no session: `stint start interactive --hours N` (or `stint resume`) |
| `compute session is …, not READY` / deadline passed | `stint resume` or a fresh `stint start interactive` |
| `the cline CLI was not found on PATH` | `npm i -g cline` |
| `resolve model from the Stint endpoint: …` | endpoint down or model gone: `curl http://127.0.0.1:8409/v1/models`, `stint status --refresh`; or pass `--model` explicitly |
| `<repo> has uncommitted tracked changes` | commit or `git stash` in the target repo; untracked files are fine |
| every task `blocked: verification did not pass` | the verify command is stricter than any one task's scope — split the command, make tasks cumulative, or verify a script that checks overall progress |
| `blocked: invocation did not complete` | Cline/endpoint failure: read the stderr tail in `deep.json`/handoff; fix auth, model, or endpoint, re-launch |
| session landed early with `queued` tasks | the landing window closed first: extend the session, re-run the same mission |
| invocations feel slow | that's the model: `coordinator.log` records per-invocation duration and token counts; a bigger model or longer context = slower |
| `stint deep status` seems stale | it reads `deep.json` directly — that file is exactly what the coordinator trusts; nothing is hidden |

---

## 8. Tuning and limits (know these)

| Knob | Default | Notes |
| --- | --- | --- |
| `--hours` | the compute deadline | may only *tighten* the paid deadline, never extend it |
| `--task-timeout` | 10m | per-invocation wall cap, enforced by process-group kill (no orphaned children) |
| `--max-attempts` | 3 | attempts per task before parking as blocked |
| `--auto-approve` | true | tool approval inside the isolated, git-rollbackable worktree |
| landing window | 10m before deadline (¼ of shorter windows, min 2m) | first-draft constant, calibrated from live runs |

Known MVP limits, stated plainly:

- **One `## Verification` command per mission** (no per-task acceptance commands yet).
- **Foreground coordinator**: the `stint deep start` process *is* the coordinator; it
  must live and the machine must stay awake. (`stint deep resume` — relaunching a
  session from `deep.json` — is the next tracked step.)
- **Rides an existing session**: Deep Work never rents/extends/destroys compute.
- **Local-only by design**: no push, PR, merge, or fetch. Landing leaves the branch for
  your judgment.
- **Truthful over optimistic**: no verification command → results are labeled
  *worker report*, never *verified*.

---

## 9. Where this is going

- `stint deep resume` — restart a session from durable state (compute via existing
  start/resume machinery)
- a `deep` profile that owns its own rental (no separate `stint start` needed)
- live `stint deep plan` — draft the mission against the repo before spending a token
- cross-review of landed branches; multiple workers ("Deep pair")

Product direction: `docs/STINT_DEEP_WORK_VISION.md` · engineering record:
`docs/DEEP_WORK_MVP_EXECUTION.md` · operator reference: `docs/DEEP_WORK.md`

---

## 10. Appendix — one-page quick reference

```sh
# setup (once)
make build && ln -sfn $PWD/bin/stint ~/.local/bin/stint
stint start interactive --hours 4 --yes                      # rent + boot + tunnel
cline auth -p openai-compatible -k stint -m qwen3.8-27b -b http://127.0.0.1:8409/v1

# ritual (before every mission)
stint status && curl -s http://127.0.0.1:8409/v1/models | head -3 && cline --version

# run
stint deep start --mission mission.md --repo <repo>          # foreground → lands → handoff.md
stint deep status        # peek (add --json for scripts)
stint deep stop          # land now, honestly, from any terminal

# after landing
git log --oneline stint/deep-<id> && git diff HEAD...stint/deep-<id> --stat
#   → merge / cherry-pick / discard, then:
git worktree remove <repo>/.stint-deep/<id> && git branch -D stint/deep-<id>

# end of day
stint down
```
