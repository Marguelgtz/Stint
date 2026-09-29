# Integrated Deep Work qualification ledger

## Pre-rental rehearsal — 2026-09-29

**Decision: NO-GO for paid execution.** The offline implementation and rehearsal checks pass, but no numeric hourly, duration, or total-cost ceiling has been authorized. The required live provider instance, independent watchdog identity, and exact-instance teardown inventory check therefore do not exist yet. No compute was rented, and no Stint PR was retargeted or closed.

The qualification target remains the production on-box route at PR #183 tip `02436c01d17ff541b1d9902baa00bf5eb08f3ac7`. PR #169 is open against `main`; PRs #170–#183 remain open in the strict ancestor chain. The inspected PR checks for #169, #174, #179, and #183 were successful. This run does not qualify remoteGit or remote-Hermes transport.

### Offline checks completed

- `go test ./...` and `go vet ./...` passed in Stint.
- `go test -race ./internal/deep ./cmd/stint` passed.
- The focused qualification, Git subject parity, exact-instance teardown, and supervised receipt-recovery tests passed. The supervised fixture confirmed a durable receipt before the coordinator exit, a surviving supervisor and watchdog, a new resume epoch, receipt reconciliation, and one Hermes invocation.
- The Python publisher, R2 archive, and off-box verification suites passed (17 tests). Chronology fixtures cover accepted retries and repairs, a repair before its dependent task, resume identities, evidence-only no-diff checkpoints, and identity conflicts.
- `bash -n` passed for the launcher, supervisor, and fresh-box provisioner. Python compilation and `git diff --check` passed.
- Spark is isolated on `stint/deep-work-qualification`, based on pinned `main` `74527236ec64c23f61903ea3bc1cd6f05663a096`. Mission and probe inputs are committed at `e85c43126f827b88934d55f3eac57d4f27747618`; the product exporter remains at the pinned baseline.
- The mission and both v2 contracts parsed. Acceptance contract identity: `2faed9e667366d9c4465e10b43393312b8be05bacc37f3a1643f2db5eee45bfb`; semantic review contract identity: `d12f25010cc0e8d7ecd608fe9bdf8ab5bfb6d32c6b2cb5acb4784072d23c6c3b`.
- Mission verifier identity is `dba6ba3f1583df88a00558c08c82f8993a055861843bacec9405fb5cbd860d31`. Both task generic verifier identities are `873e6695e956d1b4d2a7cef5daa21e19c766daf3499bebd355d3ea5199b1a55c`; acceptance probe identities are `14deeddbfe088eb79c2c3a465f3e4fc87aaf16205143c21666b7756c3419935e` for EXPORT-001 and `3c0ca6eded7e724154a38367d7978336cb79edd2c272a0b5baebd8d4c9c86cde` for EXPORT-002.
- The focused Spark core suite passed (19 files, 157 tests), followed by the root typecheck and `git diff --check`. Both acceptance probes failed on the intended baseline defects: invalid `maxRecords` values were accepted, and locale-sensitive ordering disagreed with the required code-unit order.
- pnpm `10.15.1` and frozen-lockfile installation were exercised. The exact generic verifier in the mission is `pnpm --filter @spark/core test && pnpm typecheck && git diff --check`.
- R2 credentials and bucket configuration are present. Read access passed. A unique small preflight object was uploaded, read back, deleted, and confirmed absent. The operator verifier's `boto3` dependency is installed in the isolated environment `~/.local/share/stint/qualification-venv`; use that environment's Python to run `scripts/qualification-r2-fetch-verify.py` after retrieval.
- Existing provider-safe teardown tests target a single instance ID, retain local state while inventory still shows that instance, and finalize only after inventory confirms it is gone. These tests used a fake provider; no live instance was touched.

### Live gate still required

Before recording GO, bind a concrete rental to an hourly offer ceiling, a maximum duration, and a total-cost ceiling that leaves time for both tasks, landing, evidence retrieval, and teardown. Confirm the selected instance ID, independent watchdog process/session identity, R2 prefix, and operator-side `stint down` recovery path before starting the supervisor. Acknowledged destruction is insufficient: provider inventory must show that exact ID absent.

The isolated Spark branch is committed and ready to stage. Stint qualification support is committed on the local D4 branch at `a4f2eef16e045512db833dd489a86f97b71057fa`, directly on top of #183, and its qualification binary was built with SHA-256 `6085e76739cfa19c0d7450856f539c5c7674b05672f5f73a64a373030bb980ff`. The tooling commit has not been pushed to the public PR branch. After the live run, verify the final bundle off-box, record each manifest SHA-256, confirm exact-instance destruction, score the assertions below, and only then review PR consolidation.

## Assertion ledger

Every row is live-pending. Offline evidence does not establish production qualification.

| Objective | Assertion | Expected behavior | Offline evidence | Observed behavior | Score |
|---|---|---|---|---|---|
| A | `A.QUIESCENCE.1` | Owned Hermes process group stops before verification | Supervised receipt fixture and process-group tests | No live observation | Offline-qualified only |
| A | `A.SUBJECT_BINDING.1` | Pre/post verification subject identities match | Local/remote Git parity and mutation rejection tests | No live observation | Offline-qualified only |
| A | `A.CHECKPOINT_TREE.1` | Verified and checkpoint trees match, including HEAD reuse | Exact checkpoint and reused-HEAD tests | No live observation | Offline-qualified only |
| A | `A.LANDING_TREE.1` | Final verification and landing trees match | Landing subject-binding tests | No live observation | Offline-qualified only |
| A | `A.OUTCOME.1` | Landing phase and mission outcome remain distinct | Mission outcome state and event tests | No live observation | Offline-qualified only |
| A | `A.PERFORMANCE.1` | Existing Git capture costs are measured without added scans | Timing instrumentation is implemented | No live measurements | Deferred |
| B | `B.JOURNAL_SEQUENCE.1` | Journal sequence is continuous across a new epoch | Journal replay and receipt fault fixture | No live observation | Offline-qualified only |
| B | `B.RECEIPT_RECOVERY.1` | Durable receipt reconciles after coordinator restart | Supervised receipt fault fixture | No live observation | Offline-qualified only |
| B | `B.NO_DUPLICATE_EXECUTION.1` | Recovery does not invoke Hermes twice | Supervised fixture invocation counter | No live observation | Offline-qualified only |
| C | `C.ACCEPTANCE_BOUNDARY.1` | Generic verification and checkpoint precede the separate contract decision | Acceptance runtime tests and baseline probes | No live observation | Offline-qualified only |
| C | `C.DEPENDENCY_GATE.1` | Dependent work waits for accepted prerequisite | Acceptance dependency-gate tests | No live observation | Offline-qualified only |
| D | `D.OBJECTIVE_REVIEW.1` | Fresh no-tools review is bound to the accepted checkpoint | Structured reviewer packet and protocol tests | No live observation | Offline-qualified only |
| D | `D.REPAIR_ROUTING.1` | A grounded finding follows the durable repair chain if naturally raised | Offline finding, repair, and resolution tests | No natural live finding observed | Offline-qualified only |
| D | `D.MISSION_REVIEW.1` | Fresh whole-mission review is bound to landing | Mission reviewer contract and outcome tests | No live observation | Offline-qualified only |
| Operations | `OPS.EVIDENCE.1` | Final manifest and every artifact verify off-box | Export/tamper tests and R2 write/read/delete preflight | No run bundle yet | Offline-qualified only |
| Operations | `OPS.TEARDOWN.1` | Destroy is requested for the recorded ID and that exact ID disappears | Fake-provider teardown tests | No live instance exists | Offline-qualified only |

Classify later failures as product failure, infrastructure failure, expected adversarial result, or inconclusive evidence. Use `live-qualified`, `offline-qualified only`, `failed`, `inconclusive`, or `deferred` for capability outcomes. Do not infer remote transport or large-repository qualification from this on-box Spark run.
