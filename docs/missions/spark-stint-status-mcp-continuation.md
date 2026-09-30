# Finish the Stint Deep status MCP provider

## Objective
Finish and qualify the Stint-side local status provider for the Spark–Stint MCP MVP. The starting tree contains Hermes-authored, uncheckpointed work preserved from Deep run `20260930-171348`. That run landed incomplete after two 30-minute executor timeouts and two 180-second task-verifier timeouts. Treat the code as an unaccepted draft, inspect it, repair it as needed, and produce a new verified checkpoint. Do not implement Spark-side consumption or persistence in this Stint run.

## Success
- `stint mcp serve --session <ID>` exposes exactly one stdio tool, `deep_run_status`, with no resources or prompts.
- Validate the session ID before opening state. Load status through Stint's validated durable journal/projection path. Missing or corrupt state yields a tool error.
- Return one JSON text item with the versioned, allow-listed run and bounded task fields described in `docs/missions/spark-stint-status-mcp-mvp.md`. Preserve separate phase and mission outcome. Cap the reply at 64 KiB and fail clearly on overflow.
- Exclude mission prose, prompts, verifier commands, worker output, local paths, provider data, credentials, and raw journal events. The server must make no provider calls, SSH calls, Git changes, publication, or new status store writes.
- Prove the protocol with an MCP client, including discovery, normal and resumed journaled runs, invalid/missing/corrupt state, bounds, secret exclusion, and clean stdio client shutdown. Fix any test harness errors rather than weakening the contract.
- Document local stdio client configuration and the separate Spark consumer/persistence follow-up. Update the living action plan with the prior failed path, the new checkpoint, verification, and remaining dependency.
- Deliver a Git-visible change beyond the preserved draft, a passing focused task gate, a passing acceptance check, and a passing full final mission verification.

## Constraints
- Build on the A–D Deep Work stack and the preserved Hermes draft. Preserve journal, acceptance, review, checkpoint, and outcome semantics.
- Keep this provider local and read-only. Do not change Spark's `evaluate_change` server or claim the cross-repository MVP complete.
- Keep stdout exclusively for MCP protocol frames and diagnostics on stderr.
- Use the official Go MCP SDK at a version compatible with Go 1.23.
- Treat `docs/missions/living-mission-execution-policy.md` as the governing execution policy. The action plan is provisional and requires provenance for material changes.
- The coordinator's per-task verifier has a fixed 180-second bound. Use a focused task command that completes within it; reserve the full suite and race check for final mission verification, which has a separate 10-minute bound. Do not weaken or skip final verification.

## Verification
go test ./internal/deep ./cmd/stint && go vet ./internal/deep ./cmd/stint && go test -race ./internal/deep ./cmd/stint && git diff --check

## Acceptance Contract
version: 2

## Semantic Review Contract
version: 2

## Tasks
- [ ] STINT-STATUS-MCP-FINISH-001: Finish, test, and document the preserved Hermes draft of the local `deep_run_status` provider.
  - acceptance: A real MCP client lists exactly one tool and retrieves validated, durable status with separate phase/outcome; resumed journal state is observable; invalid/missing/corrupt state, oversized output, and secret-bearing fixture text fail closed or are excluded as specified. The stdio subprocess exits cleanly when the client disconnects. The living plan and client instructions are Git-visible.
  - verify: go test ./internal/deep -run 'Test(BuildStatusReply|ValidateStatusSessionID|StatusReply)' && go test ./cmd/stint -run 'Test.*MCP' && go vet ./internal/deep ./cmd/stint && git diff --check
  - repository-change: required
  - acceptance-check: go test ./cmd/stint -run 'Test.*MCP'
  - reasoning: medium

## GitHub
- mode: engineering
- repository: Marguelgtz/Stint
- base: codex/deep-d4-mission-review
- approval: internal
