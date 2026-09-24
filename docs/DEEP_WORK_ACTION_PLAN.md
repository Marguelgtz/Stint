# Deep Work action plan — detached verification and visible task stages

Mission: ground detached Deep Work and expose task reasoning. Production Deep Work must keep
working after the on-box launcher disconnects, and `stint deep dash` must show each task's
configured reasoning from durable state.

**Status (2026-09-24):** XHIGH PLAN complete — code inspection done at checkpoint
`88b69f0f7f1aea6f30b75d8fee4f52ab8544d163` (branch `stint/deep-20260924-114452`,
worktree `.stint-deep/20260924-114452`). Findings below are grounded in the checked-out
source; they are hypotheses for the implementation stage to confirm against tests, not
evidence on their own.

## Living checklist

- [x] XHIGH PLAN: inspected the supervisor handoff, toolchain provisioning, task model,
      dashboard projection and rendering; findings, decisions, and test design recorded below.
- [ ] MEDIUM IMPLEMENTATION 1: make the detached supervisor retain the provisioned Go path;
      add a regression test for a sparse non-login PATH.
- [ ] MEDIUM IMPLEMENTATION 2: carry durable task reasoning into the Deep Dashboard Tasks view;
      render known values and keep legacy empty values safe.
- [ ] XHIGH REVIEW: inspect the complete diff and tests for correctness, detached behavior,
      narrow-terminal layout, truthful attribution, and scope; fix or record findings.
- [ ] INDEPENDENT VERIFICATION: run the mission's Go package tests, supervisor regression test,
      and `git diff --check`; record exact outcomes.
- [ ] HANDOFF: record checkpoint SHA, PR URLs, verification, and any unresolved issue. Leave all
      PRs open for Marguelgtz to review.

## Objective
Keep verification available after the on-box launcher exits, then expose each task's configured
reasoning in the operator's Deep Dashboard.

## Findings from code inspection (evidence pointers)

### Finding 1 — the detached supervisor inherits a PATH that never saw provisioning

- `scripts/provision-box.sh:13` and `:117` put `/usr/local/go/bin` and `/usr/local/bin` on
  `PATH`, but only inside the provisioning SSH session. At `:127` it preflights
  `git python3 node npm npx hermes timeout curl` (via `command -v`), and at `:131-138` it
  proves the Stint verification surface by running `go test ./...` **inside that same
  provisioning shell**. Proving `go` works there proves nothing about the supervisor, because
  the export never persists to the box's long-lived processes.
- `scripts/onbox-deep-supervisor.sh:27` is the supervisor's only `export`
  (`XDG_STATE_HOME`). `start` launches the detached process at `:307`
  (`setsid nohup "$0" run ...`) and `run_supervisor` (`:244`) spawns
  `"$STINT_BIN" deep onbox ...` (`:266`, `:269`) with **no PATH pinning at all**. The
  long-lived process therefore carries whatever PATH the launch SSH session had (a non-login
  `sh` context, not the provisioning one).
- The coordinator's verifier runs `sh -c <command>` (`cmd/stint/deep_landing.go:150-163`,
  `exec.CommandContext(vctx, "sh", "-c", command)`), so a task verify command such as
  `make test` / `go test ./...` resolves `go` only from that inherited PATH. If
  `/usr/local/go/bin` is missing there, verification fails even though provisioning
  demonstrated the toolchain works — the on-box session stalls on un-verified tasks.
- The launcher feeds the supervisor only `STINT_ONBOX_BIN/ROOT/READY_FILE/INSTANCE_ID/
  DEADLINE/STARTED_AT/CLIENTS/ORIGIN` plus GitHub/R2/watchdog vars
  (`scripts/launch-onbox-deep.sh:467-486`); no PATH or toolchain env is forwarded.
- The hermetic supervisor fixture already exists and needs no GPU:
  `scripts/test_onbox_supervisor.sh` (fake `stint` binary, temp root, `run` mode). The
  sparse-PATH regression test belongs there.

### Finding 2 — durable per-task reasoning exists but the dashboard drops it

- Durable value: `deep.Task.Reasoning` (`internal/deep/task.go:54`, JSON `reasoning`),
  parsed from the mission's per-task `reasoning:` lines
  (`internal/deep/mission.go:129-133`) and normalized by `NormalizeReasoning`
  (`internal/deep/reasoning.go:21-33`: `none|low|medium|xhigh`, empty = inherit session
  default). The coordinator already applies the per-task override per invocation
  (`cmd/stint/deep_loop.go:54`, `in.reasoning = t.Reasoning`).
- Gap: the dashboard projection model `deepdash.Task`
  (`internal/deepdashboard/render.go:23-27`) has **no Reasoning field**, and
  `project()` builds each row without it
  (`cmd/stint/deep_dashboard.go:316-323` — ID, Objective, Status, Attempts, Blocker,
  LastResult, Verify, CheckpointCommit, VerifiedAt only). `tasksView`
  (`internal/deepdashboard/render.go:222-245`) therefore cannot display it.
- Session-level `exec.Reasoning` is persisted (`internal/deep/state.go:65`) but likewise not
  surfaced in `deep dash`; the worker lane's xhigh/medium counters are a separate,
  host-wide observation (Finding 3).

### Finding 3 — wording to preserve, and the inference trap to avoid

- `cmd/stint/deep_dashboard.go:521` sets the worker telemetry scope to
  `"Shared host logs since session start; counts are not attributed to this session"`,
  built from `wire.PhaseRoutes.XHighRequests/MediumRequests`
  (`deep_dashboard.go:489-494, 521`) — host-wide route counts, not per-session or per-task
  attribution. `cmd/stint/deep_dashboard_test.go:98-99` and
  `internal/deepdashboard/render_test.go:18,36` pin that wording; it must stay green.
- Per-task reasoning must come from `deep.Task.Reasoning` (durable state) only. Deriving it
  from `XHighRequests`/`MediumRequests` would be false attribution; the two surfaces
  (durable task settings vs shared host telemetry) stay distinct in display and in tests.

## Decisions

- **D1 (implementation 1):** pin the verification toolchain PATH inside the supervisor's
  `run_supervisor` before the coordinator loop — export the same fixed set provisioning uses
  (`/usr/local/go/bin:/usr/local/bin:$HOME/.local/bin` prepended over the ambient PATH,
  matching `scripts/provision-box.sh:13`). The supervisor owns coordinator lifetime
  (`scripts/onbox-deep-supervisor.sh:8-13`), so toolchain availability is its
  responsibility; the launcher and provisioning scripts need no changes. Keep the existing
  provisioning preflight (`:127-138`) as the authoritative toolchain check.
- **D2 (implementation 2):** add `Reasoning string` to `deepdash.Task`, carry
  `task.Reasoning` in `project()`, and render it in the Tasks view. Empty value renders as
  the session default (from `exec.Reasoning`, or "inherit" when the session itself is
  empty); only the four normalized values are displayed. This is durable-state display only —
  no inference from telemetry.
- **D3 (regression tests):** extend `scripts/test_onbox_supervisor.sh` with a sparse non-login
  PATH fixture (e.g. `PATH=/usr/bin:/bin`) plus a fake `stint` that requires a tool from the
  provisioned path, asserting the supervisor still finds it. Extend
  `internal/deepdashboard/render_test.go` (and `cmd/stint/deep_dashboard_test.go`) to assert
  reasoning displays from task state and that legacy empty rows stay safe at narrow widths.
- **D4 (scope guard):** no changes to runtime selection, pricing, authentication, or any
  dashboard other than `stint deep dash`. No merging or closing of PRs; verified work goes
  out as open PRs under the mission's declared GitHub policy for Marguelgtz to review.

## Risks

- **R1 — over-broad PATH masking real gaps:** exporting a fixed path set could hide a box
  where a tool is genuinely missing. Mitigation: keep the `command -v` preflight in
  `provision-box.sh:127` as the hard gate; the supervisor only makes an already-provisioned
  path visible, it does not provision anything new.
- **R2 — narrow-terminal layout:** the Tasks view truncates via `compact(...)` against
  `m.Width` (`internal/deepdashboard/render.go:222-245`); a new reasoning field must
  truncate consistently and not blow out the frame at small widths. Cover with the existing
  narrow-width render tests (`render_test.go:83-95` runs 20 tasks through `Render`).
- **R3 — legacy sessions:** pre-mission tasks and resumed state may have empty
  `task.Reasoning`. Empty must render as "session default / inherit", never as a blank
  token or the raw session value leaking into a task row.
- **R4 — attribution truthfulness:** the reviewer must confirm the rendered reasoning is the
  durable per-task value and that the worker pane's host-wide scope note
  (`deep_dashboard_test.go:98-99`) is untouched.
- **R5 — on-box behavior:** the supervisor change runs on the GPU box; locally only the
  hermetic bash fixture and Go tests can execute here. Live smoke
  (`scripts/run-onbox-deep-smoke.sh`) and real detached runs remain for the owner's
  acceptance pass after the PRs.

## Next steps (execution order)

1. ~~XHIGH PLAN — inspection and this plan.~~ done at `88b69f0`.
2. MEDIUM IMPLEMENTATION 1: supervisor PATH pin in
   `scripts/onbox-deep-supervisor.sh` `run_supervisor`; sparse-PATH case in
   `scripts/test_onbox_supervisor.sh`.
3. MEDIUM IMPLEMENTATION 2: `deepdash.Task.Reasoning` + `project()` carry + `tasksView`
   display; render/dashboard tests; keep the host-wide scope wording tests green.
4. XHIGH REVIEW: full diff against this plan; detached lifecycle, narrow widths, truthful
   attribution, scope.
5. INDEPENDENT VERIFICATION: `make test` (go test ./...),
   `bash scripts/test_onbox_supervisor.sh`, `git diff --check`; record exact outcomes here.
6. HANDOFF: open PRs (engineering policy, owner reviews), record checkpoint SHA, PR URLs,
   verification outcomes, and any unresolved issue in this file.

## Verification surface

- Module `github.com/Marguelgtz/Stint`, Go 1.23 (`go.mod`); `make test` = `go test ./...`,
  `make check` = fmt + vet + test + build (`Makefile`).
- Supervisor lifecycle fixture: `bash scripts/test_onbox_supervisor.sh` (hermetic, no GPU).
- Acceptance for this task: this file exists non-empty at
  `docs/DEEP_WORK_ACTION_PLAN.md` (`test -s docs/DEEP_WORK_ACTION_PLAN.md`).

## Boundaries
Do not infer per-task reasoning from host-wide route counters. Do not modify runtime
selection, price limits, auth, or unrelated dashboards. Do not merge or close PRs.
