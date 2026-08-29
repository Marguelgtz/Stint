# Authentication model

Stint deliberately separates **product identity**, **repository authorization**, and **compute-provider credentials**.

## Product identity: GitHub

Stint will use the same identity model as Spark:

1. GitHub OAuth authenticates the human user.
2. PKCE/state protects the browser authorization flow.
3. The GitHub OAuth token proves identity only and is not the source of repository authorization.
4. Repository access is resolved through the GitHub App installation/user-access model.
5. Stint does not invent a second username/password system.

For the local single-user pre-v0 CLI, hosted product login is not required yet. The CLI runs as the local OS user. When Stint gains a hosted control plane/UI, `stint login` should use the same GitHub identity/session architecture as Spark rather than introducing a parallel auth system.

## Repository authorization: GitHub App

Spark remains the repository/evidence integration. GitHub App installation state determines which repositories the authenticated user may operate on. Stint should reuse that boundary for future remote orchestration and team features.

## Compute-provider authentication: separate BYOC credentials

A Vast API key is not a Stint identity credential. It is a provider credential used only to access the user's own compute account.

Pre-v0 stores it locally at:

```text
~/.config/stint/credentials.json
```

with owner-only permissions. It is never committed, sent to GitHub, sent to Spark, or exposed to the model runtime.

The minimum capability progression is:

```text
Phase 1/2  instance_read + misc
Phase 3    + instance_write
```

`misc` is required for Vast marketplace search. `instance_read` is required to inspect lifecycle state. `instance_write` is intentionally unnecessary until Stint is allowed to rent/destroy compute.

## Design invariant

```text
GitHub identity        -> who is using Stint
GitHub App             -> which repositories they may operate on
Vast provider key      -> which compute account Stint may use
Spark                  -> repository evidence/change history
```

These authorities must remain separate in code and storage.
