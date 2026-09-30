# Spark–Stint MCP MVP — living action plan

Policy: `docs/missions/living-mission-execution-policy.md` (governing; plan is provisional).
Mission objective and acceptance criteria: `docs/missions/spark-stint-status-mcp-mvp.md` (authoritative).
Run: `20260930-171348`, branch `stint/deep-20260930-171348`, base head `21cd8403` (on-box binary swap fix). Stint #183 stack (base `20177e78`) is an ancestor of HEAD; all Deep Work A–D state, journal, acceptance, review, and outcome contracts are already in this tree.

## Current graph

- Objective A (authorized, this run) — expose Stint's persisted Deep run status through a validated, bounded local stdio MCP provider.
  - `STINT-STATUS-MCP-001` — in verification (attempt 2); single work unit, repository change required, verification per mission.
    - A1 — plan checkpoint (this file, STINT-PLAN-001): done (v2 at plan time, v3 status update below).
    - A2 — Go MCP provider: done in this run — `internal/deep/status_provider.go` (projection, bounds, session-ID validation) + `internal/deep/status_provider_test.go`; `cmd/stint/mcp_status.go` (`stint mcp serve --session <ID>`, help entry in `cmd/stint/help.go`, dispatch in `cmd/stint/main.go`) + `cmd/stint/mcp_status_test.go` (protocol-level: in-memory SDK client + subprocess stdio client of the built binary).
    - A3 — documentation: done — `docs/mcp/status-provider.md` (local stdio client configuration + explicit Spark-side follow-up: consume and persist `deep_run_status`; separate repository workstream, blocking only the cross-repository MVP claim).
  - PR stack: A2 -> A3 (one coherent stack, independently landable).
- Objective B (separate repository workstream, NOT authorized in this run) — Spark consumes and persists Stint status over MCP, building on `Marguelgtz/spark-observability-test` PRs #97–#101 (the `evaluate_change` stdio server stays as-is; do not recreate or modify it).
  - Dependency: Objective A's `schemaVersion: 1` projection and a cross-repository fixture.
  - Blocking: only the complete cross-repository MVP claim. Non-blocking for independently accepting Objective A.
- Human attention — none at plan time. Escalate (with evidence and options) if SDK/network resolution, state-directory authority, or a journal-schema question blocks acceptance.
- Proposed / not authorized — nothing new discovered at plan time.

## Decisions recorded at plan time

1. SDK choice: official `github.com/modelcontextprotocol/go-sdk`, **v1.3.0** — latest release whose module `go` line is `go 1.23.0`, matching the repository's `go 1.23` baseline. Checked against cached module metadata: v1.0.0/v1.1.0/v1.2.0 are also go 1.23-compatible; v1.4.1 and v1.8.0 declare `go 1.25.0` and are excluded. Evidence: module `.mod` files under the local module cache plus `go list -m -versions` (Go 1.23 baseline from `go.mod`).
2. Command surface: `stint mcp serve --session <ID>` — new top-level `mcp` group (registry entry in `cmd/stint` help/dispatch). `--session` is required and validated (session ID format `YYYYMMDD-HHMMSS`, see `deep.NewSessionID`) **before any state path is opened**; invalid or missing selection fails before `LoadState` runs.
3. State path: load through the existing validated path `deep.LoadState(stateDir, sessionID)` (`internal/deep/state_persist.go:90`), which takes the run-state lock, replays/rebuilds the projection from the durable journal (`recoverProjectionLocked`), and validates acceptance/review/repair contracts. Missing or corrupt state therefore surfaces as an error → MCP tool error, never an unvalidated success projection.
4. No competing status store: the provider reads `DeepState` only. No new persistence, no journal/checkpoint schema or semantics changes, no `evaluate_change` rework, no provider/SSH/git-mutation/publication/replay side effects. The provider performs no writes to state paths other than whatever `LoadState`'s existing recovery path itself does (it may persist a corrected projection — that is the existing, validated behavior, not new store state).
5. Projection (allow-listed fields only; empty evidence omitted via `omitempty`):
   - top level: `schemaVersion: 1`, `sessionId`, `runId`, `executionEpochId`, `runEventWatermark`, `phase`, `missionOutcome`, `deadline` (RFC3339), `landingCommit`, `landingCheckpointTreeSha`, `missionReviewOutcome`;
   - task rows (bounded count, e.g. cap rows and fail-clearly on overflow rather than truncate silently): `id`, `source`, `status`, `attempts`, `checkpointCommit`, `checkpointTreeSha`, `acceptanceOutcome`, `reviewOutcome`.
   - `missionOutcome` is projected via `deep.DisplayMissionOutcome(state.MissionOutcome, state.Phase)` (safe label); `phase` stays raw. `landed` (operational boundary) and `missionOutcome: succeeded` (completion evidence) remain separate fields — preserved by construction.
   - Excluded by construction: mission name/objective/success/constraints prose, `Verify`/`acceptanceCheck` commands, worker output, `repoPath`/`worktreePath`/branch/handoff paths, compute/provider/credential fields, raw journal events, review findings.
6. Reply bound: single JSON text content item; marshal the safe projection, enforce ≤ 64 KiB, and return a clear MCP tool error if it exceeds the cap (no silent truncation).
7. Protocol discipline: stdio transport only (SDK `mcp.StdioServer`), stdout reserved for MCP frames, all diagnostics to stderr.
8. Tests (protocol level, in-repo, `Test.*MCP` pattern for the acceptance check `go test ./cmd/stint -run 'Test.*MCP'` plus `internal/deep` units): use the SDK client over an in-memory transport pair for discovery (exactly one tool `deep_run_status`, zero resources/prompts) and invocation; stdio smoke test of `stint mcp serve` via the SDK client where practical. Scenarios: tool discovery; representative journaled run (seeded via `BeginNewRun`/`BeginLanding`/`CompleteLanding` like `internal/deep/run_events_test.go:15` `journalFixture`); recovery across a new epoch (`BeginResumeEpoch` then a fresh process-level load — proves the status observation survives restart/epoch recovery from durable state); missing session; corrupt `deep.json`; invalid `--session` selection (bad format → fail before path open); >64 KiB safe projection → clear failure; secret-bearing fixture text (mission prose, paths, credential-like tokens) absent from every tool result.

## Risks

- SDK network dependency: `go get github.com/modelcontextprotocol/go-sdk@v1.3.0` plus transitive deps (jsonschema-go, oauth2, tools, uritemplate, jwt) must resolve via the module proxy and produce `go.sum`. If the box cannot reach the proxy, record the failure and options (proxy/mirror) — do not vendor from untrusted sources or fake sums.
- API drift: pin to v1.3.0 API surface (server.NewToolServer / stdio server / in-memory transports) as verified against the actual module sources at implementation time; do not code from memory.
- `LoadState` under a live coordinator: the provider shares the existing run-state lock; concurrent coordinator writes are safe because the provider never writes lifecycle state itself.
- Oversized-projection test must be constructed from legitimately large but safe fields (many tasks / long task IDs), never from secret prose, so the exclusion test stays independent.
- Epoch-recovery proof: must restart the *observation* (new load through the same validated path), not merely re-read in-process, to match "after process restart and epoch recovery".
- Scope guard: anything touching Spark, D1/API persistence, credentials, or journal semantics is out of scope and must become a separate workstream/human-attention item, not an absorbed change.

## Next steps (execution order)

1. A2a: add SDK dependency, verify `go build` with the Go 1.23 baseline (toolchain 1.27.1 is installed; the `go 1.23` module line governs).
2. A2b: projection + bounds + redaction in `internal/deep` (unit tests).
3. A2c: `stint mcp serve` command in `cmd/stint` (help registry entry, dispatch, required `--session` validation before path open; stdout/stderr discipline).
4. A2d: protocol-level tests (MCP client over in-memory + stdio transports) covering every mission-listed scenario; acceptance probe `go test ./cmd/stint -run 'Test.*MCP'`.
5. A3: documentation — local stdio client configuration (command + args, environment for the state dir if applicable) and the explicit Spark-side follow-up (consume `deep_run_status`, persist per Spark policy, cross-repo fixture) named as a separate workstream.
6. Full verification per mission: `go test ./internal/deep ./cmd/stint && go vet ./internal/deep ./cmd/stint && go test -race ./internal/deep ./cmd/stint && git diff --check` (plus `go test ./...` sanity if time permits).

## Evidence pointers (as of plan time)

- Mission contract: `docs/missions/spark-stint-status-mcp-mvp.md` (success/constraints/verification/acceptance contract v2).
- Governing policy: `docs/missions/living-mission-execution-policy.md`.
- Durable state and load path: `internal/deep/state.go` (`DeepState` fields incl. `RunID`, `ExecutionEpochID`, `RunEventWatermark`, `LandingCommit`, `LandingCheckpointTreeSHA`, `MissionOutcome`, `MissionReviewOutcome`), `internal/deep/state_persist.go:90` (`LoadState` → `recoverProjectionLocked`), `internal/deep/run_events.go` (`BeginNewRun`, `BeginResumeEpoch`, `CompleteLanding`).
- Task row sources: `internal/deep/task.go` (`Task` fields incl. `Source`, `Status`, `Attempts`, `CheckpointCommit`, `CheckpointTreeSHA`, `AcceptanceOutcome`, `ReviewOutcome`).
- CLI dispatch/help conventions: `cmd/stint/main.go`, `cmd/stint/deep_start.go:runDeep`, `cmd/stint/help.go` (command registry), `cmd/stint/deep_status.go` (existing human-readable status; its `--json` verbatim dump must NOT be exposed by the provider).
- Test seeding pattern: `internal/deep/run_events_test.go:15` (`journalFixture` + journal lifecycle across resume epochs).
- Base lineage: `20177e78` (Stint #183 tip, "require journaled executor admission canary") verified as an ancestor of HEAD `21cd8403`.
- Prior context: failed Stint mission `20260930-013503` (single broad MCP status task, two timeouts) — this run keeps the provider narrow and read-only.

## Provenance

- v1 (previous run `20260930-013503`): initial graph with Objective A queued as a single broad task; mission failed/timed out — path superseded, recorded, not erased.
- v2 (this plan, STINT-PLAN-001, run `20260930-171348`, checkpoint `21cd8403`):
  - Triggering evidence: mission file and repository state read at plan time (SDK version matrix checked against the Go 1.23 baseline; `LoadState` recovery path confirmed in-tree; journal fixture pattern confirmed reusable).
  - Change: split the single broad task into A2 (implementation + protocol tests) and A3 (documentation), record the SDK version decision with evidence, list risks and the ordered next steps, and keep Objective B explicitly out of this run.
  - Reason: the previously failed run shows a broad single task times out; a narrower, evidence-pinned plan with an ordered execution sequence reduces rework and keeps Objective A independently landable.
  - Acceptance impact: none — mission objective, success criteria, and constraints are unchanged.
  - Dependencies: Objective B depends on Objective A's projection; nothing in this run blocks on Objective B.
- v3 (this run, STINT-STATUS-MCP-001 attempt 2, same branch):
  - Triggering evidence: execution of A2/A3 against the in-tree contracts — SDK v1.3.0 API verified from module sources (`mcp.NewServer`/`AddTool`/`NewInMemoryTransports`, `Server.Run`+`StdioTransport`); `LoadState` confirmed to require the durable `deep.json` projection and to replay the journal over it (`internal/deep/state_persist.go`); SDK surfaces a clean stdio client disconnect as a wrapped "server is closing" error, so `runMCPCommand` maps that plus `io.EOF`/`mcp.ErrConnectionClosed` to a clean exit 0 (`isCleanStdioShutdown`, verified with a live binary probe over stdio).
  - Change: implemented `internal/deep/status_provider.go` (allow-listed projection, 64 KiB reply cap, 512-row bound, session-ID validation tightened to alnum/`-`/`_` so a crafted id cannot traverse paths — dots are legal in task IDs and were removed from the session-ID charset) and `cmd/stint/mcp_status.go` (single `deep_run_status` tool, no resources/prompts advertised, tool-only capability, stderr-only diagnostics); protocol-level tests in both packages (in-memory SDK client for discovery/projection/fail-closed cases; subprocess stdio test of the built binary for discovery, projection, cap, and secret/path exclusion); documentation in `docs/mcp/status-provider.md` including the local stdio client configuration and the explicit Spark-side consume+persist follow-up.
  - Reason: completes STINT-STATUS-MCP-001 as an independently reviewable, Git-visible, local read-only provider; no journal/checkpoint semantics, journal schema, A–D contracts, or Spark-side behavior were changed.
  - Acceptance impact: all Stint-side acceptance criteria now have in-repo evidence; the cross-repository MVP claim remains blocked on Objective B (Spark consumption/persistence) per the mission contract.
  - Dependencies: none new; Objective B still depends on this provider's `schemaVersion: 1` projection.
