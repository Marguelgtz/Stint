# Expose Spark's deterministic evaluator through MCP

## Objective
Add a minimal stdio MCP server to the current `spark-opp/spark` main that exposes the existing deterministic `evaluateChange(SparkInput): SparkEvaluation` capability as one Hermes-callable tool. The server must evaluate only the input supplied by the caller.

## Success
- Add a runnable `apps/mcp` package that registers exactly one tool named `evaluate_change`.
- Validate the complete `SparkInput` shape at the MCP boundary and return the existing `SparkEvaluation` shape from `@spark/core`; do not change evaluator behavior to satisfy the adapter.
- Add protocol-level tests that launch the stdio server through an MCP client, verify that exactly one tool is listed, verify a representative fixture result against direct `evaluateChange` output, and verify malformed input is rejected.
- Ensure server stdout contains only MCP protocol output; send diagnostics to stderr.
- Add `apps/mcp/README.md` with install/build/start instructions and an example Hermes stdio configuration that allowlists only `evaluate_change`.
- Document that the tool uses only caller-supplied values: it does not fetch GitHub or other evidence, authenticate, persist data, or mutate repositories.
- Deliver a non-documentation code diff, focused tests, full workspace tests and typecheck, and a reviewable change on a branch/PR from `main`.

## Constraints
- Keep the implementation read-only and limited to this single existing evaluator capability.
- Do not change Spark's evaluator, API, web app, database, or V0 behavior.
- Do not add GitHub fetching, evidence verification, agent execution, steering or Laya policy, Langfuse, persistence, or a broader Spark/Hermes/Laya loop.
- Use the official MCP TypeScript SDK and the repository's existing TypeScript/pnpm conventions.
- Deep Work tasks run serially. Two configured NInfer clients do not authorize task parallelism.

## Verification
npx --yes --package=pnpm@10 pnpm install --frozen-lockfile && npx --yes --package=pnpm@10 pnpm test && npx --yes --package=pnpm@10 pnpm typecheck && git diff --check

## Tasks
- [ ] MCP-001: Implement the `apps/mcp` stdio server, `evaluate_change` input validation and output, focused end-to-end MCP client tests, and package README.
  - acceptance: The implementation has actual server and test code; tests prove exact tool listing, valid fixture parity with direct `evaluateChange`, and invalid-input rejection. The server does not perform network, persistence, or repository writes.
  - verify: npx --yes --package=pnpm@10 pnpm install --frozen-lockfile && npx --yes --package=pnpm@10 pnpm --filter @spark/mcp test && npx --yes --package=pnpm@10 pnpm typecheck && git diff --check
  - reasoning: medium
- [ ] MCP-REVIEW-001: Independently review the completed MCP implementation, its protocol tests, scope, and full verification; fix any material findings before landing.
  - depends-on: MCP-001
  - acceptance: The implementation prerequisite is verified; review inspects the actual diff and confirms the tool delegates to `@spark/core` without adding hidden fetching or side effects. Record review findings and their resolution; do not accept an absent or documentation-only implementation.
  - verify: npx --yes --package=pnpm@10 pnpm test && npx --yes --package=pnpm@10 pnpm typecheck && git diff --check
  - reasoning: medium

## GitHub
- mode: engineering
- repository: spark-opp/spark
- base: main
- approval: internal
