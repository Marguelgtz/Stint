# Spark–Stint MCP MVP — living execution graph

Policy: `docs/missions/living-mission-execution-policy.md`.
Mission objective: `docs/missions/spark-stint-status-mcp-mvp.md`.

## Current graph

- Objective A — expose Stint's persisted Deep run status through a validated local MCP provider.
  - `STINT-STATUS-MCP-001` — queued for this run; acceptance and verification are in the mission.
  - PR stack — based on Stint #183 (`20177e788e5fd190fb4d82a2535bad930a7d8732`).
- Objective B — Spark consumes and persists Stint status over MCP.
  - Proposed follow-up in the Spark repository, based on the existing MCP stack #97–#101.
  - Dependency: Objective A's versioned projection and a cross-repository fixture.
  - Blocking: complete Spark–Stint integration MVP; non-blocking for independently accepting Objective A.
- Human attention — none at mission start. If secure Spark persistence requires new credentials, deployment authority, or a data-boundary decision, record the evidence and options before proceeding; do not invent credentials or deploy.

## Provenance

This graph starts from the previous Spark MCP work: #97 action-plan checkpoint, #98 `evaluate_change` stdio server, #100 independent review, and #101 final handoff. That server is caller-input-driven and has no Stint status persistence. The failed Stint mission `20260930-013503` attempted a single broad MCP status task and timed out twice. This run narrows the Stint objective to a status provider over already-durable state; Spark persistence remains a separate, explicit dependent workstream because Deep Work stages one target repository per run.

Update this graph at each major checkpoint. Record trigger evidence, source checkpoint, reason, acceptance impact, dependencies, superseded paths, and human-attention items for each material change.
