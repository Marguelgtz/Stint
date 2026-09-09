# Stint pull-request maintenance and landing

## Objective

Inspect the Stint pull-request graph, repair only straightforward review
comments, and land safe owner-authored changes in declared dependency order.
Preserve all operator-local work as an explicit boundary and leave a complete
handoff for anything that needs human review.

## Success

- Every open PR has a compact inventory and a classification.
- Only eligible, reviewed, owner-authored PRs are merged through the GPU gatekeeper.
- Deep Work branches and PRs #80–#84 remain excluded from automatic merges.
- Repairs, merges, skipped PRs, preserved work, and final checks are auditable.

## Constraints

- Use the clean `stint/provenance-easy-wins` baseline; the active checkout's dirty and untracked files are outside this run.
- Work only on Stint. Do not touch Vanta or discard operator-local files.
- Never force-push, retarget, close, delete, or administrator-merge a PR.
- New bugs and features are backlog candidates unless explicitly accepted by a task.
- Preserve each PR's declared base and the repository's merge-commit convention.

## GitHub

mode: maintenance
repository: Marguelgtz/Stint
base: main
allowed-authors: Marguelgtz
approval: internal

## Completion

policy: report-and-destroy

## Verification

go test ./...

## Tasks

- [ ] PLAN-001: Freeze the clean baseline and inventory every open PR and local/remote development branch.
  - phase: plan
  - reasoning: xhigh
  - acceptance: `deep-work/pr-maintenance/inventory.json` and `deep-work/pr-maintenance/baseline.md` record repository HEAD, PR state, dependency graph, and preserved local work.
  - verify: test -s deep-work/pr-maintenance/inventory.json && test -s deep-work/pr-maintenance/baseline.md
- [ ] T-002: Classify every PR as eligible, repairable, draft, deep-work, blocked, duplicate, already integrated, or human-review-required.
  - phase: review
  - reasoning: xhigh
  - acceptance: `deep-work/pr-maintenance/triage.md` records the classification and evidence for every open PR.
  - verify: test -s deep-work/pr-maintenance/triage.md
- [ ] T-003: Repair only straightforward review comments or mechanical inconsistencies on eligible existing PR heads.
  - phase: work
  - reasoning: medium
  - acceptance: `deep-work/pr-maintenance/repair-ledger.md` records each narrow repair, reply, commit, and fresh check result.
  - verify: test -s deep-work/pr-maintenance/repair-ledger.md
- [ ] T-004: Review and submit merge requests for eligible PRs in declared dependency order.
  - phase: review
  - reasoning: xhigh
  - acceptance: `deep-work/pr-maintenance/merge-ledger.md` records gate evidence, head SHAs, merged PRs, and precise skip reasons.
  - verify: test -s deep-work/pr-maintenance/merge-ledger.md
- [ ] CLOSE-001: Reconcile versions and repository state, record bounded backlog candidates, and publish the final handoff.
  - phase: close
  - reasoning: xhigh
  - acceptance: `deep-work/pr-maintenance/final-report.md` records final heads, tests, merged/skipped PRs, preserved work, and the next human action.
  - verify: test -s deep-work/pr-maintenance/final-report.md && go test ./...
