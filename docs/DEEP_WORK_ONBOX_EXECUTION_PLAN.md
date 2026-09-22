# Deep Work integration and on-box execution plan

**Status:** Active integration against current `main`.
**Canonical plan:** This file is the single living action plan for the Deep Work
integration mission. The dashboard plan and per-run action plans are historical or
run-scoped evidence, not competing integration plans.
**Last reconciled:** 2026-09-22.

This plan was recovered from `origin/fix/deep-work-onbox-publishing` at
`62f89ddcd0b98962f6b5f7c0d1360ac43e5ff3a6` (`docs/DEEP_WORK_ONBOX_EXECUTION_PLAN.md`)
and expanded for the integration mission. That branch's production architecture is
Hermes on-box. Earlier Cline-specific Deep Work executor behavior is legacy source,
not the target design.

## Current repository reality

- Starting target: `origin/main` =
  `c4322e32f2fbaabb027f4b9a555e40899d7f756b` (`docs: describe current Stint main and operator workflow`).
- Integration worktree: `/tmp/stint-deep-work-main-20260922`, branch
  `integrate/deep-work-main-20260922`, initially based directly on that SHA.
- The operator checkout remains on `fix/p0-ninfer-session-safety` at
  `792bb508...`; it has untracked run reports, directories, and a `stint` binary.
  They are preserved and excluded from integration commits.
- Published integration stack: draft PR #96
  (https://github.com/Marguelgtz/Stint/pull/96) contains the recovered plan and
  dynamic NInfer artifact fix; draft PR #97
  (https://github.com/Marguelgtz/Stint/pull/97) is based on #96 and adds the
  pre-rental full-session cost check. The current Hermes core branch is intended
  to stack on #97; these drafts are review checkpoints, not merged changes.
- All historical Deep Work feature PRs remain open. Their source heads share an
  old base (`d34cb2b...`) and are not safe merge units against current `main`.
  Use their code and evidence selectively on this clean integration branch.
- Core source route: #57 `feat/deep-work-mvp` (`3aa9a2f`), #59
  `feat/deep-work-hermes-worker` (`b48cc3b`), #77 `feat/deep-work-phase-reasoning`
  (`705a204`), #78 `feat/deep-work-run-config` (`bf3c95b`), #79 dashboard
  (`4c8e565`), and #80 on-box publication (`62f89dd`).
- Runtime source route: #74 `fix/ninfer-artifact-revision-size` (`800b6d1`),
  followed by #76 `fix/p0-ninfer-session-safety` (`792bb50`). Neither is merged.
  Reconcile only fixes still absent from or incorrect in current `main`.
- #85 (`feat/deep-work-github-modes`, `dd03489`) adds autonomous GitHub maintenance
  authority. Keep it separate unless dependency inspection proves core publication
  requires it and its gates can be repaired without destabilizing core execution.
- Generated/live-run PRs #81–84 and #86–89 are evidence or run products, not source
  integration units. #81–84 report a prior GPU smoke; #86–89 are later generated
  run branches and their latest reported `build-check` failed. Inspect before any
  disposition; do not merge run output as implementation by default.
- Review threads on #78 identify a fresh-box phase-proxy provisioning gap, stale
  phase-wire logs, and fallback session-cost enforcement. Threads on #79 identify
  unrelated-session telemetry attachment, timestamp-only phase/compression
  attribution, missing durable checkpoint identity, and stopping the wrong session
  from the historical dashboard. These remain hypotheses until checked against the
  selected integration source.
- The latest observed PR checks on 2026-09-22 were green for #57, #59, #77–80,
  and #85 (`spark-profile`, `go-vet`, `unit-tests`, `race-tests`, `build-check`).
  #87 and #89 reported `build-check=FAILURE`; #86 and #88 had no reported required
  check results. Passing checks on those historical heads do not verify this
  integration branch.
- #81–84 contain generated `phase-plan.md`, two phase-lane fixture outputs, and a
  final handoff. #86 and #88 each expose a broad 133-file cumulative tree against
  current `main`, not a bounded new feature checkpoint; #87 and #89 contain only
  generated handoffs. Keep these run branches as evidence pending focused review.
- Current `origin/main` still contains the NInfer model-transfer constants
  `18210531328` and `17367 MiB` in `cmd/stint/runtime.go`; the dynamic-size fix in
  #74 has not landed.
- Prior live evidence: `docs/ONBOX_DEEP_SMOKE_20260908_REPORT.md` in the #80 source
  records session `20260908-194552`, GPU `50305484`, on-box Hermes supervision,
  operator disconnect, xhigh/medium routing, three verified checkpoint PRs, and
  teardown. It explicitly says compression was configured but not exercised. This
  is historical branch evidence, not proof of the current integration head.

## Integration strategy

1. Keep current `main` as the only integration base; do not merge the historical PR
   chain wholesale.
2. Integrate the coherent Hermes-on-box core through #80, retaining the existing
   supervisor/coordinator and durable state architecture while retiring Cline as a
   first-class Deep Work path.
3. Apply correctness changes at their actual coordinator, launcher, telemetry, and
   publisher boundaries; add focused regression evidence before calling them done.
4. Reconcile #74/#76 runtime changes by semantic diff against current `main`, not by
   importing their cumulative trees.
5. Defer #85 maintenance authority if it can remain separate. Preserve the prior
   on-box checkpoint publication path only with deterministic branch/base/identity
   gates.
6. Build and verify on this isolated branch; land only after exact-head checks and
   final diff review. Use GitHub merge only if repository policy permits it.

## Living action items

### [x] Recover repository, PR, branch, and prior live-smoke reality

- **Problem / invariant:** Old plans and stack metadata may disagree with current
  `main`; no work may be called integrated from PR state alone.
- **Evidence:** `git status`, branches, worktrees, and history inspected before
  edits; `git fetch origin`; open-PR inventory and selected PR heads queried with
  `gh`; unresolved review threads queried for #57/#59/#77–80/#85–89.
- **Bounded change / acceptance:** Establish a clean current-main integration
  worktree; record exact SHAs, relevant open PRs, unresolved findings, and preserve
  dirty user artifacts. Done: worktree is based on `c4322e3`; no original worktree
  files were changed.
- **Outcome / uncertainty:** Recovery is recorded above. Check rollups, generated
  run-product file lists, and current-main NInfer constants are recorded above.
  Whether later source fixes resolved the review findings remains to be reconciled
  below.

### [x] Select and establish the integration route

- **Invariant:** One cumulative Deep Work head must not drag old main history or
  superseded Cline semantics into current `main`.
- **Evidence:** Feature PRs #57/#59/#77–80 are open and based on the historical
  `d34cb2b` line; #80 is the latest core on-box/publishing source. #85 is a separate
  maintenance layer. #74/#76 are a separate runtime stack.
- **Bounded change / acceptance:** Isolate current `origin/main` and port source
  semantics in bounded checkpoints. Done: branch
  `integrate/deep-work-main-20260922` starts at `c4322e3`.
- **Outcome / uncertainty:** Strategy above. The minimal dependency boundary between
  #80 publication and #85 maintenance is still under code inspection.

### [~] Integrate Hermes-on-box runtime and phase-aware fresh-box bootstrap

- **Problem / invariant:** A fresh qualified GPU must reach `RUNNING` only after
  Hermes, model routes, scripts, repository, and declared verification tools are
  usable through the normal production launcher.
- **Evidence:** #80 plan/report document launcher, supervisor, provider setup,
  phase routing, and a previous GPU run. #78 has an unresolved proxy-provisioning
  review thread. Mission requires xhigh planning/review and medium execution.
- **Bounded change:** Port launcher/supervisor/bootstrap and make qualification
  exercise the same path as production; retire Cline-only run selection.
- **Acceptance:** Fresh-box fixture and exact launcher-path checks establish both
  routes, verification tools, durable state, and supervisor readiness before
  `RUNNING`; restart/disconnect fixture passes.
- **Outcome / uncertainty:** Core Deep Work source, on-box launcher/supervisor,
  phase proxy, and smoke scripts are now in the isolated integration worktree.
  `go test ./cmd/stint ./internal/deep ./internal/deepdashboard` passes after
  integration. Fresh-box qualification, proxy installation on a newly provisioned
  instance, declared verification-tool preflight, and live GPU execution remain
  open; the old GPU report is historical evidence only.

### [~] Repair coordinator identity, policy reconstruction, and durable transitions

- **Problem / invariant:** Session/task identity, execution policy, verification,
  checkpoint, landing, and publication identities must survive restart without
  silent reinterpretation.
- **Evidence:** #79 review findings include compute misbinding and timestamp-only
  telemetry; mission flags likely have CLI default ambiguity; task ID namespace,
  verification/checkpoint ordering, critical-state persistence, and landing
  re-entry require source audit.
- **Bounded change:** Persist compute binding/rebind history and execution settings;
  reserve internal task IDs; make VERIFIED follow successful independent verify,
  durable checkpoint SHA, and durable state save; fail closed on state-save errors;
  make landing a resumable transaction.
- **Acceptance:** Regression tests cover mismatched compute, explicit rebind,
  duplicate task IDs, no-override resume, missing verifier, checkpoint/HEAD/save
  failures, and landing interruption/re-entry.
- **Outcome / uncertainty:** Task acceptance now requires an independent verify
  command; successful worker completion without one becomes `needs_human`. Active
  state must persist before a worker starts, failed checkpointing cannot mark a
  task verified, and landing is a resumable phase with persisted verify/handoff
  data and an exact landing commit SHA. Regression tests cover verifier absence,
  pre-invocation state failure, checkpoint failure, handoff write failure, and
  recovery after the final state save fails. Dashboard tunnel probes no longer
  mutate the process-wide port or fall back to the default endpoint. Compute
  identity/rebind history, reserved action-plan task IDs, and explicit resume
  override semantics remain open.

### [~] Make worker evidence and workspace handling truthful

- **Problem / invariant:** Hermes success is not independent acceptance; prompts
  must not be captured by target-repo `git add -A`; route/compression evidence must
  not attribute unrelated traffic to a mission by timestamp alone.
- **Evidence:** #79 review thread identifies timestamp-only phase/compression
  correlation. #80 plan describes isolated Hermes attempts and host observer.
- **Bounded change:** Keep prompts in protected Stint runtime state; label worker
  claims without verification as human-needed/unverified; propagate available
  session/task/attempt/invocation identity, otherwise document process/log isolation
  and host-level evidence scope.
- **Acceptance:** Crash-cleanup boundary, unverified worker result, unrelated route
  traffic, and honest compression scope tests pass.
- **Outcome / uncertainty:** Local Hermes prompt files now use protected system
  temporary storage rather than the target worktree. The fixed remote prompt path,
  per-attempt telemetry isolation, compute/session attribution, and compression
  evidence scope still need repair and regression coverage.

### [~] Reconcile compute cost, GitHub policy, publisher authority, and pagination

- **Problem / invariant:** Every rental obeys full requested-session budget; durable
  session policy is publisher authority; generic pushes cannot update base/main or
  arbitrary branches; merge decisions use complete evidence.
- **Evidence:** #78 unresolved fallback-cost thread; #80 contains GPU publisher;
  #85 contains distinct maintenance gates. Current-main semantics and publisher
  dependencies not yet audited.
- **Bounded change:** Recheck `MaxCostUSD` immediately before each rental; bind
  policy fields to publisher config and fail on mismatch; restrict push destinations;
  paginate relevant GitHub collections or fail closed.
- **Acceptance:** Fallback-cost, authority-mismatch, adversarial base-push, and
  pagination/completeness fixtures pass; deterministic merge gates remain intact.
- **Outcome / uncertainty:** The rental loop now repeats the full-session budget
  check directly before `CreateInstance`; a candidate can satisfy the hourly cap
  and still be rejected for the requested duration. The focused regression uses a
  $0.30/hour initial offer and a $0.40/hour fallback over seven hours against the
  $2.50 ceiling. GitHub publisher authority and pagination remain under audit;
  #85 remains deferred pending dependency and gate inspection.

### [~] Reconcile immutable NInfer revision and transfer recovery

- **Problem / invariant:** Runtime must pin immutable artifact identity and SHA while
  deriving transfer size dynamically and safely recovering corrupt/oversized files.
- **Evidence:** #74 contains immutable revision/dynamic-size work; #76 adds current
  lifecycle safety. They are open and based on historical branches.
- **Bounded change:** Compared current `main` with #74 and applied only
  `cmd/stint/runtime.go` and its focused tests. The NInfer model URL now pins
  revision `18dfc887423fa5aabf3cb56fac41490e462b3fab` while preserving SHA
  `eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e`. Bootstrap
  discovers remote content length, writes `model-total-bytes`, reports dynamic
  progress, resumes partial downloads, and discards an invalid completed-sized
  artifact before retrying. Unrelated current-main model metadata was preserved.
- **Acceptance:** Runtime tests cover dynamic metadata, progress, oversized/corrupt
  artifact redownload, checksum, and the final immutable revision.
- **Outcome / uncertainty:** The source and static regressions are integrated;
  `go test ./cmd/stint` passed, including revision pinning, absence of fixed-size
  metadata, generated-shell syntax, and checks for dynamic-size/recovery commands.
  Those tests inspect generated shell text rather than execute a corrupt-artifact
  transfer fixture. The task remains active until that behavior fixture and fresh
  GPU transfer evidence exist.

### [ ] Verify the integrated system and write final handoff

- **Problem / invariant:** Unit evidence alone cannot establish production
  readiness or landing.
- **Bounded change:** Run focused regressions, Go/Python/shell checks, repository
  tests, and fixture state-machine scenarios from the exact integration head; review
  complete diff; record run evidence and PR dispositions here and in a concise
  durable handoff.
- **Acceptance:** Exact commands, results, integration SHA, fresh-GPU command, live
  status, and any external merge blocker are recorded; verify actual `main` SHA if
  landed.
- **Outcome / uncertainty:** Not started.

## Verification so far

- `go test ./cmd/stint ./internal/deep ./internal/deepdashboard` — PASS after
  coordinator durability changes. This is focused package evidence only; full
  repository checks and a fresh GPU smoke have not run on this head.

## Next action

Finish retiring Cline as a first-class Deep Work execution path, then close the
fresh-box preflight/provisioning, compute identity, action-plan namespace, and
resume-override findings. Keep the next source changes stacked as small draft PRs
and update this plan with each verified checkpoint.
