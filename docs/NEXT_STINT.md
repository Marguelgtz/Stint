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

### Findings captured

| Finding | Result |
|---|---|
| Official Vast request type | `on-demand` in current API examples |
| Fixture planner | 2/2 candidates, fixture-fast selected |
| Previous live full discovery | 0 candidates |
| Rentable verified on-demand inventory | 250 returned at API limit |
| RTX 4090 within that verified/rentable inventory | 0 |
| One-GPU 4090 count | 0 because the GPU stage already collapsed |
| Duration-qualified count | 0 because the GPU stage already collapsed |
| <= $0.60 discovery count | 0 because the GPU stage already collapsed |
| Full 50 GB request count | 0 because the GPU stage already collapsed |
| Root collapsing stage | adding `gpu_name = RTX_4090` to verified/rentable on-demand inventory |

### Interpretation

The live Vast API is reachable and returning a full page of verified/rentable on-demand inventory. The current account-visible marketplace query has no RTX 4090 in that verified/rentable set at the time of the acceptance run. This is different from a planner failure: the fixture path remains healthy and the provider bisect isolates the zero result before local Stint eligibility runs.

The Phase 1 result does **not** prove that Vast has no RTX 4090 hosts of any kind. The bisect intentionally keeps `verified=true`, `rentable=true`, and `rented=false` in the base inventory stage. Therefore an unverified, already-rented, bid, or otherwise non-policy-compliant 4090 may still exist and is intentionally excluded.

### Exit gate

```text
[x] go test ./... passes in CI for the diagnostic implementation
[x] go vet ./... passes in CI for the diagnostic implementation
[x] Vast search uses the documented on-demand request value
[x] live run identifies the exact provider-side collapsing stage
[x] fixture still passes unchanged
[x] no instance is created
```

**Phase 1 status: complete.**

Before Phase 2, choose the explicit interactive fallback policy rather than silently weakening trust or budget requirements. The preferred direction for the first lifecycle proof is to keep verified hosts mandatory and permit another 24 GB+ NVIDIA GPU as a fallback when a verified RTX 4090 is unavailable. The rental implementation itself should remain GPU-agnostic once the planner has selected an eligible offer.

### Acceptance commands

```bash
make build
./bin/stint doctor
./bin/stint plan interactive --hours 1
./bin/stint plan interactive --hours 1 --fixture
```

Recorded live result:

```text
Vast returned zero interactive candidates; discovery bisect:
rentable=250, gpu=0, one_gpu=0, duration=0, price=0, storage=0
```

---

## Phase 2 — Safe rent and instance ownership

**Goal:** create exactly one planner-selected instance and make ownership/cleanup deterministic before installing a model.

### Work

- choose and encode the explicit interactive fallback GPU policy before enabling mutation
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
