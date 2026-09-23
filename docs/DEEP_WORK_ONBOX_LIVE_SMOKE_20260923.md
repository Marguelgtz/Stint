# Deep Work fresh-GPU smoke — 2026-09-23

## Result

The current production path completed a fresh-box Hermes-on-box Deep Work run,
survived operator-side disconnect, verified and published each task, landed the
session, archived final R2 evidence, wrote a handoff, and tore down the GPU.

The run used current `main` at
`bdd55c57b7fbdb7a2128c8ed14861cf3f527c04d` (PR #109). After the run, PR #114
fixed an observer-artifact path in the smoke harness and reduced its default
retry ceiling. That follow-up passed local tests and exact-head CI, then merged
as `822e4b880a80e5ae2062f50c8ef1fac05f59365e`. A post-fix GPU retry did not
rent: its selected offer disappeared before rental, and a fresh read-only query
found zero candidates meeting the $0.48 one-hour cap and 500 Mbps floor. The
observer-capture change is therefore locally verified but has not yet had a
second live-box run.

## Fresh-box run

- **Session:** `20260923-022052`
- **Main SHA at launch:** `bdd55c57b7fbdb7a2128c8ed14861cf3f527c04d`
- **Vast instance:** `52152693`, RTX 4090, Taiwan, listed at $0.4759259259/hour
- **Rental guard:** 1.5 hours, $0.72 session ceiling, $0.48/hour ceiling, one
  marketplace candidate
- **Measured download:** 41.0 MB/s against a 10 MB/s minimum
- **Artifact:** `/tmp/stint-final-smoke-20260923/artifacts/`
- **Supervisor session start:** `2026-09-23T02:20:52Z`; the launcher terminated
  local tunnel/watchdog processes at `02:20:57Z` and continued polling the box

The fresh host built all 246 native NInfer steps, completed model download and
SHA-256 verification, and reached `READY`. The launcher installed Hermes
0.21.4, configured the xhigh and medium local-model routes, ran the Stint repo
verification, and started the detached supervisor only after route and
two-lane qualification. The model was `qwen3.8-27b` with a native 262,144-token
context. Compression was configured for the medium summary route but was not
exercised by this short mission.

The xhigh and medium probes both returned successfully; the phase wire observed
each requested `reasoning_effort`. The ordinary headless command and the
dangerous-command denial probe passed. The concurrent lane check reported
`LANE_SMOKE_PASS concurrent=xhigh+medium lanes=2`.

All three coordinator tasks independently verified on their first attempt:

| Task | Phase | Checkpoint | Published PR |
| --- | --- | --- | --- |
| `STINT-PLAN-001` | verified | `a051c94a0f02b5d65a4e5a4fb2b7b6ee86e892f4` | [#110](https://github.com/Marguelgtz/Stint/pull/110) |
| `PHASE-001` | verified | `f7349155788c9ec0b7a0086af9b2a8f66c704cce` | [#111](https://github.com/Marguelgtz/Stint/pull/111) |
| `PHASE-002` | verified | `553c20c49f8c798cbd1074b03870fb63e642ee7a` | [#112](https://github.com/Marguelgtz/Stint/pull/112) |

The final landing commit was `7f7fd4344e9e470f19c1d2e95d75ec06357fbc00`;
the final marker verification passed and handoff PR [#113](https://github.com/Marguelgtz/Stint/pull/113)
was published. PRs #110–#113 remain open as a stack of run evidence; they were
not merged automatically.

The final R2 archive was verified with a read-only object listing at
`vanta/onbox/20260923-022052`: `deep.json`, `mission.md`, `handoff.md`,
`incidents.jsonl`, `publication.json`, and `provenance.json` were present, as
were nine heartbeat snapshots and `latest.json` (16 objects total). The archived
publication state contains all three checkpoint URLs and the handoff URL.

Teardown was verified twice: the launcher logged `Compute destroyed`, and the
Vast instance lookup returned `instances: null` for `52152693`. Stint local
status reports no active compute.

## Observer capture correction and retry

The first run's post-run capture command incorrectly called
`/root/deep-observe.sh`; the production bootstrap installs the observer at
`/root/stint-phasing/deep-observe`. Its output artifact was empty, although the
independent route probes, wire assertions, concurrent-lane check, and completed
tasks all passed. PR #114 changes capture to the installed observer, supplies the
persisted mission start time, counts non-2xx route responses, and fails if either
route is absent or has failures. `scripts/test_deep_observe.sh` covers start-time
filtering and response counting.

The retry on PR #114's merged `main` used a $0.48 one-hour ceiling and one
candidate. Offer `46801831` disappeared before the provider mutation; no
`session.json` was created and `stint status` showed no active compute. A fresh
read-only Vast query found zero RTX 4090 offers satisfying the one-hour, $0.48,
and 500 Mbps constraints. No second rental was attempted. The post-fix GPU
observer capture remains the only live-smoke item not rerun.

## Verification

- On live `main` `bdd55c5`: fresh NInfer readiness, production Deep Work
  bootstrap, operator disconnect, xhigh/medium routing, three independently
  verified tasks, GitHub checkpoint/handoff publication, R2 final archive, and
  provider teardown all passed.
- On current code `main` `822e4b8`: `go test -count=1 ./...`,
  `scripts/test_deep_observe.sh`, smoke parser preflight, smoke shell syntax, and
  `git diff --check` passed. PR #114's exact-head build, vet, unit, race, and
  Spark profile checks passed.
- The repository's original operator checkout was not modified; this work used
  isolated `/tmp` worktrees.

No provider invoice was queried. The first live rental ran about 33m35s at the
listed rate (about $0.27 compute) and used a model download estimated at about
$0.17 from the advertised transfer rate. Combined with the previously recorded
$0.52–$0.57 estimate, cumulative spend is estimated around $0.95–$1.00 before
provider fees; this is not a settled invoice total.
