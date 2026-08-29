# Next Stint: first real Cline-ready compute

## Outcome

The stint ends when a real Vast worker can be discovered, rented under a hard budget, bootstrapped with Qwen3.8-27B, exposed through `http://127.0.0.1:8409/v1`, used by Cline for one local tool cycle, and destroyed reliably.

The work is split into four sequential phases. A later phase does not begin until the previous phase's exit gate passes.

## Phase 1 — Prove live marketplace discovery

**Goal:** turn the current `0 candidates` result into an explained, reproducible provider result without changing Stint's execution budget.

### Work

1. Match Vast's documented offer-search request shape exactly where documentation is unambiguous.
2. Use `type: on-demand` for marketplace searches, matching Vast's current official request examples.
3. Keep the intended full discovery query read-only.
4. If the full query returns zero rows, automatically run a read-only filter bisect:
   - rentable verified on-demand inventory
   - + RTX 4090
   - + one GPU
   - + requested duration
   - + loose discovery price ceiling ($0.60/hr)
   - + intended 50 GB storage/full request shape
5. Return stage counts in the error so the developer can see which filter collapses inventory.
6. Do not relax the hard local `$0.40/hr` execution ceiling, reliability threshold, VRAM requirement, direct-port requirement, or session ceiling.
7. Keep fixture behavior unchanged as the planner control case.

### Findings to capture

| Finding | Result |
|---|---|
| Official Vast request type | `on-demand` in current API examples |
| Fixture planner | 2/2 candidates, fixture-fast selected |
| Previous live full discovery | 0 candidates |
| Rentable inventory count | pending real rerun |
| RTX 4090 count | pending real rerun |
| One-GPU 4090 count | pending real rerun |
| Duration-qualified count | pending real rerun |
| <= $0.60 discovery count | pending real rerun |
| Full 50 GB request count | pending real rerun |
| Root collapsing filter | pending real rerun |

### Exit gate

```text
[ ] go test ./... passes
[ ] go vet ./... passes
[ ] Vast search uses the documented on-demand request value
[ ] live run returns >=1 candidate OR identifies the exact provider-side collapsing stage
[ ] fixture still passes unchanged
[ ] no instance is created
```

If the bisect identifies a provider semantic mismatch, fix only that mismatch and repeat Phase 1 acceptance. If the marketplace genuinely has no acceptable 4090, record that result and make the fallback-GPU decision explicitly before Phase 2.

### Acceptance commands

```bash
make build
./bin/stint doctor
./bin/stint plan interactive --hours 1
./bin/stint plan interactive --hours 1 --fixture
```

A zero live result is acceptable only if its error contains the discovery bisect counts.

---

## Phase 2 — Safe rent and instance ownership

**Goal:** create exactly one planner-selected instance and make ownership/cleanup deterministic before installing a model.

### Work

- add and verify `instance_write`
- introduce a narrowly scoped create/rent method for the selected offer only
- revalidate offer price and session ceiling immediately before mutation
- persist instance ID atomically as soon as Vast returns it
- implement lifecycle states `PLANNING -> RENTING -> BOOTING -> SSH_READY`
- poll instance state with bounded timeouts
- prove SSH using the dedicated Stint key and a trivial remote command
- implement `stint down`
- guarantee cleanup on startup failure, Ctrl-C, and deadline expiry
- verify destroy completion through Vast before clearing local state

### Exit gate

```text
[ ] one real instance can be rented
[ ] ID is persisted immediately
[ ] SSH succeeds
[ ] stint down destroys it
[ ] forced startup failure also destroys it
[ ] no orphan instance remains
[ ] actual spend is recorded
```

---

## Phase 3 — Remote Qwen runtime and stable localhost tunnel

**Goal:** convert the owned GPU into a stable OpenAI-compatible inference endpoint.

### Work

- bootstrap a pinned llama.cpp runtime
- fetch/persist the chosen Qwen3.8-27B GGUF on the worker
- start llama-server on remote `127.0.0.1:8080`
- poll runtime/model readiness with explicit timeout
- supervise the SSH forward `127.0.0.1:8409 -> remote 127.0.0.1:8080`
- verify `/v1/models`
- send one completion through localhost
- collect time-to-SSH, bootstrap time, model-load time, TTFT and decode tok/s
- make tunnel/runtime failure transition to cleanup safely

### Exit gate

```text
[ ] localhost:8409/v1 survives a real inference request
[ ] remote model is Qwen3.8-27B
[ ] endpoint does not expose Vast host details to the client
[ ] timer/budget remain active
[ ] teardown destroys compute and tunnel
```

---

## Phase 4 — Cline acceptance and repeatability

**Goal:** prove the actual product invariant rather than just raw inference.

### Work

- configure Cline once to `http://127.0.0.1:8409/v1`
- use a local repository; repository and terminal remain local
- ask Cline to inspect a file, make a trivial edit and run a local test
- stop the stint
- start a second stint that lands on any valid Vast host
- repeat without changing Cline configuration
- document operator UX, startup time, actual cost and failure/recovery behavior

### Final exit gate

```text
[ ] Cline completes one real multi-turn tool cycle
[ ] Cline config remains unchanged across a new remote worker
[ ] local repo/tools never move to Vast
[ ] hard session deadline tears compute down
[ ] manual stint down tears compute down
[ ] no orphaned instance remains
[ ] measured cost and latency are understood
```

## Explicitly out of scope for this stint

- two-worker RTX 3090 deep mode
- Spark reciprocal-agent collaboration
- provider failover
- daemon/background scheduling
- multiple model routing
- UI
- caching optimization beyond what is required for the first reliable run
