# Stint Deep Work — MVP Execution (Living Document)

**Status:** ACTIVE execution run (supersedes the PROPOSED gate model of `STINT_DEEP_WORK_INVESTIGATION.md`).
**Date started:** 2026-09-02 (run 1); continued run 2 (probe + `internal/deep`); continued run 3 (coordinator, CLI, tests, worktree recovery).
**Repository baseline (run 2):** branch `fix/vast-marketplace-resilience`, HEAD `bbf3037` (pushed, clean tree, `go build ./...` OK, go1.27.0).
**Implementation workspace (run 3):** separate git worktree `/home/marguel/Documents/projects/stint-deepwork` on branch `feat/deep-work-mvp` (cut from `bbf3037`). The user's active checkout (`~/Documents/projects/Stint`) is shared and was mid-dashboard-work during run 3 — see F-DW-007; all Deep Work implementation, tests, and commits happen in the worktree.
Run 1 declared baseline `feat/cli-ansi-styling` @ `f6d0263`; run 2 rebases the effort onto the current checkout because it carries the newest compute-recovery work (queued Vast candidates, deferred marketplace refresh) that Deep Work sessions ride on. `origin/main` (5a2a866) is older than both.
**Governing intent:** `docs/STINT_DEEP_WORK_VISION.md` (product direction), reconciled below against
the historical planning baseline `STINT_DEEP_WORK_INVESTIGATION.md`.

This is a **living** document. Every executed step appends to **Findings** / **Validation**; the
**Active Task** section always names the single next executable step. It is not a document to be
approved before work begins.

---

## 1. Current Baseline (reconstruction, summarized)

- **Stint today** is a local Go control plane (zero third-party dependencies, `go.mod` has no
  `require` block): it rents a Vast GPU instance, bootstraps a pinned model runtime (NInfer on
  RTX 4090 / llama.cpp elsewhere), tunnels it to a stable local OpenAI-compatible endpoint
  `http://127.0.0.1:8409/v1`, and enforces a hard budget-capped deadline via a detached watchdog.
  All execution logic lives in `cmd/stint` (`package main`); `internal/*` holds small pure pieces.
- **The Cline invariant** (README): Cline and the repository live on the developer's machine and
  never touch Vast. Cline (IDE extension, v4.1.16, running in Cursor) is configured once against
  the Stint endpoint.
- **What exists for Deep Work:** compute acquisition/replacement/resume, deadline + watchdog,
  durable atomic-JSON state convention (`~/.local/state/stint/`), observability, and test
  conventions (dependency injection, fakes, fixture offers, `-race` in CI). **What does not
  exist:** any agent executor, task model, git integration, or evidence/verification machinery.
- **Untracked planning artifacts** at the repo root / `docs/`: `STINT_DEEP_WORK_INVESTIGATION.md`
  (historical baseline), `docs/STINT_DEEP_WORK_VISION.md`, `docs/SPARK_STINT_BOUNDARY.md` (+ tarball).
- **Live evidence at run start:** the Stint endpoint is **up and serving** —
  `curl http://127.0.0.1:8409/v1/models` → `qwen3.8-27b` (`context_window 262144`,
  `owned_by ninfer`). A Vast instance is currently rented and ready. Node v24.15.0 / npm 11.12.1
  available for driving the Cline CLI.

## 2. Superseded Assumptions

Explicit record of what the historical baseline assumed and this run rejects:

1. **Native-executor-first is under challenge and now rejected for the MVP.** The old plan's
   "largest missing primitive" (a Stint-native model-client + tool-protocol executor: DW-001
   protocol probes, DW-005 native executor, DW-015 native sandbox, DW-017 fake-model harness)
   presumed Stint must own the coding-agent loop. This run establishes that the **official Cline
   CLI** (npm package `cline`, Apache-2.0, published by the Cline team) provides a complete
   headless coding-agent runtime: prompt in, bounded autonomous execution out, structured JSON
   event stream, timeout, auto-approval policy, and working-directory/worktree control.
   **Stint orchestrates; Cline executes.** The native executor is built only if concrete evidence
   shows Cline prevents Stint from owning Deep Work continuity.
2. **The fixed 8-hour G-6 gate is superseded.** Deep Work duration is arbitrary and
   user-configured (`--hours` is an input, never a success criterion). Validation gates now prove
   *capabilities* (multiple invocations, continuation, fresh-context reconstruction, task
   transition, blocker parking, verification, deadline awareness, landing, truthful handoff)
   using whatever configured duration each experiment needs.
3. **G-0 human sign-off is not a blocker.** D-1…D-8 were reclassified as ordinary engineering
   decisions and resolved from current evidence (see §5). The human is surfaced only genuinely
   consequential product decisions that cannot be safely bounded.
4. **The old 20-task graph is not automatically the MVP graph.** Tasks are re-marked
   SUPERSEDED / SIMPLIFIED / DEFERRED below; new stable IDs (`DWX-*`) carry the revised path.
   History is preserved, never erased.
## 3. Run-2 reconciliation (this run, 2026-09-02 14:52–15:30 UTC)

### Git / GitHub audit (Gate A evidence)

* 37 local branches; all relevant to Deep Work is **untracked documentation only**.
  The only commit present on no remote is `7b7af72` on
  `backup/fix-ninfer-cold-start-before-resync` — unrelated to Deep Work (backup of an
  interactive cold-start fix); left untouched.
* 3 stashes: two WIP on `feat/instance-ssh-access` (resume/SSH/runtime + a progress test)
  and one `pre-pr11 local model limits` on `main` — all user WIP, unrelated to Deep Work,
  untouched.
* `gh pr list` (~34 open, newest #55 `feat/startup-time-to-serving`): **no Deep Work PRs
  exist, open or merged**; nothing Deep Work-related anywhere on GitHub.
* `git log --all --grep=deep` (case-insensitive): zero commits; source grep for
  `deepwork|deep_work|deep-work`: zero hits outside the four known planning docs.
* **Conclusion: run 1 (2026-09-02 03:42–05:04) stopped after writing §1–§2 of this
  document. No code, no branches, no probes were executed; the Cline CLI was not installed
  at that time.** The trustworthy continuation point is: finish the execution doc, then
  execute the Cline seam probe (Gate B).

### Environment at run 2

* Cline CLI **v3.0.61** installed (`npm i -g cline`, Apache-2.0, 329 packages).
* Live compute: Vast RTX 4090 instance 49652103, NInfer runtime, `qwen3.8-27b`
  (context 262144) serving on `http://127.0.0.1:8409/v1`; session READY.
* At 14:54 UTC the session deadline (15:12 UTC) was extended +2h via
  `stint extend 2h --yes` → **new deadline 17:12:20 UTC**, projected session cost $1.19
  against a $2.50 ceiling. No rerent, no tunnel/model disturbance.
* Host timezone is BST (UTC+1); state-file JSON times are UTC (avoid mtime confusion).

## 4. MVP boundary (run 2)

**IN**

* `stint deep` command group: `start`, `status` (+ `--json`), `stop`. Slice-1 `start`
  rides an existing READY compute session (1:1 mapping); it does not rent compute itself.
* Markdown mission file (stdlib-parsed): objective, success criteria, constraints,
  one verification command, an explicit small task list with per-task acceptance notes.
* Durable Deep Work state: `~/.local/state/stint/deep/<session-id>/` with atomic 0600
  `deep.json` (state + tasks + findings + checkpoints), mission copy, `coordinator.log`,
  `handoff.md` — same atomic-write convention as `session.json` (F-007).
* Workspace: **Stint-owned git worktree** on branch `stint/deep-<session-id>` cut from
  the target repo's current branch; the developer's active checkout is never mutated;
  the coordinator commits task checkpoints locally (no push, no merge, no PR).
* Execution adapter: **Cline CLI subprocess**
  (`cline -c <worktree> --json --auto-approve <bool> -t <timeout> -P openai-compatible
  -m <model> -k <key> --config <dir> '<prompt>'`); JSONL event stream parsed;
  `run_result`/exit code is the invocation lifecycle truth.
* Continuation policy (the product core): on invocation end the coordinator asks
  "is the task accepted?" — runs the mission's verification command and checks evidence —
  then transitions: VERIFIED → checkpoint + next task; INCOMPLETE → reconstruct durable
  context (mission + task + prior attempt result + git state) and reinvoke (attempt cap);
  BLOCKED → park and select next queued task; NEEDS_HUMAN → record and continue.
* Deadline-aware loop: land before the compute deadline (landing window), run final
  verification, write a truthful `handoff.md` (verified / partial / unverified / blocked),
  commit, persist state; early landing when no safe useful work remains is valid.
* Offline testability: executor behind an interface; tests use a fake cline binary and a
  fake clock; race-clean; no spend in CI.

**OUT (recorded, not silently absorbed)**

* Two workers / multiple compute leases (V2 "Deep pair", roadmap §3).
* Autonomous task decomposition/replanning — missions supply explicit task lists for the
  slice; discovery creates *new* tasks only if implemented later.
* Cline `--id` conversation resume and Cline-side `--worktree` — Stint reconstructs
  durable context instead and owns the workspace.
* Opening the `deep` profile in the rental pipeline (`stint deep start` renting its own
  compute) — DWX-012, after the loop is proven (touches the most battle-tested code).
* Live `stint deep plan` (fixture planning already exists) — DWX-013, deferred.
* Cross-review, Spark, remote workspaces, automatic push/merge/PR, TUI watch,
  generalized executor plugins, native in-Stint executor (DW-005 SUPERSEDED).

## 5. Historical task reconciliation (DW-\* → DWX-\*)

| Old ID | Old scope | Disposition | Carried by |
| --- | --- | --- | --- |
| DW-001 | Native protocol probes | SUPERSEDED (partial) | DWX-002 answers the load-bearing questions |
| DW-002 | Mission model | SIMPLIFIED | DWX-003 (Markdown mission, explicit task list) |
| DW-003 | Deep state layer | KEEP (simplified) | DWX-003 (`deep.json` atomic state) |
| DW-004 | Coordinator core | KEEP | DWX-005 (loop in `cmd/stint`, F-019) |
| DW-005 | Native executor | SUPERSEDED | DWX-004 (Cline subprocess adapter) |
| DW-006 | Git integration | SIMPLIFIED | DWX-005 (Stint-owned worktree + local commits) |
| DW-007 | Verification | SIMPLIFIED | DWX-005/006 (coordinator runs mission verify command) |
| DW-008 | Live deep planning | DEFERRED | DWX-013 |
| DW-009 | Deep start wiring | SPLIT | slice-1 rides READY session (DWX-005); gate lift = DWX-012 |
| DW-010 | Grounding + decomposition | DEFERRED (simplified) | explicit mission tasks + context reconstruction |
| DW-011 | Execution loop + anti-loop | SIMPLIFIED | DWX-005 (attempt caps + per-invocation timeout) |
| DW-012 | Landing + handoff | KEEP | DWX-006 |
| DW-013 | Recovery integration | SPLIT | DWX-008; compute loss reuses existing `stint resume` |
| DW-014 | Observability | SIMPLIFIED | DWX-007 (`stint deep status` + `--json`) |
| DW-015 | Safety rails hardening | DEFERRED | MVP rails: worktree, timeout, auto-approve policy, local-only commits |
| DW-016 | Documentation package | KEEP | DWX-011 |
| DW-017 | Fixture E2E harness | SIMPLIFIED | DWX-009 (fake cline + fake clock) |
| DW-018 | Live calibration runs | KEEP | DWX-010 |
| DW-019 | Release gating | DEFERRED | post-MVP |

## 6. Current task graph (DWX-\*)

| ID | Task | Depends | Status |
| --- | --- | --- | --- |
| DWX-001 | Gate A: reconcile Git/GitHub/local-only work/artifacts | — | COMPLETED (run 2) |
| DWX-002 | Gate B: Cline headless seam probe vs live Stint endpoint | DWX-001 | COMPLETED (run 2, §9) |
| DWX-003 | `internal/deep`: mission parsing, task model, atomic state, context reconstruction + unit tests | DWX-002 | COMPLETED (run 2/3) |
| DWX-004 | Cline execution adapter (subprocess + JSONL event/result parsing, injectable) | DWX-003 | COMPLETED (run 3) |
| DWX-005 | `stint deep start`: worktree setup, coordinator loop (continuation, parking, checkpoints), rides READY session | DWX-004 | COMPLETED (run 3) |
| DWX-006 | Landing + truthful handoff + `stint deep stop` | DWX-005 | COMPLETED (run 3) |
| DWX-007 | `stint deep status` (+ `--json`) | DWX-005 | COMPLETED (run 3) |
| DWX-008 | `stint deep resume` (coordinator recovery from state; compute via existing machinery) | DWX-006 | queued |
| DWX-009 | Offline E2E: fake cline + fake clock (continuation, park, landing, handoff), race-clean | DWX-006 | COMPLETED (run 3) |
| DWX-010 | Live bounded mission run + findings + constant calibration | DWX-009 | COMPLETED (run 3, session 20260902-180013) |
| DWX-011 | Docs: `docs/DEEP_WORK.md`, CLI.md/README deep sections, mission skeleton | DWX-010 | queued |
| DWX-012 | Open `deep` profile in rental pipeline (`stint deep start` self-rents) | DWX-010 | DEFERRED |
| DWX-013 | Live `stint deep plan` | — | DEFERRED |
| DWX-014 | Safety hardening (command allow-list policy, incident log) | DWX-010 | DEFERRED |

## 7. Active task

**DWX-003 — `internal/deep` package.** Steps:
1. Branch `feat/deep-work-mvp` from current HEAD `bbf3037`.
2. `internal/deep/mission.go` (Markdown mission parser), `task.go` (task model/statuses),
   `state.go` (atomic `deep.json` save/load, session id), `context.go` (task prompt
   reconstruction), with unit tests.
3. Then DWX-004 (Cline adapter) and DWX-005 (coordinator loop) in `cmd/stint`.

## 8. Findings (run 2)

* **F-DW-001 — Cline CLI v3.0.61 exposes a complete headless seam** *(verified)*: prompt
  positional (or piped stdin; `--json` refuses interactive), `-c/--cwd`, `--json` (JSONL
  stdout), `--auto-approve` (default true), `-t/--timeout <s>`, `--retries`, `--thinking`,
  `--compaction`, `-P/--provider` (default `cline`), `-k/--key`, `-m/--model`,
  `--config <dir>` (default `~/.cline`), `--data-dir` (isolated state), `--worktree`
  (Cline-side convenience), `--id` (session resume), `--acp` (editor protocol),
  `auth -p -k -m -b` non-interactive provider configuration.
* **F-DW-002 — Gate B probe result** *(verified, live endpoint)*: provider configured via
  `cline auth -p openai-compatible -k stint -m qwen3.8-27b -b http://127.0.0.1:8409/v1`;
  task "create probe.txt containing exactly `probe-ok`" in a scratch git dir completed in
  **23.4 s** (`run_result: finishReason completed, iterations 5, input 31043 / output
  1765 tokens`), file written exactly, truthful final report, clean process exit, 663
  JSONL events. Stderr carries only benign AI-SDK deprecation warnings. A first attempt
  failed only because `--json` requires an explicit prompt — adapter requirement: always
  pass the prompt explicitly.
* **F-DW-003 — `deep` profile already exists in `internal/core/plan.go`** (2 workers,
  RTX 3090, $0.18/h cap, 8 h default, $3.00 ceiling) but `start`/`resume`/live-`plan`
  hard-gate to `interactive` (`cmd/stint/lifecycle.go:41`, `resumable_start.go:34`,
  `resume.go:46`, `main.go:133`). Slice 1 deliberately rides a READY session;
  profile/rental changes are DWX-012.
* **F-DW-004 — Run-1 stop point**: this document at 59 lines (§1–§2 only); no code,
  probes, branches, or PRs existed for Deep Work before run 2.
* **F-DW-005 — Deadline machinery reuses as-is**: `stint extend` moved the live session
  deadline without rerent (watchdog re-reads state) — Deep Work lands before the
  deadline; the existing watchdog remains hard-deadline authority.
* **F-DW-006 — Timing caution**: host clock is BST; `session.json` times are UTC. Use
  `run_result.durationMs` for probe wall-clock, not file mtimes.
* **F-DW-007 — Shared-checkout collision (run 3), recovered without data loss**: during
  run 3 the user's own workflow stashed the shared checkout
  (`stash@{0}` "local work before dashboard test" on `feat/dashboard-client-context`)
  and switched branches, sweeping the untracked Deep Work sources and planning docs out
  of the tree. Everything was recovered read-only from the stash's untracked commit
  (`git show 'stash@{0}^3:<path>'`) into the new dedicated worktree
  `/home/marguel/Documents/projects/stint-deepwork` (branch `feat/deep-work-mvp`).
  Consequences: (a) Deep Work implementation now lives in the dedicated worktree and
  must not be written back into the shared checkout; (b) the user's checkout currently
  carries the run-3 CLI wiring as uncommitted tracked changes (`cmd/stint/main.go`,
  `cmd/stint/help.go` — the `deep` dispatch + help spec) plus untracked
  `cmd/stint/deep_{run,start,status}.go`; if the user commits their dashboard work as-is,
  the Deep CLI registration ships with it — they should review and either move those
  hunks to `feat/deep-work-mvp` or keep them (they are dependency-free and harmless);
  (c) `stint deep` state still lives under `~/.local/state/stint/deep/` and is
  checkout-independent, so sessions survive any workspace churn.
* **F-DW-008 — Timeouts must kill the process group.** A plain
  `exec.CommandContext` kill of `sh -c '…; sleep N'` (or a CLI that spawns children)
  orphans the children, which keep the stdout pipe open and hang `Wait()` until the
  children exit. The Cline adapter runs invocations in their own process group
  (`Setpgid`) and SIGKILLs the whole group on context expiry (with a short retry loop
  for late-starting groups). Proven by `TestClineExecutorTimeout` (300 ms bound on a
  5 s sleeper: bounded, no hang).
* **F-DW-009 — Lifecycle truth stays dual.** Completion is accepted only on
  `run_result` `finishReason:"completed"`, or a `done` event with exit 0 (fallback for
  stream truncation); progress-only streams count as NOT completed. The adapter also
  enforces `execInput.timeout` as its own context bound so any direct caller (tests,
  future executors) gets hard bounding even if the coordinator forgets its wrapper.
* **F-DW-010 — Silent verification commands (live run)**: `test … && grep -q …` passes
  with empty output, which the first landing reported as "Final verification did not
  run". Fixed: landing reports pass/fail as its own signal (output is supplementary),
  and the handoff phase renders as `landed`. Regression test
  `TestDeepLandingSilentVerifyReported`.

## 9. Validation evidence

**Gate A (state reconciled)** — `git status/branch -vv/worktree/stash`;
`git log --branches --not --remotes` (one unrelated local-only commit); `gh auth status`;
`git fetch --all --prune`; `gh pr list --state all` (~34 open, none Deep Work);
`git log --all --grep=deep` (empty); source grep (empty); artifacts read. Stopping point:
run 1 ended mid-execution-doc at §2.

**Gate B (execution seam proven)** — `cline -c /tmp/dw-probe-1 --json --auto-approve true
-t 900 --retries 2 '<probe task>'` with provider `openai-compatible → qwen3.8-27b @
127.0.0.1:8409/v1` (live Vast NInfer session). Result: `probe.txt` written exactly
(`probe-ok`, 8 bytes), `run_result` completed (23418 ms, 5 iterations, 31043/1765
tokens), structured JSONL event stream, clean process exit. Bounded task ✓, intended
workspace ✓, Stint endpoint + model selection without touching the user's IDE Cline
config ✓, observable lifecycle ✓, auto-approval active ✓, fresh invocation with
fully reconstructed prompt context (no conversation memory) ✓.

**Gates C/D (continuation + parking, proven offline at unit level, run 3)** —
`cmd/stint/deep_loop_test.go` drives the real coordinator over a real temporary git
repo with a real `git worktree` (branch `stint/deep-testsession`) and fake
executor/clock:

* `TestDeepLoopContinuation`: attempt 1 of T-001 fails; the loop selects the same
  task again, builds a fresh prompt carrying the task ID/objective, the previous
  attempt result, the branch, HEAD, recent log, and uncommitted changes; attempt 2
  verifies. Task reaches `verified` with `attempts=2`; the worktree contains
  checkpoint commits beyond the session base; the handoff is written and recorded.
  This is Gate C's mechanism with conversational memory removed.
* `TestDeepLoopParkAndContinue`: a task that never passes verification is parked
  `blocked` after its attempt cap with a blocker string; the coordinator continues
  and verifies the next task; the handoff reports the split honestly
  (`blocked` + Remaining work section). Gate D's mechanism.
* `TestDeepLoopLandsOnClock` / `TestDeepLoopParksWhenBudgetExhausted`: landing
  window and time-budget parking behave as specified (queued tasks land unstarted,
  executor never invoked past the budget).
* `TestDeepLoopStopsOnExternalLanding`: a second process lands the session from
  durable state; the running loop observes the phase change on its next iteration
  and exits without further invocations (the `stint deep stop` protocol).
* `deep_executor_test.go` (fake `cline` shell binary, no network, no spend):
  success with full `run_result` parsing (iterations/usage/text, event count),
  missing-`run_result` (progress-only stream ⇒ not completed), `done`-only fallback,
  nonzero exit with stderr tail, 300 ms context bound on a 5 s sleeper (process
  group kill, F-DW-008), and exact argv assembly (prompt always explicit, F-DW-002).

Full suite: `go build ./...` OK; `go test -race -count=1 ./...` green
(`internal/deep` + `cmd/stint` + all pre-existing packages). gofmt clean.

**DWX-010 (live bounded mission, run 3, session `20260902-180013`)** —
`/tmp/stint-deep deep start --mission /tmp/dw-live-1/mission.md --repo /tmp/dw-live-1
--task-timeout 3m --max-attempts 2 --model qwen3.8-27b`, riding the live READY
session (instance 49668730, deadline 18:28:47Z). The coordinator started its own
worktree (`/tmp/dw-live-1/.stint-deep/20260902-180013`, branch
`stint/deep-20260902-180013`) and ran four real Cline→NInfer invocations, landing
itself in **1m18s** (deadline untouched):

* T-001 (create `a.txt`): attempt 1 — Cline reported `completed` (22 s, 6
  iterations); the coordinator ran the mission verify command, it failed
  (`done.txt` absent), so the task transitioned **INCOMPLETE → continuation**:
  a fresh invocation with reconstructed context (task ID/objective, previous
  attempt result, branch/HEAD/log/dirty state).
* T-001 attempt 2 — Cline correctly recognized attempt 1's file already satisfied
  T-001's acceptance (24 s); mission verify still failed (T-001's scope cannot
  produce `done.txt`), attempt cap reached → **BLOCKED** ("verification did not
  pass") and parked.
* T-002 (create `done.txt`): one Cline invocation (33 s) → mission verify
  **passed** → **VERIFIED** + checkpoint commit.
* Landing: "no safe useful work remaining"; truthful handoff written to state dir
  and worktree (`T-001 blocked/2 attempts`, `T-002 verified/1`), final state
  persisted atomically; `deep.json` phase `landed`.
* Independent re-check after landing: `test -f a.txt && test -f done.txt && grep -q
  ok done.txt` → pass (`alpha` / `ok` present); worktree log:
  `baseline → add mission → T-002 verified → handoff`.
* Calibration signals for DWX-010 follow-up: 3 m task timeout was never close to
  binding (largest invocation 33 s); attempt cap 2 parked a correctly-scoped task
  that was unsatisfiable under the mission-level verify command — per-task
  acceptance commands (vs one mission-level command) is the next precision step;
  landing window (10 m) unused.

**Gate C/D verdict (MVP):** the offline unit proofs (above) plus the live session
prove the core loop end-to-end: fresh invocations continue from durable state +
git alone, workers' self-reports are never trusted without coordinator
verification, unsatisfiable tasks park without stalling the mission, and the
session lands before the deadline with an honest handoff.

## 10. Blockers

None. (Deadline pressure is managed: the live run must land before the session deadline;
if the session dies first, `stint resume`/`stint start` re-establishes compute per
existing machinery and DWX-010 runs then.)

## 11. Human decisions

None pending. Recorded ordinary operation: `stint extend 2h --yes` at 14:54 UTC
(+$0.79 exposure, projected $1.19 vs $2.50 ceiling) to secure the probe window on the
already-active rental.

## 12. Next work (recovery for a fresh agent)

1. Read this document end to end. Implementation lives in the dedicated worktree
   `/home/marguel/Documents/projects/stint-deepwork` (branch `feat/deep-work-mvp`,
   cut from `bbf3037`). Do NOT write Deep Work files into the shared checkout
   `~/Documents/projects/Stint` (F-DW-007).
2. DWX-010 is COMPLETED (run 3, evidence in §9). The procedure below is retained
   as the recipe for future calibration runs:
   a. Pre-flight (before spending): `date -u`; `cat ~/.local/state/stint/session.json`
      (READY + deadline in future?); `curl -s -m 3 http://127.0.0.1:8409/v1/models`.
      If the session is gone, `stint start interactive` (or `stint resume`) first —
      Deep Work rides the READY session.
   b. Build the binary in the worktree: `go build -o /tmp/stint-deep ./cmd/stint`.
   c. Create a small scratch target repo (e.g. `/tmp/dw-live-1`): `git init -b main`,
      one small file + initial commit, plus a mission file with 2–3 small tasks and a
      cheap `## Verification` command (e.g. `test -f expected-file && grep ok expected-file`)
      so acceptance evidence is coordinator-controlled.
   d. Run: `/tmp/stint-deep deep start --mission mission.md --repo /tmp/dw-live-1
      --task-timeout 5m --max-attempts 2`. It rides the live session and lands
      itself; keep the machine awake.
   e. Evidence to capture for this doc: session id, task transitions in
      `~/.local/state/stint/deep/<id>/coordinator.log` + `deep.json`, the worktree
      commits (`git -C <worktree> log --oneline`), and the handoff.
      Gate C live proof: a task whose first Cline attempt ends incomplete and whose
      reconstructed second attempt verifies (attempts=2 in `deep.json`).
   f. Append the live findings + calibrated constants (task timeout, landing
      window, attempt cap) to §8/§9; mark DWX-010 COMPLETED; commit the doc update
      on `feat/deep-work-mvp`.
3. Conventions (unchanged): zero third-party deps, atomic 0600 JSON state under
   `~/.local/state/stint/deep/<id>/`, executor behind an interface, orchestration in
   `cmd/stint` (F-019), fake cline binary + fake clock for tests, `go test -race ./...`.
4. Next tasks: DWX-008 (`stint deep resume` — coordinator restart from durable
   state; compute re-establishment is the existing `stint resume` machinery),
   DWX-011 (docs package: mission-file skeleton, operator guide, `docs/DEEP_WORK.md`),
   then the calibration follow-ups recorded in §9 (per-task acceptance commands).
