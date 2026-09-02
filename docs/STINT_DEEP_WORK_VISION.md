# Stint Deep Work

## Vision

Stint Deep Work is an execution mode for bounded, unattended software engineering.

The core idea is simple:

> Allocate a fixed amount of compute, time, and authority to one or more coding agents, allow them to work autonomously for several hours, and return an inspectable, verified engineering result rather than a long conversation transcript.

A typical use case is an overnight session:

```text
23:00
Developer defines an objective and constraints.

23:05
Stint acquires suitable compute and initializes isolated workspaces.

23:15–06:00
Agents investigate, implement, test, review, and re-plan.

06:00–07:00
Stint transitions into verification and stabilization.

07:00
Developer receives a structured handoff containing commits,
evidence, unresolved decisions, rejected approaches, and test results.
```

Deep Work is not intended to replace interactive coding.

Interactive Stint optimizes for low-latency collaboration with a human in the loop.

Deep Work optimizes for:

```text
useful, verified engineering progress
-------------------------------------
        compute cost × time
```

The product therefore requires a different execution architecture.

---

# 1. Product Model

Stint currently treats compute primarily as infrastructure for an interactive model endpoint.

Conceptually:

```text
developer
    ↓
Stint
    ↓
GPU instance
    ↓
model server
    ↓
Cline / agent
```

Deep Work introduces an orchestration layer:

```text
developer
    ↓
mission
    ↓
Stint Deep Work Coordinator
    │
    ├── compute scheduler
    ├── task graph
    ├── repository state
    ├── worktree leases
    ├── evidence store
    ├── checkpoint manager
    ├── agent supervisor
    └── budget / deadline controller
             ↓
       coding workers
             ↓
         repository
```

The central design principle is:

> The model performs reasoning and engineering. Stint owns continuity, state, constraints, resources, and authority.

This avoids making the LLM responsible for maintaining eight hours of reliable execution state inside a single conversation.

---

# 2. The Mission

A Deep Work session begins with a mission.

A mission is more structured than a normal coding prompt and acts as the execution contract for the session.

Example:

```yaml
mission:
  name: spark-evidence-grounding

objective:
  Implement evidence grounding behind the evaluation repository boundary.

success:
  - Existing API behavior remains compatible.
  - Existing tests remain green.
  - New behavior is covered by tests.
  - Changes are committed by logical concern.

constraints:
  writable:
    - packages/evaluation/**
    - apps/api/**
    - tests/**

  forbidden:
    - infrastructure/**
    - migrations/**
    - deployment/**

authority:
  allow:
    - read_repository
    - edit_files
    - create_commits
    - run_tests
    - run_builds

  deny:
    - merge
    - deploy
    - modify_production
    - force_push

deadline:
  hours: 8

budget:
  compute_usd: 3.00
```

The mission may be provided directly through the CLI:

```bash
stint deep start \
  --hours 8 \
  --budget 3 \
  --mission ./deep-work/spark-grounding.yaml
```

or generated interactively before execution.

The mission should remain immutable during the run unless the coordinator explicitly records a revision.

This creates a stable reference against which agent behavior can be evaluated.

---

# 3. Execution State

Deep Work should not depend on conversation history as its source of truth.

Stint maintains explicit session state.

A session might contain:

```text
.deep-work/
└── 01J8Y2...
    ├── mission.yaml
    ├── state.json
    ├── tasks.json
    ├── findings/
    ├── decisions/
    ├── checkpoints/
    ├── evidence/
    ├── reviews/
    └── handoff.md
```

A simplified internal session representation could resemble:

```json
{
  "session_id": "dw_01J8Y2",
  "status": "executing",
  "deadline": "2026-09-01T07:00:00Z",
  "budget": {
    "limit": 3.0,
    "spent": 0.84
  },
  "workers": [
    {
      "id": "worker-a",
      "task": "T3",
      "worktree": "deep-a",
      "status": "running"
    },
    {
      "id": "worker-b",
      "task": "T4",
      "worktree": "deep-b",
      "status": "running"
    }
  ]
}
```

The important distinction is that this state survives:

* model context compaction
* worker replacement
* compute replacement
* failed inference processes
* agent restarts
* long execution windows

An agent is therefore disposable.

The session is not.

---

# 4. Grounding Before Execution

An unattended system should strongly bias toward understanding before modifying.

Deep Work begins with a grounding phase.

Workers inspect relevant:

* repository structure
* Git history
* recent pull requests
* architecture documentation
* package boundaries
* existing tests
* CI workflows
* build configuration
* public interfaces
* existing implementation

The purpose is to establish claims about the current system before writing code.

Example:

```text
FINDING F-014

Claim:
EvaluationRepository is currently the canonical boundary
for normalized evaluation state.

Evidence:
- packages/evaluation/repository.ts
- tests/evaluation/repository.test.ts
- PR #184
- commit 71ad921

Confidence:
high

Implication:
Normalization should happen behind this boundary rather
than in the HTTP route layer.
```

This makes findings reusable across workers and prevents each model context from rediscovering repository architecture independently.

---

# 5. Task Graph

The coordinator converts the mission and grounding results into an executable task graph.

For example:

```text
T1 Establish current evaluator behavior
 │
 ├── T2 Identify compatibility constraints
 │
 └── T3 Define normalization boundary
          │
          ├── T4 Implement normalization
          │
          └── T5 Add behavioral tests
                  │
                  └── T6 Integrate API
                           │
                           ├── T7 Cross-review
                           └── T8 Full verification
```

Tasks have explicit state:

```text
pending
ready
active
blocked
review
verified
failed
needs-human
abandoned
```

Example task record:

```json
{
  "id": "T4",
  "title": "Implement evaluation normalization",
  "depends_on": ["T3"],
  "owner": "worker-a",
  "status": "active",
  "scope": [
    "packages/evaluation/**"
  ],
  "acceptance": [
    "Existing fixtures remain valid",
    "New normalization tests pass"
  ]
}
```

This task graph prevents an agent from treating an eight-hour mission as an undifferentiated prompt.

It also allows work to continue around blocked areas.

---

# 6. Multi-Agent Execution

Deep Work should support multiple workers, but the coordinator should remain the authority over shared state.

A likely default configuration is two peer agents.

```text
                    coordinator
                   /           \
             worker A         worker B
             worktree A       worktree B
                 \               /
                  \             /
                    cross-review
```

Both workers are capable of:

* investigation
* implementation
* testing
* reviewing
* proposing replans

They should not have permanently asymmetric roles such as "coder" and "reviewer."

Instead, review responsibility rotates according to the task graph.

This reduces the risk of creating one primary agent whose assumptions dominate the entire session.

---

# 7. Worktree Isolation

Workers never modify the same Git checkout.

Stint creates isolated Git worktrees:

```text
repo/
├── .git/
├── worktrees/
│   ├── deep-a/
│   └── deep-b/
```

Each active task receives a scope lease.

Example:

```text
LEASE L-021

owner:
worker-a

scope:
packages/evaluation/**

expires:
task completion

conflicts:
worker-b write access denied
```

The lease system does not necessarily need to operate directly at the filesystem level.

It may initially be coordinator-enforced.

Before a worker modifies a path, it checks that its task owns the relevant scope.

If two tasks require overlapping ownership, the coordinator serializes the work or changes the task graph.

This avoids autonomous agents creating conflicting edits that later require expensive semantic merging.

---

# 8. Evidence-Driven Completion

An agent stating that a task is complete is insufficient.

Deep Work should treat completion as a claim requiring evidence.

The basic structure is:

```text
claim
  ↓
implementation
  ↓
evidence
  ↓
peer review
  ↓
accepted checkpoint
```

Example:

```text
CLAIM C-031

"Existing API behavior remains backwards compatible."

Evidence:

E-044
36 existing API integration tests pass.

E-045
Golden response fixture is byte-equivalent.

E-046
No public route schema files changed.

Peer verification:
worker-b

Status:
accepted
```

Evidence may include:

* test execution
* build output
* lint results
* benchmarks
* static analysis
* diff inspection
* Git history
* fixture comparison
* API snapshots
* dependency graph checks
* independent agent review

Stint does not need to prove that software is correct.

It needs to make the system's reasons for believing the work is correct explicit and inspectable.

---

# 9. Checkpointing and Context Compression

A long-running coding agent will eventually accumulate large amounts of obsolete context.

Deep Work should therefore treat context compression as an execution primitive.

After each meaningful stage, workers produce checkpoints.

Example:

```text
CHECKPOINT CP-07

Current understanding:
- EvaluationRepository is canonical.
- API routes still expose the old response shape.
- Historical runs may not contain evidence identifiers.

Completed:
- Added normalized EvidenceSet representation.
- Added compatibility adapter.

Verification:
- package tests: 47/47
- typecheck: pass

Open:
- historical-run behavior
- full API integration

Rejected:
- database migration
  outside mission scope
```

The next worker context can then be assembled dynamically:

```text
mission
+
current task
+
relevant findings
+
accepted decisions
+
latest checkpoint
+
selected repository files
```

instead of:

```text
entire previous conversation
+
every command output
+
every abandoned hypothesis
```

This is particularly important for self-hosted models where context size and KV cache have direct performance and memory costs.

---

# 10. Replanning

Failure is expected during autonomous engineering.

Repeated failure without new information should not be.

The coordinator tracks attempts and progress signals.

Example:

```text
attempt 1
test X fails

attempt 2
same failure signature

attempt 3
same failure signature
```

At this point Stint can automatically:

```text
stop task
↓
record failure state
↓
request independent investigation
↓
update findings
↓
re-plan task graph
```

Replanning triggers might include:

* repeated identical failures
* repeated edit/revert cycles
* no meaningful diff progress
* decreasing test pass rate
* task scope growing substantially
* unexpected architecture boundary
* missing credentials
* destructive change required
* mission ambiguity
* agent confidence below threshold

The system should prefer an explicit:

```text
NEEDS HUMAN DECISION
```

over speculative architectural changes.

Other independent tasks can continue.

---

# 11. Deadline Awareness

The coordinator treats remaining time as a first-class execution variable.

The optimization target changes throughout the session.

An eight-hour run might be divided approximately into:

```text
0%                                      100%

| grounding | implementation | verification |
```

Early-stage behavior may allow:

* exploration
* competing hypotheses
* architecture investigation
* small experiments

Middle-stage behavior prioritizes:

* accepted implementation paths
* test creation
* integration

Late-stage behavior becomes conservative.

For example, once 20% of execution time remains:

```text
freeze new speculative branches
finish active viable changes
abandon incomplete experiments
run integration tests
run build
inspect diffs
cross-review
create coherent commits
produce handoff
```

The coordinator therefore optimizes not simply for progress, but for:

> maximum inspectable value at deadline.

---

# 12. Compute Scheduling

Deep Work changes the economics of Stint.

Interactive sessions value:

* token latency
* inference speed
* immediate availability
* developer responsiveness

Deep Work values:

* cost
* reliability
* sufficient model capability
* sustained throughput
* session duration
* restartability

A developer might request:

```bash
stint deep start \
  --hours 8 \
  --budget 3 \
  --workers 2 \
  mission.yaml
```

Stint may decide:

```text
Worker A
RTX 3090
$0.13/hr
8h estimated: $1.04

Worker B
RTX 3090
$0.14/hr
8h estimated: $1.12

Coordinator
local

Estimated total:
$2.16
```

## Historical evidence is not inventory

Persistence creates an opportunity to learn from completed compute attempts. Stint may accumulate observations about startup success, time to SSH, measured transfer speed, model-load time, disconnects, runtime stability, GPU class, region, provider, and other properties that were visible when an offer was rented.

Those observations describe **past compute**. They do not make a marketplace machine part of Stint's future inventory.

An offer seen during one session may be rented by someone else, repriced, relisted with different terms, reconfigured, removed permanently, or never appear again. A provider identifier may help correlate observations if the corresponding resource reappears, but persistence of an identifier is not evidence of current rentability.

The scheduling order must therefore remain:

```text
discover currently available offers
        ↓
apply current hard policy and current marketplace facts
        ↓
match relevant historical observations, when confidence is sufficient
        ↓
rank only the candidates that are available now
        ↓
revalidate the selected offer immediately before rental
```

Historical evidence may match at several levels: an exact provider machine identifier, a provider account, a GPU model, a region, a network or hardware cohort, a runtime configuration, or a combination of observable properties. Each match carries different identity confidence and staleness. Exact-machine evidence is potentially the strongest match, but even it is probabilistic: the host may have changed hardware, drivers, networking, workload, or operating conditions.

Unseen candidates remain first-class candidates. "No Stint history" means unknown, not bad. A future evidence-aware planner should use a conservative prior or cohort baseline, surface uncertainty, and preserve some opportunity to try new machines rather than creating a closed incumbent set.

Current marketplace state and hard safety/budget requirements always dominate history. Historical evidence can eventually act as a bounded, confidence-aware adjustment for outcomes such as successful startup probability, startup overhead, usable duration, network qualification, runtime reliability, or expected cost per usable compute hour. It must never revive an unavailable offer, bypass current policy, or turn an observed machine into a reservation assumption.

Stint should not implement this ranking until it has established:

* which provider identifiers are stable enough for correlation and within what scope
* which offer properties remain comparable after relisting or reconfiguration
* signal-specific staleness and decay from longitudinal observations
* minimum effective sample sizes and uncertainty behavior
* an explicit prior and exploration policy for unseen candidates
* a bounded weighting rule against current price, availability, verification, and measured properties
* offline replay evidence that the signal improves outcomes without systematically excluding new supply

Until those gates are met, Stint may retain append-only observations for later analysis, but live discovery and the existing current-offer ranking remain authoritative.

The optimal model may also differ from the interactive model.

For example:

```text
Interactive:
higher throughput model
short response latency
large active context

Deep Work:
slower but capable coding model
aggressive context reconstruction
checkpoint-driven execution
multiple concurrent workers
```

Stint may eventually benchmark providers and models using a metric closer to:

```text
verified tasks completed
------------------------
       dollar-hour
```

rather than raw tokens per second.

---

# 13. Compute Failure Recovery

Deep Work sessions should assume cloud compute is ephemeral.

Worker state must therefore be recoverable independently of the GPU instance.

If a provider disappears:

```text
worker-a GPU lost
        ↓
coordinator marks worker interrupted
        ↓
latest Git state preserved
        ↓
task/checkpoint state preserved
        ↓
Stint rents replacement instance
        ↓
model context rebuilt
        ↓
worker resumes T4
```

The architecture should avoid any essential session state living only on the rented machine.

At minimum, Stint should preserve:

* Git commits
* uncommitted patch where possible
* checkpoint
* task state
* evidence
* model configuration
* current task prompt
* execution logs

This also makes worker replacement possible when another model or provider is preferable.

---

# 14. Authority Model

Autonomy should be explicitly bounded.

A session has a capability policy.

Example:

```yaml
authority:
  git:
    read: true
    branch: true
    commit: true
    push: false
    merge: false

  filesystem:
    repository: write
    home: deny

  network:
    package_registries: allow
    arbitrary_hosts: restricted

  secrets:
    production: deny
    development: selected

  infrastructure:
    read: false
    mutate: false
```

The initial Deep Work implementation should be intentionally conservative.

The agent may freely make and undo changes inside its isolated repository workspace.

It should not independently gain authority to alter external systems.

Future versions could support approval policies such as:

```text
safe
developer
trusted
custom
```

but these should resolve to explicit capabilities rather than vague autonomy levels.

---

# 15. Final Handoff

The handoff is one of the primary outputs of the session.

A developer should be able to understand the overnight result without reading the agent transcript.

Example:

```text
STINT DEEP WORK
Session dw_01J8Y2

Mission
Spark evidence grounding

Duration
7h 58m

Compute
2 × RTX 3090

Cost
$2.21

Result
SUBSTANTIALLY COMPLETE

Changes
- Added evidence normalization layer.
- Moved normalization behind repository boundary.
- Preserved existing API representation.
- Added 14 behavioral tests.

Commits
5f21aa normalization types
82c34b repository integration
991de2 behavioral tests
81d993 API adapter

Verification
unit          94/94
integration   38/38
lint          pass
typecheck     pass
build         pass

Peer Review
worker-a → worker-b
3 findings, all resolved

worker-b → worker-a
2 findings
1 resolved
1 deferred

Needs Human Decision
Historical evaluations currently remain legacy-only.

Alternative:
synthesize evidence IDs for historical records.

Current implementation intentionally chooses the conservative path.

Rejected Approaches
Database migration
Reason: unnecessary and outside mission.

Eager route-level normalization
Reason: violated repository boundary.

Recommended Review
81d993..HEAD
```

The developer can then run:

```bash
stint deep inspect latest
stint deep diff latest
stint deep resume latest
stint deep accept latest
stint deep discard latest
```

The handoff should be machine-readable as well as human-readable so another interactive coding agent can continue directly from it.

---

# 16. Relationship to Spark

Deep Work should function without Spark.

A normal Git repository already contains substantial usable evidence:

* commits
* diffs
* pull requests
* tests
* CI
* code ownership
* file structure
* package boundaries
* configuration

Stint should derive a usable execution model from those primitives.

Spark can later provide a richer intelligence layer.

Conceptually:

```text
              repository
                  │
          ┌───────┴────────┐
          ↓                ↓
        Spark             Stint
  repository meaning   execution control
          │                │
          └───────┬────────┘
                  ↓
              Deep Work
                  ↓
              changes
                  ↓
                Spark
```

Spark may eventually provide:

* repository health signals
* change trajectories
* historical risk
* architectural claims
* conventions
* recurring failure patterns
* sensitive boundaries
* prior agent performance
* probable blast radius

Stint can use this intelligence to improve:

* task planning
* scope constraints
* worker assignment
* review depth
* verification strategy
* escalation thresholds

The boundary should remain:

> Spark describes and evaluates repository state.
> Stint allocates intelligence and executes bounded work.

---

# 17. CLI Direction

A possible initial interface:

```bash
stint deep plan mission.yaml
```

Produces:

```text
MISSION
Spark evidence grounding

ESTIMATED WORKERS
2

ESTIMATED COMPUTE
$2.10–$2.80

PROPOSED MODELS
qwen3.8-27b

EXPECTED SESSION
8h

AUTHORITY
local repository writes
Git commits
tests/builds

NO
push
merge
deploy
production access
```

Start:

```bash
stint deep start mission.yaml
```

Status:

```bash
stint deep status
```

Potential output:

```text
SESSION          dw_01J8Y2
ELAPSED          03:14 / 08:00
COST             $0.88 / $3.00

WORKER A
T4 Normalization implementation
running

WORKER B
T5 Behavioral tests
running

TASKS
3 verified
2 active
3 pending

LATEST CHECKPOINT
CP-07  4m ago

RISKS
1 medium
0 high
```

Inspect while running:

```bash
stint deep watch
```

Resume:

```bash
stint deep resume dw_01J8Y2
```

Terminate safely:

```bash
stint deep stop
```

The stop operation should checkpoint work before terminating rented compute.

---

# 18. Initial Architecture

A reasonable implementation boundary could look like:

```text
packages/
├── compute/
│   ├── marketplace
│   ├── providers
│   └── leases
│
├── deep/
│   ├── coordinator
│   ├── missions
│   ├── tasks
│   ├── workers
│   ├── checkpoints
│   ├── evidence
│   ├── authority
│   └── handoff
│
├── git/
│   ├── worktrees
│   ├── patches
│   └── commits
│
└── runtime/
    ├── llama
    ├── ninfer
    └── openai-compatible
```

The Deep Work coordinator should not know implementation details of Vast, llama.cpp, or NInfer.

It should request capabilities from the compute layer:

```ts
interface WorkerRequirement {
  model: string;
  minVramGb: number;
  minSessionHours: number;
  maxHourlyPrice?: number;
  networkPolicy?: NetworkPolicy;
}
```

The compute scheduler returns a worker endpoint:

```ts
interface WorkerLease {
  id: string;
  endpoint: string;
  provider: string;
  expiresAt: Date;
  release(): Promise<void>;
}
```

The agent layer then operates against an OpenAI-compatible model abstraction.

This preserves Stint's ability to swap:

* Vast
* local GPUs
* homelab machines
* future marketplaces
* different inference runtimes

without modifying the Deep Work orchestration model.

---

# 19. Minimal Viable Deep Work

The first version does not need a sophisticated multi-agent research system.

A useful V1 could contain only:

```text
1 coordinator
2 workers
Git worktrees
explicit task list
periodic checkpoints
test execution
cross-review
deadline awareness
final handoff
```

Specifically:

### V1

* User provides Markdown mission.
* Stint rents one or two workers.
* Stint creates isolated worktrees.
* Coordinator produces initial task decomposition.
* Workers execute tasks sequentially or concurrently.
* After every task:

  * tests run
  * checkpoint generated
  * commit created
* Second worker reviews changes.
* Coordinator stops speculative work near deadline.
* Full test/build verification runs.
* Final `handoff.md` is generated.
* Compute is destroyed.

No Spark dependency.

No production deployment.

No automatic merge.

No generalized distributed agent protocol.

No complicated planner hierarchy.

The initial objective is to answer one question:

> Can Stint reliably convert eight hours of unattended rented compute into engineering work that a developer is comfortable reviewing the next morning?

If the answer becomes consistently yes, the deeper orchestration system can grow from evidence rather than speculation.

---

# 20. Long-Term Direction

The deeper opportunity is not merely autonomous coding.

Stint could become a runtime for allocating engineering intelligence.

Instead of thinking in terms of:

```text
rent GPU
```

the abstraction becomes:

```text
allocate 8 hours of engineering effort
```

Stint controls four resources:

```text
COMPUTE
Which machines and models perform the work?

TIME
How much autonomous effort is available?

AUTHORITY
What may that intelligence change?

EVIDENCE
What must exist before work is accepted?
```

From that model, multiple modes naturally emerge:

```text
stint interactive
Human-driven, low-latency coding.

stint deep
Long-duration autonomous engineering.

stint investigate
Repository analysis without changes.

stint review
Independent examination of a branch or PR.

stint repair
Bounded attempt to resolve CI or regressions.

stint maintain
Recurring repository health work.
```

All of them can eventually share the same primitives:

```text
missions
workers
compute leases
task graphs
authority
evidence
checkpoints
handoffs
```

This makes Deep Work more than an additional CLI command.

It becomes the architectural point where Stint evolves from a temporary GPU launcher into a system for managing bounded autonomous engineering.
