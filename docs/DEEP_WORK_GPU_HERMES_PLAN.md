# Deep Work on the GPU with Hermes — Implementation Plan

**Status:** P0–P3 done. **P1** implemented and tested — the `--worker hermes` executor,
the remote git/verify seam over Stint's SSH channel, and the on-box worktree are in
`cmd/stint/deep_remote.go` + `deep_start.go`/`deep_resume.go`/`deep_run.go` (branch
`feat/deep-work-hermes-worker`, on top of `feat/deep-work-with-dashboard`), gated by
`go test ./cmd/stint/ ./internal/deep/`, `-race`, and `make check`. **P2** was run
against the live box (Ubuntu 24.04.3; Node v22.23.2 + Hermes v0.21.0 installed and
configured; provider smoke + headless-approval probes passed — §4.1) and is reproducible
via `scripts/provision-box.sh` + `scripts/box-smoke.sh` on any fresh Vast instance.
**P3** Vanta prep landed: per-task `verify:` for all 7 tasks, verified against the real
Stint parser, LENIENT pass / STRICT expected-fail on the pristine repo, baseline commit
`de47058`. **P4** (synthetic dry run on a dedicated box) and **P5** (real CP1 launch,
operator-driven) are prepared. The Vanta CP1 launch requires a *dedicated* compute box:
the first P2 box later became this session's inference host and must not be re-provisioned.
Supersedes the Cline-local-worker assumption for the first real (Vanta CP1) run.

**Goal:** run Deep Work so that the *heavy work executes on the rented GPU box* —
the agent worker, the target repo/worktree, the S0 implementations, and all
verification run **on the box** — while the Stint coordinator, durable state,
watchdog, and handoff remain on the operator's machine. The model is served on the
box by NInfer at `http://127.0.0.1:8080/v1`; the worker is **Hermes**, not Cline.

> If you literally want the *coordinator process itself* to run on the box (fully
> remote), that is Option C below. It is **not** recommended and not what this plan
> implements: the coordinator is a lightweight local loop whose value is its durable
> local state + resume/watchdog integration. The GPU box is for inference + agent
> tool execution, which is where all the compute is.

---

## 1. Architecture

```
Operator machine (local)                              Rented GPU box (Vast, e.g. RTX 4090)
──────────────────────────────                        ─────────────────────────────────────
stint deep start  (coordinator, foreground)
  ├─ parses mission (local file)                       NInfer  →  127.0.0.1:8080/v1  (model)
  ├─ durable state  ~/.local/state/stint/deep/        Hermes worker (installed on box)
  ├─ watchdog (existing)                                 └─ terminal/tool commands run HERE
  └─ per attempt, over SSH (Stint key, root@host:port):        └─ Vanta worktree  /root/stint-deep/<id>/
        1. push reconstructed prompt → <box>/.stint-prompt-<task>.md   git, checkpoints, verify all HERE
        2. ssh root@host:port 'cd <worktree> && hermes chat \
             --query-file <prompt> --oneshot --quiet --provider custom'  ← model via 127.0.0.1:8080
        3. ssh root@host:port 'cd <worktree> && <per-task|mission verify cmd>'
        4. ssh root@host:port 'git -C <worktree> add -A && git commit …'  (checkpoint)
  └─ landing: handoff written locally AND to box worktree; final verify over SSH
```

Why the coordinator stays local: it owns `deep.json` (resume truth), `coordinator.pid`
(single-coordinator guard), the compute watchdog relationship, and the handoff. All of
that is cheap and local. Everything that needs the GPU — model inference and the
agent's actual file/shell work — moves to the box.

---

## 2. What exists to reuse (source of truth: current branch)

| Concern | Existing code | Reuse |
|---|---|---|
| SSH exec to the box | `cmd/stint/lifecycle.go:477 runSSH` + `:508 sshArgs` (Stint key, `BatchMode`, `StrictHostKeyChecking=accept-new`, per-state `known_hosts`) | **reuse verbatim** for worker + verify + git |
| Vast API command channel (no-SSH fallback) | `internal/provider/vast/command.go:34 ExecuteInstanceCommand` | optional fallback if SSH is flaky |
| Session remote endpoint | `session.State{SSHHost, SSHPort}` (`session/state.go`) | drives the SSH target |
| Mission parse | `internal/deep/mission.go ParseMissionFile` | unchanged (worker-agnostic) |
| Coordinator loop / acceptance / parking | `cmd/stint/deep_loop.go runTask/accept/selectTask` | **unchanged** except the two seams below |
| Reconstructed prompt | `internal/deep/context.go BuildTaskPrompt` + `CommandPolicySection` | unchanged; policy text is now the **Hermes** allowlist (see §5) |
| Verify runner | `cmd/stint/deep_landing.go:147 runVerifyCmd` (local `sh -c`) | **replace** with remote SSH variant |
| Executor | `cmd/stint/deep_executor.go clineExecutor` (local `cline` subprocess) | **replace** with an SSH-Hermes executor |
| Worktree | `deep_landing.go worktreeAdd/commitAll` (local `git`) | **replace** with remote `git over SSH` |
| Git summaries for prompt | `deep_loop.go:98 repoSummary` (local `gitRunner`) | **replace** with remote |

The seams to change are exactly two: **`executor`** and **`verify`** (plus the `gitRunner`
used for worktree + summaries). Everything else — the loop, acceptance rules
(VERIFIED/INCOMPLETE/BLOCKED), attempt caps, landing window, checkpointing cadence,
state, incident log, handoff — is **worker-agnostic and stays as-is**.

---

## 3. Stint-side changes (the implementation)

### 3.1 New remote seam (`cmd/stint/deep_remote.go`)
- `type remoteRunner` wraps `runSSH(paths, state, remoteCommand)` + `runSSHStreaming`
  (both already in `lifecycle.go`) with the session's `SSHHost`/`SSHPort` and the Stint key.
- `RemoteGitRunner`: same interface as `gitRunner` but each op is
  `runSSH(… "git -C <wt> <args>")` — `repoHead`, `logOneline`, `statusShort`, `diffStat`,
  `worktreeAdd` (`git init` + branch on box, see §3.4), `commitAll`.
- `runVerifyCmdRemote(ctx, command, boxWorktree)`:
  `runSSH(… "cd <wt> && sh -c <q>")` bounded by the same 3-min cap
  (`context.WithTimeout`); returns tail + pass/fail. Mirrors the local `runVerifyCmd`.

### 3.2 New executor (`cmd/stint/deep_executor_hermes.go`)
- `type hermesExecutor struct{ remote *remoteRunner; model, base string }`
  implementing the existing `executor` interface (`run(ctx, in) (execResult, error)`).
- Invocation (all on the box):
  1. write `in.prompt` to `<box>/.stint-prompt-<task>-<attempt>.md` via
     `runSSH` heredoc/`base64` (avoid arg-length/quoting issues),
  2. `runSSH(ctx, "cd <wt> && timeout <sec> hermes chat --query-file <f> --oneshot --quiet --provider custom -m <model> 2>&1")`
     — `timeout <sec>` = the task timeout, so a wedged Hermes is killable (mirrors
     Cline's `Setpgid` group-kill; on the box we rely on `timeout` + SSH context
     cancellation),
  3. parse the box's exit code + final text into `execResult`
     (`completed` = exit 0; `outputText` = tail of stdout; `stderrTail` = last lines).
- The coordinator's existing `accept()` then runs the per-task/mission verify over SSH
  and decides VERIFIED/INCOMPLETE/BLOCKED exactly as today.

### 3.3 Wire it up (`deep_start.go`, `deep_resume.go`, `deep_run.go`)
- Add `--worker hermes|cline` (default `hermes` for this run; keep `cline` path working).
- `deepRunSession` selects the executor + verify + git runner by `--worker`.
- Pre-flight for `hermes`: SSH to the box works (probe `runSSH(… "true")`), `hermes`
  is on the box's PATH, model reachable at `http://127.0.0.1:8080/v1/models` **from the
  box** (probe over SSH, not the local tunnel).
- Persist `Worker: "hermes"` in `ExecSettings` so `deep resume` reconstructs the same
  remote worker without an operator flag.

### 3.4 Worktree on the box
- The Vanta repo must exist on the box first (§4). Coordinator cuts the Deep Work
  worktree **on the box**: `runSSH("git -C <repo> worktree add <wt> -b stint/deep-<id> HEAD")`.
- All checkpoints (`commitAll`) and the landing handoff-commit run over SSH.
- `repoSummary()` (branch/HEAD/log/status/diff) is gathered over SSH and folded into the
  reconstructed prompt — a fresh Hermes invocation on the box continues from durable git
  truth alone, same contract as the Cline design.

### 3.5 State / landing / handoff
- `deep.json`, `coordinator.log`, `incidents.jsonl`, `handoff.md` stay **local** (state
  dir). The worktree `DEEP_WORK_HANDOFF.md` copy is written over SSH at landing.
- The box worktree is left in place (on the box) for review, mirroring today's behavior;
  `stint deep status` shows local state and the box path.
- On compute loss: `stint resume` re-establishes the box; `stint deep resume`
  re-anchors the deadline and re-attaches the box worktree over the existing branch
  (branch holds every checkpoint) — same recovery model, target = box.

---

## 4. Box provisioning (the critical path — do this first)

A Vast GPU box runs a minimal **inference** image (Ubuntu + CUDA + NInfer). For
"everything on the box" it must also carry the worker toolchain. **Phase 0 is a single
read-only capability probe over SSH** to avoid over-provisioning or discovering gaps
mid-run:

```
ssh -i <stint-key> -p <port> -o BatchMode=yes root@<host> \
  'for c in git python3 node go hermes timeout bash; do printf "%s: " $c; command -v $c || echo MISSING; done; \
   python3 -c "import cryptography,sys;print(\"py-crypto OK\")" 2>/dev/null || echo "py-crypto MISSING"; \
   node -e "require(\"crypto\").generateKeyPairSync(\"ed25519\");console.log(\"node-ed25519 OK\")" 2>/dev/null || echo "node-ed25519 MISSING"; \
   curl -s -m3 http://127.0.0.1:8080/v1/models | head -c 200; \
   echo "OUTNET: $(curl -s -m5 -o /dev/null -w %{http_code} https://registry.npmjs.org/ || echo fail)"'
```

### P0 RESULT (run 2026-09-03 against live box 60.53.150.159:62327, RTX 4090 / NInfer)

Real probe output (read-only SSH with the Stint key):

| Capability | Box state |
|---|---|
| OS | Ubuntu 24.04.3 LTS (Noble) |
| `git` 2.43.0 | **present** |
| `python3` 3.12.3 + `pip3` 24.0 | **present** |
| `py-cryptography` 46.0.3, `pynacl` | **present** → Ed25519 works with no install |
| `node` / `npm` | **MISSING** (installable) |
| `go` | **MISSING** (installable) |
| `hermes` | **MISSING** (installable) |
| model `127.0.0.1:8080/v1` | **serving qwen3.8-27b** (NInfer, 262144 ctx) |
| outbound net | **NOT air-gapped**: `registry.npmjs.org` 200, `pypi.org` 200 |

### P2 RESULT (run 2026-09-03 against the live box; reproducible via the scripts)

`scripts/provision-box.sh` (run over SSH with the Stint key) installed Node v22.23.2
(NodeSource) and Hermes v0.21.0; `scripts/box-smoke.sh` configured Hermes
(`model.provider custom`, `base_url http://127.0.0.1:8080/v1`, `api_key dummy`,
`default qwen3.8-27b`, `approvals.mode manual`) and proved, end-to-end over SSH:

| Probe | Result |
|---|---|
| A: `hermes chat --oneshot -Q --provider custom -m qwen3.8-27b` reaches the local NInfer endpoint | exit 0, model answered |
| B: an allowlisted ordinary command (`echo`) runs headless inside the agent run | produced expected output, exit 0 |
| C: a non-allowlisted ordinary command (`id`) in a headless oneshot | **ran** — see §5: ordinary commands are not approval-gated at all |
| bare-SSH PATH (what `stint deep start`'s preflight sees) | `/usr/local/bin` (hermes), `/usr/bin` (node, git, python3) all present |

`bash`, `git`, `timeout`, `curl` and the model endpoint were already present. P2 is now
a script pair, not a one-off: re-run both on any fresh box before the launch.

Consequences:
1. **Air-gap risk (top risk) is resolved positively** — the box has outbound network, so
   Node + Hermes can be installed on the box. "Everything on the GPU" is achievable.
2. **Language pair for S0 — now recommends Go as one of the two.** The box has
   outbound net and Python crypto, but the *zero-dependency* property the mission
   values is strongest for Go: `crypto/ed25519` + `crypto/sha256` are pure stdlib
   (no C library, no CGO). And the box needs **no Go toolchain** — build the
   implementation into a **static binary** (`CGO_ENABLED=0 go build`, ~1.8 MB,
   `ldd` = "not a dynamic executable") on the operator machine and ship that binary
   to the box. Verified 2026-09-03: pure-stdlib sign/verify round-trips
   (`verify=true`, 32-byte pub / 64-byte sig) and cross-builds to a static
   linux/amd64 ELF. This removes the "Go tarball on the box" bootstrap cost that
   previously made Go the heaviest option.
   - Best pair for *crypto independence* + zero-dep: **Go (pure-Go Ed25519) + Node
     (stdlib `crypto`/OpenSSL)** — two different Ed25519 code paths, so byte-identical
     cross-verification is a stronger interop proof. Node is the only runtime to
     install on the box (missing, ~1-min, box has net).
   - Alternative: **Go + Python** (Python via `cryptography`/`pynacl`), both Ed25519 —
     works on this box (crypto libs present) but Python is the least zero-dep of the
     three and would not run on an air-gapped box.
   - This is a **Vanta-level (VANTA-001) finding**: the mission lists the language pair
     as a reversible choice to be *recorded* in `deep-work/findings.md`, so the final
     pair is logged there (with the box-toolchain probe evidence), not hardcoded. VANTA-001's
     "probe the toolchains" step must probe **the box**, and the probe biases toward
     the already-present runtimes + the static-Go-binary trick.
3. **Per-run bootstrap on the box** (transient Vast instances) must (re)install:
   Node (NodeSource or `nvm`), Hermes (`install.sh`), and the Vanta repo (clone/bundle).
   Idempotent, ~1–2 min. Part of P2.

| Need (for Vanta CP1) | Provision |
|---|---|
| `git` | `apt-get install -y git` (already present on this box class) |
| `node` (one S0 impl, stdlib `crypto` Ed25519) | NodeSource / `nvm` — the only runtime to install on the box (has net) |
| Go S0 impl | **no on-box install**: built as a static binary on the operator machine and shipped to the box (zero toolchain, zero deps) |
| `hermes` (the worker) | `curl -fsSL https://hermes-agent.nousresearch.com/install.sh \| bash` (needs python3, present) |
| outbound net (npm) | required for the above; **blocker if air-gapped** — P0 confirmed present on the live box |

Then a one-time **Hermes config on the box** (custom provider → local NInfer):
`hermes config set model.provider custom`, `model.base_url http://127.0.0.1:8080/v1`,
`model.model qwen3.8-27b`, `model.api_key dummy`, and the allowlist from §5. This is
idempotent and part of bootstrap.

> The two S0 implementations are deliberately kept dependency-free so they run on the
> box with no heavyweight crypto libs to install or compile: **Go** via pure stdlib
> `crypto/ed25519`+`crypto/sha256` (shipped as a static binary) and **Node** via stdlib
> `crypto` (OpenSSL Ed25519) — two different Ed25519 code paths, so byte-identical
> cross-verification is a genuinely independent interop proof. This is a constraint that
> already exists in the mission and is *further* reinforced by the GPU target.

---

## 5. Command/approval policy for the Hermes worker (source-verified on the box)

P2 verified the box's Hermes v0.21.0 approval source (`tools/approval.py`) and probed
it headless. The model is **not** a Cline-style positive allow-list:

1. **Ordinary commands run freely.** The terminal guard
   (`check_dangerous_command`) only gates commands its detector classifies as
   *dangerous* (destructive rm, disk/partition writes, service/process control,
   network exposure, privilege escalation). `git`, `bash scripts/...`, `node`,
   `python3`, `cat`, `ls`, `grep`, `timeout` ... are all ordinary and are approved by
   default in any mode — probe C confirmed it (even `id` ran under `mode: manual`).
2. **`command_allowlist` is NOT a positive allow-list.** It only pre-approves
   *dangerous* patterns the model might want; it does not restrict ordinary commands.
3. **Dangerous commands are denied in headless one-shot runs.** Our executor invokes
   Hermes with `--oneshot -Q` (a `-q` single-query context), where the approval gate
   resolves via `approvals.single_query_mode` — default **`deny`** (no TTY prompt, no
   hang). The box config sets `approvals.mode: manual` for interactive work, but the
   oneshot runs are governed by `single_query_mode`.
4. **Hardline floor, always:** `rm -rf /`, `mkfs`, `dd` to raw devices,
   `shutdown/reboot`, fork bombs, `kill -1` are blocked before any bypass, even
   under yolo.
5. **No `--yolo`:** the executor passes no bypass flag, so rails 3–4 stay intact.

Consequences for CP1: every command the mission realistically needs is ordinary and
runs; anything destructive is denied in the headless context, and a worker that
attempts one gets a clean denial (recorded, retriable) rather than a hang.
`--allow-command` is a Cline-worker concept and has no meaning for `--worker hermes` —
the policy lives in the box's `~/.hermes/config.yaml` (set by `box-smoke.sh`). File
edits never pass through the terminal guard, so writing S0 code, fixtures, scenarios,
and findings is unrestricted within the worktree — the intended scope.

## 6. Vanta CP1 prep changes (worker-agnostic; still required)

These are independent of Cline-vs-Hermes and of local-vs-GPU, and remain the correct
shape of the mission. Confirmed against the current parser + verifier:

1. **Per-task `verify:` for the tasks whose own scope is narrower than the cumulative
   verifier.** The coordinator's `accept()` uses the task's own `verify:` when present,
   else the mission `## Verification` (`deep_loop.go accept()`). The mission's
   `bash scripts/verify-cp1` LENIENT mode returns 0 on a *pristine empty repo* (verified:
   exit 0), so **a task could be marked VERIFIED purely because the cumulative verifier
   exited 0** — before that task's own artifacts even exist. Give each task a per-task
   `verify:` that checks *its own* artifacts (e.g. VANTA-002 → `test -x s0/expA/test &&
   bash scripts/verify-cp1 && ./s0/expA/test`), so VERIFIED means "this task's evidence
   is present and valid," not "the cumulative script happened to pass." The mission-level
   command remains the final cumulative landing check (STRICT after VANTA-007). This is
   the exact scenario Stint's `TestDeepLoopPerTaskVerifyOverridesMissionVerify`
   (`deep_loop_test.go:377`) was built to fix.
2. **Verifier is correct as-is** — LENIENT passes on the empty repo by design; STRICT
   fails only on the *expected missing CP1 artifacts* (findings, envelope, fixtures,
   both impls + outs, interop, both scenarios, rotation, report) — not a verifier bug.
   Confirmed by running both modes on the pristine repo.
3. **Bounds are safe**: each impl's test is capped at 80 s inside the verifier, worst
   case well under the 3-min verify cap; `--task-timeout` (default 10 m) is fine for
   writing + testing a ≤300-line impl; 7 tasks is within the "keep the list short"
   guidance.
4. **No remotes + no baseline commit** → before launch: create the baseline commit,
   then make the repo available on the box (a remote/clone, or `rsync`/`git bundle`
   over SSH). See §3.4/§4.
5. **Language pair**: with the GPU target, prefer **Node + Python** (both likely
   present/provisionable on an Ubuntu GPU image; Go would need a tarball install). The
   mission already lists these two first and both have stdlib Ed25519. This is a
   reversible choice to be recorded as a finding in `deep-work/findings.md`.

---

## 7. Execution phases (verification gates)

- **P0 — Box capability probe** (read-only SSH). Confirms git/python3/node/hermes/model
  + outbound net. Output decides the §4 provisioning list. *Gate: probe results logged.*
- **P1 — Stint: remote seam + Hermes executor + remote verify + remote git**, with unit
  tests using a fake `runSSH` (mirror the existing `fakeExecutor` pattern).
  *DONE 2026-09-03.* Implemented as `cmd/stint/deep_remote.go` (the `gitOps` interface,
  `remoteCmd` = the SSH seam, `remoteGit`, `hermesExecutor`, `runVerifyCmdRemote`,
  `shellQuote`), wired through `deep_start.go`/`deep_resume.go`/`deep_run.go` (the
  `--worker hermes|cline` flag, `ExecSettings.Worker` persistence, worker-aware preflight
  and worktree, and `ExecSettings`/coordinator selection). Verified: `go test
  ./cmd/stint/ ./internal/deep/` green (new `deep_remote_test.go`: executor success /
  non-zero exit / SSH failure, remote verify, `shellQuote`, and an end-to-end remote
  coordinator loop), `go vet` clean, `make build` clean, `--worker` in `deep --help`.
- **P2 — Box provisioning + Hermes config + Vanta repo on box** (baseline commit first).
  *Gate: `hermes chat … --oneshot -Q --provider custom` over SSH reaches
  `127.0.0.1:8080/v1`.* **DONE 2026-09-03** against the live box (which then became
  this session's inference host): Node v22.23.2 + Hermes v0.21.0 installed, custom
  provider configured, probes A/B/C passed (§4.1). Captured as
  `scripts/provision-box.sh` + `scripts/box-smoke.sh`; re-run on the dedicated
  launch box. The Vanta repo (baseline commit `de47058`) transfers to the box by
  `rsync`/`git bundle` over SSH at launch — not provisioned on a box that may not
  outlive the run.
- **P3 — Vanta prep edits** (§6). **DONE 2026-09-03:** all 7 tasks carry scoped
  per-task `verify:` (each ending in the lenient cumulative check), the language
  shortlist corrected to the box reality (Node v22; Go as prebuilt static binary —
  the box has no Go toolchain), reviewer notes record the deny-by-default execution
  policy. Gated: real Stint mission parser on the updated file (7/7 verify present),
  `verify-cp1` LENIENT exit 0 / STRICT exit 1 with exactly the expected missing
  artifacts, and each task's leading `test` clause failing on the pristine tree.
  Vanta baseline commit `de47058`.
- **P4 — Dry run on a tiny synthetic mission** (1–2 tasks, e.g. "write a file +
  per-task verify", plus the mission-level command), end-to-end over SSH with a
  **dedicated** box + Hermes: provision (scripts), transfer Vanta, run
  `stint deep start --worker hermes` for a bounded window. *Gate: task VERIFIED by
  its own per-task command over the box channel, checkpoint commit on the box branch,
  `DEEP_WORK_HANDOFF.md` written on the box, honest handoff locally. (Cannot run on
  the current box — it hosts this session's inference and must not be disturbed.)*
- **P5 — Launch the real CP1 mission** (see §8). Do not auto-start; operator launches.

---

## 8. Recommended first-run launch (once P0–P4 pass)

Compute: the box is the GPU session; Deep Work rides it. Use the `deep` profile runway
(default 8 h) or extend the live session, then:

```
# 0. dedicated box: provision + smoke (re-run the P2 scripts)
ssh -i <stint-key> -p <port> root@<host> 'bash -s' < scripts/provision-box.sh
ssh -i <stint-key> -p <port> root@<host> 'bash -s' < scripts/box-smoke.sh
# 1. transfer the Vanta baseline (local repo, commit de47058) over SSH
rsync -a -e "ssh -i <stint-key> -p <port>" Vanta/ root@<host>:/root/stint-vanta/
# 2. start the compute session on that box (stint start interactive --yes), then:
stint deep start \
  --mission /home/marguel/Documents/projects/Vanta/deep-work/CP1_MISSION.md \
  --repo  /root/stint-vanta \
  --worker hermes \
  --provider custom --model qwen3.8-27b \
  --task-timeout 12m --max-attempts 3 \
  --hours 8
```

No `--allow-command`/`--auto-approve`: with `--worker hermes` the command policy is
the box's Hermes config (§5) — ordinary commands run, dangerous ones are denied in
the headless oneshot context, hardline commands are always blocked. (Exact `--repo`/
flag names as finalized in P1.)

---

## 9. Risks / decisions needed (these may block the first run)

1. **Box toolchain + outbound network (top risk).** **Resolved by P0/P2:** the offer
   class has outbound net and Node+Hermes install in ~2 min (`provision-box.sh`).
   Standing rule: require an offer with outbound net; Go needs no box toolchain
   (prebuilt static binary).
2. **Hermes on the box.** **Decided: full box install each run** (faithful to
   "everything on the GPU"; Hermes cannot cleanly run only its shell remotely).
   Cost: ~2 min per fresh instance, scripted.
3. **Coordinator local vs on-box.** **Decided: coordinator local** (Option A) — it
   owns `deep.json`, the pid guard, the watchdog relationship, and the handoff.
4. **`--worker hermes` Stint capability.** **Done (P1)** on
   `feat/deep-work-hermes-worker`, stacked on `feat/deep-work-with-dashboard`.
   Remaining launch dependency: a dedicated box (the first P2 box became this
   session's inference host and is out of scope for re-provisioning).
5. **Model quality on a 27B local model for autonomous multi-task agentic work** (two
   independent cryptographic implementations + cross-language byte-identity). This is a
   real capability risk independent of architecture; the per-task `verify:` + attempt
   caps + honest handoff are the mitigations. *Note only — not an architecture blocker.*

---

## 10. Out of scope (unchanged)
No substrate choice, no base build, no Vanta redesign — CP1 stays evidence-gathering.
This plan adds an execution *substrate* for the Deep Work *coordinator's* worker; it does
not touch Vanta's protocol vision or CP0→CP1 boundary.