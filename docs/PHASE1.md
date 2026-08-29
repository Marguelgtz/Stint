# Phase 1 — Local foundation and Vast authentication

## Outcome

Phase 1 makes a developer machine ready for Stint without installing the Vast CLI or renting compute.

The exit condition is:

```text
stint doctor

Vast credentials   ✓ authenticated
OpenSSH            ✓
Stint SSH key      ✓
Local port 8409    ✓ available
```

At this point Phase 2 can safely query the live Vast marketplace.

## Execution plan

### 1. Own local Stint state

Use XDG-style local paths:

- `~/.config/stint/credentials.json` — Vast API key, mode `0600`
- `~/.config/stint/ssh/id_ed25519` — dedicated Stint private key, mode `0600`
- `~/.config/stint/ssh/id_ed25519.pub` — public key
- `~/.local/state/stint/` — runtime state reserved for later phases

The repo never contains provider credentials or private keys.

### 2. Authenticate directly against Vast

`stint auth vast` reads the API key with terminal echo disabled, verifies it using an authenticated `GET /api/v0/instances/`, and persists it only after a successful response.

For automation or an already-exported variable, `stint auth vast --from-env` reads `VAST_API_KEY`.

This establishes the provider boundary directly through Vast's REST API; no `vastai`, Python, or pip dependency is introduced.

### 3. Establish a dedicated SSH identity

`stint setup ssh` uses the system `ssh-keygen` to generate an Ed25519 keypair owned by Stint. It never replaces the user's normal SSH keys.

The command prints the public half for one-time registration in Vast Account → Keys → SSH Keys. Vast applies account SSH keys to newly created instances, which is the behavior Phase 3 will rely on.

### 4. Prove local readiness

`stint doctor` checks:

- credentials exist and still authenticate against Vast;
- the OpenSSH client exists;
- the dedicated Stint keypair exists;
- `127.0.0.1:8409` is currently available.

`stint status` provides a non-secret local summary.

## Security rules

- Never print the Vast API key.
- Verify a key before saving it.
- Credential and private-key files are owner-readable/writable only.
- Never write credentials into the repository.
- No mutating Vast endpoint exists in Phase 1.
- Port `8409` is reserved as the stable local Cline endpoint for the later tunnel.

## Manual acceptance sequence

```bash
make build
./bin/stint auth vast
./bin/stint setup ssh
# Add the printed public key to the Vast account once.
./bin/stint doctor
./bin/stint status
```

Phase 1 does not rent a GPU. Phase 2 begins only after this sequence is green.
