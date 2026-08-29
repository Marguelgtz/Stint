# Phase 2 — Live RTX 4090 planner

## Outcome

Phase 2 turns `stint plan interactive` into a real, read-only Vast marketplace decision engine.

The command must answer:

```text
Which real RTX 4090 should Stint use right now,
for this requested duration,
within hard cost/reliability/network/runtime constraints?
```

It must not be capable of renting, stopping, or destroying compute.

## Product invariant

Cline will eventually remain fixed on:

```text
http://127.0.0.1:8409/v1
```

Phase 2 does not create that tunnel yet. It establishes that Stint can safely select the remote worker Phase 3 will attach to that local endpoint.

## Authentication boundary

Stint product identity follows Spark: GitHub OAuth for human identity and GitHub App authorization for repositories. Vast authentication remains a separate local BYOC provider credential. See `docs/AUTH.md`.

For this local pre-v0 phase, no hosted Stint login is required. `stint auth vast` verifies only the provider capabilities needed for planning and future lifecycle inspection:

- `instance_read`
- `misc` / marketplace search

No `instance_write` call exists in Phase 2.

## Execution sequence

### 1. Correct and narrow the Vast transport

Use current Vast endpoints directly from Go:

```text
GET  /api/v1/instances   provider auth / instance_read check
POST /api/v0/bundles     read-only offer search / misc permission
```

The client must:

- send Bearer auth only to the configured Vast origin
- apply bounded request timeouts
- cap error bodies and response bodies
- classify 401, 403 and 429 into actionable errors
- never log the API key
- expose no generic mutation method

### 2. Search with the actual Phase 3 shape

The marketplace query must model the machine Stint intends to create, rather than showing unrealistically cheap default pricing.

Interactive pre-v0 policy:

```text
GPU                     RTX_4090 only
GPU count               1
verified                true
rentable                true
already rented          false
hourly total            <= $0.40
reliability             >= 0.985
GPU RAM                 >= 24,000 MB
GPU max power           >= 350 W
direct ports            >= 1
internet download       >= 100 MB/s
allocated storage       50 GB
offer duration          >= requested stint duration
instance type           on-demand
```

Vast's `inet_down` search field is documented in MB/s; Stint keeps that unit explicit in policy, code, JSON, and CLI output.

`allocated_storage=50` is included in the Vast search so `dph_total` reflects the intended disk allocation instead of Vast's smaller default allocation.

### 3. Normalize provider data at the boundary

Vast response DTOs stay inside `internal/provider/vast`.

Only durable Stint concepts enter `core.Offer`:

- offer ID
- canonical GPU model
- VRAM / max power
- total hourly cost
- reliability / DLPerf
- network throughput and download cost
- direct-port count
- location
- machine ID
- verified/rentable/rented flags

No raw Vast response leaks into the planner or CLI.

### 4. Re-apply every hard constraint locally

Server-side search is an optimization, not a trust boundary.

After normalization, `core.Qualifies` must independently reject an offer when any hard constraint fails.

This protects Stint against:

- API behavior changes
- stale marketplace rows
- provider filter regressions
- malformed fixtures/tests
- future provider adapters that do weaker server-side filtering

Stint never silently relaxes price, reliability, network, power, VRAM or direct-port limits.

### 5. Rank for interactive latency

Among qualifying offers, deterministic ranking is:

```text
1. preferred GPU order
2. higher DLPerf
3. higher reliability
4. higher GPU max-power allowance
5. lower total hourly cost
6. offer ID tie-break
```

Network is a hard provisioning threshold, not treated as measured inference latency. Geography is surfaced but does not receive an invented latency score in Phase 2.

Phase 4 can replace heuristics with measured TTFT / request latency data.

### 6. Enforce both hourly and session budgets

Planning must fail closed when either boundary is exceeded:

```text
hourly ceiling    $0.40/hour
session ceiling   $2.50
```

For example, a valid `$0.40/hour` worker is acceptable for five hours but an eight-hour plan is rejected because `$3.20` exceeds the session ceiling.

### 7. Make live planning the default UX

```bash
stint plan interactive --hours 5
```

uses the real marketplace.

Development escape hatches remain explicit:

```bash
stint plan interactive --hours 5 --fixture
stint plan interactive --hours 5 --json
```

Live deep planning remains out of scope until the single-worker path is proven.

### 8. Show the decision, alternatives and safety state

Human output must contain:

- selected offer
- location
- hourly cost
- reliability
- DLPerf
- network
- direct ports
- GPU power ceiling
- up to three alternatives
- requested duration
- planned storage
- estimated compute cost
- hourly/session ceilings
- provider download charge when reported

Every human plan ends with:

```text
NO COMPUTE HAS BEEN RENTED.
```

JSON includes explicit `mutating=false` and `computeRented=false` fields.

## Failure states

Phase 2 must produce specific failures rather than generic HTTP errors:

```text
401   API key rejected; re-authenticate
403   missing misc/search permission
429   Vast rate limit; retry later
timeout/network   provider unavailable
malformed JSON    provider response rejected
zero offers       no policy-compliant 4090 currently available
budget exceeded   plan rejected
```

No failure path broadens the policy automatically.

## Verification

Required unit coverage:

- current `/api/v1/instances` auth route
- Bearer header
- exact read-only `/api/v0/bundles` method/path
- on-demand request type
- RTX 4090 filter
- GPU count
- requested duration
- 50 GB allocated storage pricing
- hourly cap
- reliability threshold
- network threshold
- direct-port threshold
- VRAM threshold
- GPU power threshold
- response normalization
- 401 / 403 / 429 handling
- malformed response
- local fail-closed constraints
- deterministic ranking
- session-cost ceiling

An explicit integration test may query Vast only when:

```bash
STINT_VAST_INTEGRATION=1 VAST_API_KEY=... go test ./internal/provider/vast -run Integration
```

The integration test is search-only.

## Acceptance run on the developer machine

```bash
make build
./bin/stint auth vast
./bin/stint doctor
./bin/stint plan interactive --hours 1
./bin/stint plan interactive --hours 5
./bin/stint plan interactive --hours 5 --json
```

Then verify in Vast that:

- no instance exists because of these commands
- account balance has not incurred compute rental
- selected offer exists in the marketplace
- GPU is one RTX 4090
- displayed `dph_total` matches the provider result

Repeat the plan several times to exercise marketplace churn.

## Exit gate

Phase 2 is complete only when:

```text
[ ] Phase 1 doctor passes on the real machine
[ ] instance_read capability passes
[ ] misc/search capability passes
[ ] real RTX 4090 offers are returned
[ ] all hard constraints are locally revalidated
[ ] deterministic winner + alternatives are displayed
[ ] requested storage/duration are priced into the query
[ ] session budget is enforced
[ ] errors fail closed
[ ] repeated plans create zero instances
[ ] go test ./... passes
[ ] go vet ./... passes
[ ] Spark observes the PR/evidence
```

Only then may Phase 3 add the first mutating provider capability: create exactly the already-selected offer under a hard session budget and guaranteed cleanup path.
