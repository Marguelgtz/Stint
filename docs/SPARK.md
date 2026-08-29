# Spark integration

Stint uses Spark as the repository-change observability and collaboration boundary.

For pre-V0, Spark has two jobs in this repository:

1. Observe Stint pull requests and produce deterministic change/evidence history.
2. Provide the contract boundary that the future deep-worker collaboration engine will use for task handoffs, review evidence, and change trajectory.

Spark itself remains a separate GitHub App/service. Stint does not embed Spark's evaluator or GitHub credentials.

## Repository profile

The checked-in profile is `.spark/profile.yml` and follows Spark's version-1 repository profile shape.

Stint marks these surfaces as high criticality:

- interactive control plane (`apps/cli`, `packages/core`, `packages/router`)
- Vast compute provider
- llama.cpp model runtime
- Spark/collaboration contracts
- release and automation configuration

The current expected evidence names are:

- `spark-profile`
- `typecheck`
- `unit-tests`

The GitHub Actions workflow intentionally uses those exact job/check names so Spark can observe the evidence without aliases.

## Onboarding this repository

After the repository has been created and pushed to GitHub:

```bash
pnpm stint -- onboard spark
```

Then:

1. Open the Spark dashboard.
2. Install/enable the Spark GitHub App for `Marguelgtz/stint`.
3. Verify `.spark/profile.yml` is present on the default branch.
4. Open a small pull request.
5. Confirm GitHub Actions emits `spark-profile`, `typecheck`, and `unit-tests`.
6. Confirm Spark posts an evaluation check for the PR head SHA and the PR appears in Spark's dashboard/history.

Do not put Spark credentials into Stint. Installation tokens are owned by the Spark service and generated on demand.

## Pre-V0 collaboration boundary

Stint currently keeps worker lifecycle behind `@stint/contracts` and repository profile/onboarding logic in `@stint/spark`.

The next collaboration slice should extend this boundary with small artifacts rather than copying Spark's entire domain model into Stint:

- worker became available / retired
- task assigned / checkpointed / blocked
- candidate commit ready for peer review
- review finding created / resolved
- deterministic test evidence attached
- integration candidate accepted

Spark should remain the durable trajectory/evidence layer; Stint remains responsible for compute and agent execution.
