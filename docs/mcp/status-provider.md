# Stint Deep run status MCP provider (local, read-only)

This document is the Stint-side slice of the Spark–Stint MCP MVP. Stint
exposes the selected Deep run's validated, durable status as a **local,
read-only stdio Model Context Protocol (MCP) server**:

```
stint mcp serve --session <ID>
```

The server exposes **exactly one tool**, `deep_run_status`, and **no MCP
resources or prompts**. It reads status from Stint's existing durable Deep
Work state and journal recovery path (the same `LoadState`/journal-replay
path the coordinator uses). It creates **no competing status store**,
changes **no journal or checkpoint semantics**, and makes **no provider
calls, no SSH, no Git mutation, no publication, and no remote transport**.
stdout carries only MCP protocol frames; diagnostics go to stderr.

## Reply contract

`deep_run_status` takes no arguments (the session is bound at process start
via `--session`) and returns **one JSON text content item** with
`schemaVersion: 1`:

| Field | Meaning |
| --- | --- |
| `schemaVersion` | Always `1`. |
| `sessionId` | The selected Deep Work session id. |
| `runId` | Stable run identity (equal to the session id for journaled runs). |
| `executionEpochId` | Current execution epoch; a new epoch after resume/restart. |
| `runEventWatermark` | Durable run-event sequence position. |
| `phase` | Operational boundary (e.g. `executing`, `landing`, `landed`). |
| `missionOutcome` | Completion evidence (e.g. `pending`, `incomplete`, `succeeded`). |
| `deadline` | Run deadline (RFC 3339); omitted when absent. |
| `landingCommit` | Landing checkpoint commit; omitted when not landed. |
| `landingCheckpointTreeSha` | Landing checkpoint tree; omitted when not landed. |
| `missionReviewOutcome` | Mission review outcome; omitted when absent. |
| `tasks` | Bounded task rows; omitted when empty. |

Each task row contains **only** `id`, `source`, `status`, `attempts`,
`checkpointCommit`, `checkpointTreeSha`, `acceptanceOutcome`, and
`reviewOutcome`; empty evidence fields are omitted.

**`landed` ≠ mission success.** `phase` and `missionOutcome` are separate
fields on purpose: a run can be operationally `landed` while its mission
outcome is `incomplete` (blocked/failed tasks remain), or `pending`.
Consumers must read both.

## Safety guarantees (verified by tests)

- **Validation before disk access.** The session identity is validated
  (non-empty, ≤128 chars, alnum/`-`/`_` only — dots are rejected so a
  crafted id can never traverse out of the state directory) before any
  state path is opened or resolved. Invalid or missing selection fails
  without touching disk.
- **Fail closed.** Missing state, corrupt/invalid state, over-cap
  projection, or invalid selection all return a tool error
  (`"deep run status unavailable"`); the server never reports an
  unvalidated success projection.
- **Bounded.** Replies are capped at **64 KiB** and a task-row bound of 512
  rows; a projection that exceeds either fails clearly instead of being
  silently truncated.
- **No sensitive content.** The projection deliberately excludes mission
  prose (name/objective/success/constraints/verify), prompts, verifier and
  acceptance commands, worker output, findings, local paths, credentials,
  and raw journal events. `stint deep status --json` is never exposed
  verbatim.
- **Durable observations.** A status observation remains available after a
  process restart and across epoch recovery, because the provider re-runs
  the standard state load and journal replay; the provider itself never
  writes durable state, so it cannot create duplicate persistence.

## Local stdio client configuration

Any MCP client that launches a stdio server can point at Stint. The
command runs with your normal user environment; the selected run's state
must already exist under the usual Stint state home
(`$XDG_STATE_HOME/stint` or `~/.local/state/stint`), which the server
resolves from the standard environment variables.

### Generic MCP client JSON (Claude-Desktop style)

```json
{
  "mcpServers": {
    "stint-deep-status": {
      "command": "/usr/local/bin/stint",
      "args": ["mcp", "serve", "--session", "20260930-171348"],
      "env": {
        "XDG_STATE_HOME": "/var/lib/stint-onbox/state",
        "PATH": "/usr/local/bin:/usr/bin:/bin"
      }
    }
  }
}
```

Replace `--session` with the selected Deep run's session id. On the
on-box deployment the state home is `/var/lib/stint-onbox/state`; in
development use your local `~/.local/state`. The server is read-only and
local: it opens only the selected run's state files and exits cleanly when
the client closes the session (exit code 0).

### Smoke check without a full MCP client

```sh
{ printf '%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"smoke","version":"0"}}}'
  sleep 0.2
  printf '%s\n' '{"jsonrpc":"2.0","method":"notifications/initialized"}'
  sleep 0.2
  printf '%s\n' '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
  sleep 0.2
  printf '%s\n' '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"deep_run_status","arguments":{}}}'
  sleep 1
} | stint mcp serve --session 20260930-171348
```

Expect one JSON line for `tools/list` containing exactly
`["deep_run_status"]` and one JSON line for `tools/call` whose single text
content item is the allow-listed projection.

## Verification

```sh
go test ./internal/deep ./cmd/stint
go vet ./internal/deep ./cmd/stint
go test -race ./internal/deep ./cmd/stint
```

Protocol-level tests use a real MCP client (in-memory transport plus a
subprocess stdio test of the built binary) and cover: tool discovery of
exactly one tool with no resources or prompts, a representative journaled
run's durable phase and separate mission outcome, recovery across a new
epoch, missing/corrupt state, invalid session selection, the bounded
output cap, and exclusion of secret-bearing fixture text and local paths.

## Spark-side follow-up (separate repository workstream)

Stint publishes; **Spark must consume and persist** this status for the
cross-repository MVP to be complete. The Spark side (in
`Marguelgtz/spark-observability-test`) must:

1. **Launch** `stint mcp serve --session <ID>` as a local stdio MCP server
   (or otherwise reach it on the same machine) and call `deep_run_status`.
2. **Consume** the allow-listed projection: treat `phase` (operational
   boundary) and `missionOutcome` (completion evidence) as distinct facts,
   and track `runEventWatermark`/`executionEpochId` so it can detect a
   resumed epoch rather than a new run.
3. **Persist** the observation in Spark's own store with the provider
   identity and the observed `executionEpochId`, so a later reader sees the
   durable Stint-side status even after the Stint provider process exits.

Until that Spark-side consumption and persistence is implemented and
verified, the cross-repository MVP is **not complete**. This Stint provider
is independently reviewable progress: it is fully local, read-only, and
requires no Spark changes, no D1/API persistence, and no changes to
Spark's `evaluate_change` evaluator.
