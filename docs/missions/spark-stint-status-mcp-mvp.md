# Stint Deep run status provider for the Spark–Stint MCP MVP

## Objective
Extend the current Stint Deep Work stack at PR #183 with a local, read-only stdio MCP server that exposes the selected Deep run's validated, durable status. This is the Stint provider slice of the Spark–Stint MCP MVP. Build on the Spark MCP implementation already present in `Marguelgtz/spark-observability-test` PRs #97–#101; do not recreate Spark's `evaluate_change` server or claim the cross-repository integration is complete until Spark-side status consumption and persistence are verified.

## Success
- Add `stint mcp serve --session <ID>` with exactly one tool named `deep_run_status`; expose no MCP resources or prompts.
- Validate the session ID before opening any state path. Load the run through Stint's existing validated Deep Work state and journal recovery path. Missing or corrupt state returns a tool error, never an unvalidated success projection.
- Return one JSON text content item with `schemaVersion: 1`, `sessionId`, `runId`, `executionEpochId`, `runEventWatermark`, `phase`, `missionOutcome`, `deadline`, `landingCommit`, `landingCheckpointTreeSha`, `missionReviewOutcome`, and bounded task rows. Each task row contains only `id`, `source`, `status`, `attempts`, `checkpointCommit`, `checkpointTreeSha`, `acceptanceOutcome`, and `reviewOutcome`. Omit empty evidence fields. Preserve the distinction between `landed` and a successful mission outcome.
- Cap replies at 64 KiB and fail clearly if the safe projection exceeds the cap.
- Never expose mission prose, prompts, verifier or acceptance commands, worker output, local paths, provider credentials, or raw journal events.
- Read status from Stint's existing durable journal/projection; do not create a competing status store or change journal/checkpoint semantics. Prove that a status observation remains available after process restart and epoch recovery.
- Keep the server local and read-only: no provider calls, SSH, Git mutation, publication, replay command execution, or remote transport.
- Add protocol-level tests using an MCP client for tool discovery, a representative journaled run, recovery across a new epoch, missing/corrupt state, invalid session selection, bounded output, and secret-bearing fixture text exclusion.
- Document a local stdio client configuration and the explicit Spark-side follow-up needed to consume and persist this provider's status.
- Deliver a Git-visible Go implementation, focused acceptance probe, Go tests, vet, and race checks.

## Constraints
- Build on the existing A–D Deep Work state, journal, acceptance, review, and outcome contracts at PR #183. Do not change their decisions, journal schema, or checkpoint semantics.
- Keep this provider local and read-only. Do not fetch Spark evidence, add D1/API persistence, introduce credentials, or change Spark's `evaluate_change` evaluator.
- Do not expose `stint deep status --json` verbatim; return only the allow-listed projection above.
- Keep stdout exclusively for MCP protocol messages and diagnostics on stderr.
- Use the official Go MCP SDK at a version compatible with the repository's Go 1.23 baseline.
- Treat `docs/missions/living-mission-execution-policy.md` as the governing execution policy for this mission. The plan is provisional; preserve provenance for every material replan.
- The Spark-side consumer/persistence work is a separate repository workstream and remains a blocking dependency for claiming the complete cross-repository MVP. Preserve this Stint provider as independently reviewable progress.

## Verification
go test ./internal/deep ./cmd/stint && go vet ./internal/deep ./cmd/stint && go test -race ./internal/deep ./cmd/stint && git diff --check

## Acceptance Contract
version: 2

## Semantic Review Contract
version: 2

## Tasks
- [ ] STINT-STATUS-MCP-001: Implement the validated, bounded `deep_run_status` stdio MCP provider over durable Deep Work state.
  - acceptance: A protocol-level MCP client lists exactly one tool, retrieves the selected run's current durable phase and separate mission outcome, and proves that secret-bearing fixture prose and local paths are absent. Tests prove invalid selection and missing/corrupt state fail closed, and resumed journal state is observable without creating duplicate persistence. The implementation and client instructions are Git-visible.
  - verify: go test ./internal/deep ./cmd/stint && go vet ./internal/deep ./cmd/stint && go test -race ./internal/deep ./cmd/stint && git diff --check
  - repository-change: required
  - acceptance-check: go test ./cmd/stint -run 'Test.*MCP'
  - reasoning: medium

## GitHub
- mode: engineering
- repository: Marguelgtz/Stint
- base: codex/deep-d4-mission-review
- approval: internal
