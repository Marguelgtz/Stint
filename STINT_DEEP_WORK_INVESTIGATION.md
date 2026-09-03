# Stint Deep Work — Repository Investigation & Pre-Execution Design

**Status:** PROPOSED — pending human review. Nothing in this document is approved for execution.
**Date:** 2026-09-01
**Scope:** Investigation of how Deep Work fits into the Stint repository *as it exists today* (branch `feat/cli-ansi-styling`, HEAD `f6d0263`), plus a proposed V1 boundary, architecture, task graph, and validation strategy.
**Companion document:** `docs/STINT_DEEP_WORK_VISION.md` (product/architectural intent; not treated as ground truth about the current codebase).

**Labeling convention used throughout:**

- **FACT** — currently true in the repository (file/line evidence given).
- **INTERPRETATION** — conclusion drawn from evidence.
- **PROPOSAL** — recommended future architecture or boundary.
- **OPEN QUESTION** — not yet determined by repository evidence.
- **HUMAN DECISION** — a product choice implementation alone should not make.

---

## 1. Executive Summary

**Current fit between Stint and Deep Work.** Stint today is a local Go control plane that rents a Vast GPU instance, bootstraps a pinned self-hosted model (NInfer on RTX 4090, llama.cpp elsewhere), tunnels it to a stable local OpenAI-compatible endpoint (`127.0.0.1:8409`), and enforces a hard, mutable, budget-capped deadline via a detached watchdog. The *agent* (Cline) and the *repository* both live on the developer's machine and never touch Vast (FACT: README "Cline invariant", `docs/NEXT_STINT.md`). In other words, Stint already owns **compute allocation, bounded time, and recovery** — three of the four resources the vision says Stint should control (COMPUTE, TIME, AUTHORITY, EVIDENCE). What it does not own is **execution**: there is no agent-executor, no task model, no git integration, and no evidence/verification machinery anywhere in the codebase.

**Strongest architectural leverage.**

1. The deadline machinery (`internal/session/deadline.go`, `cmd/stint/deadline_watchdog.go`, `cmd/stint/session_deadline.go`, `cmd/stint/lifecycle_lock.go`) is a complete, tested, race-safe "authoritative deadline + detached enforcer + serialized extension" system. Deep Work's time model should *reuse* it, not rebuild it.
2. The compute-replacement machinery (`cmd/stint/network_candidate_retry.go`, `cmd/stint/resumable_start.go`) — a paid-attempt budget, stale-offer replenishment without spending budget, measured network qualification, `RECOVERABLE` sessions, and `stint resume` — is exactly the primitive Deep Work needs for "the GPU can disappear mid-run."
3. The durable-state convention (atomic JSON files under `~/.local/state/stint/`, keyed caching like `performance.json`, XDG config under `~/.config/stint/`) gives a ready-made pattern for Deep Work session state.
4. The observability stack (7-domain session snapshots, `stint status --refresh`, `stint dash` cockpit with deadline controls, passive inference observation) is a natural surface for Deep Work status and human handoff.
5. The test conventions (dependency-struct injection, fake clock/providers, fixture offers, `go test -race` in CI) mean the coordinator can be developed and validated without spending a single rental.

**Largest missing primitive.** An **agent executor**: a component that takes the local OpenAI-compatible endpoint and turns it into engineering action (model invocation + tool execution against a repository + result inspection). FACT: the only code in the repository that sends a request to the model endpoint on Stint's own behalf is the `stint perf` benchmark (`cmd/stint/perf.go:195`, inline `net/http`, no reusable client). In interactive mode, the external harness (Cline) owns the entire agent loop. Deep Work requires Stint to own that loop, and nothing in the repository exists to do it. This single gap determines most of the V1 work.**Likely V1 direction.** A `stint deep` mode in which: one detached coordinator process (modeled on the existing watchdog process pattern) supervises **one local worker** that edits a dedicated git branch/worktree of the user's local repository using a rented model endpoint; the user supplies a Markdown mission, a configured duration (`--hours N`, any positive value — never a fixed 8h constant), and a conservative authority set (repo read/write, branch commits, run tests; no push/merge/network/deploy); the coordinator maintains an explicit small task graph plus checkpoints in `~/.local/state/stint/deep/<session-id>/`; it reuses the existing start/resume/deadline/compute-replacement machinery for the compute side; and it ends in a landing/finalization window that produces a human- and machine-readable handoff. Multi-worker, cross-review, dynamic replanning, and remote workers are deferred (see §12–§13).

**Major risks.** (1) The agent executor is the riskiest new code: a self-hosted 27B model's reliability as an unattended tool-calling worker is unproven, and V1's success depends on a prompt/protocol design that works with the pinned model. (2) Overnight unattended execution presumes the developer machine stays awake and the local coordinator process survives — the existing system only proves survival of the *watchdog*, and only while the machine is on. (3) All deep-work compute logic currently lives in `package main` (`cmd/stint/*.go`); reusing it from a new package would force a refactor, so V1 must deliberately co-locate deep code in `cmd/stint` or pay the refactor cost. (4) Long sessions consume boot time inside the paid window and shrink qualifying Vast inventory (the search filters `duration >= hours`, `internal/provider/vast/client.go:204`), so duration is both a scheduling input and a marketplace constraint. (5) Orchestration complexity risk: the vision's coordinator has ~10 subsystems; V1 must resist building them all.

**Major ways repository reality validated or changed the vision.**

- *Validated:* deadline-first session model; compute ephemerality with replacement and recovery; conservative safety invariants (plan never pays, explicit confirmation, fail-closed policy); provider/runtime independence at the endpoint boundary; machine-readable + human-readable status output; "no Spark dependency" for the execution core (matches `docs/SPARK_STINT_BOUNDARY.md`).
- *Changed:* the vision's TypeScript-style `packages/compute|deep|git|runtime` layout is adapted to this Go repo's actual layout (`cmd/stint` + small `internal/...` packages). The vision's V1 with 2 workers and cross-review is reduced to **1 worker** (README: "Two cheaper deep workers follow only after the single-worker lifecycle is reliable"; single-instance state model). The vision's `.deep-work/` in-repo state directory is adapted to Stint's XDG state convention, with git artifacts (branch, commits, handoff) as the in-repo record. The mission format is Markdown (repo has **zero third-party dependencies** — no YAML library — and the vision's own V1 says "User provides Markdown mission"). The `deep` profile that exists in code (2×RTX 3090, `internal/core/plan.go:116-132`) conflicts with `stint.config.example.yaml`'s `collaboration: engine: spark` for deep — a conflict to resolve as a human decision, not silently.

---

## 2. Deep Work Vision Interpretation

`docs/STINT_DEEP_WORK_VISION.md` describes Deep Work as an **execution mode for bounded, unattended software engineering**: the developer defines a mission (objective, success criteria, writable/forbidden paths, authority, deadline, budget); Stint acquires compute and supervises one or more coding workers; the system optimizes for "useful, verified engineering progress / (compute cost × time)"; and the session ends with an inspectable handoff rather than a transcript.

Interpretation notes for this investigation:

1. **Duration is a first-class, user-configured input, not a constant.** The vision repeatedly uses 8-hour/overnight examples (§1 timeline, §11 "An eight-hour run might be divided..."), and the mission example uses `deadline: hours: 8`. These are *examples*. This investigation treats the abstraction as **configured execution duration**: the architecture must work for 1h, 2h, 4h, 8h, 12h, or other values, with behavior that *adapts* to time remaining. Nothing in this document bakes an 8-hour constant into the architecture. (Where the vision says "once 20% of execution time remains", the 20% is a proportion of *whatever* the configured duration is — proportions, not absolute minutes, are the duration-independent formulation.)
2. **The central principle is supervision ownership:** "The model performs reasoning and engineering. Stint owns continuity, state, constraints, resources, and authority." A model response ending is not a task ending, not a mission ending, not a session ending.
3. **Stint must own execution continuity**, which in this codebase means Stint must become (or host) the agent loop — see Finding F-004. The vision is explicit that workers are disposable and the session is not (§3), and that state must survive compute loss (§13).
4. **The vision explicitly de-scopes V1** (§19): no Spark dependency, no deployment, no automatic merge, no generalized distributed agent protocol, no complicated planner hierarchy. This investigation's V1 boundary (§12–§13) is *more* conservative than the vision's V1 and is grounded in repository state (single-worker reliability is the repo's own stated precondition).
5. **The long-term direction** (§20) — `stint deep / investigate / review / repair / maintain` sharing missions, workers, compute leases, task graphs, authority, evidence, checkpoints, handoffs — is noted and should shape where new primitives are placed, but is not a V1 requirement.---

## 3. Current Stint Architecture Relevant to Deep Work

### 3.1 System shape

```text
developer machine
├── Stint CLI (single Go binary, zero third-party deps, Go 1.23)
│   ├── Vast REST client (internal/provider/vast)   [search/create/show/destroy/ssh-key]
│   ├── SSH tunnel process (127.0.0.1:8409 → remote 127.0.0.1:8080)
│   ├── detached watchdog process (stint _watchdog) [deadline enforcer]
│   └── state: ~/.local/state/stint/session.json (authoritative),
│             performance.json, logs, known_hosts
├── Cline / harness (external) ──uses──► http://127.0.0.1:8409/v1
└── user's repository (never touches Vast)

rented Vast instance
└── pinned model server only (NInfer or llama.cpp) + Qwen3.8-27B artifact
    (no repository, no tools, no agent — FACT: remote command builders in cmd/stint/runtime.go)
```

FACT: `go.mod` contains only the module line and `go 1.23` — **zero third-party dependencies**. The terminal dashboard, SSH wrapper, and telemetry are all hand-rolled. This constrains Deep Work format choices (e.g., mission parsing must be dependency-free; see §12).

### 3.2 CLI surface (`cmd/stint/main.go:75-102`, `cmd/stint/help.go:252-260`)

| Command | Behavior relevant to Deep Work |
| --- | --- |
| `auth vast`, `setup ssh`, `doctor`, `status` | setup/diagnostics; `status` renders the session snapshot (local or `--refresh`) |
| `plan <profile>` | read-only marketplace planning for `interactive` (live) or `interactive`/`deep` (`--fixture`); selected offer, alternatives, session cost vs ceiling; ends "NO COMPUTE HAS BEEN RENTED." |
| `start interactive` | paid start: candidate pool → rent → SSH → measured network qualification → runtime bootstrap → model start → tunnel → READY (`cmd/stint/resumable_start.go:29-560`) |
| `resume` | reattach a `RECOVERABLE` session: deadline check, tunnel rebuild, remote runtime/model checks, model restart, watchdog respawn (`cmd/stint/resume.go:20-268`) |
| `extend` / `shorten <duration>` | mutate the deadline under the lifecycle lock, budget-capped (`cmd/stint/session_deadline.go`); preview-then-confirm never holds the lock |
| `down` | destroy instance + tunnel + watchdog, clear state (`cmd/stint/lifecycle.go`, `runDown`) |
| `dash` | TUI cockpit: countdown/cost/health/GPU/inference views, deadline +/−, benchmark, resume, teardown (`cmd/stint/dashboard.go`, `docs/DASHBOARD.md`) |
| `perf` | the only place Stint itself calls the model endpoint: streaming `/v1/chat/completions` via inline `net/http` (`cmd/stint/perf.go:195`) |
| `onboard spark` | prints read-only Spark onboarding plan (`internal/spark/onboard.go`) |
| `_watchdog` | hidden internal command: the detached deadline enforcer (`cmd/stint/deadline_watchdog.go:36`) |

Key constraints observed in the code:

- `start` and `resume` **reject any profile other than `interactive`** (`cmd/stint/resumable_start.go:34`, `cmd/stint/resume.go:46`). The `deep` profile exists for planning fixtures only.
- Live `plan` is interactive-only; deep planning requires `--fixture` (`cmd/stint/main.go:133-135`).
- `stint start` hard-fails if any session is already recorded — **exactly one active session** at a time (single `session.json`, `internal/session/state.go:61-63`).
- Dead code from a previous generation of the lifecycle (`runStart`, `runWatchdog`, `startRemoteModel` in `cmd/stint/lifecycle.go`) remains in the tree alongside the active `runStartResumable`/`runDynamicWatchdog` paths; the codebase evolves by superseding, so Deep Work should extend the *resumable* path, not resurrect or parallel the old one (Finding F-018).### 3.4 Startup and runtime lifecycle

Stages and checkpoints are persisted as they are reached (`cmd/stint/resumable_start.go`; status/checkpoint constants in `internal/session/state.go:16-36`):

```text
RENTING → BOOTING → [INSTANCE_CREATED] → SSH_CONNECTING → SSH_READY [SSH_READY]
→ RUNTIME_BOOTSTRAP → RUNTIME_READY [RUNTIME_READY]
→ MODEL_STARTING → MODEL_STARTED [MODEL_STARTED]
→ MODEL_LOADING (tunnel up; model loading, up to 20 min — cmd/stint/resumable_start.go:548)
→ READY [READY]
(crash/failure post-SSH) → RECOVERABLE  (resume reattaches)
```

- Runtime selection (`cmd/stint/runtime.go:38-100`): `auto` → NInfer on 4090 (pinned GHCR image `ghcr.io/marguelgtz/stint-ninfer:981b685e-cuda12.8`, CUDA ≥ 12.8 host floor via `internal/provider/vast/cuda_policy.go`), llama.cpp prebuilt Vast image elsewhere (CUDA ≥ 12.9); auto falls back to llama.cpp on the same host unless a 2-client NInfer contract forbids it.
- The model artifact is pinned and SHA-256-verified (Qwen3.8-27B; `docs/NINFER.md`). The remote server binds `127.0.0.1:8080`; Stint tunnels it to `127.0.0.1:8409` (`cmd/stint/lifecycle.go` `startTunnel`; `clinePort=8409`, `clineRemotePort=8080`).
- NInfer `--clients 2` (dual lanes) is merged and supported: two concurrent client requests share the KV pool (`cmd/stint/ninfer_clients.go`, `docs/NINFER.md`). This is the existing mechanism for **two concurrent agent clients on one rented 4090** (Finding F-012).
- The remote machine hosts *only the model server*. No repository, no shell tools for agents, no git (`docs/NINFER.md` "Startup path"; remote command builders in `cmd/stint/runtime.go`).
- Deep ports are already reserved in code: `DefaultDeepAPort = 8301`, `DefaultDeepBPort = 8302` (`internal/runtime/llama/config.go:3-5`) — evidence of the planned 2-endpoint deep topology, unused by any command today.

### 3.5 Session identity, deadline, budget, lifecycle

- **Session identity:** the single file `~/.local/state/stint/session.json` (`internal/session/state.go:38-59`): instance id, offer id, profile, GPU, runtime/context/clients, hourly rate, hours, `StartedAt`, `Deadline`, SSH host/port, tunnel PID, watchdog PID, status, checkpoint, last error. Atomic write (temp file + rename, mode 0600); `Save` refuses to persist a session without a Vast instance id.
- **Deadline is the source of truth**; `Remaining`, `Elapsed`, `ScheduledDuration` are always *derived* (`internal/session/deadline.go:18-43`). `extend`/`shorten` produce validated proposals, applied under a `flock` lifecycle lock (`cmd/stint/lifecycle_lock.go`) with optimistic revalidation; extend is capped by the profile session ceiling via `maxAdditionalDuration = MaxCostUSD/HourlyUSD − current` (`cmd/stint/session_deadline.go:225-236`); shorten may not expire immediately (use `down`).
- **Budget handling:** planned cost must fit `profile.Session.MaxCostUSD` (`internal/core/plan.go:260`); runtime extension cannot exceed the ceiling; cost accounting is *scheduled-exposure* based (`hourlyUSD × duration`), not metered provider spend.
- **Detached watchdog** (`cmd/stint/deadline_watchdog.go:36-201`): spawned immediately after the instance id is persisted; polls `session.json` every second so extend/shorten take effect without replacing the process; on the apparent deadline it takes the lifecycle lock, re-reads state under the lock (closing the extend-at-expiry race), destroys the instance, and *retries destruction every 15 s on provider failure* rather than abandoning a paid resource; it is instance-ID-scoped so a stale watchdog never touches a replacement instance. All behavior is behind an injectable `deadlineWatchdogDeps` struct — the template for a testable Deep Work coordinator loop.
- **The paid clock starts at rental, before READY** (`cmd/stint/resumable_start.go:325-331`: `StartedAt`/`Deadline` set before `CreateInstance`). Boot, model download, and load all consume the configured window. For a short deep session a substantial fraction can be startup (Finding F-014).
- **Recovery:** anything that fails after SSH is preserved as `RECOVERABLE` and `stint resume` reattaches (port release, remote runtime/model checks, model restart, `ensureWatchdogAlive`); if the deadline passed while away, resume destroys and clears (`cmd/stint/resume.go:76-90`). The dashboard exposes recoverable-session resume controls (`cmd/stint/dashboard_recovery.go`).
- **Single-session model:** one `session.json`, one instance, one tunnel, one watchdog. Nothing today represents "a session with N compute leases" (Finding F-015).### 3.6 Persistence inventory

| State | Location | Survives |
| --- | --- | --- |
| Session identity/deadline/status/checkpoint | `~/.local/state/stint/session.json` | process death; *not* `stint down`/deadline destroy (intentional) |
| Perf benchmark sample | `~/.local/state/stint/performance.json` | keyed to instance+runtime+context; invalidated on mismatch (`cmd/stint/performance_store.go:96-104`) |
| Vast API key | `~/.config/stint/credentials.json` (0600) | forever, user-managed |
| SSH keypair | `~/.config/stint/ssh/` | forever, user-managed |
| Logs | `~/.local/state/stint/{tunnel,watchdog}.log` | process death |
| Model artifact, model log, PID file | remote `/workspace/stint/` | instance lifetime only |
| **Agent/task/mission state** | **does not exist** | — |
| **Anything in Git** | nothing (Stint never runs git; the repo lives only on the user's machine) | — |

Everything durable is on the developer machine. Nothing of value lives on the rented instance except the model process. Provider state is only the instance itself.

### 3.7 Testing conventions

- Pure-logic packages (`internal/core`, `internal/session`) have direct unit tests; `cmd/stint` tests use **dependency-struct injection** (`deadlineWatchdogDeps` with `load/now/wait/lock/destroy/recordFail`, `cmd/stint/deadline_watchdog.go:23-30`, with fakes in `cmd/stint/deadline_watchdog_test.go`), **fixture offers** (`vast.FixtureOffers`), **table-driven cases**, and **clock injection**. This is the pattern a Deep Work coordinator should follow: a `coordinatorDeps`-style struct makes the whole loop testable with a fake model endpoint and fake clock, i.e., **Deep Work can be developed and validated end-to-end offline before spending any compute** (Finding F-009).
- CI (`.github/workflows/ci.yml`): `spark-profile`, `go-vet`, `unit-tests`, `race-tests` (`go test -race ./...`), `build-check`. The race gate is directly relevant to a multi-goroutine coordinator.
- Optional live integration test exists for Vast search only (`STINT_VAST_INTEGRATION=1`, never creates compute).

### 3.8 Observability

- 7-domain session snapshot (`cmd/stint/session_snapshot.go:96-105`: session/time/cost/health/gpu/inference/performance) with strict separation of *lifecycle authority* from *observation* (a failed probe degrades to `unavailable`, never mutates state; `docs/TELEMETRY.md` safety invariants).
- `stint status` (local, no SSH) vs `stint status --refresh` (bounded ~4 s passive remote probes) vs `stint dash` (TUI, ~1 s local / ~10 s remote cadence design, `docs/TELEMETRY.md`).
- Passive live-inference observation polls the engine's `/metrics` + `/slots` through the tunnel and tracks concurrent "agents" — today that vocabulary means *observed inference clients*, not supervised workers (Finding F-012).
- `stint status --json` provides a machine-readable snapshot — the convention a Deep Work handoff should follow (human-readable `handoff.md` + machine-readable JSON).---

## 4. Existing Planning and Dynamic-Task Conventions

The repository already has a mature, project-local planning methodology. Deep Work planning documents should follow it rather than inventing a parallel one.

**Evidence of the conventions (FACT):**

1. **Phased execution plans with explicit exit gates** — `docs/NEXT_STINT.md`: "The work is split into four sequential phases. A later phase does not begin until the previous phase's exit gate passes." Each phase has Goal / Work / Exit gate (checkbox list) / recorded live results. Phase 1 status is marked complete with a findings table; Phase 2 carries "PENDING" evidence slots that are filled by real reruns.
2. **Findings logs inside plan docs** — `docs/PHASE2.md` "Findings log": numbered findings (Finding 01…04) each with the exact command run, observed output, and a one-line conclusion; findings are appended over time (2026-08-29 dated entries) and referenced from the plan. `docs/NEXT_STINT.md` uses a compact "Findings captured" table instead.
3. **Living boundary documents** — `docs/SPARK_STINT_BOUNDARY.md`: a pointer doc that states roles, what may/may not cross the product boundary, and "known data caveats" from a dated production audit — i.e., recorded uncertainty kept next to the plan.
4. **Vision docs + roadmap checklists** — `docs/STINT_DEEP_WORK_VISION.md` (this task's companion), `docs/roadmap.md` (checkbox phases, some marked done), `README.md` "Target command surface" listing `stint deep start --hours 8` under "Phase 3+".
5. **Commit/branch discipline** (git history): small single-concern commits, frequently paired `Test X` / `Document X` / `Add|Fix X` triples; branch names `feat/<area>`, `fix/<area>`, `pre-v0/phase-N-<area>`, `release/v0.1.0`; one branch per feature with its own origin ref. The recent `feat/dynamic-start-candidate-replenishment` branch shows the pattern in miniature: model change → accounting tests → docs → help-semantics tests.
6. **Distinguishing planned from discovered work:** in practice, new work is added as *new branches/tasks with their own tests and doc updates*, and findings docs are appended to rather than rewritten; "PENDING" evidence slots make the distinction between "planned" and "proven" explicit.

**What carries forward into Deep Work planning (PROPOSAL):**

- Phase/exit-gate structure (this document's §20 gates).
- Stable finding IDs (`F-xxx`) and task IDs (`DW-xxx`) so later documents and commits can reference them.
- Append-only findings/history; superseded items marked, not deleted (matches how `lifecycle.go` dead code and `SPARK_STINT_BOUNDARY.md` caveats are handled).
- Evidence slots left `PENDING` until a live run fills them (the repo's honest treatment of live-marketplace results).
- Test+doc pairs as the atomic unit of a task's acceptance.

**Where Deep Work requires something different (INTERPRETATION):** the existing conventions assume a *human* is the loop between phases. Deep Work execution itself needs a *machine-maintainable* living task graph (stable IDs, dependencies, discovered-task provenance) inside durable state, because the coordinator — not a human — must be able to add tasks mid-run and preserve history. The document-level conventions above are for humans; the in-run task graph is for the coordinator. Both are needed, and the coordinator's task records should be shaped so that a later human (or interactive agent) can read them directly in the handoff.---

## 5. Existing Capabilities We Can Reuse

Concrete, evidence-backed primitives a Deep Work V1 can build on (each "reuse" below is a FACT about what exists; the mapping to a Deep Work role is an INTERPRETATION):

1. **Authoritative deadline + derived time** (`internal/session/deadline.go`): `Remaining/Elapsed/ScheduledDuration` derived from one stored `Deadline`; extend/shorten as validated proposals. → Deep Work's "time remaining" is this same computation.
2. **Detached deadline enforcer** (`cmd/stint/deadline_watchdog.go`): survives CLI death, observes deadline mutations live, lock-serialized destruction, retry-on-provider-failure, instance-scoped. → The Deep Work session deadline (compute + work) can be enforced by the same watchdog; the coordinator additionally schedules *work* before the deadline (landing window).
3. **Budget-capped time extension** (`cmd/stint/session_deadline.go:225-236` + `internal/core/plan.go:260`): `MaxCostUSD` ceiling enforced at plan time and at every extension. → Deep Work budget enforcement reuses this directly.
4. **Lifecycle lock + optimistic revalidation** (`cmd/stint/lifecycle_lock.go`, `cmd/stint/session_deadline.go:83-109`): all mutations (start/resume/down/extend/shorten/destroy) serialize on one flock; confirmation prompts never hold the lock. → Deep Work `stop`/`extend`/teardown must join this scheme, not invent a second lock.
5. **Compute replacement pool** (`cmd/stint/network_candidate_retry.go`): paid-attempt budget vs candidate queue, stale-offer replenishment without budget cost, `seen` machine set. → Mid-run worker-compute replacement is the same pattern applied to a new offer search when an instance dies.
6. **Measured qualification before commitment** (`cmd/stint/network_qualification.go`): probe the *actual next step* (model transfer) and preserve its partial work. → For Deep Work, "qualification" generalizes to verifying a worker/compute can do the work (endpoint responds; worktree tools run) before burning task time.
7. **RECOVERABLE + resume** (`cmd/stint/resume.go`, `cmd/stint/dashboard_recovery.go`): post-failure preservation, reattach without re-renting, watchdog respawn. → Deep Work compute loss recovery path.
8. **Atomic durable state files** (`internal/session/state.go`, `cmd/stint/performance_store.go`): temp+rename, 0600, versioned records, keyed invalidation (instance/runtime/context). → Deep Work mission/task/checkpoint/handoff records.
9. **Multi-worker planning economics** (`internal/core/plan.go` `CreateSessionPlan`/`SelectOffers`, `deep` profile with `Workers: 2`): selection + aggregate cost ceilings already computed for N workers. → Deep Work planning/preview surface.
10. **Snapshot/telemetry + dash cockpit** (`cmd/stint/session_snapshot.go`, `cmd/stint/status_telemetry.go`, `cmd/stint/dashboard.go`): 7-domain snapshot, local/refresh modes, JSON contract, TUI with deadline controls. → Deep Work `status`/`dash` views and the human handoff surface.
11. **Detached process supervision** (`spawnWatchdog`, `killPID`, `ensureWatchdogAlive`, PID fields in state): the pattern for running the coordinator as a supervised detached process.
12. **Dependency-injection test seams** (`deadlineWatchdogDeps`, `defaultSnapshotProbeDeps`): offline coordinator/worker testing with fake model endpoint, fake clock, fake process manager.
13. **Model endpoint as stable contract** (Cline invariant; `docs/NINFER.md`): any client speaks OpenAI-compatible to `127.0.0.1:8409/v1`. → The Deep Work worker's model client is just another client of this endpoint; runtimes (NInfer/llama.cpp) and providers (Vast) stay behind it.
14. **Streaming chat-completions client logic** (`cmd/stint/perf.go:189-244`): request build, SSE parse, TTFT/usage extraction, keep-alive handling, retry — extractable into the reusable model-client primitive (Finding F-004).
15. **Deterministic diagnostics convention** (planner never emits an unexplained zero-result; `DiscoveryEmptyError` bisect stages; `planDiagnostics`). → Deep Work failures (no compute, worker stuck) must be equally explainable.---

## 6. Capability Map

Classification key: **Existing** (genuinely present today) / **Derivable** (existing primitives suffice) / **Partial** (pieces exist, meaningful function missing) / **Implied** (repo direction suggests it, not implemented) / **Missing** (no current mechanism).

| Deep Work capability | Class | Evidence / notes |
| --- | --- | --- |
| Mission representation | **Missing** | No mission type anywhere; `collaboration.WorkPacket` (`internal/collaboration/contracts.go`) is the closest existing contract (ID/RepoPath/Branch/Intent) but is a 4-field Spark handoff stub, not a mission. |
| Configurable execution duration | **Existing** | `--hours <float>` on `plan`/`start` (`cmd/stint/resumable_start.go:44`), profile `DefaultHours` (`internal/core/plan.go:114,131`); deadline computed as `startedAt.Add(hours*3600s)`. Any positive float accepted (validation at line 57-60). |
| Session deadline | **Existing** | `Deadline` field + `internal/session/deadline.go`; enforced by `runDynamicWatchdog`. |
| Remaining-time tracking | **Existing** | `sessionstate.Remaining(state, now)` always derived; surfaced in status/dash (`cmd/stint/session_snapshot.go:108-133`). |
| Compute budget (cost ceiling) | **Existing** | `MaxCostUSD` per profile; plan-time check (`internal/core/plan.go:260`); extend cap `maxAdditionalDuration` (`cmd/stint/session_deadline.go:225-236`). Caveat: accounting is exposure-based, not metered. |
| Durable Deep Work session state | **Partial** | The *pattern* is proven (`session.json`, `performance.json` atomic writes); a *deep* record (mission, tasks, checkpoints, handoff, worker/compute mapping) does not exist. |
| Coordinator lifecycle | **Missing** (process pattern **Existing**) | No coordinator concept; the detached-supervised-process pattern (`spawnWatchdog`/`ensureWatchdogAlive`) exists and is the model to copy. |
| Task representation | **Missing** | No task type in any package. |
| Task dependencies / priority / state | **Missing** | Same. |
| Dynamic task creation | **Missing** | No in-run mutation of a work plan exists (plans are computed at start only). |
| Task ownership / worker identity / worker lifecycle | **Missing** | `Workers` in a profile is a *count of compute offers*, not identities; `Clients` is an inference-lane count. `WorkPacket` implies future worker handoff but is unused. |
| Model invocation (by Stint itself) | **Partial** | Only `stint perf` (`cmd/stint/perf.go:195`): inline HTTP, no reusable client, no tool-calling, single-purpose payload. |
| Continuation after a worker turn ends | **Missing** | No conversation/turn management exists in-repo (it lives in Cline, outside Stint). |
| Blocker handling / human-decision parking | **Missing** | No blocked/needs-human state exists anywhere. |
| Retry handling | **Partial** | Retry is mature for *compute* (candidate pool, SSH retries, watchdog destroy retry) and for the perf probe; no retry policy for model turns/tasks. |
| Repeated-failure / stagnation detection | **Missing** | The paid-attempt budget is the only "stop trying the same thing" mechanism, and it applies to machines, not approaches. |
| Checkpoints | **Partial** | Compute-stage checkpoints exist (`INSTANCE_CREATED`…`READY`); task/work checkpoints do not. |
| Findings / decisions records | **Missing** (docs convention only) | Findings exist as *human* documents (`docs/PHASE2.md`), not as structured in-run records. |
| Evidence / verification | **Partial** | `stint perf` captures measurable evidence (TTFT/tok/s/VRAM); CI evidence exists via Spark (external). Stint itself never runs the user's tests, builds, or linters. |
| Context reconstruction / recycling | **Missing** | Context management lives in the harness/model (NInfer KV, `--preserve-thinking`); Stint has no notion of worker context. |
| Git isolation / worktrees / path ownership | **Missing** | Stint never invokes git (no `exec.Command("git"...)` anywhere); the repo is owned by the user/Cline. |
| Multiple workers (execution) | **Partial** | Planning for 2 workers exists (`deep` profile `Workers: 2`); dual-lane NInfer runtime exists; deep ports 8301/8302 reserved; but no worker execution, state, or endpoints beyond 8409. |
| Worker replacement | **Derivable** | Worker state (context) reconstructed from durable records + compute replacement via candidate pool/resume. Requires the missing context-reconstruction piece. |
| Compute replacement | **Existing** | `startupCandidatePool` + `RECOVERABLE`/`resume` + watchdog. |
| Cross-review | **Missing** | No reviewer concept; `collaboration` package is a stub. |
| Deadline-aware work selection | **Missing** (deadline data **Existing**) | Remaining time is known to status/dash/watchdog; nothing selects work by it. |
| Landing / finalization phase | **Missing** | Teardown at deadline exists; no "stop starting new work, finish, verify, commit, hand off" behavior. |
| Final human handoff | **Missing** | No report generation of any kind (status output is the nearest analog). |
| Resuming a previous Deep Work session | **Partial** | Compute-side resume exists and is robust; deep-side resume (coordinator + task state recovery) missing. |---

## 7. Findings

**F-001 — Stint's "session" is a paid compute lease, not an execution session.** *(Confidence: high)*
*Observation:* `session.json` carries exactly one Vast instance, one tunnel PID, one watchdog PID, one deadline (`internal/session/state.go:38-59`). Every command (`start/resume/extend/shorten/down/dash/perf`) operates on that single record.
*Implication:* A Deep Work session is a *different, longer-lived entity* that **owns** one or more compute sessions. V1 must introduce a second state layer (deep session → compute lease mapping) rather than overloading `session.json` with work semantics.

**F-002 — The deadline machinery is complete, race-safe, and directly reusable.** *(Confidence: high)*
*Observation:* authoritative `Deadline`, derived `Remaining`, budget-capped extend/shorten under flock with optimistic revalidation, a detached watchdog that survives the CLI, observes deadline edits live, retries failed destroys, and is instance-scoped (`internal/session/deadline.go`, `cmd/stint/deadline_watchdog.go:36-201`, `cmd/stint/session_deadline.go`, `cmd/stint/lifecycle_lock.go`; tests in `deadline_watchdog_test.go`).
*Implication:* Deep Work's time enforcement should be the same watchdog plus coordinator-side scheduling. Do not build a second timer system.

**F-003 — Compute replacement is already proven end-to-end.** *(Confidence: high)*
*Observation:* paid-attempt pool with stale-offer replenishment, measured network qualification, destroy-and-retry on failed hosts, `RECOVERABLE` preservation, and `stint resume` reattachment (`cmd/stint/network_candidate_retry.go:24-129`, `cmd/stint/resumable_start.go:283-497`, `cmd/stint/resume.go:20-268`; recent commits `79747a3…83bb19d`).
*Implication:* "The GPU disappears mid Deep Work run" is solvable with existing machinery: preserve deep state (new), detect instance death, replace compute via the pool pattern, rebuild the model, resume the task from checkpoint/context.

**F-004 — No agent-executor primitive exists; Stint speaks to the model only for benchmarking.** *(Confidence: high)*
*Observation:* the only in-repo model invocation is `stint perf`'s inline streaming `/v1/chat/completions` call (`cmd/stint/perf.go:189-244`); the agent loop, tool calls, and repo edits belong to the external Cline harness; the remote machine hosts only the model server.
*Implication:* the largest new subsystem is a **model client + tool executor** ("worker executor"). V1's core risk concentrates here (model quality at unattended tool use; protocol design for the pinned Qwen3.8-27B across NInfer and llama.cpp).

**F-005 — Stint never touches the repository; zero git integration.** *(Confidence: high)*
*Observation:* no `git` invocations anywhere in `cmd/` or `internal/`; the Cline invariant keeps repo and tools local (`README.md`, `docs/NEXT_STINT.md` Phase 4: "local repo/tools never move to Vast").
*Implication:* Deep Work must add the first git integration (branch/worktree/commit/diff/checkout state). This is new surface area with new failure modes, and it is also the primary *safety* mechanism for unattended work.

**F-006 — A `deep` profile exists in code but no deep lifecycle does; config examples conflict with the vision.** *(Confidence: high)*
*Observation:* `core.BuiltinProfiles["deep"]` = 2×RTX 3090, $0.18/hr, 0.98 reliability, $3.00 ceiling, 8h default, objective `validated_work_per_dollar` (`internal/core/plan.go:116-132`); `start`/`resume` reject non-interactive profiles; live `plan deep` is disabled; `stint.config.example.yaml` gives deep `collaboration: engine: spark, mode: reciprocal` while the vision and `docs/SPARK_STINT_BOUNDARY.md` forbid Spark from owning execution; the sample YAML is not parsed by any code (profiles are code-embedded).
*Implication:* (a) deep's *planning* economics can be reused immediately; (b) the example config's Spark-collaboration line is stale relative to the vision and must be reconciled by human decision; (c) "deep" today is a GPU shopping list, not a product mode.**F-007 — Durable state is XDG-local, atomic, versioned JSON; nothing durable lives remotely or in git.** *(Confidence: high)*
*Observation:* `~/.local/state/stint/` holds `session.json` + `performance.json` + logs; `performance.json` shows the keyed-cache pattern (instance/runtime/context match, `cmd/stint/performance_store.go:96-104`); remote `/workspace/stint/` holds only model/log/pid.
*Implication:* Deep Work state belongs in `~/.local/state/stint/deep/<session-id>/` following the same atomic-write conventions; git artifacts (branch, commits, handoff) are the natural *redundant* in-repo record.

**F-008 — Observability infrastructure is mature and is the natural Deep Work surface.** *(Confidence: high)*
*Observation:* 7-domain snapshots, local vs refresh modes, `--json` contract, `stint dash` cockpit with deadline +/−, resume, teardown, benchmark (`cmd/stint/session_snapshot.go`, `cmd/stint/status_telemetry.go`, `cmd/stint/dashboard.go`, `docs/TELEMETRY.md`, `docs/DASHBOARD.md`).
*Implication:* Deep Work status/watch/handoff should extend the snapshot pattern (add deep domains: tasks, worker, checkpoint, handoff) rather than create a new reporting system.

**F-009 — Test conventions make the coordinator developable offline.** *(Confidence: high)*
*Observation:* injectable dependency structs with fake clocks (`deadlineWatchdogDeps`), fixture offers, table-driven tests, `go test -race` in CI (`.github/workflows/ci.yml`).
*Implication:* a `coordinatorDeps`/`workerDeps` design (fake model endpoint, fake git, fake clock, fake process manager) yields a full offline validation path — the backbone of the validation strategy in §23.

**F-010 — Planning conventions (phases, exit gates, findings logs, living docs) are established project practice.** *(Confidence: high)*
*Observation:* `docs/NEXT_STINT.md` (phases with exit gates + status), `docs/PHASE2.md` (numbered findings log with observed outputs and conclusions), `docs/SPARK_STINT_BOUNDARY.md` (living boundary doc with caveats), `docs/roadmap.md` (checklists).
*Implication:* the Deep Work document set (this file + later execution plans) should use the same shapes: stable IDs, gates, append-only findings, PENDING evidence slots.

**F-011 — Process supervision primitives exist for exactly the needed shape.** *(Confidence: high)*
*Observation:* tunnel/watchdog PIDs tracked in state; `spawnWatchdog`, `killPID`, `ensureWatchdogAlive` (respawn on resume), `waitForPortAvailable` (`cmd/stint/lifecycle.go`, `cmd/stint/resume.go:308-329`, `cmd/stint/port_release.go`).
*Implication:* the Deep Work coordinator runs as another detached supervised process (e.g., `stint _deepwork <session-id>`), with PID recorded in deep state and respawned by `stint deep resume` — mirroring the watchdog exactly.

**F-012 — Dual-lane NInfer is merged: two concurrent agent clients on one rented 4090 are already supported at the runtime level.** *(Confidence: high)*
*Observation:* `--clients 2` launches NInfer with `--max-concurrency 2` over a shared dynamic KV pool (`cmd/stint/ninfer_clients.go`, `cmd/stint/ninfer_config.go`, `docs/NINFER.md`; commit `cbe0dbf`); "agents" already appears in live-inference telemetry as observed inference clients (`cmd/stint/session_telemetry.go`, `docs/TELEMETRY.md`).
*Implication:* a 2-worker Deep Work session is feasible on a *single* 4090 lease (cheaper than the 2×3090 plan in `core.BuiltinProfiles["deep"]`), provided the worker protocol is well-behaved under shared context. This is a concrete alternative topology for V1+ (see §25).

**F-013 — The provider boundary is thin; there is no "compute lease" abstraction.** *(Confidence: high)*
*Observation:* `Provider` covers only `SearchOffers` (`internal/provider/vast/client.go:47-49`); create/destroy/show/ssh-key are concrete `Client` methods; `internal/provider/cloudflare/client.go` is an empty placeholder; all lifecycle calls live in `cmd/stint`.
*Implication:* the vision's `WorkerLease` interface (§18) has no current home. For V1, deep code should call the same `vast.Client` + start/resume machinery as interactive; a genuine lease abstraction is deferred unless multi-provider needs force it (avoid forcing the vision's shape onto a Vast-only codebase).

**F-014 — The paid clock includes startup, and duration filters the marketplace.** *(Confidence: high)*
*Observation:* `StartedAt`/`Deadline` are set before `CreateInstance` (`cmd/stint/resumable_start.go:325-331`); provider startup timeout 6 min, model load up to 20 min (`resumable_start.go:22,548`); Vast search requires `duration >= ceil(hours*3600)` (`internal/provider/vast/client.go:204`).
*Implication:* for a 1-hour session, boot can consume a large share of the window; for 8-hour sessions, the set of qualifying offers is structurally smaller. Scheduling must be *relative to remaining time after ready*, and short sessions need proportionally shallower grounding/landing. The coordinator should treat "READY" as the true start of the work clock.**F-015 — The single-instance state model is a hard constraint on multi-worker V1.** *(Confidence: high)*
*Observation:* one `session.json` = one instance/tunnel/watchdog (`internal/session/state.go`); `start` refuses to run with any session recorded; the tunnel is fixed to `127.0.0.1:8409` (`cmd/stint/main.go:25`).
*Implication:* a 2-compute-lease deep session (2×3090, ports 8301/8302) requires either a deep-state file that tracks N leases plus N tunnels, or single-lease multi-lane (F-012). Both are bigger than V1's bar; a single lease + single local worker is the coherent V1 shape (see §12, §25).

**F-016 — In the current topology, the coordinator must run on the developer machine, and "overnight" depends on that machine staying up.** *(Confidence: medium)*
*Observation:* the model endpoint is a local loopback tunnel; the repo is local (Cline invariant); the watchdog is a local detached process. No component today tolerates the host machine sleeping for hours (wall-clock deadlines keep advancing; `resume` destroys if the deadline passed).
*Implication:* "overnight" Deep Work either requires the machine to stay awake (product guidance / startup warning) or the remote-worker topology (deferred). This is a real product constraint the vision's 23:00→07:00 timeline glosses over, and it is an OPEN QUESTION for V1 (HUMAN DECISION D-3, §11).

**F-017 — Multi-worker planning economics already exist in `core`.** *(Confidence: high)*
*Observation:* `CreateSessionPlan`/`SelectOffers` handle `profile.Workers: N` with aggregate `MaxCostUSD` (`internal/core/plan.go:233-264`); `plan deep --fixture` already prints 2-worker deep plans.
*Implication:* the deep planning surface (`stint deep plan`) is close to free to enable (live deep planning + budget preview), and de-risks the paid path before any execution code exists — a good first executable milestone.

**F-018 — The lifecycle is mid-evolution: superseded paths remain in the tree.** *(Confidence: high)*
*Observation:* `runStart`, `runWatchdog` (one-shot), `startRemoteModel` in `cmd/stint/lifecycle.go` are dead code superseded by `runStartResumable`/`runDynamicWatchdog`/`startRemoteModelSafe`; both compile and coexist.
*Implication:* Deep Work work should extend the *active* resumable path and its helpers (`startTunnel`, `runSSH`, `retryAttachSSHKey`, `waitForSSH*`, candidate pool), not the dead ones; and it should be willing to remove superseded code as part of the change (repo hygiene the project already practices).

**F-019 — All execution-side logic lives in `package main` (`cmd/stint`).** *(Confidence: high)*
*Observation:* start/resume/watchdog/deadline-mutation/candidate-pool logic is in `cmd/stint/*.go`; `internal/` packages hold small pure pieces (config, core, session, provider, runtime, spark, collaboration, dashboard render). A new `internal/deep` package *cannot call* `runStartResumable` or the candidate pool without a refactor.
*Implication:* V1 co-locates deep orchestration in `cmd/stint` (e.g., `deep_*.go` files) consistent with the repo's actual shape, keeping only dependency-free pure logic (mission parsing, task model) in a new `internal/` package. A big "move everything to internal" refactor is explicitly deferred.

**F-020 — The repository's own stated sequencing puts deep mode behind single-worker reliability.** *(Confidence: high)*
*Observation:* README: "The first target is one interactive RTX 4090… Two cheaper deep workers follow only after the single-worker lifecycle is reliable."; `docs/roadmap.md` §3 "Deep pair" is the final pre-V0 phase; v0.1.0 was released on the interactive lifecycle with its recent fix branches.
*Implication:* V1 Deep Work = 1 worker. This is not merely scope reduction; it is the repository's own ordering principle.

**F-021 — The repository has zero third-party dependencies; new formats must be parseable in pure Go stdlib.** *(Confidence: high)*
*Observation:* `go.mod` has no `require` block and there is no `go.sum`; dashboard/terminal/SSH wrappers are hand-rolled.
*Implication:* the mission format must be Markdown (with a simple, documented structure) or plain JSON — not YAML/another DSL. The vision's V1 ("User provides Markdown mission") aligns.---

## 8. Architectural Insights

Syntheses that emerge from multiple findings:

1. **Two-session architecture** (from F-001, F-015; INTERPRETATION). Stint will have two distinct session concepts: the **compute session** (existing: lease + runtime + endpoint + deadline + watchdog) and the **Deep Work session** (new: mission + tasks + checkpoints + handoff, owning ≥1 compute sessions). The compute session remains the unit of *payment and destruction*; the Deep Work session is the unit of *work and continuity*. Deep Work V1 is the simple 1:1 mapping (one deep session owns one compute session), which is why it can reuse nearly all existing machinery untouched.
2. **The endpoint, not the instance, is the stable interface** (from F-012, F-013; INTERPRETATION). Everything above the model (the Cline harness today, Deep Work workers tomorrow) talks to `127.0.0.1:8409/v1`. Deep Work workers should be *clients of the endpoint*, which keeps provider (Vast), runtime (NInfer/llama.cpp), and even model swaps orthogonal to orchestration — the vision's `WorkerLease → endpoint` intent, achievable without a new provider abstraction in V1.
3. **Supervision is already a first-class citizen** (from F-002, F-011; INTERPRETATION). Stint's core skill is running and reaping detached processes against wall-clock authority (tunnel, watchdog; respawn on resume). The Deep Work coordinator is the *next* supervised process; the difference is what it supervises (a work loop instead of a timer).
4. **Recovery is layered by stage, and Deep Work adds a new layer** (from F-003, F-001; INTERPRETATION). Today: pre-SSH failure → destroy & replace; post-SSH failure → preserve & resume; deadline → destroy. Deep Work adds: *work-state loss is impossible by construction* — all essential state is local + in git, never on the rented box (matching vision §13's "avoid any essential session state living only on the rented machine") — so the only thing that must be rebuilt after compute loss is the *model context*, reconstructed from durable records.
5. **The missing center is small in surface, large in risk** (from F-004, F-005; INTERPRETATION). Everything the coordinator needs *around* the model (time, budget, compute, state, supervision, observability) exists. The one genuinely new capability — "make a self-hosted 27B model perform bounded, tool-using engineering work and report honestly" — is the **worker executor**. Its protocol (how the model emits actions; how tool calls are validated and sandboxed; how completion is claimed and checked) is the design decision that most determines V1's success.
6. **Duration is a scheduling input *and* a marketplace constraint** (from F-014; INTERPRETATION). The same `--hours` value sizes the Vast duration filter, the paid window (including startup), and the work schedule. Keeping one duration as the single source of truth (as `Deadline` already is for time) keeps these three coupled surfaces consistent; the coordinator should treat the endpoint becoming READY as the true start of the *work* clock.---

## 9. Vision vs Repository Reality

| # | Vision claim / concept | Repository reality | Verdict |
| --- | --- | --- | --- |
| 1 | "Stint currently treats compute primarily as infrastructure for an interactive model endpoint" | Exactly true: one tunnel to one model server; Cline is the consumer. | **Matches** |
| 2 | Coordinator with compute scheduler, task graph, repository state, worktree leases, evidence store, checkpoint manager, agent supervisor, budget/deadline controller | Deadline/budget controller, compute scheduler (candidate pool), and agent-*process* supervision exist; task graph, worktree leases, evidence store, work-level checkpoint manager, turn-level agent supervisor do not. | **Partially matches** — ~3 of the 8 named components exist; the rest are the V1 build list |
| 3 | "The model performs reasoning and engineering. Stint owns continuity, state, constraints, resources, and authority." | Stint already owns state/constraints/resources for *compute*; continuity and authority over *engineering work* are new. Authority today is de facto (no push/merge exists anywhere) but implicit, not modeled. | **Matches in direction; authority is implicit, not explicit** |
| 4 | Mission as YAML with objective/success/constraints/authority/deadline/budget | Repo has zero YAML dependency; vision's own V1 says Markdown mission. | **Needs refinement:** Markdown mission with a simple documented structure (F-021) |
| 5 | `.deep-work/<session-id>/` state directory *inside the repo* | Stint convention is XDG state outside the repo (`~/.local/state/stint/`); the repo is kept clean. | **Needs refinement:** state in `~/.local/state/stint/deep/<id>/`; branch/commits/handoff in repo (F-007) |
| 6 | V1: 1 coordinator, 2 workers, worktrees, cross-review | Repo sequencing explicitly defers 2 workers until single-worker reliability; single-instance state; dual-lane 4090 is the cheapest 2-client path if ever needed. | **Changed:** V1 = 1 worker (F-020, F-015); cross-review deferred |
| 7 | 8-hour/overnight timeline as the use case | No duration constant anywhere in code; `--hours` is any positive float; 8h is just the `deep` profile's `DefaultHours`. | **Validated as example, not constant** (§15) |
| 8 | "Stint acquires suitable compute and initializes isolated workspaces" (workers on rented compute) | The rented machine hosts only the model server; the Cline invariant keeps repo/tools local; moving the repo to Vast would break the product's core safety model. | **Changed:** V1 workers execute *locally* against the rented model endpoint; remote workspace is a later topology (§14) |
| 9 | `stint deep plan/start/status/watch/resume/stop/inspect/diff/accept/discard` | `plan` exists for deep (fixture-only); no `deep` subcommand tree; `stint deep start --hours 8` is already listed in README's target surface. | **Mostly missing; command shape pre-committed in README** |
| 10 | No Spark dependency for V1 | True in code (Spark is onboarding/evidence only; `WorkPacket` unused); but `stint.config.example.yaml` still says deep `collaboration: engine: spark`. | **Matches, with one stale artifact to reconcile** (F-006) |
| 11 | WorkerLease abstraction; coordinator independent of Vast/llama.cpp/NInfer | No lease abstraction exists (thin `Provider` interface); runtime independence is real *at the endpoint* but lifecycle code is Vast-concrete in `cmd/stint`. | **Partially matches:** endpoint-level independence yes; lease abstraction deferred (F-013) |
| 12 | Findings/checkpoints/claims/evidence as structured in-run records | Findings exist only as human docs; checkpoints are compute-stage markers; no claims/evidence records. | **Missing; V1 adopts the lightest version** (checkpoint = git commit + task record + test output) |
| 13 | Long-term modes: investigate/review/repair/maintain sharing primitives | Nothing exists; but the primitive placement (state dirs, task model, handoff) is mode-agnostic by design. | **Direction noted; out of V1 scope** |---

## 10. Open Technical Questions

Questions the repository does *not* answer and that implementation (or a short live probe) must resolve:

1. **Q-1 Tool protocol for the pinned model.** Does Qwen3.8-27B under NInfer *and* llama.cpp reliably emit machine-parseable tool calls through the OpenAI-compatible `tools` parameter? If not, what text protocol (structured command blocks) works across both runtimes? This determines the worker executor's core design and should be answered with live probes (the `stint perf` request path is a ready scaffold) before large executor code is written.
2. **Q-2 Context economics of long autonomous work.** What prompt assembly (mission + task + findings + checkpoint + selected files) fits the 126k–262k context presets and keeps decode sane over multi-hour runs? The NInfer presets define ceilings; the *reconstruction policy* (what to include, when to re-ground) is new territory. `stint perf --prompt-tokens` is the existing measurement tool.
3. **Q-3 Host-machine power policy for overnight runs.** What should Stint warn or require about machine sleep (F-016)? Detect suspend? Document "keep machine awake"? (Also human decision D-3, §11.)
4. **Q-4 Worktree vs branch for V1 isolation.** A dedicated branch in the user's checkout (simple; requires a clean-tree precondition) vs `git worktree` (cleaner isolation; slightly more machinery). Both are cheap in git terms; the choice affects failure handling when the user's tree is dirty mid-run.
5. **Q-5 Where test/build execution happens and its guardrails.** Workers run shell commands locally (repo is local); safe defaults needed: timeout, working dir = worktree/branch checkout, command allow-list (e.g., `go test ./...`, `go build ./...`) vs free-form. V1 is likely an allow-list of build/test commands plus model-driven file edits only.
6. **Q-6 Turn timeout and stall semantics.** `stint perf` bounds a single completion; an autonomous turn may run minutes across many tool calls. What per-turn and per-task budgets (tokens, wall time, tool-call count) prevent one turn from eating the session? What distinguishes "turn ended" from "endpoint error" from "model rambled to stop"?
7. **Q-7 How to verify completion claims cheaply.** Minimum evidence per task: focused tests? full `go test ./...`? build? (This repo's tests are fast — see §23; other repos may be slow, and V1 missions should be chosen accordingly.)
8. **Q-8 Deep ↔ interactive session interaction.** A deep session and an interactive session cannot both own `127.0.0.1:8409` (F-015). V1 should refuse to start one while the other is active — confirm as desired behavior.
9. **Q-9 Cost metering in the handoff.** Exposure-based accounting (`hourlyUSD × scheduled hours`) overstates spend when teardown happens early (landing, early completion, stop). V1 handoff should report both scheduled exposure and actual wall time × rate — confirm the reporting choice.
10. **Q-10 Coordinator death during a run.** The watchdog pattern proves spawn+track+respawn; a coordinator respawn must additionally reconcile "which task was active, did the worker process die, is the branch dirty?" Git state should be the tiebreaker: uncommitted changes stay on the branch; task records say what they meant to be.---

## 11. Human Decisions

Decisions only a human can make — each blocks specific parts of the task graph (§19).

- **D-1 — Scope: V1 = single worker, single compute lease, local execution, local coordination.** *(Recommended: yes.)*
  Follows the repo's own sequencing (F-020), the single-instance state model (F-015), and keeps the risk concentrated in the worker protocol rather than also in multi-lease lifecycle code. Cross-worker review, 2×3090, and remote workspaces move to V2+. **Blocks:** the whole graph's shape.
- **D-2 — Mission input format.** *(Recommended: Markdown mission with a documented, simple structure; optional JSON for machine-authored missions.)*
  Consistent with the zero-dependency rule (F-021) and the vision's own V1 statement; the coordinator parses a human-written Markdown file (objective / constraints / success criteria / boundaries / duration / budget) with a small stdlib parser, or the user passes structured flags. **Blocks:** DW-002, DW-010.
- **D-3 — Overnight / host-machine policy.** *(Recommended: V1 documents "machine must stay awake" + a startup check/warning; no OS-level sleep control.)*
  The coordinator is a local process (F-016). Alternatives: refuse non-desktop hosts, or later a remote-coordinator topology (deferred). **Blocks:** DW-004, user docs.
- **D-4 — V1 worker isolation: branch vs worktree.** *(Recommended: dedicated branch with a clean-tree precondition check; worktree only if Q-4 live evidence favors it.)*
  Branch = least new surface; worktree = safer against a user working in the same checkout during the run (which should be disallowed/detected anyway). **Blocks:** DW-008, DW-016.
- **D-5 — Authority model for V1.** *(Recommended: no push, no merge, no destructive ops; worker may commit on its deep branch only; `accept`/`discard` are human commands that operate on the branch.)*
  The vision's "push" and "merge PRs" are explicitly out for V1 (§12); keep the README's "no push, no merge, no deletion" invariant and extend it to deep mode. **Blocks:** DW-008 tool allow-list, DW-014 handoff.
- **D-6 — Model/protocol fallback strategy.** *(Recommended: run Q-1 probes first; if native tool-calling is unreliable on the pinned model/runtimes, V1 uses a structured text protocol (command blocks) uniformly across runtimes.)*
  A uniform text protocol is less pretty but removes a whole class of runtime divergence (NInfer vs llama.cpp). **Blocks:** DW-004 / DW-013 design.
- **D-7 — Session interaction policy (Q-8).** *(Recommended: hard mutex between deep and interactive sessions; `stint deep start` fails clearly if any session is recorded, symmetric with `stint start`.)*
  **Blocks:** DW-011, DW-020.
- **D-8 — Stale config reconciliation (F-006).** *(Recommended: update `stint.config.example.yaml` so the deep profile no longer references Spark collaboration; document that profiles are code-embedded and the example file is illustrative.)*
  Cheap doc/config cleanup that removes an active contradiction with the vision. **Blocks:** nothing; can land anytime.---

## 12. Recommended V1 Boundary

**Scope (in):**

- One **Deep Work session** at a time, owning **one compute session** (existing interactive start/resume/deadline machinery, `deep` profile), executing **locally** on the developer machine, coordinated by a **detached Stint coordinator process** (`stint _deepwork`).
- Commands: `stint deep plan` (live deep planning + cost/budget preview), `stint deep start <mission.md> --hours <n> [--yes]`, `stint deep status` (local + `--refresh`), `stint deep watch` (polling TUI reusing dash conventions), `stint deep resume` (reattach after coordinator/compute loss), `stint deep stop` (graceful: land, commit, handoff, teardown), `stint deep inspect <session>` (records), `stint deep diff` (branch diff vs base), `stint deep accept` / `discard` (branch disposition, local only), `stint deep handoff` (report). This matches the README's pre-committed target surface, with `watch`/`handoff` added and everything local-only for V1.
- **Single worker** (one coordinator-supervised executor loop using the local endpoint; Cline remains fully usable on the same endpoint — the protocol must not exclude Cline, per the vision's "Stint must not create a parallel harness that replaces it").
- **Phases:** grounding (repo understanding + task decomposition into a small durable task list) → execution (task loop: model turn → tool calls → verification → checkpoint commit; dynamic task addition/revision with history preserved) → landing (stop new work when `remaining < landing budget`; finish + verify current task; commit; write handoff) → teardown (existing watchdog/destroy machinery).
- **Git integration (first-ever in Stint):** branch `stint/deep-<session-id>` from the user's current branch (D-4), clean-tree precondition, worker commits only on that branch (D-5), `accept`/`discard` = local merge/delete performed by the human.
- **State:** `~/.local/state/stint/deep/<session-id>/{state.json, mission.md (copy), tasks.json, findings.log, handoff.md, coordinator.log}` (atomic writes per F-007) + in-branch artifacts (commits) with the handoff also copied into the repo (`DEEP_WORK_HANDOFF.md`) for visibility.
- **Recovery:** compute loss → existing replacement/resume; coordinator death → respawn + reconcile from `state.json` + git state (Q-10); deadline → coordinator enters landing, watchdog remains the final authority (F-002).
- **Verification:** per-task — the *coordinator* re-runs the worker-claimed tests/builds (never the model's word) and records output; session-level — full test suite during landing; handoff states verified / unverified / partial honestly.
- **Safety invariants (non-negotiable, from repo + vision):** no push/merge/destructive git by the worker; path confinement to the working branch's repo; command allow-list (D-5); one deep session at a time (D-7); deadline + cost ceiling hard-enforced by the existing watchdog; all essential state local + in git (never only on the rented box); Cline continues to work against the endpoint.**Out of scope (deferred, with destination):**

| Deferred | Why | Destination |
| --- | --- | --- |
| 2+ compute leases (2×3090), deep ports 8301/8302 | F-015/F-020; single-lease unproven | V2 "Deep pair" (roadmap §3) |
| Dual-lane 4090 2-worker mode (F-012) | Cheap, but still multi-worker protocol + shared-KV behavior unmeasured | V2 candidate |
| Remote execution (workspaces on rented box) | Breaks the Cline invariant; needs the vision's lease abstraction | V3+ |
| Cross-review, multi-agent debate | Needs 2+ reliable workers first | V2+ |
| Spark collaboration for deep | Boundary doc forbids Spark owning execution; `WorkPacket` unused | Revisit when the collaboration contract matures |
| `WorkerLease` provider abstraction | Vast-only today; the endpoint suffices (F-013) | When a second provider lands |
| Push / merge / PR authority | Safety | Explicit future phase with human design |
| Metered provider cost | Vast API limitation (exposure accounting today) | When the API supports it |
| Historical offer/infrastructure ranking | Identity stability, staleness, sample sufficiency, and ranking value are unproven; history is not inventory | Observation-only phase, then offline replay gate (§28) |
| Host sleep control / remote coordinator | Product policy + large architecture change | Post-V1 (Q-3) |
| YAML mission / mission DSL | Zero-dep rule (F-021) | Never, unless the deps policy changes |
| `internal/` reorg of `cmd/stint` lifecycle logic | Risk without V1 benefit (F-019) | Separate refactor |

**Explicit non-goal for V1:** matching Cline's agentic capability. V1 proves *supervised continuity*: bounded tasks, honest evidence, durable checkpoints, clean handoff. A run that completes 40% of a mission and hands off a truthful state is a **success**, per the vision's own criteria.

---

## 13. Explicitly Deferred Work

The deferred table above is the V1 boundary; this section records the *next* concrete workstreams and their entry conditions, so nothing silently evaporates:

1. **Deep pair (V2):** enable `stint deep start` to lease 2 instances (or 1×4090 dual-lane per F-012), track N leases/tunnels in deep state, run 2 workers with independent branches + cross-review. Entry conditions: V1 gate G-1 green (single worker completes a real mission overnight); `session.json` extended or deep state owning N compute sub-sessions; deep ports 8301/8302 wired.
2. **Remote workspaces (V3+):** workers execute on rented compute in isolated workspaces; requires the vision's `WorkerLease` abstraction (F-013), repo sync strategy, and a re-negotiation of the Cline invariant (which is currently a hard product rule). Entry: V2 stable + explicit human decision to relax the local-repo invariant.
3. **Authority extension (push/merge/PR):** only after deep branch disposition (`accept`/`discard`) is proven boring across many sessions; needs its own safety design (protect branches, CI requirements).
4. **Metered billing:** watch Vast API for spend endpoints; swap exposure accounting for metered in cost reports.
5. **Spark collaboration revisited:** if the collaboration contract (currently a 4-field stub) matures into a real inter-agent work handoff, deep sessions could *publish* findings to Spark — execution ownership stays with Stint per the boundary doc.
6. **Coordinator on rented compute:** removes the F-016 host-power constraint; large (coordinator needs the repo, or syncs it). Track as long-term.
7. **Model rotation:** when a stronger pinned model lands, V1's Q-1 probe suite re-qualifies the protocol automatically (the probe suite is a first-class artifact — DW-004).---

## 14. Proposed Architecture

Proposed V1 architecture (PROPOSAL; reuses marked → R# from §5):

```text
developer machine (stays on all the time — F-016)
│
├── user / Cline (human or interactive harness)
│      │  stirs, inspects, accepts, discards, extends time
│      ▼
├── Stint CLI (stint deep …)                 [cmd/stint/deep_*.go]
│      │  plan · start · status · watch · resume · stop · inspect · diff · accept · discard · handoff
│      │  (start: reuses resumable_start.go pipeline + candidate pool → R5, R6, R7)
│      ▼
├── detached coordinator process:  stint _deepwork <session-id>    [R11 pattern, like _watchdog]
│      │  owns: task graph (durable), phase scheduler, deadline awareness (R1),
│      │       verification re-runs, checkpoint commits, landing, handoff writer
│      │  supervises:
│      │    ├── worker turn loop (coordinator IS the executor in V1 — see note below)
│      │    └── compute session (via existing watchdog: stint _watchdog → R2)
│      │
│      ├── model client: OpenAI-compatible /v1 → http://127.0.0.1:8409/v1   [R13, R14]
│      │      (tool-call or structured-text protocol per D-6; probe suite validates both runtimes)
│      │
│      ├── local executor: git (branch/stint/deep-<id>, commit, diff — Q-4) +
│      │      allow-listed shell (build/test — Q-5) + file edits confined to repo
│      │
│      └── state: ~/.local/state/stint/deep/<session-id>/  [R8]
│             state.json (phase, active task, checkpoints, compute lease ref)
│             mission.md (copy)  tasks.json  findings.log  handoff.md  coordinator.log
│
├── existing compute-session machinery (unchanged)
│      session.json · deadline watchdog · candidate pool · tunnel 8409 · perf
│
└── repository
     base branch (user's) ← new branch stint/deep-<session-id>
     worker commits on the deep branch only; human merges via `stint deep accept`

rented Vast instance (unchanged role)
     pinned model server only (NInfer 4090 / llama.cpp 3090) + tunnel endpoint
```

**Component inventory:**

| Component | Status | Notes |
| --- | --- | --- |
| Compute acquisition / replacement / resume | Reuse (R5, R6, R7) | `runStartResumable`, candidate pool, `RECOVERABLE`/`resume` — called for the `deep` profile once profile gates open |
| Deadline + watchdog + budget cap | Reuse (R1, R2) | Coordinator *reads* the deadline; the watchdog still owns destruction. Landing is a coordinator behavior triggered by remaining-time thresholds |
| Tunnel / endpoint / runtime bootstrap | Reuse (R13) | No changes; the deep ports 8301/8302 stay reserved for V2 |
| Deep state directory + atomic records | New (pattern R8) | `~/.local/state/stint/deep/<id>/`, same write/invalidation conventions as `performance.json` |
| Coordinator process | New (pattern R11) | detached, PID in state, respawn on `deep resume`; dependency-injected seams (R12) for offline tests |
| Task model (durable) | New | small: id, title, goal, scope hints, status (queued/active/blocked/done/dropped), evidence refs, provenance (planned/discovered), parent link |
| Worker executor (model client + tool protocol + local git/shell tools) | New — the risk center (F-004) | V1: the coordinator *runs the loop itself* (one process, one worker). A separate worker process is a natural V2 extraction (and the seam for 2 workers), but V1 keeps one process to minimize new supervision surface |
| Verification re-runner | New | coordinator executes the claimed test/build commands itself (allow-listed) and records output; a task is "done" only when coordinator-verified or explicitly parked as unverified |
| Landing + handoff writer | New | deterministic: stop accepting tasks at threshold → finish active → full-suite run → commit → `handoff.md` + repo `DEEP_WORK_HANDOFF.md` |
| Deep dash/status views | Extend (R10) | new "deep" domains in the snapshot pattern; `stint deep watch` reuses dash rendering conventions |### 14.1 Design notes

- **Why the coordinator runs the loop in V1:** one process means no inter-process protocol, no extra supervised process, and the "worker" is just a phase of the coordinator's state machine. It also sidesteps Q-8 entirely (deep session owns the endpoint for its work; Cline use during a run is *allowed but uncoordinated* — the human can peek with Cline on the same dual-lane 4090, exactly as today; concurrency semantics are shared-KV, same as two Cline sessions). If D-1 is ever revisited, the loop extracts to a worker process behind an in-repo contract (the `collaboration` package is the natural home).
- **Why local execution (vision divergence, §9 row 8):** keeps the product's core invariant ("local repo/tools never move to Vast") intact, makes the tool executor trivially sandboxable (path confinement + allow-list), and means a compute failure never endangers work product. Remote workspaces are the principled V3+ item.
- **What "worker" means in V1:** one executor loop = one worker (satisfies the vision's "one coordinator, one worker" and the repo's 4090-first sequencing). Dual-lane 4090 still lets a human run Cline concurrently, which is how today's 4090 users already use the machine.

---

## 15. Deep Work Session and Time Model

| Concept | V1 model | Grounding |
| --- | --- | --- |
| Session duration | user-supplied `--hours <float>` (any positive value; 8 is an *example*, from the deep profile's `DefaultHours: 8` — not a constant); also accepted via mission file | `cmd/stint/resumable_start.go:44`; `internal/core/plan.go:114,131`; vision timeline is illustrative |
| Work clock start | **READY** (endpoint serving), not rental — the coordinator records `readyAt`; all scheduling is relative to `deadline − readyAt` minus a startup buffer | F-014; paid clock already starts at rental (`resumable_start.go:325-331`) |
| Deadline authority | unchanged: `Deadline` in `session.json`, enforced by the existing watchdog; deep extend/shorten goes through the existing `extend`/`shorten` machinery (budget-capped, locked) | R1, R2, F-002 |
| Landing budget | `landingWindow = max(15 min, 10% of (deadline − readyAt))` — while `remaining <= landingWindow`: no new tasks accepted; active task must complete-or-park; verification + handoff run | PROPOSAL (calibrates F-014's startup tax; tune after first live runs) |
| Task timeboxes | grounding tasks: ≤ 10% of work window; execution tasks: adaptive (coordinator estimates from mission size, re-plans once mid-run); per-turn budgets per Q-6 (e.g., ≤ 5 min wall / token cap per turn, ≤ 3 retried turns per task) | PROPOSAL; constants are first drafts, calibrated in the fixture loop (§23) |
| Early completion | if all tasks done+verified before landing window: enter landing immediately, teardown at handoff (saves money; exposure accounting noted per Q-9) | PROPOSAL |
| Deadline passed while coordinator alive | coordinator cannot extend past ceiling; watchdog destroys on the wall deadline; if handoff not written, coordinator writes a *degraded* handoff (partial) as its final action before destroy — ordering: handoff is written *before* teardown, never after | PROPOSAL; mirrors `resume.go:76-90`'s "destroy if deadline passed" semantics for the deep layer |
| Session vs compute session | 1:1 in V1: `deepState.computeSessionRef` = the `session.json` record; compute death while deep active → coordinator requests replacement (pool pattern) or, if budget exhausted, parks remaining tasks and hands off | F-001, F-003 |

**Phase machine:**

```text
PLANNED → ACQUIRING (existing start pipeline, deep profile) → READY → GROUNDING → EXECUTING ⇄ (task loop)
        → LANDING (at remaining ≤ landingWindow, or all-done, or stop) → HANDOFF_WRITTEN → TEARDOWN
any phase after ACQUIRING: COORDINATOR_LOST (respawn+reconcile on resume) / COMPUTE_LOST (replace or park)
HARD_STOP (deadline / stop command / budget exhaustion) from any phase → degraded handoff → TEARDOWN
```---

## 16. Task and Planning Model

- **Mission (input, D-2):** human-written Markdown with a documented skeleton (objective; success criteria; constraints; forbidden actions; optional explicit task hints; duration; budget). The coordinator parses the skeleton into a `Mission` record; free-text beyond the skeleton is preserved and handed to the model verbatim. A machine-authorable JSON equivalent exists for the coordinator's own re-plans (internal format, stdlib `encoding/json`).
- **Task record (durable, in `tasks.json`):** `id` (stable, `T-001…`), `title`, `goal`, `scope` (files/areas), `verification` (the commands the coordinator will re-run), `status` ∈ {queued, active, blocked(needs-human), done(verified), done(unverified), dropped}, `attempts`, `estWindow`, `evidenceRefs`, `provenance` (planned-by-grounding | discovered-at \<task-id/time\>), `parent` (optional link to the task during which it was discovered), `notes`.
- **Dynamic planning rules:** during grounding the coordinator produces the initial task list (small: 3–10 tasks for V1 missions). During execution, a task may *spawn* follow-up tasks (provenance = discovered) when it uncovers unknowns; spawning is capped (e.g., ≤ 2 new tasks per completed task, total ≤ 3× initial) to bound scope creep; tasks can be *re-estimated* once; *dropped* tasks keep their record and reason (history preserved — the vision's "the task graph must be a living record, not a static checklist").
- **Scheduling policy:** single worker → simple priority queue: (1) unblock critical path, (2) earliest-est-first among queued, with deadline-aware demotion: as `remaining` shrinks, only tasks whose `estWindow` fits `remaining − landingWindow` are scheduled (time-aware selection, F-014). No preemption of an active task except at turn boundaries (a turn is the atomic unit; never mid-turn).
- **Blocked / needs-human:** a task may mark itself blocked with a structured question; the coordinator parks it, continues with others, and surfaces the question in status/watch/handoff; the human answers via `stint deep note <task> "answer"` (a thin command that appends to the task record) — the only V1 human-in-the-loop mechanism besides final `accept`/`discard`.
- **Findings:** append-only `findings.log` entries (task-scoped): decisions, surprises, evidence pointers — the in-run analog of the repo's human findings logs (F-010), machine-writable.

---

## 17. Continuation Model (How a Long Session Keeps Cohering)

Long autonomous runs fail by drifting: losing the objective, re-reading everything, or repeating failed approaches. V1's coherence mechanisms, all cheap because state is durable and local:

1. **Checkpoint = commit + record.** Every verified task ends with a coordinator-authored commit on the deep branch (message: task id, goal, evidence pointer) *and* an update to `tasks.json`/`findings.log`. The git branch is thus a readable narrative of the run — `git log` on `stint/deep-<id>` doubles as an audit trail.
2. **Turn prompt assembly is deterministic.** Each model turn receives (fixed shape, size-bounded): mission skeleton (never free-text-only), active task record, last-N findings, current git state summary (`status`/`log --oneline`/affected files from Q-2 budget), and the protocol instructions. No free accumulation of conversation history across tasks — context is *reconstructed* from durable records at every turn boundary. This makes context loss non-catastrophic (the model re-derives state from records) and is the same principle as NInfer's "new process + same KV" restart story, applied to *records* instead of KV.
3. **Within-task continuity:** inside one task (multiple turns), the coordinator appends the tool-call transcript summary (not raw tokens) to the task's in-memory context with a size cap; at each turn boundary the cap forces summarization. If a turn is lost (endpoint error, model restart), replay uses the task record + last checkpoint commit, not the transcript.
4. **Anti-loop detection:** repeated-failure signature = same task + same verification command failing with identical output hash for N consecutive attempts (N=2) → the task is auto-escalated: re-estimate, change approach (the model is told the previous approach failed, with the error), or park as blocked with the failure evidence. This is the work-level analog of the paid-attempt budget (F: `network_candidate_retry.go`), and it is the main defense against a 27B model chewing on one problem for hours.
5. **Re-grounding cadence:** every K tasks (K≈3) or at LANDING, the coordinator asks the model a short "state check" turn: summarize progress vs mission success criteria; if the model reports the plan is stale, one re-plan is permitted (new task list, history preserved).
6. **Resume after coordinator loss:** on respawn (Q-10), the coordinator loads `state.json` → active task + phase; loads git state; if the branch has uncommitted changes, the active task's turn is considered lost → re-derive from checkpoint (uncommitted work is kept on the branch but re-verified, never auto-committed). The invariant: *a resumed run never needs memory it didn't persist*.---

## 18. Recovery Model

| Failure mode | Detection | V1 response | Basis |
| --- | --- | --- | --- |
| Compute instance destroyed (provider-side) mid-run | watchdog / status probe: tunnel down + provider shows instance gone | coordinator: preserve deep state → search replacement (pool pattern) → rent + qualify (measured transfer) → rebuild runtime/model → tunnel → `readyAt'` recorded (work clock continues against the *same* deadline) → resume active task from checkpoint. Budget: replacement attempts count against remaining paid window only; if `remaining < minViable` (e.g., landingWindow) skip replacement, park + hand off | F-003, R5–R7; existing `resume` proves reattachment; new: deep-state survival is by construction (local + git) |
| Model process died, instance alive | endpoint probe 4xx/timeout while instance alive | existing remote model restart path (`resume.go:145-209`); coordinator re-issues active turn from checkpoint | R7 |
| Endpoint transiently unreachable | tunnel probe failures (n consecutive) | wait-and-retry with backoff within a per-turn budget; then treat as model-process case | `runSSH`/retry helpers |
| Coordinator process died | PID check on `deep resume`; or human notices via status | respawn `stint _deepwork <id>`; reconcile per §17.6 (state.json + git tiebreaker) | R11, Q-10 |
| Worker turn hangs (model streaming stalls) | per-turn wall/token budget (Q-6) | abort turn, retry from checkpoint (≤ 3 times per task), then anti-loop escalation (§17.4) | PROPOSAL |
| Worker claims success, coordinator verification fails | coordinator re-run of verification commands | task not done: feed failure to model, new attempt (same budgets) | core honesty mechanism |
| Model emits malformed actions / path escape / disallowed command | executor validates every action (schema + path confinement + allow-list) | reject action, return structured error to model (one corrective retry), log incident | D-5 safety invariants |
| Deadline reached mid-task | remaining-time check at each turn boundary + watchdog as final authority | if `remaining ≥ minLand`: finish-or-park active task, run landing (verify + handoff), teardown; if not: immediate degraded handoff + teardown | F-002; ordering: handoff before destroy |
| User runs `stint deep stop` | explicit command (locks lifecycle) | same as deadline path but without destroy-by-timer: graceful landing, teardown via existing `down` machinery | R4 |
| User's machine slept | wake: deadline may already be passed or near | if passed: watchdog already destroyed compute; coordinator writes degraded handoff from state; if near: landing immediately. Documented per D-3 | F-016 |
| Dirty user tree at start | preflight git check | refuse to start (clean-tree precondition) unless user confirms `--force` (work proceeds on the deep branch anyway; user's uncommitted changes stay untouched in the working tree) | D-4 |

**Invariants across all recovery paths:** (1) essential state never exists only on rented compute; (2) money is never lost to a zombie instance (watchdog retry-on-destroy, instance-scoped); (3) the deep branch never receives unverified auto-commits as "done"; (4) recovery decisions are recorded in `findings.log` and visible in `status`/`handoff`.

---

## 19. Task Graph

Task IDs are stable for cross-document/commit reference. Dependencies: "needs" = must land first (code or evidence). Gates G-x are defined in §20.

### Phase 0 — De-risking (no paid compute required)

- **DW-001 — Q-1 live protocol probe suite (tools vs structured text, both runtimes).**
  *Purpose:* answer whether Qwen3.8-27B emits reliable machine-parseable actions under NInfer and llama.cpp via the pinned endpoint; produces the probe harness (request builder + response classifier) that becomes the executor's test fixture.
  *Files:* new `cmd/stint/deep_probes.go` (or `internal/deep/protocol_test` scaffold), `internal/runtime/llama/config.go` (preset reference), test fixtures.
  *Tests:* unit tests for parser/classifier on recorded responses; live probe run (optional, `STINT_VAST_INTEGRATION`-style env gate) logged as finding.
  *Gate evidence:* G-1a. *Depends:* D-6 (strategy can be provisional: build both, prefer whichever passes). *Risk:* low (small code).
- **DW-002 — Mission format + parser (D-2).**
  *Purpose:* documented Markdown mission skeleton; stdlib-only parser into `Mission` struct; validation (duration/budget sanity) with deterministic error messages (F-010 convention).
  *Files:* new `internal/deep/mission.go` + `mission_test.go`; doc `docs/DEEP_WORK_MISSION.md`.
  *Tests:* table-driven parse/validate cases. *Depends:* D-2. *Gate evidence:* G-2.
- **DW-003 — Deep state layout + records (state/tasks/findings writers).**
  *Purpose:* `~/.local/state/stint/deep/<id>/` conventions; atomic write/invalidation helpers mirroring `session`/`performance_store`; `deepState` struct (phase, compute ref, coordinator PID, readyAt, landingWindow).
  *Files:* new `cmd/stint/deep_state.go` + tests (or `internal/deep/state.go`).
  *Tests:* round-trip, concurrency (two writers → flock), invalidation on mismatch. *Depends:* none. *Gate evidence:* G-2.- **DW-004 — Coordinator skeleton: phase machine + supervision + offline fixture loop.**
  *Purpose:* the `stint _deepwork` detached process: spawn/PID tracking/respawn (R11 pattern), phase machine (§15), deadline reader (R1), landing-window computation, dependency-injected seams (`deepCoordinatorDeps`: clock, state I/O, model client, executor, compute controller) so the *entire* loop runs against fakes in unit tests — the repo's proven convention (R12, F-009).
  *Files:* new `cmd/stint/deep_coordinator.go`, `cmd/stint/deep_deps.go` + `deep_coordinator_test.go`.
  *Tests:* table-driven phase transitions (incl. all recovery modes of §18 as fixture scenarios); race gate. *Depends:* DW-003. *Gate evidence:* G-2, G-3. *Risk:* medium (largest new file set, but each seam is small).
- **DW-005 — Worker executor: model client + tool protocol + local tools (git, allow-listed shell, file edit).**
  *Purpose:* the risk center (F-004). Turn loop: assemble prompt (§17.2) → call endpoint (reusing `perf.go`'s streaming client logic, extracted to a shared helper) → parse actions (per D-6 protocol) → validate (schema, path confinement, allow-list) → execute locally → append result → repeat until turn-end signal or per-turn budget. Tool set (V1): `read_file`, `write_file` (repo-confined), `run` (allow-listed commands, e.g. `go build ./...`, `go test ./...` + timeout), `git` read-only (`status`, `log`, `diff`, `show`) — commit is coordinator-only (D-5).
  *Files:* new `cmd/stint/deep_executor.go` + tools files; extracted `chatClient` from `perf.go` (shared, behavior-identical for `perf`).
  *Tests:* fake-endpoint turn scenarios (success, malformed action, path escape, allow-list violation, stall timeout, endpoint error); executor unit tests with a `fakeRunner`. *Depends:* DW-001 (protocol), DW-004 (loop home). *Gate evidence:* G-3. *Risk:* **high** — this is where model reliability actually gets tested; D-6 mitigates by allowing the text protocol.
- **DW-006 — Git integration: branch lifecycle + checkpoint commits + diff/accept/discard commands.**
  *Purpose:* first git surface in Stint (F-005): preflight clean-tree check; create `stint/deep-<session-id>` from current branch; checkpoint commit authorship (coordinator identity, structured messages); `stint deep diff` (diff vs base branch), `accept` (local merge of deep branch into base, human-initiated, refuses on conflicts), `discard` (delete branch, refuses if commits not saved elsewhere).
  *Files:* new `cmd/stint/deep_git.go` + tests using `os/exec` against a temp repo (real `git` in a fixture repo — no network, no new deps).
  *Tests:* temp-repo scenarios: dirty tree, conflict on accept, discard-with-commits, branch already exists. *Depends:* D-4, DW-003. *Gate evidence:* G-3. *Risk:* medium (well-understood git; failure modes are the point of tests).
- **DW-007 — Verification re-runner + evidence records.**
  *Purpose:* coordinator-side execution of task `verification` commands (allow-listed, timed, output-captured to evidence refs); task status transition rules (done requires coordinator-passed verification or explicit unverified marking with reason); evidence feeds handoff.
  *Files:* extends `deep_executor.go` tool `run` into a reusable `verifier`; `deep_evidence.go`.
  *Tests:* pass/fail/mixed-verification fixtures; timeout handling. *Depends:* DW-005. *Gate evidence:* G-4.
- **DW-008 — Deep planning surface: `stint deep plan` (live) + budget preview.**
  *Purpose:* enable live deep planning (F-017): open the `deep` profile for `plan` without `--fixture`, print 1-worker V1 plan (session cost vs `MaxCostUSD` ceiling, selected offer + alternatives, duration filter effects per F-014), still ending "NO COMPUTE HAS BEEN RENTED."
  *Files:* `cmd/stint/main.go` (gate), `cmd/stint/plan.go` (profile gate + deep plan rendering), help text, `docs/CLI.md`.
  *Tests:* fixture-based plan output tests; gate behavior (live deep allowed, start still gated by DW-010). *Depends:* none (pure reuse of `core` planning). *Gate evidence:* G-2. *Risk:* low.
- **DW-009 — `stint deep start` wiring: profile gate + preflight + acquire-via-existing-pipeline.**
  *Purpose:* accept the `deep` profile in the *existing* `runStartResumable` pipeline (remove the interactive-only checks for the deep path), preflight checks (mission parses, git preflight per D-4, no active session per D-7, machine-awake warning per D-3), then hand off to the standard acquisition → READY flow; on READY, spawn `_deepwork` with the deep state initialized.
  *Files:* `cmd/stint/deep_start.go`, edits to `resumable_start.go` gates, `main.go`, help/docs.
  *Tests:* gate matrix (profile × existing-session × dirty-tree × missing-mission) in unit tests with injected deps; live smoke via env gate. *Depends:* DW-002, DW-003, DW-004, DW-006, DW-008; D-1, D-3, D-4, D-7. *Gate evidence:* G-4. *Risk:* medium — touches the most battle-tested code; keep the interactive path byte-identical (regression tests from existing suite must stay green).- **DW-010 — Grounding + initial task decomposition.**
  *Purpose:* at READY, the coordinator runs grounding: repo survey (language, layout, test commands — via executor tools), mission parsing into 3–10 initial tasks with `estWindow` + `verification` per task; writes `tasks.json`; re-plan budget: once.
  *Files:* `cmd/stint/deep_grounding.go` + tests with fake executor returning scripted survey results.
  *Tests:* decomposition structure assertions against fixture missions (not LLM-quality assertions); task caps enforced. *Depends:* DW-002, DW-005. *Gate evidence:* G-4. *Risk:* medium (model-dependent output quality; mitigated by deterministic caps and re-plan).
- **DW-011 — Execution loop + anti-loop escalation + dynamic task rules.**
  *Purpose:* the §16/§17 machinery wired into the coordinator: scheduling queue, turn budgets, anti-loop detection (§17.4), task spawning caps, blocked-task parking + `stint deep note`.
  *Files:* `cmd/stint/deep_loop.go` + tests (fixture scenarios: stall, repeated failure, spawn cap hit, blocked task).
  *Tests:* each escalation path as a table case with fake clock/executor. *Depends:* DW-004, DW-005, DW-007. *Gate evidence:* G-5. *Risk:* medium-high (policy constants are untested until live).
- **DW-012 — Landing + handoff writer.**
  *Purpose:* landing trigger (remaining ≤ landingWindow / all-done / stop), finish-or-park active task, full-suite verification run, `handoff.md` (mission summary, task table with status+evidence, findings digest, cost actual vs scheduled, next-steps for human/Cline) + repo `DEEP_WORK_HANDOFF.md` + degraded-handoff variant; then existing teardown.
  *Files:* `cmd/stint/deep_landing.go` + `deep_handoff.go` + tests (full, partial, degraded fixtures).
  *Tests:* handoff content assertions from fixture state; ordering property (handoff written before destroy, even on timeout). *Depends:* DW-004, DW-006, DW-007. *Gate evidence:* G-4, G-6.
- **DW-013 — Recovery integration: compute replacement for deep sessions + coordinator respawn.**
  *Purpose:* §18 table made real: detect (probe) → replace (pool pattern, new search) → rebuild → resume from checkpoint; coordinator respawn + reconcile (Q-10); `stint deep resume` command.
  *Files:* `cmd/stint/deep_recover.go` + tests with fake compute controller (instance-vanish mid-task scenario end-to-end in fixtures).
  *Tests:* the full recovery matrix of §18 as fixture scenarios; race gate. *Depends:* DW-003, DW-004, DW-011. *Gate evidence:* G-5. *Risk:* medium (reuses proven machinery; new part is the reconcile procedure).
- **DW-014 — Observability: `stint deep status` / `watch` + dash integration + `--json`.**
  *Purpose:* deep domains in the snapshot pattern (phase, active task, queue, blocked, compute, cost/remaining, handoff status); `watch` = polling TUI with dash conventions; `status --json` contract for automation; dash cockpit gains a deep view when a deep session is active.
  *Files:* `cmd/stint/deep_status.go`, dash view additions, `docs/TELEMETRY.md` update.
  *Tests:* snapshot rendering from fixture state; JSON contract test. *Depends:* DW-004. *Gate evidence:* G-6.
- **DW-015 — Safety rails hardening: path confinement audit, allow-list policy file, incident log.**
  *Purpose:* final safety pass before paid runs: every executor action validated (confinement to repo root; allow-list with `--deep-allow` user extension); incident log (rejected actions, protocol errors) in deep state; `stint deep inspect` renders it.
  *Files:* policy in `internal/deep/policy.go` + tests; `deep_inspect.go`.
  *Tests:* escape attempts (symlinks, `..`, absolute paths outside repo); allow-list edge cases. *Depends:* DW-005. *Gate evidence:* G-3.- **DW-016 — Documentation package.**
  *Purpose:* `docs/DEEP_WORK.md` (concept, phases, safety model, recovery, commands), `docs/DEEP_WORK_MISSION.md` (mission skeleton), CLI.md/README updates (deep section, target-surface completion), `stint.config.example.yaml` reconciliation (D-8), boundary-doc note (Spark stays out of execution).
  *Files:* docs + example config. *Depends:* D-8; content depends on the other DW tasks landing. *Gate evidence:* G-6.
- **DW-017 — Fixture-mission battery + offline E2E test harness.**
  *Purpose:* the validation backbone (F-009): a fake-model-endpoint server in tests (scripted turn behaviors), a fixture repo (small Go module with fast tests), full coordinator E2E: start → ground → execute (3 tasks incl. one discovered, one anti-loop) → landing → handoff, all offline and race-clean. Plus a recorded-response corpus from DW-001 for regression.
  *Files:* `internal/deep/testharness/` (fake endpoint, fixture repo builder), `deep_e2e_test.go`.
  *Tests:* IS the test suite. *Depends:* DW-004, DW-005, DW-011, DW-012. *Gate evidence:* G-3, G-5. *Risk:* medium.
- **DW-018 — Live calibration runs (human-supervised, paid).**
  *Purpose:* the only step that spends money: 2–3 real missions of 1–2 h first, then one 8 h overnight; measure: protocol reliability, turn latency, context usage (via `stint perf` tools), anti-loop firing rate, handoff quality; record as findings (F-010 convention) and calibrate §15 constants (landing window, turn budgets, re-grounding K).
  *Files:* run notes appended to a new living `docs/DEEP_WORK_FINDINGS.md`; constant adjustments in code.
  *Tests:* none (live); evidence = findings log + `status --json` captures. *Depends:* gates G-1…G-5 green. *Gate evidence:* G-6. *Risk:* the honest risk surface — where the vision's claims get checked against the pinned 27B model.
- **DW-019 — CI integration.**
  *Purpose:* new packages/tests into existing CI (unit, race, vet, build); no new jobs expected; ensure no live calls in CI (env gates like the existing Vast integration test).
  *Files:* `.github/workflows/ci.yml` (only if a job change is needed — likely none), test hygiene. *Depends:* all code tasks. *Gate evidence:* G-2 (continuous).
- **DW-020 — V1 release cut: `stint deep` command group complete, `--yes` policy, `doctor`/`status` top-level deep awareness, tag.**
  *Purpose:* command-surface polish (help, `doctor` checks for deep readiness: ssh key, machine-awake warning, endpoint reachability during a deep session), `--yes` for unattended start, final doc pass, version tag per project release conventions.
  *Files:* help/doctor wiring, release notes. *Depends:* DW-014, DW-016, DW-018 findings incorporated. *Gate evidence:* G-6.

**Dependency graph (condensed):**

```text
D-1..D-8 (human) ─┬─────────────────────────────────────────────┐
                   │                                            │
DW-001 (probes) ───┼─► DW-005 (executor) ─► DW-007 (verify) ───┤
DW-002 (mission) ──┼─► DW-010 (grounding) ─┬─► DW-011 (loop) ──┤
DW-003 (state) ────┼─► DW-004 (coordinator)┤        │          │
                   │            │          │        │          │
DW-006 (git) ──────┼────────────┴──► DW-012 (landing/handoff) ◄┤
DW-008 (plan)      │                          │                │
                   │            DW-013 (recovery) ◄────────────┤
DW-009 (start wiring) ◄── DW-002,003,004,006,008 ─────────────┤
DW-014 (observability) ◄── DW-004                                │
DW-015 (safety) ◄── DW-005 │                                     │
DW-017 (E2E harness) ◄── DW-004,005,011,012  ───────────────────┤
DW-019 (CI) ◄── all code tasks                                   │
DW-016 (docs) ◄── most tasks; D-8                                │
DW-018 (live calibration) ◄── G-1..G-5 green                     │
DW-020 (release) ◄── DW-014,016,018                              ▼
                                              [V1 complete]
```---

## 20. Validation Gates

Gates are the repo's exit-gate convention (F-010) applied to Deep Work. Evidence for paid-compute gates is marked *live* and stays `PENDING` until actually run (repo convention for marketplace/live results).

- **G-0 — Investigation accepted.** This document reviewed; D-1…D-8 resolved. *Evidence: human sign-off.* *(PENDING)*
- **G-1 — Protocol proven (live).** DW-001 probe suite run against a real rented endpoint (both runtimes if feasible): ≥ 20/20 structured actions parsed and executed correctly by the executor harness for the V1 tool set; a written protocol choice (native `tools` vs text protocol) recorded as a finding. *Evidence: probe run log + finding entry.* *(PENDING — needs ~1 h of paid compute, cheapest qualifying offer)*
- **G-2 — Durable backbone green (offline).** `go vet`, unit + race tests green for: state layout/atomicity (DW-003), mission parser (DW-002), coordinator phase machine incl. all §18 recovery transitions on fakes (DW-004), live deep `plan` output (DW-008), CI green (DW-019). *Evidence: `go test -race ./...` output, CI check run.*
- **G-3 — Executor safe and sound (offline).** Fake-endpoint scenarios all green: success, malformed action, path escape, allow-list violation, stall, endpoint error; git integration scenarios (dirty tree, accept conflict, discard guards) green on temp repos; E2E harness (DW-017) runs start→ground→execute→landing→handoff offline with correct handoff contents. *Evidence: test output incl. race gate.*
- **G-4 — Deep start wired (offline + short live).** Gate matrix (DW-009) green; then **one 1 h live run** (human at keyboard): real mission, 2–4 tasks, clean landing + handoff written before teardown, no budget breach, Cline still usable on the endpoint during the run. *Evidence: live findings entry + `status --json` + handoff file.* *(PENDING)*
- **G-5 — Recovery proven (offline + one fault-injected live).** Fixture recovery matrix green (DW-013); **one live fault-injected run**: destroy the rented instance mid-run (user runs `vast` destroy manually) → replacement happens, task resumes from checkpoint, handoff still written. *Evidence: live findings entry.* *(PENDING)*
- **G-6 — V1 complete.** G-1…G-5 all recorded as passed; **one unsupervised overnight (8 h) run** completes: machine stayed awake (D-3 warning heeded), landing at deadline window, truthful handoff (verified/partial explicitly stated), `accept` or `discard` exercised by the human; docs package (DW-016) shipped; calibrated constants (from DW-018) landed; release tag cut (DW-020). *Evidence: findings log, handoff, release notes.* *(PENDING)*

Gate sequencing: G-0 → G-1 ∥ G-2 (parallel) → G-3 → G-4 → G-5 → G-6. No gate may be waived by scope cuts; a failing gate returns work to its tasks with a logged finding (repo convention: findings drive re-planning).

---

## 21. Execution Order and Work Breakdown

Recommended order (one branch per workstream, matching repo branching conventions; `feat/deep-work-*`):

| Order | Workstream (branch) | Tasks | Gate | Est. effort |
| --- | --- | --- | --- | --- |
| 1 | `feat/deep-work-protocol` | DW-001, DW-002, DW-003, DW-008 | G-1, G-2 (partial) | S–M |
| 2 | `feat/deep-work-coordinator` | DW-004, DW-017 (harness first), DW-019 | G-2 | M |
| 3 | `feat/deep-work-executor` | DW-005, DW-007, DW-015 | G-3 | **L** (risk center) |
| 4 | `feat/deep-work-git` | DW-006 | G-3 | M |
| 5 | `feat/deep-work-loop` | DW-010, DW-011, DW-012, DW-013, DW-009, DW-014 | G-3 → G-4 → G-5 | L |
| 6 | `feat/deep-work-live` | DW-018 (calibration runs, findings, constant tuning) | G-4 → G-5 → G-6 | M (paid) |
| 7 | `feat/deep-work-release` | DW-016, DW-020 (+ D-8 config fix) | G-6 | S–M |

S = ≤ 1 day-equivalent, M = ~3, L = ~1+ week of focused work (calibration estimates; the project's own commit cadence suggests each row is a sequence of small test-paired commits, not a single drop).

Pragmatics: workstreams 1–2 overlap heavily on fixtures and should share the test-harness early (DW-017 scaffold lands in workstream 2's first commits); the protocol probe (DW-001) is deliberately first because its *answer* (D-6 final form) reshapes DW-005 — doing it before the executor avoids rework, and it is the cheapest paid step in the entire program.---

## 22. Dynamic Discovery and Adaptation

Where this plan is expected to change its own shape during execution, and the mechanism for each:

1. **Protocol choice (after DW-001 / G-1).** If native `tools` is reliable on both runtimes, the text-protocol fallback may be dropped (smaller executor). If it is flaky on one runtime, V1 standardizes on the text protocol everywhere (D-6). Either outcome is a *finding* appended to `docs/DEEP_WORK_FINDINGS.md`, and DW-005 is shaped to match — the task graph absorbs this without renumbering.
2. **Landing-window and turn-budget constants (after DW-018 / G-4, G-5).** §15's numbers (15 min / 10%, 5-min turns, 3 retries, K=3 re-grounding) are first drafts. Calibration is an expected, planned modification: each live run appends measurements; constants move only with a recorded rationale.
3. **Task timebox policy (after first real missions).** If grounding consistently under/over-estimates `estWindow`, the scheduler's demotion rule (§16) is retuned; the mechanism (deadline-aware scheduling) does not change.
4. **Verification depth (after G-4).** If coordinator re-verification is too slow for some mission types (long test suites), V1 may offer per-mission `verification: quick|full` (mission skeleton field) — a small extension of DW-002's format, no architecture change.
5. **Discovered needs during DW-009 (start wiring).** If opening the `deep` profile in `runStartResumable` exposes assumptions (e.g., runtime selection, `--yes` flows), those become *new sub-tasks* under DW-009 with their own tests — the repo's own pattern (the candidate-replenishment work grew exactly this way across commits).
6. **Model upgrade.** If the pinned model changes, G-1 re-runs (probe suite is reusable) and G-3's recorded corpus extends; no other gate is invalidated.
7. **Host-power reality (during G-4/G-5 live runs).** If a laptop sleeps despite warnings, D-3 may escalate (detect via wall-clock gaps in `coordinator.log`; refuse `--hours > N` without `--stay-awake`-equivalent acknowledgment). This is a policy change with doc + test, not an architecture change.

**Standing rule:** any change discovered during execution lands as (a) a finding entry, (b) a task-graph modification (new/re-scoped task with stable new ID, old task marked superseded-not-deleted), (c) test + doc updates in the same commit stream — the repo's proven loop (F-010).

---

## 23. Validation Strategy

Layered so that money is spent only on what offline validation cannot prove:

**Layer 1 — Offline (free, fast, always in CI).**
- Unit + race tests for every new package (repo convention: `go test -race ./...` in CI; `.github/workflows/ci.yml`).
- Fake-model-endpoint harness (DW-017): scripted turn behaviors covering the §18 matrix; assertions on state files, task records, handoff contents, and *ordering* (handoff-before-destroy).
- Temp-repo git tests (real `git` binary, no network) for DW-006/DW-015.
- Deterministic plan tests for `stint deep plan` (fixture offers already exist in `internal/provider/vast`).
- Zero-dependency check implicit in `go.mod`/build (F-021): any new import from outside stdlib fails the build gate by policy review, not tooling.

**Layer 2 — Live, cheap (small paid spend).**
- G-1 protocol probe (~1 h, cheapest qualifying 4090/3090 offer; reuse `stint plan` to pick).
- G-4 first 1 h supervised run: validates the whole paid pipeline (plan → rent → qualify → ready → ground → execute → land → teardown) with a human at the keyboard for safety.
- `stint perf` measurements before/after for context/decode behavior of the executor's prompt shapes (Q-2 data, free once the endpoint exists).

**Layer 3 — Live, expensive (the real claim).**
- G-5 fault-injected replacement run (manual instance destroy mid-run).
- G-6 unsupervised overnight: the single strongest evidence that the vision holds — continuity, honesty of handoff, cost within ceiling, Cline compatibility, no zombie instance.

**What would count as V1 failure (stated up front, per the vision's own success criteria):** a run that ends with compute lost *and* work state lost; a handoff that claims done what verification contradicted; money spent beyond the ceiling; an interactive Cline session broken by deep mode; a zombie rented instance after deadline. Each of these maps to an explicit mechanism above (state locality, coordinator re-verification, watchdog + budget cap, D-7 mutex, watchdog retry-on-destroy) — the validation strategy exists to prove those mechanisms, not to hope.---

## 24. Duration-Aware Considerations

Duration is the single parameter that changes *everything* in a deep run. How V1 treats it across scales:

| Duration class | Startup tax (F-014) | Schedule shape | Notes |
| --- | --- | --- | --- |
| **< 2 h** | Severe — rent+SSH+model load can consume 30–50% of the window | grounding skipped or 2 tasks max; landingWindow clamps to 10 min; most missions infeasible — `deep plan` should *warn* and `start` should require `--yes` acknowledging short-window risk | Deep work is not well-defined for tiny windows; the product answer is "use interactive" — say so in help text |
| **2–8 h** (the default band; 8 h = deep profile default) | Moderate | full phase machine; landingWindow = max(15 min, 10%); task timeboxes as §15 | The vision's 23:00→07:00 overnight case sits here |
| **> 8 h** (up to ceiling) | Small fraction | longer EXECUTING; re-grounding K and mid-run re-plan more valuable; marketplace filter shrinks candidate set (F-014: `duration >= hours*3600` filter) so `deep plan` must surface how few offers qualify; budget ceiling ($3.00 default for deep) binds long-and-expensive combos — plan output makes this explicit before purchase | Also extends the host-power risk (F-016) quadratically; D-3 warning mandatory |

Cross-cutting rules:
- **One duration, three consumers** (§8.6): the same `--hours` value sizes the Vast filter, the paid window (incl. startup), and the work schedule. The coordinator's `readyAt` subtraction (F-014) is what makes the work schedule honest; all displayed "time remaining for work" is `deadline − now − (startup allowance already spent)`.
- **Landing scales with the window, not the wall clock** — an 8 h run gets a much longer landing than a 2 h run, so teardown+handoff is never a scramble.
- **Extension mid-run** is the existing `extend` command (budget-capped): a human watching `deep watch` can buy more time *before* the landing window hits, which is the designed valve for "it's almost done, give it an hour more."
- **Ceiling interaction:** because cost = rate × scheduled duration, the `MaxCostUSD` ceiling is *also* a max-duration guard (e.g., deep: $3.00 at $0.18/h ≈ 16.6 h max for that profile's economics; a 4090 at ~$0.55/h caps far lower) — `deep plan` must show "requested hours vs affordable hours at selected offer."

---

## 25. Multi-Worker Evaluation

The vision wants 2 workers; the repo defers them. Where the evidence actually lands:

**Why V1 stays single-worker (already argued in D-1; the full case):**
1. F-020: the repository's own sequencing ("Two cheaper deep workers follow only after the single-worker lifecycle is reliable").
2. F-015: state model is one instance/tunnel/watchdog; two leases means new lifecycle code in the least testable corner (the paid path).
3. Risk concentration: the worker protocol (F-004) is unproven. Two unproven workers doubles the failure surface (conflicting edits, double-spend of context, shared-endpoint contention) before any of it is proven once.
4. The honest alternative for "more parallelism" today is free: dual-lane NInfer already lets a human run Cline alongside the deep run (F-012) — a 4090 deep session is *effectively* 2-client from day one.

**V2 options, ranked by evidence strength:**

| Option | Evidence for | Evidence against | Verdict |
| --- | --- | --- | --- |
| **A. Dual-lane 4090, 2 deep workers, 1 lease** | NInfer `--clients 2` merged and live-tested (commit `cbe0dbf`, `docs/NINFER.md`); "agents" telemetry already counts concurrent clients; halves compute cost vs 2×3090 | shared KV pool under two *long-context* agents unmeasured (deep prompts are much bigger than Cline chat); worker protocol must be well-behaved (no KV thrash); contention on one model = two slow workers instead of two fast ones | **Preferred V2 candidate** — cheapest test of multi-worker semantics; start with *asymmetric* pairing (deep worker + Cline/human, then deep+deep once KV behavior is measured) |
| **B. 2×3090 leases, deep ports 8301/8302** | ports + 2-worker planning already exist in code (`config.go:3-5`, `core` multi-worker plans); full isolation (separate KV, separate endpoints) | new multi-lease lifecycle (F-015); 2× the spend; 3090 model-load time doubles the startup tax; candidate pool must qualify *two* machines (2× the measured-transfer qualification) | V2 fallback if A's KV contention proves bad; V3 if the product wants true parallelism |
| **C. Remote workers (workspaces on rented compute)** | vision §18 end-state; removes F-016 host constraint | breaks the core local-repo invariant; requires the `WorkerLease` abstraction (F-013); largest new surface | V3+ only, with explicit human re-decision on the invariant |

**Cross-review placement:** only after either A or B is stable — a reviewer is just a second worker pointed at the deep branch's diff with a different mission (review mission), which the V1 task/handoff machinery already supports at the document level.

**Decision requested (adds to §11 as informational, not blocking V1):** approve Option A as the V2 design center, with the KV-contention measurement as its G-1-equivalent gate.---

## 26. Risks

| ID | Risk | Likelihood | Impact | Mitigation |
| --- | --- | --- | --- | --- |
| R-01 | Pinned 27B model cannot reliably follow the tool protocol (malformed actions, ignored constraints) | Medium-High (the central unknown) | High — V1's premise | D-6 fallback to uniform text protocol; G-1 probe gate *before* executor build; anti-loop + verification means bad model behavior degrades to "less work done + honest handoff," never to unsafe work; model rotation path (§13.7) |
| R-02 | Long-context degradation: multi-hour runs accumulate drift/hallucinated state | Medium | Medium-High | deterministic per-turn prompt reconstruction from durable records (§17.2) instead of accumulated chat; re-grounding cadence; handoff forces a final truth-check against success criteria |
| R-03 | Worker does damage to the user's repo/machine | Low (design) | **Severe** | branch isolation (D-4), path confinement, command allow-list, no push/merge/destructive (D-5), coordinator-only commits, incident log, DW-015 escape-test battery, clean-tree precondition; Cline-invariant preserved (local execution) |
| R-04 | Unattended overnight: machine sleeps, network drops, coordinator wedges | Medium (laptops) / Low (desktop) | Medium | D-3 policy + startup check; wall-clock gap detection in `coordinator.log`; watchdog remains final authority; degraded handoff on wake-after-deadline; Q-3 follow-up |
| R-05 | Money lost to zombie instance or budget breach | Low | High (trust) | existing watchdog retry-on-destroy + instance scoping + budget cap (all proven in interactive mode, F-002/F-003); deep adds no new destruction path — it reuses the same one |
| R-06 | Scope creep of the dynamic task graph eats the session | Medium | Medium | spawn caps + total caps (§16), deadline-aware demotion, landing window, re-plan budget = 1; all recorded so the human sees the creep in the handoff |
| R-07 | Interactive-mode regressions from shared pipeline edits (DW-009 touches `resumable_start.go`) | Low-Medium | High (v0.1.0 just shipped on this code) | deep gates additive, interactive path byte-identical; full existing suite stays green per PR; `deep` profile changes isolated behind the new gates; repo's fix-branch discipline (`fix/resume-*`, `fix/vast-*`) continues to apply |
| R-08 | `cmd/stint` package bloat (already the largest concentration, F-019) | High (certain) | Low-Medium (maintainability) | consistent `deep_*.go` naming; pure logic in `internal/deep`; the deferred `internal/` reorg (§13) remains the exit strategy; review discipline on file sizes |
| R-09 | Verification commands lie (passing tests ≠ mission success) | Medium | Medium | mission success criteria are human-authored and restated in the handoff's final "is the objective met?" section; coordinator reports *evidence*, humans report *truth*; partial statuses are first-class (never silently "done") |
| R-10 | Marketplace can't fill a long `duration` filter (few offers for 8 h) | Medium | Medium | `deep plan` surfaces candidate inventory before purchase (existing diagnostics, `DiscoveryEmptyError` bisect); user can shorten; the filter is provider-side and honest |
| R-11 | Protocol choice (D-6) rework after executor built | Low (G-1 precedes DW-005) | Medium | sequencing in §21 makes the probe first; executor parser layer isolated so protocol swap is one file |
| R-12 | Historical evidence becomes a de facto static inventory or incumbent bias | Medium if persistence is added without a selection design | High — stale/unavailable hosts are preferred and new supply is suppressed | Live discovery is always step one; history is a bounded soft signal only; unknown is neutral; exact-offer IDs never create future availability; offline replay/calibration gate before ranking (§28) |

**Residual risk accepted for V1:** a run can finish having done *poor-quality* work that nonetheless passes its own tests (R-09 is not fully solvable by automation). The product answer is the handoff contract: Cline/human review is the final authority, and `accept` is the human's explicit endorsement. Deep Work makes the work *inspectable*, not *infallible*.

---

## 27. Recommended Next Steps

**Immediate (human, no code):**
1. Review this document; resolve D-1…D-8 (§11). D-1, D-5, and D-6 are the load-bearing ones; D-8 is a five-minute cleanup that can land first.
2. Approve the V1 boundary (§12) and the gate ladder (§20) — especially the rule that no paid run happens before G-1…G-3.
3. Nominate the first live missions (G-4): 1–2 small, fast-test, single-repo missions from this project or a fixture project — chosen for *verification speed*, not ambition.

**First execution branch (after G-0):**
4. `feat/deep-work-protocol`: DW-001 probe suite + DW-002 mission parser + DW-003 state layout + DW-008 live deep plan. End with the cheap paid probe run (G-1) and a recorded protocol decision.
5. Parallel-safe second branch: DW-004 coordinator skeleton + DW-017 harness (G-2), then DW-005/007/015 executor (G-3) shaped by G-1's answer.

**Then, strictly in gate order:** wiring (G-4, short supervised live run) → recovery (G-5, fault-injected) → overnight (G-6) → release (DW-020). Each live step appends to `docs/DEEP_WORK_FINDINGS.md` and may retune §15 constants — that feedback is the point of the ladder, not a deviation from it.

**Explicitly do not start:** anything in §13 (multi-lease, remote workspaces, cross-review, push/merge, Spark collaboration, `internal/` reorg) before G-6 is green.

---

## 28. Historical Offer and Infrastructure Evidence

### 28.1 Conclusion

Persistence makes a historical evidence store possible, but the current repository and provider contract do **not** justify historical ranking yet.

The safe product model is:

> Historical evidence describes past compute. Current marketplace discovery determines what can actually be rented now.

The planner must never start from a remembered machine list. It must discover live offers, evaluate current price/availability/properties under hard policy, and only then ask whether past observations provide a useful probabilistic signal for one of those live candidates. A history record cannot produce a candidate, reserve a candidate, or establish that a candidate will exist later.

```text
live provider search at decision time
        ↓
current normalized offers
        ↓
current hard-policy evaluation
        ↓
eligible candidates that exist now
        ↓
optional historical matching + uncertainty
        ↓
bounded soft ranking adjustment
        ↓
immediate pre-rental offer revalidation
```

This is consistent with the repository's existing stale-offer behavior: `startupCandidatePool` refreshes the marketplace when an ask disappears and distinguishes an unavailable offer from a paid machine attempt (`cmd/stint/network_candidate_retry.go:24-129`, `cmd/stint/resumable_start.go:237-371`). The refresh path, not remembered inventory, is the recovery primitive.

### 28.2 What is known about identity today

Vast's current documentation distinguishes a **machine** (one physical host system) from an **offer** (a rentable configuration or slice published by that machine). One machine can publish multiple offers. The search API separately exposes `id` as the offer ID, `machine_id` as the host-machine ID, and `host_id` as the host user ID. It also says an instance is created by accepting the current offer ID. Sources: [Vast concepts](https://docs.vast.ai/guides/concepts), [search offers API](https://docs.vast.ai/api-reference/search/search-offers), and [creating instances with the API](https://docs.vast.ai/api-reference/creating-instances-with-api).

That establishes field meaning, not a stability guarantee. The documentation does not promise that:

* an offer ID will survive relisting, repricing, slicing, or other term changes
* a machine ID can be used to predict that the physical host will be listed tomorrow
* a machine retaining its ID has unchanged GPUs, drivers, storage, network path, thermal behavior, or contention
* a host account's machines share operational quality
* any identifier is globally meaningful outside its provider and account/API scope

Current repository support is narrower still:

| Identity/property | Present in live Vast response | Normalized into `core.Offer` | Persisted in `session.json` | Defensible current use |
| --- | --- | --- | --- | --- |
| offer `id` | yes | yes (`Offer.ID`) | yes (`OfferID`) | mutate/revalidate the current ask; not durable inventory identity |
| `machine_id` | yes | yes (`MachineID`) | no | avoid retrying the same machine within one in-memory startup pool |
| `host_id` | yes | no | no | none; provider-account matching is unavailable today |
| GPU model / region / advertised network / price | yes | partially | GPU model + price only | current eligibility/ranking and display |
| measured network result | produced after SSH | no durable record | no | one startup qualification decision |
| SSH-ready/model-ready timing | observable during lifecycle | no aggregate record | timestamps are insufficient to reconstruct stages | console/log output only |
| runtime disconnect/usable duration outcome | partly observable | no historical record | active session only | recovery/teardown for the current lease |

`networkCandidateKey` preferring `machine_id` over offer ID is an **in-run deduplication rule**, not evidence that `machine_id` is a permanent inventory key (`cmd/stint/network_candidate_retry.go:47-52`). That distinction should be explicit in any future schema and tests.

Identity should be represented as provider-scoped claims with documented confidence, never as one universal `machine_key`:

| Match level | Candidate key example | What it may support | Main limitation |
| --- | --- | --- | --- |
| exact current offer | `vast/offer/<id>` | joining a discovery snapshot to the immediate rental attempt | terms/availability are ephemeral; do not carry it into future planning |
| exact provider machine | `vast/machine/<machine_id>` | strongest correlation if the same live machine reappears | availability and configuration are not guaranteed; stability needs longitudinal validation |
| provider account | `vast/host/<host_id>` | weak host-operations prior after enough distinct machines/runs | one account may operate heterogeneous infrastructure; Stint does not capture it today |
| configuration cohort | provider + GPU + region + hardware/network/runtime buckets | backoff prior for comparable live candidates | correlations are broad and confounded; bucket definitions must be versioned |
| broad baseline | provider + workload/runtime | prior for new/unmatched candidates | deliberately weak; preserves first-class unseen supply |

An exact-machine match may select a more specific evidence bucket; it must never skip the initial live search or imply that the machine is reservable.

### 28.3 Observation model: events first, aggregates second

If Stint begins collecting evidence, the durable source should be append-only attempt/outcome events. Aggregates such as `4/7 successful startups` are derived views, not the canonical record. Raw events permit later correction of definitions, staleness functions, cohort membership, and censored outcomes without losing information.

A conceptual observation contains:

```text
observation ID + schema version
provider + provider-account scope
session ID + attempt ID
discovered-at / rental-at / stage timestamps
current offer snapshot at decision time
provider identity claims (offer, machine, host) and confidence/version
runtime configuration (image/build, model, context, clients, storage)
stage outcomes:
  offer accepted or stale before rental
  provider running
  SSH accepted
  network probe completed/qualified
  runtime ready
  model ready
  serving ready
measured values:
  SSH-ready seconds
  transfer MB/s + probe method/version
  runtime/model startup seconds
  ready-to-deadline usable seconds
termination:
  normal deadline / user stop / provider loss / disconnect / startup rejection
failure stage + normalized reason + raw diagnostic reference
```

Important separations:

1. **Discovery snapshots are not outcomes.** Merely seeing an offer must not count as a success, failure, or availability promise.
2. **A stale ask is marketplace churn, not machine startup failure.** Today it correctly does not consume the paid-attempt budget; historically it should update an offer-churn signal, not poison the machine's SSH/runtime reliability.
3. **Provider-running, SSH-ready, network-qualified, runtime-ready, and model-ready are separate Bernoulli/time domains.** Collapsing them into one success bit destroys actionable signal.
4. **Normal teardown is censored reliability data, not proof the machine would have failed next.** A session surviving to its user deadline establishes a lower bound on usable duration. User stop, deadline stop, local sleep, provider loss, and Stint defects require different termination codes.
5. **Measurements require method versions.** A bandwidth result is comparable only when probe target, warm-up, sample duration, units, and runtime/model path are compatible. Runtime startup evidence needs image/build/model/context compatibility.
6. **Failures caused by Stint must not be attributed to infrastructure.** A broken image, invalid startup hook, credential error, local tunnel conflict, or client sleep is not host unreliability.

The event store should remain separate from `session.json`. Lifecycle state is current paid-resource authority; historical evidence is observational, analogous to the existing separation of `performance.json` from session authority (`docs/TELEMETRY.md`). Collection failures must never change teardown, deadline, or recovery behavior.

### 28.4 Comparability and matching

Historical evidence is relevant only when the current candidate and the observation agree on the properties that can materially affect the measured outcome.

Example compatibility rules:

| Signal | Required or important match dimensions | Reasons to reject/downweight the match |
| --- | --- | --- |
| offer acceptance/churn | provider, offer type, possibly duration/price regime | exact old offer ID alone; provider API/policy changes |
| provider startup / SSH readiness | provider machine (strongest), host cohort, verification state, connection mode, image/startup-hook family | machine reconfiguration, changed virtualization/ports, new startup hook, Stint version regression |
| measured transfer | machine or network cohort, region, probe method/target/version, time period | different target/CDN, changed route, ISP, region, units, sample method |
| runtime/model startup | GPU model/count/VRAM, driver/CUDA compatibility, runtime image/build, model artifact, context/clients, cache state | new runtime build/model, warm cache vs cold cache, materially different storage/network |
| disconnect/usable duration | machine/host/provider, runtime family, session length, local-host availability | user stop, deadline censoring, laptop sleep, planned maintenance, unknown cause |
| inference throughput | GPU and offer slice, runtime/model/build, context/clients, benchmark depth/method | incompatible benchmark or contention state |

Mutable fields should be stored both as observed values and as versioned comparison buckets. For example, `RTX_4090 + UK + 500–1000 Mbps advertised + NInfer build X` is a possible cohort, not an identity. Overly specific cohorts have no sample size; overly broad cohorts hide meaningful differences. Bucket definitions therefore belong to a versioned analytical layer and must be evaluated offline before affecting planning.

Current values beat historical values when they conflict. Examples:

* a historically cheap machine with a current price over the hard ceiling is rejected
* a previously verified machine currently marked deverified/unverified follows current policy
* a historically fast offer currently advertising insufficient ports or duration is rejected
* a machine with good past bandwidth still undergoes the current paid measured-network qualification

History may predict the chance of passing a current measurement; it does not replace the measurement when the lifecycle already performs one.

### 28.5 Staleness

The repository contains no longitudinal dataset from which defensible half-lives can be estimated. Fixed decay periods would therefore be invented policy. The initial conclusion is **collect first; calibrate decay later**.

Staleness is signal-specific:

* offer price/rentability/terms become stale almost immediately and should always come from the live response
* route-dependent bandwidth can change quickly with provider networking, congestion, target, and geography
* SSH/provider startup behavior may survive relisting but can change with host software, ports, virtualization, or startup hooks
* runtime/model startup measurements are comparable only within compatible image/build/model/configuration eras
* GPU-model priors may age more slowly, while exact-machine operational history may be invalidated instantly by a detected configuration change
* provider-account evidence should decay and shrink heavily because it pools heterogeneous machines

A future calibration job should use time-ordered replay: predict each attempt using only earlier observations, sweep decay/maximum-age choices per signal, and compare calibration and decision outcomes on later attempts. Configuration changes should be explicit invalidation boundaries, not merely elapsed-time decay.

Until enough data exists, history can be displayed as annotations (for example, `7 observations; last 18 days ago; identity match: machine; configuration match: partial`) without changing sort order.

### 28.6 Unseen candidates and sparse evidence

New machines must remain first-class. Missing history is **unknown**, never a zero success rate and never a rejection reason.

A defensible future estimator uses hierarchical backoff and shrinkage:

```text
exact-machine evidence, if compatible and sufficient
        ↓ shrink toward
host/provider cohort, if validated and sufficient
        ↓ shrink toward
GPU/region/runtime cohort
        ↓ shrink toward
broad provider/workload prior
```

Sparse exact matches should remain close to the broad prior. Seven observations are not automatically seven useful observations: repeated runs from one short period, obsolete runtime configurations, or heavily decayed samples may have a much smaller effective sample size. Each estimate should carry match level, raw count, effective sample size, recency, and uncertainty.

The planner also needs an exploration policy. Even a calibrated history signal can entrench incumbents because Stint observes outcomes only for offers it selected. Options to test offline include a capped history bonus/penalty, a minimum share of attempts chosen without exact-machine preference, or uncertainty-aware selection among otherwise close candidates. Hard policy remains unchanged in every case.

### 28.7 Weighting against current marketplace evidence

Historical evidence must be a late, bounded soft signal:

1. Live search defines the candidate set.
2. Current hard policy removes unsafe/over-budget/unrentable candidates.
3. Current workload compatibility and price establish the base rank.
4. Compatible history produces estimates with uncertainty.
5. Only estimates passing a predeclared evidence gate may adjust the base rank.
6. The adjustment is capped so history cannot overwhelm a material current-price/performance difference.
7. The chosen offer is revalidated immediately before mutation; a stale offer triggers live replenishment.

Potential outcome estimates include:

* probability of offer acceptance at mutation time (churn, not host quality)
* probability of provider-running, SSH-ready, network-qualified, runtime-ready, and model-ready
* expected time from rental to READY
* probability of surviving the requested usable duration
* expected measured transfer and inference performance
* expected cost per usable compute hour

The final metric needs explicit semantics. A useful conceptual form is:

```text
expected billed cost (including startup and failed paid attempts)
---------------------------------------------------------------
expected READY-to-stop usable compute hours
```

It must include failures and startup tax, handle deadline/user-stop censoring, and report uncertainty. Optimizing only the successful sessions would reward unreliable cheap machines by dropping their failed attempts from the denominator.

### 28.8 Evidence sufficiency and validation gates

Historical ranking should remain disabled until all of the following are demonstrated:

**Identity gate**

* Capture `machine_id` and `host_id` without treating either as availability.
* Observe repeated live searches over time and document whether IDs reappear, whether offer IDs change, and which material properties change under the same machine ID.
* Provider-scope every key and version the identity contract.

**Collection gate**

* Record complete append-only attempt events, including failed and censored attempts.
* Classify Stint/local/user/provider causes separately.
* Version measurement methods and runtime configurations.
* Prove observation-write failure cannot affect lifecycle authority or cleanup.

**Statistical gate**

* Define signal-specific compatibility, invalidation, decay, minimum effective sample size, and uncertainty.
* Demonstrate calibrated probabilities/time estimates in forward, time-ordered validation—not random train/test splitting that leaks future host behavior.
* Report results by exact-machine, host, and cohort level; broad aggregate accuracy is insufficient.

**Decision gate**

* Replay historical marketplace decisions using only candidates that were live in each snapshot.
* Compare against the current history-free ranker on startup success, READY latency, usable duration, network qualification, runtime reliability, and cost per usable hour.
* Measure unseen-candidate selection and subgroup/cohort effects so gains are not merely incumbent exclusion.
* Require material improvement with bounded downside before enabling even a soft adjustment.

**Operational gate**

* Surface why history affected a rank: match level, observations, effective sample size, age, estimate, uncertainty, and capped adjustment.
* Provide a history-disabled baseline and kill switch.
* Keep current measured qualification and immediate offer revalidation in place.

No repository evidence currently clears these gates. The recommended sequencing is therefore:

```text
Phase H0  document provider identity semantics and capture missing fields
Phase H1  append observation events; ranking unchanged
Phase H2  build offline summaries, decay analysis, and time-ordered replay
Phase H3  shadow-score live candidates; ranking unchanged
Phase H4  consider a bounded soft signal only if validation shows value
```

This work is outside Deep Work V1. Persistence is a prerequisite for learning, not evidence that the learned signal is ready or that marketplace supply has become durable.
