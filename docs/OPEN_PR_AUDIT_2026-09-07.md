# Open PR integration audit — 2026-09-07

Scope: all **52** open PRs, compared against their current declared bases and the
original checkout commit `e78ceef308d85c9cac7c71e7d172bed7c66c4182`.
Cleanup worktree: `/tmp/stint-provenance-pass`, branch `stint/provenance-easy-wins`.
The original checkout's uncommitted work is excluded and preserved.

## Result

- **29** PR heads are already ancestors of the original checkout.
- **1** (#61) was integrated in the previous turn.
- **5** more (#55, #60, #62, #65, #66) are integrated in this follow-up.
- **17** are deferred for the specific reasons below.

These are integration dispositions, not claims that every deferred PR is broken.
All 52 received ancestry/base/diff-scope screening; selected small changes received
implementation and dependency review plus local tests. Large out-of-scope changes
were not exhaustively audited for correctness. No GitHub PR was merged or closed.
The 29 existing heads are present on this development line, not necessarily main.

GitHub head IDs were checked against refreshed refs. SSH fetch failed in the earlier
turn; HTTPS fetch succeeded here without changing the configured origin URL:

```sh
git fetch https://github.com/Marguelgtz/Stint.git '+refs/heads/*:refs/remotes/origin/*'
```

Each base-relative patch was obtained using `git diff <base>...<head>`, and presence
was checked using `git merge-base --is-ancestor <head> e78ceef`. Snapshot metadata
and all 52 patches are retained locally in `/tmp/stint-pr-audit/`.

## Every open PR

| PR | Declared base → head | GitHub diff size | Disposition | Evidence / reason |
|---|---|---:|---|---|
| [#11](https://github.com/Marguelgtz/Stint/pull/11) Fix Cline model limits and add configurable context | `main` → `0f3a30b6` | +136/−33 | Superseded / incompatible | Current --context uses resolveLlamaContext (1024–131072) and ContextTokens; this older proposal adds a different RuntimeContext field and 16384–32768 limit. Do not replay it wholesale. |
| [#20](https://github.com/Marguelgtz/Stint/pull/20) Qualify Vast hosts for NInfer CUDA 12.8 | `fix/perf-transient-eof` → `0b33947c` | +74/−7 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#23](https://github.com/Marguelgtz/Stint/pull/23) Restore direct Vast SSH endpoint handling | `fix/ninfer-cuda128-hosts` → `996ddef5` | +56/−10 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#24](https://github.com/Marguelgtz/Stint/pull/24) Use Ubuntu 24.04 Vast image for NInfer | `fix/restore-vast-ssh-direct-endpoint` → `d34cb2b6` | +8/−1 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#25](https://github.com/Marguelgtz/Stint/pull/25) Add interactive SSH access to active instances | `fix/ninfer-ubuntu2404-image` → `ba77d766` | +163/−1 | Defer: new command | Adds PTY SSH access and remotely writes ~/.no_auto_tmux. Predates the current help router; requires command integration and transport validation, not a tiny existing-command fix. |
| [#26](https://github.com/Marguelgtz/Stint/pull/26) Reduce interactive cold-start time safely | `fix/ninfer-ubuntu2404-image` → `f7389ed9` | +464/−59 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#27](https://github.com/Marguelgtz/Stint/pull/27) Repair Vast SSH authorization modes | `fix/ninfer-cold-start` → `4d6faed9` | +196/−25 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#28](https://github.com/Marguelgtz/Stint/pull/28) Retry measured network qualification across Vast candidates | `fix/vast-ssh-key-registration` → `511172fb` | +248/−64 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#29](https://github.com/Marguelgtz/Stint/pull/29) Use Vast prebuilt llama.cpp runtime | `feat/network-qualified-candidate-retry` → `5bb8550e` | +78/−25 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#30](https://github.com/Marguelgtz/Stint/pull/30) Fix llama model launch shell quoting | `feat/vast-prebuilt-llama` → `52722a28` | +13/−1 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#31](https://github.com/Marguelgtz/Stint/pull/31) Make interactive startup observable and resilient | `fix/llama-launch-shell-quoting` → `030b3164` | +189/−15 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#32](https://github.com/Marguelgtz/Stint/pull/32) Use a prebuilt Vast NInfer runtime | `fix/continuous-startup-reliability` → `8f141b44` | +199/−10 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#33](https://github.com/Marguelgtz/Stint/pull/33) Qualify hosts with real model transfer throughput | `feat/vast-prebuilt-ninfer` → `5baa04d0` | +336/−35 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#34](https://github.com/Marguelgtz/Stint/pull/34) CLI: data-driven help and usage experience | `feat/model-transfer-qualification` → `af14f2e7` | +513/−39 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#35](https://github.com/Marguelgtz/Stint/pull/35) Docs: CLI reference (docs/CLI.md) | `feat/cli-help` → `f68db63b` | +174/−0 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#36](https://github.com/Marguelgtz/Stint/pull/36) Add a Cline configuration helper | `feat/model-transfer-qualification` → `0daaf644` | +392/−0 | Defer: product feature | 392 additions; new command writes third-party Cline globalState.json. Requires current Cline compatibility and safe configuration-write review. |
| [#37](https://github.com/Marguelgtz/Stint/pull/37) Add mutable session deadline controls | `docs/cli-reference` → `c3187818` | +1542/−41 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#38](https://github.com/Marguelgtz/Stint/pull/38) Add session snapshot telemetry | `feat/session-deadline-controls` → `a0abc69a` | +1361/−82 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#39](https://github.com/Marguelgtz/Stint/pull/39) Add live session dashboard | `feat/session-telemetry` → `f9f5570a` | +1643/−0 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#40](https://github.com/Marguelgtz/Stint/pull/40) Fix dashboard layout and terminal wrapping | `feat/session-dashboard` → `66be39d8` | +387/−332 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#41](https://github.com/Marguelgtz/Stint/pull/41) Add arrow-key dashboard navigation | `fix/dashboard-layout` → `15819910` | +165/−7 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#42](https://github.com/Marguelgtz/Stint/pull/42) Make `stint dash` the primary cockpit command | `feat/dashboard-arrow-navigation` → `6121539c` | +63/−14 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#43](https://github.com/Marguelgtz/Stint/pull/43) Finish recoverable dashboard session controls | `feat/dash-command` → `c791b211` | +815/−231 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#44](https://github.com/Marguelgtz/Stint/pull/44) Benchmark real prompt depth in stint perf | `feat/dashboard-recovery` → `7b830054` | +351/−35 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#45](https://github.com/Marguelgtz/Stint/pull/45) Observe live inference traffic in status and dashboard | `feat/perf-prompt-depth` → `d089434c` | +1242/−27 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#46](https://github.com/Marguelgtz/Stint/pull/46) Add dual NInfer client lanes | `feat/inference-observation` → `cbe0dbff` | +262/−9 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#47](https://github.com/Marguelgtz/Stint/pull/47) Retry stale Vast offers during startup | `feat/ninfer-dual-lanes` → `6df14566` | +130/−1 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#48](https://github.com/Marguelgtz/Stint/pull/48) Prototype relocatable NInfer runtime bundle | `fix/stale-vast-offer-retry` → `bb3eea21` | +292/−0 | Defer: runtime experiment | New Docker bundle packaging, CI and smoke-test architecture; explicitly experimental. |
| [#49](https://github.com/Marguelgtz/Stint/pull/49) Record startup lifecycle timing events | `feat/ninfer-runtime-bundle` → `441a3072` | +221/−1 | Defer: persistence/telemetry | Hooks session Save to append a new event stream; introduces storage and failure semantics beyond a terminal timing line. |
| [#50](https://github.com/Marguelgtz/Stint/pull/50) Publish NInfer runtime bundle as immutable release | `feat/startup-timing-log` → `e5e4ea79` | +71/−0 | Defer: release dependency | Small YAML alone is misleading: publishes bundles built by #48; cannot integrate independently as an easy release fix. |
| [#51](https://github.com/Marguelgtz/Stint/pull/51) Start NInfer from Vast base image plus runtime bundle | `ci/ninfer-bundle-release` → `946097fa` | +207/−22 | Defer: runtime experiment | Opt-in base-image/bundle deployment with generated downloader and runtime bridge; depends on experimental bundle infrastructure. |
| [#52](https://github.com/Marguelgtz/Stint/pull/52) Style one-shot CLI output with TTY/NO_COLOR-aware ANSI | `feat/dynamic-start-candidate-replenishment` → `f6d0263e` | +329/−216 | Defer: broad presentation change | 329 additions / 216 deletions across eight files. ANSI styling is product presentation work with widespread output changes. |
| [#54](https://github.com/Marguelgtz/Stint/pull/54) Replenish stale startup candidates without spending retry budget | `fix/stale-vast-offer-retry` → `83bb19db` | +358/−46 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#55](https://github.com/Marguelgtz/Stint/pull/55) Measure startup time from `stint start` to model serving | `main` → `3b1a6465` | +55/−0 | Integrated this follow-up | Adapted start-to-serving display to current retry lifecycle; monotonic time retained; no session-state changes. |
| [#56](https://github.com/Marguelgtz/Stint/pull/56) Show resident NInfer context by lane in dashboard | `feat/ninfer-dual-lanes` → `6f8f1137` | +399/−29 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#57](https://github.com/Marguelgtz/Stint/pull/57) Add Deep Work MVP (stint deep start/status/stop) | `fix/vast-marketplace-resilience` → `3aa9a2f6` | +7488/−1 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#58](https://github.com/Marguelgtz/Stint/pull/58) Show live NInfer lanes separately in dashboard | `feat/dashboard-client-context` → `3963e7c3` | +140/−60 | Defer: overlaps #68 | Same net lane/UI patch as #68; changes active-agent interpretation plus lane rendering. Must integrate once and reconcile inference expectations, not merge both. |
| [#59](https://github.com/Marguelgtz/Stint/pull/59) deep: --worker hermes — run the Deep Work agent on the compute box | `feat/deep-work-with-dashboard` → `b48cc3b6` | +1334/−42 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#60](https://github.com/Marguelgtz/Stint/pull/60) docs: correct NInfer lane semantics and health probe endpoints | `feat/deep-work-hermes-worker` → `d40a0f59` | +12/−6 | Integrated this follow-up | Documentation/comment correction becomes consistent once #62 is present. No runtime changes. |
| [#61](https://github.com/Marguelgtz/Stint/pull/61) test: mirror the real NInfer /slots schema in the probe fixture | `stint/easy-wins-1-docs` → `55dbf221` | +11/−7 | Integrated previous turn | Test-only schema correction already cherry-picked as 16151cd. |
| [#62](https://github.com/Marguelgtz/Stint/pull/62) perf: raise the inference probe fetch budget to survive slow tunnels | `stint/easy-wins-2-test-fixture` → `4f9b9151` | +11/−2 | Integrated this follow-up | 1.2s to 2.5s fetch timeout under bounded parent context; added failing-before/passing-after slow HTTP regression. |
| [#63](https://github.com/Marguelgtz/Stint/pull/63) feat: warn on a stale local deadline in stint status | `stint/easy-wins-3-probe-budget` → `20d71e14` | +119/−1 | Defer: misleading guidance | Warning tells users status --refresh resyncs a remote deadline. Current collectSessionSnapshot probes health/telemetry; it does not fetch a remote authoritative deadline or save one. |
| [#64](https://github.com/Marguelgtz/Stint/pull/64) feat: keep the last-good dashboard inference sample on failed refreshes | `stint/easy-wins-4-stale-deadline-warning` → `8fa9dbcb` | +107/−1 | Defer: UI state semantics | Retains samples keyed only by instance ID, not runtime/context; adds persistent controller cache and stale rendering. Needs invalidation review and rendering coverage before calling it an easy integration. |
| [#65](https://github.com/Marguelgtz/Stint/pull/65) feat: per-consumer refresh budgets for live telemetry probes | `stint/medium-1-dashboard-last-good` → `4c8b60cc` | +37/−6 | Integrated this follow-up | Independent bounded dashboard budget (6s), status stays 4s. Removed comment claiming retained-sample UI exists on this line; no dependency on #64 needed. |
| [#66](https://github.com/Marguelgtz/Stint/pull/66) fix: compute NInfer cache reuse as hits/(hits+non-cached) | `stint/medium-2-probe-budgets` → `71abe24e` | +32/−6 | Integrated this follow-up | NInfer hits/(hits+non-cached), llama unchanged. Added runtime/zero/missing-counter regressions; missing non-cached telemetry stays unavailable. |
| [#67](https://github.com/Marguelgtz/Stint/pull/67) feat: type-to-confirm + destroy verification for stint down and the watchdog | `stint/medium-3-ninfer-cache-reuse` → `2acc198d` | +174/−5 | Defer: lifecycle semantics | Adds confirmation, --yes and provider disappearance polling to down/watchdog. Alters automation contract; coordinate with current safety stack. |
| [#68](https://github.com/Marguelgtz/Stint/pull/68) feat: agents semantics fix + per-lane live rows (merge of the live-branch dashboard) | `stint/medium-6-destroy-confirmation` → `8db77e82` | +140/−60 | Defer: overlaps #58 | Same net lane/UI patch as #58; not independent work. No need to import the teardown ancestry to get its diff. |
| [#69](https://github.com/Marguelgtz/Stint/pull/69) feat: session-state durability (archive + watchdog) and staleness signal | `stint/medium-4-agents-semantics` → `2e21edea` | +594/−10 | Defer: lifecycle/persistence | 594 additions, 14 files; archives session state across teardown paths, watchdog detachment and staleness semantics. |
| [#70](https://github.com/Marguelgtz/Stint/pull/70) feat: operator-side lane correlation, client tag, and explicit no-identity rendering | `stint/medium-5-session-durability` → `ae041bcd` | +354/−5 | Defer: state/UI feature | 354 additions; new persisted client tag and lane event correlation/rendering. Product and state-format scope. |
| [#73](https://github.com/Marguelgtz/Stint/pull/73) docs: P4 dry run records — run-1 incident + run-2 findings | `feat/deep-work-hermes-worker` → `e78ceef3` | +223/−0 | Already in checkout | Head is an ancestor of e78ceef: no new code to integrate here. Open PR status does not imply missing implementation. |
| [#74](https://github.com/Marguelgtz/Stint/pull/74) fix: pin NInfer artifact revision and remove hardcoded transfer size | `stint/medium-7-lane-correlation` → `800b6d16` | +226/−24 | Defer: acquisition semantics | 226 additions; URL pin plus dynamic metadata, transfer qualification and corrupt/oversized file recovery. Original worktree already has an uncommitted pin. Keep out of this integration. |
| [#76](https://github.com/Marguelgtz/Stint/pull/76) P0: integrate lifecycle safety on current NInfer stack | `fix/ninfer-artifact-revision-size` → `792bb508` | +1497/−30 | Defer: substantial draft | 1497 additions, 12 files; lock-owner verification/preemption, provider inventory, active doctor and evidence paths. Separate safety review; no partial lifecycle merge. |

## Integration details and validation

- #62: slow first-epoch HTTP regression failed on the old 1.2s timeout and passed
  after the 2.5s change. The parent deadline still cancels the second epoch;
  a retained single epoch does not invent token rates.
- #60: cherry-picked once #62 made the 2.5s documentation accurate.
- #65: keeps status at 4s; dashboard gets 6s under its 10s cadence. The misleading
  comment referring to #64's absent UI was replaced. Budget/probe tests pass.
- #66: runtime-specific ratio cases failed before the correction. NInfer zero hits,
  all-cached and ordinary reuse now work; absent denominator remains unknown.
  Existing llama.cpp probe expectations continue to pass.
- #55: old patch context no longer matched the expanded startup function. The
  same three instrumentation points were applied to the current start path, using
  time.Now() without UTC conversion to preserve monotonic elapsed measurement.
  Duration includes user confirmation and rejected candidates. Existing boundary
  and start tests pass; no paid startup was attempted.

Final checks: `go test ./...`, `go test -race ./...`, `go vet ./...`, `make build`,
and `git diff --check` pass in the isolated worktree. Builds do not replace the
original PATH binary. No provider calls or paid validation were performed.
