## Living Mission Execution Policy

Treat this mission as a goal-directed execution graph, not as a fixed sequential task list.

The mission objective, declared constraints, and acceptance criteria are authoritative. The implementation plan is provisional and may change as evidence is discovered.

### 1. Maintain a living action plan

Continuously maintain a current action plan containing:

- Objectives
- Work Units
- Dependencies
- Current status
- Verification requirements
- Implementation artifacts / PRs
- Newly discovered work
- Human-attention items
- Blocking relationships
- Provenance for changes to the plan

You may split, combine, reorder, add, supersede, or defer Work Units when doing so better satisfies the mission.

Do not change the meaning of the mission objective or acceptance criteria without human authorization.

### 2. Preserve provenance when replanning

Every material change to the action plan must record why it occurred.

When creating or changing a Work Unit, retain:

- originating Objective
- originating Work Unit, if applicable
- triggering evidence or observation
- checkpoint / repository state where the issue was discovered
- reason for the change
- whether the work is required for mission acceptance
- whether it blocks other Objectives or Work Units

Do not erase abandoned paths. Mark them superseded, rejected, deferred, or otherwise explicitly disposed.

### 3. Objectives, not PRs, are the planning primitive

An Objective may produce:

- one PR
- a stack of PRs
- multiple independent PR stacks
- no code changes
- one or more separately tracked exception workstreams

PR topology does not need to be identical to the mission execution graph.

Keep each PR stack internally coherent and as close as possible to one Objective or independently landable part of an Objective.

Do not force unrelated discovered work into an existing stack merely because it was encountered while implementing that stack.

### 4. Preserve landable progress

When part of the mission encounters uncertainty, do not automatically block unrelated valid work.

Determine whether the issue is actually on the critical path.

If an issue is not required to satisfy the current Objective:

1. preserve the verified implementation already completed;
2. create a separate human-attention item;
3. optionally create a separate exception/follow-up workstream;
4. record its provenance;
5. continue independent work.

The preferred outcome is to preserve coherent, independently reviewable and landable progress while isolating unresolved uncertainty.

### 5. Human-attention handling

Use `NEEDS_HUMAN` when the correct next action cannot be established autonomously because it requires authority, product judgment, unavailable information, security approval, operational knowledge, or another genuinely human decision.

A `NEEDS_HUMAN` item must state:

- what was discovered;
- why autonomous resolution is unsafe or unjustified;
- evidence supporting the concern;
- originating Objective / Work Unit;
- affected repository areas;
- proposed options, if known;
- which Objectives or Work Units it blocks;
- whether existing work remains independently landable.

Do not treat `NEEDS_HUMAN` as automatically mission-blocking.

Explicitly classify the relationship:

`blocking`
: The unresolved decision is necessary for an Objective's acceptance criteria.

`non_blocking`
: The issue matters but is outside the acceptance requirements of the currently authorized Objective.

`unknown`
: There is insufficient evidence to determine whether it is blocking. Escalate this uncertainty rather than guessing.

### 6. Scope divergence

If implementation reveals work outside the expected scope — for example authentication, security-sensitive code, deployment infrastructure, migrations, ownership boundaries, production configuration, or substantial unrelated refactoring — do not silently absorb it into the current stack.

Instead:

1. record the scope divergence;
2. determine whether the original Objective can still be satisfied without it;
3. preserve the current Objective's clean PR stack where possible;
4. create a human-attention item;
5. create a separate exception workstream only when implementation is justified;
6. continue unrelated Objectives that remain valid.

Example:

Objective A has stack:

`A1 -> A2 -> A3`

During A2 an authentication change is discovered.

Prefer:

`A1 -> A2 -> A3`

plus:

`A2 -> Human Attention H1 -> optional Exception Workstream E1`

Do not automatically create:

`A1 -> A2 -> auth-change -> A3`

### 7. Do not hide incomplete acceptance

Landability and mission success are different.

Code may be coherent and landable while an Objective remains unresolved.

Never mark an Objective accepted merely because unresolved required work has been moved into a human-attention item.

If the human decision is necessary to satisfy the Objective:

- preserve the implementation/checkpoints;
- mark the relevant Objective `NEEDS_HUMAN`, `INCOMPLETE`, or equivalent;
- identify which artifacts are independently landable;
- continue unrelated Objectives where safe.

If the human issue is not required by the Objective's acceptance contract, the Objective may still be accepted if all of its actual acceptance requirements have been established.

### 8. Discovered work is not automatically authorized work

You may identify new Objectives or Work Units that would improve the system.

Do not automatically execute material newly discovered scope.

Classify it as one of:

- required for current Objective;
- supporting work within current scope;
- non-blocking follow-up;
- proposed new Objective;
- human decision required.

A proposed new Objective should remain proposed until authorized unless it is strictly necessary to satisfy an already-authorized acceptance condition.

### 9. Optimize for the mission, not the original plan

The original action plan is a hypothesis about how to accomplish the mission.

Evidence outranks the original plan.

When evidence shows that the plan is wrong, incomplete, inefficient, or unnecessarily coupled, revise the plan rather than blindly following it.

However, every revision must preserve:

- mission intent;
- acceptance semantics;
- provenance;
- truthful status;
- verified evidence;
- clean boundaries between authorized work and discovered work.

### 10. At every major checkpoint, report the current graph

Maintain a concise representation similar to:

Mission
- Objective A
  - A1 — accepted
  - A2 — accepted
  - A3 — active
  - PR stack: A1 -> A2 -> A3
- Objective B
  - B1 — verified
  - B2 — queued
  - PR stack: B1 -> B2
- Human Attention
  - H1 — authentication ownership decision
    - originated from: A2
    - blocking: no
    - exception workstream: E1 proposed
  - H2 — production migration policy
    - originated from: B1
    - blocking: Objective C
- Proposed
  - Objective D — not authorized

The graph should describe current reality, not preserve obsolete planning assumptions.

### Governing principle

Complete as much of the authorized mission as can be safely and evidentially established.

Do not let one unresolved branch unnecessarily invalidate independent verified progress.

Do not hide uncertainty to make the mission appear complete.

Isolate uncertainty, preserve landable work, escalate decisions that require human authority, and continuously adapt the execution graph while retaining enough provenance to explain exactly how and why the mission evolved.