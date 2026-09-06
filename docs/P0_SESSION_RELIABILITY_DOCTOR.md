# Stint P0 Session Reliability & Doctor

**Status:** P0
**Type:** Living action plan
**Branch:** `fix/p0-session-reliability-doctor`
**Primary objective:** Prevent paid Stint sessions from becoming opaque, unnecessarily destroyed, or unintentionally left running when startup/connectivity fails.

## Reliability invariants

1. Paid compute is never assumed destroyed. Active paid-resource state is cleared only after Vast confirms destruction or confirms the instance no longer exists.
2. Every paid session remains diagnosable after teardown.
3. `stint doctor` actively verifies reality; `stint status` reports recorded state plus cheap health.
4. Startup progress such as model download is not misclassified as runtime failure.
5. `stint doctor` is read-only by default.
6. Recoverable compute is preferred over a second rental.

## Execution order

- [~] **P0-A Paid-resource destruction safety**
  - [ ] Shared destroy-and-confirm primitive
  - [ ] Retry transient provider/network failures
  - [ ] Preserve state on unconfirmed destruction
  - [ ] Migrate watchdog and `stint down`
  - [ ] Tests
- [ ] **P0-B Durable session evidence**
  - [ ] Per-session log/history paths
  - [ ] Archive state on teardown
  - [ ] Terminal disposition
- [ ] **P0-C Active diagnostic engine**
  - [ ] Provider probe
  - [ ] SSH probe
  - [ ] Runtime/model probe
  - [ ] Tunnel probe
  - [ ] Local endpoint probe
  - [ ] Stable diagnostic classification
- [ ] **P0-D CLI integration**
  - [ ] Active-session `stint doctor`
  - [ ] `stint doctor --last`
  - [ ] `status --refresh` summary
- [ ] **P0-E Fault injection tests**
- [ ] **P0-F Controlled live validation**

## Incident evidence

The observed shared tunnel log contained repeated remote-forward connection refusals, but also entries from multiple Vast hosts. The append-only log is therefore insufficient to attribute every line to one rental. The watchdog log also showed a destroy attempt failing on a DNS timeout. This makes reliable teardown the first safety gate.

## Diagnostic model

Treat the path as independent layers:

`Vast API -> instance -> SSH TCP -> SSH auth -> runtime/bootstrap -> model acquisition -> runtime process -> remote :8080 -> SSH tunnel -> local :8409 -> /v1/models -> client`

A downstream symptom must not be reported as the root cause when a deeper layer can be tested.

## Initial diagnostic classes

`OK`, `PROVIDER_UNREACHABLE`, `INSTANCE_MISSING`, `INSTANCE_NOT_RUNNING`, `SSH_METADATA_MISSING`, `SSH_TCP_UNREACHABLE`, `SSH_CONNECTION_REFUSED`, `SSH_AUTH_FAILED`, `SSH_COMMAND_FAILED`, `RUNTIME_BINARY_MISSING`, `RUNTIME_PROCESS_DEAD`, `RUNTIME_PROCESS_STARTING`, `MODEL_DOWNLOADING`, `MODEL_VERIFYING`, `MODEL_LOAD_IN_PROGRESS`, `MODEL_DOWNLOAD_FAILED`, `MODEL_CHECKSUM_FAILED`, `REMOTE_PORT_NOT_LISTENING`, `REMOTE_ENDPOINT_REFUSED`, `REMOTE_ENDPOINT_TIMEOUT`, `REMOTE_ENDPOINT_HTTP_ERROR`, `TUNNEL_PROCESS_MISSING`, `TUNNEL_PROCESS_DEAD`, `TUNNEL_FORWARD_INVALID`, `LOCAL_PORT_NOT_LISTENING`, `LOCAL_PORT_CONFLICT`, `LOCAL_ENDPOINT_REFUSED`, `LOCAL_ENDPOINT_TIMEOUT`, `LOCAL_ENDPOINT_HTTP_ERROR`, `WATCHDOG_MISSING`, `DESTROY_UNCONFIRMED`.

Severity vocabulary: `HEALTHY`, `PROGRESS`, `DEGRADED`, `RECOVERABLE`, `FATAL`, `SAFETY`.

## Exit criteria

- [ ] Watchdog retries transient destruction failures.
- [ ] Manual `down` and watchdog share destruction-confirmation logic.
- [ ] State is never cleared after an unconfirmed destroy.
- [ ] Sessions have isolated logs.
- [ ] Destroyed sessions retain forensic metadata.
- [ ] `stint doctor` actively diagnoses an existing paid session.
- [ ] Doctor distinguishes provider, SSH, runtime, model, tunnel, and local endpoint failures.
- [ ] Doctor distinguishes expected model startup from actual runtime failure.
- [ ] Doctor produces stable diagnostic codes and concrete recovery guidance.
- [ ] `stint doctor --last` works after teardown.
- [ ] Deterministic tests cover transient destroy failure and remote-port-refused-during-download.
- [ ] One controlled Vast validation completes the lifecycle and confirms teardown.

## Living-plan protocol

Use `[ ] planned`, `[~] in progress`, `[x] verified`, `[!] blocked`, and `[-] deferred`.

Do not erase disproven hypotheses. Move them into a rejected/superseded section with the evidence that changed the decision. Each live validation should append date, commit, instance ID, runtime/config, observed timeline, diagnosis, unexpected behavior, cost, and final destruction confirmation.
