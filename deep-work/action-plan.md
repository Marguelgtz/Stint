# Stint PR maintenance living action plan

This plan is owned by the GPU supervisor for this run. Keep it current as evidence arrives.

## Decisions
- Baseline: clean `stint/provenance-easy-wins`.
- GitHub mode: maintenance; repository `Marguelgtz/Stint`; base `main`.
- Approval: internal evidence bound to the exact reviewed head SHA.
- Completion: report-and-destroy.
- Preserve the operator checkout boundary recorded in `operator-boundary.json`.

## Working method
- Inventory all open PRs once, then retrieve detailed context only for the PR under review.
- Repair only narrow, mechanical review comments; record every side effect in the GPU ledger.
- Merge only when every deterministic gate and xhigh review evidence passes.
- Keep PRs #80–#84 and all `stint/deep-*` branches excluded.
- Record new bugs/features as bounded backlog candidates.

## Evidence to maintain
- inventory and baseline evidence
- per-PR classifications and gate decisions
- repair and merge ledgers with head SHAs
- final tests, publication state, preserved work, and next human action
