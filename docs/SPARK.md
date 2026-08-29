# Spark integration

Stint dogfoods Spark on its own pull requests. Spark remains a separate GitHub App and evidence system; Stint owns compute provisioning and runtime lifecycle.

## Repository profile

`.spark/profile.yml` marks these Go surfaces as high criticality:

- `cmd/stint/**` and `internal/core/**` for the interactive control plane.
- `internal/provider/vast/**` for marketplace and future provisioning behavior.
- `internal/runtime/llama/**` for model runtime behavior.
- `internal/spark/**` and `internal/collaboration/**` for the collaboration/evidence boundary.
- release automation and the profile itself.

## Evidence

GitHub Actions publishes stable checks that match the profile:

- `spark-profile`
- `go-vet`
- `unit-tests`

## Onboarding

Run:

```bash
go run ./cmd/stint onboard spark
```

Then install the Spark GitHub App for `Marguelgtz/Stint`, keep the profile on `main`, and use pull requests so Spark can observe exact-head evaluations and change trajectory.
