# Deep Work dashboard — action plan

**Status:** implementation in progress — P0–P3 are implemented on the feature
branch; P4 requires the dedicated GPU compression smoke.
**Owner:** Stint Deep Work.  
**Scope:** a terminal dashboard that lets an operator judge whether a Deep Work
session is progressing safely while Hermes executes on the GPU box.

## 1. Decision and boundaries

Add a dedicated command:

```text
stint deep dash [--session <id>] [--no-color]
```

`stint dash` remains the **compute** cockpit: instance lifecycle, cost, endpoint,
GPU, inference lanes, and deadline controls. `stint deep dash` is the **execution**
cockpit: mission progress, task evidence, coordinator health, and the GPU worker's
sanitized operational signals. Keeping the commands separate means a Deep Work
handoff or a lost compute session remains inspectable from durable local state.

The dashboard is presentation and control over existing authorities. It must not
become a second coordinator, mutate task state directly, launch model requests, or
own billing state.

| Authority | Owned by | Dashboard use |
| --- | --- | --- |
| Task phase, attempts, worktree, deadline, handoff | `deep.json` | read-only |
| Executor, verification, checkpoint decisions | Deep Work coordinator | read-only events |
| Compute lifecycle and billing | `session.json` + lifecycle commands | existing passive snapshot |
| GPU-side Hermes/NInfer condition | bounded remote observer | read-only, sanitized telemetry |

## 2. Operator questions and success criteria

The default screen must answer these without reading raw model output:

1. **Is it alive?** Is the coordinator running, is compute reachable, and is a
   worker request active or stalled?
2. **Is it advancing?** Which task is active, how long has the attempt run, which
   tasks have been independently verified, and is the retry budget being consumed?
3. **Is it safe to leave alone?** How much time remains before the landing window,
   are verification/checkpoint failures accumulating, and did Hermes compression
   complete without truncation?

The dashboard is successful when an operator can distinguish all of the following
within one refresh cycle:

- normal active work;
- an idle but healthy gap between tasks;
- a task retrying after failed verification;
- a parked/blocker state;
- a dead coordinator with durable work safe to resume;
- endpoint/runtime trouble;
- an observed compression failure that should stop further unattended work.

## 3. User experience

### 3.1 Run view (default)

```text
DEEP WORK  ● EXECUTING                         3h 18m to landing
Mission    CP1                    Coordinator   running (pid 12345)
Worker     Hermes on GPU          Compute       READY · endpoint healthy

NOW        VANTA-003  attempt 1 / 2  ·  06m 42s running
NEXT       VANTA-004  queued
PROGRESS   2 verified · 1 active · 3 queued · 0 blocked

LAST CHECK VANTA-002 verified · checkpoint committed · 2m ago
CONTEXT    42k resident · no compaction needed in this attempt
```

The active-attempt timer is derived from the latest `executor-invoke` incident for
the active task. It is labelled as elapsed time, never as a worker-provided progress
percentage. If no matching event exists, render `active; start time unavailable`.

### 3.2 Task view

Render tasks in mission order with status, attempts, objective, latest verifier
result, and a bounded blocker. The status labels are:

| State | Meaning |
| --- | --- |
| `queued` | not yet invoked |
| `active` | coordinator has started an attempt |
| `incomplete` | retryable; next invocation receives reconstructed context |
| `verified` | coordinator's verifier passed and a checkpoint was attempted |
| `blocked` / `needs_human` | parked with the recorded reason |
| `dropped` | intentionally excluded from remaining work |

This view must make a verification failure visible even if the worker reported a
successful-looking final answer.

### 3.3 Activity view

Show a reverse-chronological, bounded timeline built from `incidents.jsonl` and the
coordinator log. Events include executor invocation/error, verification pass/fail,
checkpoint failure, resume, external stop, and landing. Show event kind, task ID,
timestamp, and a compacted detail. Do not render prompts, tool arguments, agent
responses, credentials, or full remote command output.

### 3.4 Worker view

The Worker view combines existing passive inference telemetry with a small,
content-free GPU observer:

- endpoint/runtime reachability, GPU utilization, active lanes, queue, prompt depth;
- NInfer context, KV capacity, and default completion budget;
- phase-route request counts and latest phase (`xhigh` or `medium`);
- Hermes compaction state: `not observed`, `running`, `completed`, `failed`, or
  `truncated`;
- last sanitized worker observation time and its error, if any.

Deep Work launches a fresh Hermes one-shot process for each task attempt. Context and
compaction therefore describe the active **attempt**, not a single chat that spans a
whole mission. `not observed` is neutral in normal work; the compression smoke has a
separate gate requiring `completed`.

### 3.5 Keys and actions

| Key | Action |
| --- | --- |
| `1`–`4`, arrows, `Tab` | Run, Tasks, Activity, Worker views |
| `r` | reload local Deep Work state and start one passive worker/compute refresh |
| `s` | open a graceful-land confirmation |
| `S` in confirmation | invoke existing `stint deep stop`; compute remains running |
| `q`, `Ctrl+C` | exit the dashboard only |

The first release has no one-key resume, deadline change, teardown, command execution,
or worker configuration action. The screen directs recovery through the existing
`stint resume` and `stint deep resume` commands.

## 4. Data and refresh contract

```text
1 second   derived elapsed/remaining timers; no I/O
5 seconds  deep.json, coordinator PID, bounded local incident/log tails
10 seconds existing passive compute snapshot + one bounded GPU worker observer
manual     graceful-land confirmation only
```

The remote observer must have a hard timeout no greater than the existing telemetry
budget and must run at most once at a time. A failed worker observation is displayed
inside the Worker domain; it must not change `deep.json`, session state, the deadline,
or compute lifecycle.

### 4.1 Local projection

Add a `deepDashboardSnapshot` projection layer rather than binding rendering directly
to `DeepState`. It loads atomically persisted `deep.json`, checks the coordinator PID
with signal 0, derives task totals and active-attempt elapsed time, and tails only a
bounded number of incident/log records. The projection exposes missing/corrupt state
as a display error, never as a fabricated stopped session.

### 4.2 GPU worker observer

Provision `/root/stint-phasing/deep-observe` for phased Hermes runs. It emits one
small JSON object and performs only local reads on the box. Its contract is:

```json
{
  "collectedAt": "2026-09-08T12:00:00Z",
  "ninfer": {
    "running": true,
    "maxContext": 262144,
    "kvCapacity": 262144,
    "defaultMaxTokens": 262144
  },
  "phaseRoutes": {
    "xhighRequests": 2,
    "mediumRequests": 8,
    "latestPhase": "medium",
    "latestAt": "2026-09-08T11:59:58Z"
  },
  "compression": {
    "state": "completed",
    "completed": 1,
    "failed": 0,
    "truncated": 0,
    "lastAt": "2026-09-08T11:59:55Z"
  }
}
```

It may parse only known, content-free Hermes log markers:

- `context compression done`;
- `context compression attempt telemetry:`;
- `Context compression summary was truncated`;
- terminal compression failures.

It must never return the surrounding Hermes log lines. To attribute phase-route
events accurately, extend `phaseproxy.py` records with a UTC timestamp and HTTP result
class. The dashboard filters events from the Deep Work session start marker onward.

## 5. Delivery plan

### P0 — local dashboard foundation

1. Register `stint deep dash` and `stint deep dashboard` alias with `--session` and
   `--no-color`.
2. Add the local Deep Work projection and renderer: Run, Tasks, Activity, Worker
   placeholder views.
3. Reuse terminal, ANSI, narrow-terminal, and modal primitives from
   `internal/dashboard`; keep the model and controller specific to Deep Work.
4. Support dashboard operation with no active compute session so landed and
   recoverable work remains reviewable.

**Gate:** fixture state renders correct task counts, coordinator status, landing
countdown, recent events, and blockers without contacting a box.

### P1 — passive compute strip

1. Reuse `collectSessionSnapshot` for the existing endpoint/runtime/GPU/inference
   telemetry.
2. Render compute as an observational strip in Run and Worker views.
3. Preserve independent degradation: a failed SSH sample cannot hide valid local Deep
   Work state or turn a session into a recoverable one.

**Gate:** tests cover READY, DEGRADED, missing compute, and a live Deep Work session
whose coordinator has already exited.

### P2 — structured GPU worker observer

1. Add timestamped, response-classed phase-proxy records.
2. Add the content-free `deep-observe` script and provision it with the phase setup.
3. Add a bounded remote observer seam in the Deep Work dashboard. Parse JSON strictly;
   surface parse/SSH timeout errors as Worker-domain observations.
4. Add the Worker view's compaction and route indicators.

**Gate:** static observer fixtures prove correct rendering for completed compression,
truncation, missing medium route, a dead phase proxy, and an unavailable observer.

### P3 — safe control and documentation

1. Implement `s` → confirmation → existing `runDeepStop` path.
2. Document ownership, cadence, data retention, failure states, and the fact that
   dashboard exit never stops Deep Work or compute.
3. Add a non-TTY fallback: one static Deep Work status snapshot with no remote worker
   probe unless `--refresh` is explicitly requested.

**Gate:** controller tests prove lowercase `s` does nothing destructive, only uppercase
`S` confirms landing, and compute lifecycle is untouched.

### P4 — box smoke and first-run acceptance

1. Run the normal box phase smoke.
2. Run the short forced-compression smoke with the lower temporary threshold.
3. Keep `stint deep dash` open from the operator machine throughout the smoke.
4. Record a screenshot/transcript showing the active task, completed compaction,
medium-route observation, verifier pass, and checkpoint.

**Pass condition:** the dashboard reports a completed compaction and medium route,
while the raw evidence contains no truncated-summary or context-overflow marker and
the task is independently verified.

## 6. Test matrix

| Area | Required coverage |
| --- | --- |
| Rendering | wide/narrow terminal, no color, no Deep Work session, landed session |
| State projection | active timer, task totals, blocker truncation, stale coordinator PID |
| Events | malformed final JSONL line, verification fail, checkpoint fail, landing |
| Compute strip | healthy, degraded, unavailable, no active compute |
| Worker observer | valid JSON, timeout, malformed JSON, truncation, route mismatch |
| Controls | refresh is passive; graceful land requires confirmation; exit is inert |
| Regression | existing `stint dash`, `stint deep status`, resume, and stop tests remain green |

## 7. Explicit non-goals for v1

- streaming agent thought, prompts, tool calls, or source-code edits;
- dashboard-originated shell commands on the box;
- automatic retry, automatic resume, automatic extension, or automatic teardown;
- a claim that no compaction event means compression is healthy;
- a web service or persistent telemetry database.

## 8. Risks and mitigations

| Risk | Mitigation |
| --- | --- |
| Dashboard mistakes a quiet task for a hang | show attempt elapsed plus last coordinator event; do not infer progress from GPU load alone |
| Remote log scraping leaks task content | observer emits only whitelisted counters and timestamps |
| Extra refreshes interfere with active inference | one bounded, read-only observer every 10 seconds; no generation requests |
| Compute loss hides durable Deep Work evidence | Deep Work dashboard loads local state independently of compute state |
| Compression is never triggered in ordinary tasks | call it `not observed`; use the forced-threshold smoke for the actual compression gate |
| Controller/display state contradicts durable state | `deep.json`, incidents, and lifecycle state remain authoritative; dashboard never writes them except through confirmed `deep stop` |

## 9. Definition of done

The feature is done when `stint deep dash` can monitor a running GPU-backed Hermes
session and, after compute loss or landing, still show truthful durable progress. It
must show independently verified task state, coordinator/compute condition, sanitized
worker route/compaction evidence, and a safe graceful-stop control. The short
compression smoke must supply end-to-end evidence before this dashboard is relied on
for the multi-hour run.
