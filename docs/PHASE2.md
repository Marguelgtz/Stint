# Phase 2 — Live RTX 4090 planner

## Outcome

Phase 2 turns `stint plan interactive` into a real, read-only Vast marketplace decision engine. It must discover real RTX 4090 inventory, explain why candidates do or do not qualify, rank eligible machines deterministically, and remain incapable of renting, stopping, or destroying compute.

## Product invariant

Cline will eventually remain fixed on:

```text
http://127.0.0.1:8409/v1
```

Phase 2 does not create that tunnel. It proves that Stint can safely choose the worker Phase 3 will attach to the endpoint.

## Authentication boundary

Stint product identity follows Spark: GitHub OAuth for human identity and GitHub App authorization for repositories. Vast remains a separate local BYOC provider credential. Phase 2 requires only `instance_read` and `misc/search`. No `instance_write` path exists.

## Marketplace policy

The Phase 2.1 policy deliberately separates provider discovery, hard eligibility, and preferences.

### Provider discovery filters

These are sent to Vast to bound inventory without hiding useful candidates from Stint:

```text
GPU                     RTX_4090
GPU count               1
verified                true
rentable                true
already rented          false
instance type           on-demand
offer duration          >= requested stint duration
allocated storage       50 GB
discovery price guard   <= $0.60/hour
limit                   250 candidates
```

The `$0.60/hour` discovery guard is not the Stint budget. It only prevents clearly irrelevant inventory from dominating the response. Stint still enforces its stricter local `$0.40/hour` hard ceiling.

### Hard local eligibility

Every normalized offer is independently evaluated by Stint. An offer is rejected when any of these fail:

```text
GPU                     RTX_4090
hourly total            <= $0.40
reliability             >= 0.985
GPU RAM                 >= 24,000 MB
direct ports            >= 1
verified                true
rentable                true
already rented          false
```

Session cost remains independently capped at `$2.50`.

### Preferences, not hard gates

These are surfaced and used in ranking but do not make an otherwise valid offer disappear:

```text
internet download       preferred >= 50 MB/s
GPU max power           preferred >= 350 W
DLPerf                  higher is better
```

Network throughput is not a reliable proxy for inference latency after the model is loaded. GPU power is also a proxy rather than an outcome. Phase 3 can replace both with measured startup time, TTFT, prompt throughput, and decode throughput.

## Deterministic interactive ranking

Among hard-eligible offers:

```text
1. preferred GPU order
2. higher DLPerf
3. higher reliability
4. higher internet download throughput
5. lower total hourly cost
6. higher GPU max-power allowance
7. offer ID tie-break
```

The ordering stays explicit and inspectable. No opaque weighted score is introduced in Phase 2.

## Diagnostic contract

The planner must never collapse a real marketplace search to an unexplained `zero offers` result when Vast returned candidates.

For every search Stint computes `OfferEvaluation` records with all hard rejection reasons. The CLI reports:

```text
MARKETPLACE DIAGNOSTICS
Candidates inspected  N
Hard qualified        M

Rejected by hard constraint:
  price               ...
  reliability         ...
  direct_ports        ...
  vram                ...
  ...

Closest rejected candidates:
  location  price  reliability  DLPerf  network  power  fails=[...]

NO COMPUTE HAS BEEN RENTED.
```

JSON planning output includes the same diagnostics. A failed plan still exits non-zero after printing the diagnostic evidence.

## Precise Phase 2.1 action plan

### A. Baseline real-machine observation

- [x] Build Stint locally.
- [x] Verify `instance_read` against the real Vast account.
- [x] Verify `misc/search` against the real Vast account.
- [x] Verify OpenSSH, Stint SSH key, and local port 8409.
- [x] Run `stint plan interactive --hours 1` against Vast.
- [x] Record that the original strict provider query returned no qualifying offer.
- [x] Run the fixture planner and verify ranking/session output.

### B. Correct provider/planner boundary

- [x] Remove reliability, network, direct-port, VRAM and power policy thresholds from the Vast discovery request.
- [x] Keep provider-side inventory identity/safety filters and duration/storage shape.
- [x] Add a bounded `$0.60/hour` discovery guard.
- [x] Increase candidate discovery limit to 250.
- [x] Keep all actual Stint eligibility checks local.

### C. Separate hard constraints and preferences

- [x] Keep price, reliability, VRAM, direct ports and provider state as hard eligibility.
- [x] Convert network throughput from a hard gate to a ranking preference.
- [x] Convert GPU max power from a hard gate to a ranking preference.
- [x] Keep the `$0.40/hour` and `$2.50/session` safety ceilings unchanged.

### D. Make failures explainable

- [x] Add typed rejection reasons.
- [x] Add `EvaluateOffer` / `EvaluateOffers`.
- [x] Count all rejection reasons instead of only first failure.
- [x] Show up to three closest rejected candidates.
- [x] Emit diagnostics in JSON and human output.
- [x] Preserve explicit `mutating=false` / `computeRented=false` safety state.

### E. Regression coverage

- [x] Test that provider discovery no longer applies local reliability/network/port/VRAM/power gates.
- [x] Test that network and power do not hard-reject an offer.
- [x] Test that multiple hard rejection reasons are retained.
- [x] Keep deterministic ranking tests.
- [x] Keep session budget tests.
- [ ] CI `go test ./...` green on the final Phase 2.1 head.
- [ ] CI `go vet ./...` green on the final Phase 2.1 head.

### F. Real-account rerun

After pulling the final branch locally:

```bash
git pull
make build
./bin/stint doctor
./bin/stint plan interactive --hours 1
./bin/stint plan interactive --hours 1 --json
```

Record the real candidate count, hard-qualified count, dominant rejection reasons, and selected machine. Do not relax any remaining hard constraint without evidence from this output.

### G. Phase 2 exit gate

- [ ] Real marketplace returns candidate inventory.
- [ ] At least one hard-qualified RTX 4090 is found, or diagnostics identify the exact remaining blocker.
- [ ] Selected offer is <= `$0.40/hour`.
- [ ] Session budget is enforced.
- [ ] Repeated plan commands create zero instances.
- [ ] Vast balance/instance list confirms no compute rental from planning.
- [ ] CI green.
- [ ] Findings below updated with the real rerun.

Only after this gate should Phase 3 introduce the first mutating capability.

## Findings log

### Finding 01 — 2026-08-29: real account setup passes

Observed on the developer machine:

```text
Vast instance read   ✓ instance_read authorized
Vast search          ✓ misc/search authorized
OpenSSH              ✓ /usr/bin/ssh
Stint SSH key        ✓ local keypair ready; Vast registration not verified
Local port 8409      ✓ available for Cline tunnel
```

Conclusion: authentication, local prerequisites, and search permission are not the cause of the planner failure.

### Finding 02 — 2026-08-29: original live policy produced zero qualifying offers

Command:

```bash
./bin/stint plan interactive --hours 1
```

Observed:

```text
Searching Vast for interactive compute...
stint: no qualifying interactive offers found within the hard policy limits
```

Conclusion: the original server-side query was too restrictive to diagnose. It filtered by price, reliability, network, direct ports, VRAM and GPU power before Stint could inspect the candidate population.

### Finding 03 — 2026-08-29: planner logic works with known-good inventory

Fixture command succeeded and selected the expected higher-DLPerf 4090 at `$0.350/hour`, with a `$0.35` one-hour session estimate and a `$2.50` session ceiling.

Conclusion: the failure is marketplace-policy interaction, not basic ranking/session-plan mechanics.

### Finding 04 — Phase 2.1 code change

Implemented:

- broad read-only Vast discovery
- local hard eligibility
- network/power preferences
- typed rejection diagnostics
- candidate and rejection counts
- closest rejected offer output

Pending evidence:

```text
CI result:                    PENDING
Real candidates inspected:   PENDING
Hard-qualified candidates:   PENDING
Dominant rejection reason:   PENDING
Selected offer:              PENDING
Selected hourly price:       PENDING
Selected DLPerf:             PENDING
Selected reliability:        PENDING
Selected network:            PENDING
Selected GPU power:          PENDING
```

## Safety invariant

Every Phase 2 human plan or diagnostic failure must make clear that no compute was rented. No provider mutation method is introduced by this phase.
