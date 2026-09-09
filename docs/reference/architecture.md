# Architecture

## Control plane

Stint is a local Go control plane. Agent harnesses should only need stable OpenAI-compatible localhost endpoints; Stint hides provider instance IDs, SSH addresses, model boot, and teardown behind those endpoints.

```text
Cline / OpenCode / other harness
              |
              v
          Stint daemon
              |
     +--------+---------+
     |                  |
Compute broker       Runtime manager
     |                  |
   Vast           llama.cpp / future
     |                  |
4090 interactive   fixed local tunnel
3090 deep A/B      ports 8409/8301/8302
```

## Package boundaries

- `internal/core`: provider-neutral profiles, offers, ranking, and session plans.
- `internal/provider/vast`: Vast API/CLI normalization. Pre-V0 is read-only until offer selection is proven.
- `internal/runtime/llama`: runtime/endpoint configuration.
- `internal/router`: chooses the profile and eventually model/GPU topology.
- `internal/spark`: onboarding and evidence contract with Spark.
- `internal/collaboration`: narrow data contract for future reciprocal workers.

## First topology

Interactive prioritizes user latency: one RTX 4090, Qwen3.8-27B, llama.cpp, local endpoint `127.0.0.1:8409`.

Deep work prioritizes parallel validated work per dollar: two independent RTX 3090 workers, isolated worktrees, reciprocal build/review loops, endpoints `127.0.0.1:8301` and `127.0.0.1:8302`.

## Safety boundary

Search/ranking is separate from provisioning. A future mutating provider interface must require an explicit session budget, maximum hourly price, requested duration, and teardown deadline. No provider key is stored in repository configuration.
