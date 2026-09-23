# Deep Work integration and on-box execution plan

**Status:** Integration landed on current `main`; fresh-GPU live acceptance remains incomplete.
**Canonical plan:** This file is the single living action plan for the Deep Work
integration mission. The dashboard plan and per-run action plans are historical or
run-scoped evidence, not competing integration plans.
**Last reconciled:** 2026-09-23.

**Landing:** PR #107 merged the cumulative integration stack in merge commit
`2041498ed00724cef3bca3769ccaa9907e87efa2` on 2026-09-23. This lands the stack
from starting `main` `c4322e32f2fbaabb027f4b9a555e40899d7f756b`. The merged tree
passes `go test -count=1 ./...`; full fresh-box execution did not pass and remains
an explicit follow-up.

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
- Published integration stack: PR #96
  (https://github.com/Marguelgtz/Stint/pull/96) contains the recovered plan and
  dynamic NInfer artifact fix; PR #97
  (https://github.com/Marguelgtz/Stint/pull/97) is based on #96 and adds the
  pre-rental full-session cost check. PR #98
  (https://github.com/Marguelgtz/Stint/pull/98), branch
  `integrate/deep-work-hermes-core-20260922`, stacks on #97 and ports the Hermes
  coordinator/dashboard core. PR #99
  (https://github.com/Marguelgtz/Stint/pull/99), branch
  `integrate/deep-work-hermes-only-20260922`, stacks on #98 with commit
  `5401752` for compute identity, Hermes-only execution, and dashboard safety.
  PR #100 (https://github.com/Marguelgtz/Stint/pull/100), branch
  `integrate/deep-work-bootstrap-20260922`, stacks on #99 with commit
  `453c91f` for fresh-box qualification and current operator docs. PR #101
  (https://github.com/Marguelgtz/Stint/pull/101), branch
  `integrate/deep-work-onbox-recovery-20260922`, stacks on #100 with commit
  `1356ef4` for durable resume/rebind on restored compute. PR #100 reported no
  standalone checks; the cumulative integration head passed exact-head CI.
  PR #102 (https://github.com/Marguelgtz/Stint/pull/102), branch
  `integrate/deep-work-publisher-policy-20260923`, stacks on #101 with commit
  `04151f5` for persisted GitHub policy and bounded engineering publication;
  plan/evidence commit `4bf3dd1` records its passing exact-head CI. PR #103
  (https://github.com/Marguelgtz/Stint/pull/103), branch
  `integrate/deep-work-final-evidence-gate-20260923`, stacks on #102 with commit
  `8aea2bd` for durable terminal-state checks and fail-closed configured R2
  archiving; plan/evidence commit `ad55713` records passing exact-head CI. PR
  #104 (https://github.com/Marguelgtz/Stint/pull/104), branch
  `integrate/deep-work-ninfer-recovery-fixture-20260923`, stacks on #103 with
  commit `d971d24` for executable NInfer transfer-recovery evidence; plan/evidence
  commit `19e712a` records its passing exact-head CI. PR #105
  (https://github.com/Marguelgtz/Stint/pull/105), branch
  `integrate/deep-work-smoke-cli-preflight-20260923`, stacks on #104 with commit
  `d497ce1` to align the live-smoke rental command with the current CLI; commit
  `ad422e0` adds a lower-only session cap and a true parser-only preflight, and
  `e658c09` adds the session-capped hourly offer ceiling required by current
  NInfer-compatible marketplace pricing. Commit `11acb62` limits startup to one
  rental candidate per authorized launch. Commit `53b5bcc` raises the advertised
  network floor and reserves $1.50 of the remaining smoke budget for the second
  single-candidate launch; the current follow-up tightens the final retry's cap
  using measured transfer-time and bandwidth-cost evidence. PR #106
  (https://github.com/Marguelgtz/Stint/pull/106), branch
  `integrate/deep-work-ninfer-default-token-budget-20260923`, stacks on #105 to
  fix the fresh-box NInfer launch contract discovered by the live test.
- PR #107 (https://github.com/Marguelgtz/Stint/pull/107) carried the cumulative
  #96–106 stack directly against current `main`; it merged by merge commit
  `2041498`. GitHub marked #96 merged; PRs #97–106 were closed after their
  commits landed through #107. Their remote branches remain available for
  provenance. Exact-head `build-check`, `go-vet`, `unit-tests`, `race-tests`, and
  `spark-profile` passed on the cumulative head before merge.
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
  authority. Direct source inspection found generic arbitrary-branch push authority
  (including the configured base), environment-owned config with no durable mission
  policy comparison, existing-PR lookup capped at 10 without pagination, inventory
  capped at 1,000 without failing when the cap is reached, review/comment/file/
  commit/check-run collections capped at 100, review-thread comments capped at 20,
  and overrideable API/git URLs not bound to the configured repository. Keep it
  separate; core checkpoint publication does not require merge authority.
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

### [!] Integrate Hermes-on-box runtime and phase-aware fresh-box bootstrap

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
- **Outcome / uncertainty:** The production launcher now transfers and runs the
  runtime provisioner, phase proxy/setup, observer, route smoke, and optional
  two-lane smoke before it starts the supervisor. `run-onbox-deep-smoke.sh` now
  calls that same launcher path instead of manually preparing the GPU. Provisioning
  installs missing Hermes/Node and a checksum-verified current Go toolchain for a
  repo with `go.mod`; for Stint itself it runs `go test ./...` before startup and
  checks NInfer flags and the requested model. Generic missions still receive
  executable-presence preflight only; arbitrary language dependencies are not
  installed. Commit `453c91f` was pushed in PR #100, stacked on #99, and is now
  included in the #107 landing. Local
  full Go tests, shell syntax, Python byte-compilation, and diff checks pass. A
  fresh GPU has not run this launcher yet, so actual install/provider behavior,
  readiness, and disconnect recovery remain unverified; historical GPU evidence
  is not evidence for this head. First live-smoke attempt at
  `/tmp/stint-live-smoke-run.pTn41I/artifacts/launcher.log` was rejected locally
  because the script passed removed flag `--tunnel-port`; the current CLI returned
  before provider mutation and `stint status` confirmed there was no active
  compute. PR #105 removes that flag and runs the exact bounded argument array
  through the CLI parser before rental. The first actual preflight-approved
  marketplace search returned no candidate after policy ranking: the NInfer
  request requires RTX 4090/CUDA, while the profile's $0.40/hour cap excluded
  current qualifying 4090 offers. A read-only marketplace plan returned 45
  offers and 9 base-policy qualifiers, led by RTX 3090; the displayed 4090
  candidates were $0.496-$0.614/hour and rejected by the $0.40/hour cap. No
  rental or session creation occurred, and `stint status` reported no active
  compute. Commit `e658c09` added an explicit per-run hourly limit, permitted
  above the profile's $0.40 only alongside an explicit session ceiling; the
  existing full-session check remains immediately before every rental.
  Commit `11acb62` limits each authorized launcher run to one rental candidate
  so automatic startup retries cannot stack individually capped rentals. On
  2026-09-23 the first paid bounded live attempt rented RTX 4090 instance `52145135`
  at $0.425/hour (projected rental $0.64 for 1.5 hours) after SSH became ready
  in 3:02. The real NInfer model-transfer probe measured 11.8 MB/s, below the
  30 MB/s minimum, so Stint rejected and destroyed the instance before model
  startup. Pre-teardown spend estimate was $0.02; the isolated local status is
  now clear. No Hermes, RUNNING handshake, or Deep Work task ran. A subsequent
  read-only plan returned 43 marketplace offers and 5 base-profile qualifiers;
  displayed 4090 examples included $0.552/hour at 820-898 Mbps with about
  $0.0133/GB inbound bandwidth cost, while the failed host was $0.425/hour at
  457 Mbps. The second paid attempt rented instance `52146240` at $0.545/hour with
  846 Mbps advertised network, reached SSH in 1:27, and measured 13.8 MB/s.
  Although below the original 30 MB/s floor, its transfer projection was 21:44
  and projected READY was 24:19 from rental. Stint still rejected and destroyed
  the host before model startup; estimated active-rental spend was about $0.02.
  Thus neither instance ran NInfer or Deep Work; no session remains. In line
  The final one-candidate attempt used a 500 Mbps advertised-network minimum,
  10 MB/s measured floor, $0.80/hour ceiling, and $1.20 scheduled-session cap.
  It rented RTX 4090 instance `52147295` in the Netherlands at $0.625/hour
  (projected session $0.94), measured 100.3 MB/s, and reached model-serving
  `READY` after 13:50 from rental. Native NInfer compiled all 246 build steps
  with CUDA 12.8.93, the model checksum passed, and Stint established the
  watchdog and tunnel. The production launcher transferred the pinned runtime
  and mission, verified GitHub destination, installed Node 22, Hermes, and Go
  1.27.1, and passed the target repository's full `go test ./...` preflight.
  It then failed closed five times because the running NInfer argv omitted
  `--default-max-tokens 262144`, required by `scripts/provision-box.sh`. The
  launcher never reported `RUNNING`, Hermes never started, and no Deep Work
  task ran; smoke cleanup destroyed the instance and `stint status` now shows no
  active compute. This is a production runtime launch bug, not an environment
  or marketplace blocker. PR #106 adds the missing flag using the selected
  context limit and regression coverage. Its exact-head checks pass
  (`build-check`, `go-vet`, `race-tests`, `spark-profile`, and `unit-tests`; Spark
  Observability is skipped). A corrected bounded attempt then rented RTX 4090
  instance `52149289` in Romania at $0.603/hour with 1268 Mbps advertised
  network. Vast reported running and supplied SSH metadata after 2:07, but
  Stint's authenticated `echo stint-ssh-ready` probe timed out during its
  four-minute SSH window. Stint rejected and destroyed the instance; isolated
  `stint status` is clear. The rental lasted about 6m26s, or approximately
  $0.065 at the listed rate before provider fees. No measured-throughput probe,
  model transfer, NInfer readiness, production launcher, `RUNNING` handshake,
  Hermes task, publication, or handoff occurred on this host. Detailed evidence
  is in `/tmp/stint-live-corrected.vTQEzO/artifacts/launcher.log`. Across the
  bounded paid attempts, estimated spend is about $0.52-$0.57; actual provider
  totals may include small storage or transfer charges from earlier attempts.
  No further rental was started after this SSH failure. The corrected launch
  contract is covered by local tests and exact-head CI, but its fresh-box
  behavior and the complete on-box Deep Work flow remain unverified. The
  cumulative stack is now on `main` through PR #107; `go test -count=1 ./...`
  passes at merge commit `2041498`. The live acceptance is still blocked before
  `READY` after the last host's authenticated SSH timeout.

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
  mutate the process-wide port or fall back to the default endpoint. The current
  PR #99 persists the Vast instance ID, requires an explicit reason for
  replacement-compute rebinds, rejects coordinator-reserved mission IDs, and
  makes dashboard controls identity-aware. PR #101 completes the on-box recovery
  side: the launcher preserves restored state/repository, validates the exact
  session branch/worktree and current compute before qualification, and requires
  an explicit audited rebind on mismatch. The coordinator recovers the saved
  branch before binding, reanchors deadline to the READY compute deadline,
  preserves omitted settings, and writes RUNNING after durable state and checks.
  Tests cover settings overrides, plan retargeting, resume deadline/rebind,
  implicit-rebind refusal, and readiness JSON. Commit `1356ef4` was in PR #101;
  its code landed through #107. Actual volume restore remains external:
  resume requires the durable state and repository to be mounted at the same
  configured root; the launcher does not copy lost disks or restore R2 objects.

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
  temporary storage rather than the target worktree. Remote prompts now use
  per-invocation protected `/tmp` files with cleanup traps; Hermes quiet mode is
  omitted. Dashboard phase/compression totals are explicitly labeled as shared
  host log counts that are not attributed to the session. The telemetry stream
  still lacks session/task/attempt correlation, so those totals are not
  coordinator acceptance evidence.

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
  $2.50 ceiling. PR #102 now persists mission GitHub mode/repository/base/allowed-
  authors/approval and checks launcher, resume, durable state, and publication
  records against that authority. Its engineering publisher restricts pushes to
  generated refs for the same session, validates exact checkpoint/PR/landing
  identities, preserves mismatched publication records, and fully paginates its
  existing-PR query with a 10,000-result bound plus lookahead failure. Seven Python
  fixture tests cover policy, API/push URL binding, push restrictions, pagination
  limits, PR identity, retry preservation, and exact final landing. The broader
  #85 maintenance and merge APIs are not dependencies of core publication and
  remain deferred because their generic push and bounded collection gaps are not
  repaired by this slice. PR #102 stacked on #101 and landed through #107; local
  verification passed and its GitHub `build-check`, `go-vet`, `unit-tests`,
  `race-tests`, and `spark-profile` checks passed. No live GPU smoke has been run.

### [~] Reconcile immutable NInfer revision and transfer recovery

- **Problem / invariant:** Runtime must pin immutable artifact identity and SHA while
  deriving transfer size dynamically and safely recovering corrupt/oversized files.
- **Evidence:** #74 contains immutable revision/dynamic-size work; #76 adds current
  lifecycle safety. Those source PRs remain open and based on historical branches.
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
  Commit `d971d24` in PR #104 extracts the production artifact preparation
  command and adds a no-network execution fixture. The fixture passes dynamic size
  discovery, valid partial resume, oversized and same-size corrupt artifact
  replacement, corrupt partial clean retry after SHA mismatch, and a model path
  containing spaces. Full `go test -count=1 ./...` passes. Actual fresh-GPU
  transfer remains unverified.

### [~] Gate completion on durable publication and final archive

- **Problem / invariant:** Supervisor success must mean the durable session is
  terminal, required publication converged, and any configured final evidence
  archive succeeded before the supervisor exits.
- **Evidence:** `scripts/onbox-deep-supervisor.sh` previously selected terminal
  state from an asynchronously refreshed heartbeat and ignored errors from the
  configured R2 archive both inside `archive_final` and in the EXIT cleanup.
- **Bounded change:** Read terminal phase directly from `deep.json`; fail if the
  coordinator exits zero before durable `landed`/`stopped`; require a configured
  archive helper and durable state, propagate archive failure as an incomplete
  exit, and preserve on-box state for recovery.
- **Acceptance:** No-GPU supervisor fixture proves successful archive permits
  success; archive failure, missing helper, and nonterminal coordinator exit fail;
  archive-disabled fixture keeps existing optional behavior; source state remains
  on-box after finalization.
- **Outcome / uncertainty:** Commit `8aea2bd` in PR #103 adds these gates
  and the fixture. Bash syntax and `bash scripts/test_onbox_supervisor.sh` pass;
  PR #103 exact-head `build-check`, `go-vet`, `unit-tests`, `race-tests`, and
  `spark-profile` pass. Plan/evidence commit `ad55713` is pushed. The live R2
  service and actual GPU teardown ordering remain unverified.

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

- PR #99 commit `5401752`: `go test ./cmd/stint ./internal/deep ./internal/deepdashboard` and `git diff --check` — PASS.
- PR #100 commit `453c91f`: `go test ./...` — PASS; `bash -n` for the launcher, provisioner, phase setup, smoke, and supervisor scripts — PASS; Python byte-compilation for phase proxy, observer, and publisher — PASS; `git diff --check` — PASS. ShellCheck is unavailable.
- PR #100 currently reports no GitHub checks. No fresh GPU validation has been run against PR #100. Local static checks do not prove package installs, Hermes configuration compatibility, route inference, watchdog startup, or supervisor recovery on a clean GPU.
- PR #101 commit `1356ef4`: `go test -count=1 ./cmd/stint ./internal/deep ./internal/session` — PASS; `go test ./...` — PASS; Bash syntax for launcher, supervisor, provisioner, and live-smoke scripts — PASS; embedded resume-preflight Python compile and fixture checks (same identity, implicit mismatch refusal, explicit mismatch allowance, missing branch refusal) — PASS; `git diff --check` — PASS. GitHub `build-check`, `go-vet`, `race-tests`, `spark-profile`, and `unit-tests` — PASS. No live replacement-compute or volume-restore run has been performed.
- PR #102 implementation commit `04151f5`: `go test -count=1 ./...` — PASS; `python3 scripts/test_onbox_github_publish.py` — PASS (7 tests); Python byte-compilation — PASS; Bash syntax for launcher, smoke, supervisor, and provisioner — PASS; embedded resume-preflight Python compilation — PASS; `git diff --check` — PASS. PR #102 exact-head GitHub `build-check`, `go-vet`, `unit-tests`, `race-tests`, and `spark-profile` — PASS. It was closed after landing through #107. No live GPU smoke was run at that checkpoint.
- PR #103 commit `8aea2bd`: `bash -n scripts/onbox-deep-supervisor.sh scripts/test_onbox_supervisor.sh`, `bash scripts/test_onbox_supervisor.sh`, and `git diff --check` — PASS. PR #103 exact-head GitHub `build-check`, `go-vet`, `unit-tests`, `race-tests`, and `spark-profile` — PASS. Plan/evidence commit `ad55713` is pushed. It was closed after landing through #107. No live GPU smoke was run at that checkpoint.
- PR #104 commit `d971d24`: `go test -count=1 ./...` — PASS, including the five-case local NInfer artifact fixture; publisher safety tests (7) and supervisor completion fixture — PASS; shell syntax and `git diff --check` — PASS. PR #104 exact-head GitHub `build-check`, `go-vet`, `unit-tests`, `race-tests`, and `spark-profile` — PASS. Plan/evidence commit `19e712a` is pushed. It was closed after landing through #107. No live GPU smoke was run at that checkpoint.
- PR #105 commit `d497ce1` removed the stale tunnel flag; its exact-head GitHub `build-check`, `go-vet`, `unit-tests`, `race-tests`, and `spark-profile` passed. Commit `ad422e0` added a lower-only `--max-cost-usd` and `--validate-only`, replacing the ineffective `--help` preflight. Commit `e658c09` added `--max-hourly-usd`; increasing the profile's $0.40/hour limit requires an explicit session cap and the per-candidate full-session check still guards each rental. Commit `11acb62` limits each live-smoke launch to one candidate; `53b5bcc` and `0f1b198` narrow the remaining-budget retries. `go test -count=1 ./...`, publisher fixtures (7), supervisor fixture, smoke preflight fixture, shell syntax, and `git diff --check` pass through `0f1b198`; PR #105's exact-head GitHub `build-check`, `go-vet`, `unit-tests`, `race-tests`, and `spark-profile` pass. Paid attempts one and two rented `52145135` and `52146240`, then were destroyed before model startup after 11.8 and 13.8 MB/s failed the 30 MB/s floor. Attempt three rented `52147295` at $0.625/hour, qualified at 100.3 MB/s, compiled NInfer, verified the model SHA, and reached `READY` in 13:50. Production fresh-box preflight installed Node/Hermes/Go, passed `go test ./...`, then failed closed on the missing `--default-max-tokens 262144` launch flag across all five bounded provision retries. No `RUNNING` handshake or Hermes task occurred. PR #106 adds that context-matched token limit and regressions; its local `go test -count=1 ./cmd/stint`, `go test -count=1 ./...`, publisher fixture (7), supervisor fixture, smoke preflight fixture, shell syntax, and `git diff --check` pass. Exact-head GitHub `build-check`, `go-vet`, `unit-tests`, `race-tests`, and `spark-profile` pass. The next bounded host, `52149289`, exposed SSH metadata but failed Stint's authenticated SSH probe and was destroyed before `READY`; no launcher or model transfer ran. All four hosts are torn down and isolated Stint status is clear. Estimated total provider spend is $0.52-$0.57, with small earlier storage/transfer charges potentially settling separately.
- PR #107 landing verification: the merge commit is `2041498ed00724cef3bca3769ccaa9907e87efa2`; `go test -count=1 ./...` — PASS from a detached worktree of the exact merged `main`. PR #107 exact-head CI passed all five required checks. At landing, `main` advanced from `c4322e3` to `2041498`.

## Next action

The source integration landed via PR #107 at `2041498`; the exact merged tree
passes `go test -count=1 ./...`. Fresh-box validation remains blocked before
`READY`: the latest provider host failed the authenticated SSH probe and was
destroyed. Preserve that result as a provider startup failure, not as evidence
for or against the corrected NInfer launch. The next live step is to build the
current `main` and rerun `scripts/run-onbox-deep-smoke.sh` with the secure token
file, GitHub destination, and a fresh isolated artifact root. Keep its built-in
1.5-hour duration, $1.20 rental cap, $0.80/hour ceiling, and single-candidate
limit. The full run must reach `RUNNING`, survive operator disconnect, complete
xhigh/medium work, verify and publish checkpoints, archive final R2 evidence,
write handoff, and tear down before live readiness can be claimed. Check required
local credentials/artifacts by presence only; do not expose their contents.
PR #101's explicit compute-resume gate does not restore state volumes, so
replacement-compute recovery remains unproven until a durable-volume restore is
exercised or recorded as an external dependency.

Use this command shape from a clean current-`main` checkout, replacing only the
secure token-file path with its configured location:

```bash
mkdir -p /tmp/stint-main-onbox-smoke
go build -o /tmp/stint-main-onbox-smoke/stint ./cmd/stint
STINT_BIN=/tmp/stint-main-onbox-smoke/stint \
STINT_ONBOX_RUN_ID=20260923-postland-live \
STINT_ONBOX_RUN_ROOT=/tmp/stint-main-onbox-smoke/state \
STINT_ONBOX_CONFIG_ROOT=/tmp/stint-main-onbox-smoke/config \
STINT_ONBOX_ARTIFACT_DIR=/tmp/stint-main-onbox-smoke/artifacts \
STINT_GITHUB_TOKEN_FILE=/path/to/secure/github.token \
STINT_GITHUB_REPOSITORY=Marguelgtz/Stint \
STINT_GITHUB_BASE=main \
STINT_GITHUB_MODE=engineering \
STINT_GITHUB_APPROVAL=internal \
bash scripts/run-onbox-deep-smoke.sh
```
