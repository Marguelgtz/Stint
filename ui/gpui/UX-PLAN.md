# GPUI UX — Living Action Plan

> Living document. Update status as work lands. Owner: Hermes.
> Branch: `ui/gpui-dashboards` (stacked on `ui/gpui-interactive-controls`).
> Last touched: 2026-10-20

## Objective
Make the Stint GPUI app a calm, glanceable **operator console** for Deep Work / Deep Run /
Interactive sessions. Phase 1 (done): left side nav + de-duplicated shared UI. Phase 2 (current):
add the two **CLI dashboards** (interactive live dash + deep board) as native GPUI panels that show
the *same figures the CLI TUI renders* — with the Go CLI as the single source of truth for data.

## Principles
- **Monitor/Operate** surface: dense, at-a-glance, no marketing chrome.
- **DRY**: one `Sidebar`; shared row/card/badge/progress helpers reused across ALL panels.
- **Single source of truth**: Go owns dashboard projection. The UI never re-derives figures — it
  deserializes the exact model the CLI marshals via `--json`.
- **Honest state**: refresh failures and stale data must be visible, never silent.
- **Safe actions**: confirm stop/resume; disable invalid actions; history is read-only.

## Goals
Phase 1 (sidenav + step flow + run monitor) — ✅ all done (see task board).
Phase 2 (dashboards):
1. `stint dashboard --json` + `stint deep dash --json` emit the exact TUI model. ✅
2. `ui/gpui/src/dashboard.rs`: serde wire models + `load_interactive`/`load_deep` fetchers. ✅
3. Interactive live-dash panel: session header, GPU, health, inference (+ lane table), perf, logs. ✅
4. Deep board panel: mission header, progress, compute, worker, tasks, recent events. ✅
5. Sidebar items (`Cpu` = Live Dashboard, `LayoutDashboard` = Deep Board) + 5s auto-refresh. ✅
6. `cargo check` + `cargo build` + headless xvfb smoke test green. ⏳ in progress
7. Commit + PR → main (stacked on prior UI work). ⬜ pending

## Task Board

| # | Task | Status |
|---|------|--------|
| 1 | Left `Sidebar` nav (3 panels + compute-status footer + run-count suffix) | ✅ done |
| 2 | Deep Run step-flow launch panel | ✅ done |
| 3 | Run monitor: history + detail + progress bar + stale warning | ✅ done |
| 4 | Shared badge / progress / header helpers (DRY) | ✅ done |
| 5 | Fix compile errors (icons, `Sidebar.width`, `Div.style_with`, borrows, `w_18`) | ✅ done |
| 6 | `cargo check` green | ✅ done |
| 7 | `cargo build` + headless render smoke test (xvfb) | ✅ done — no panic, benign DRI3 fallback only |
| 8 | **Go:** `--json` on `stint dashboard` + `stint deep dash` (reuse `loadLocal`/snapshot; thread through on-box SSH route) | ✅ done — `go build ./...` clean, JSON verified |
| 9 | **Go:** camelCase `json` tags on `internal/dashboard` + `internal/deepdashboard` model structs | ✅ done |
| 10 | **Rust:** `dashboard.rs` serde models (camelCase) + `load_interactive`/`load_deep` | ✅ done — keys verified 1:1 against live CLI JSON |
| 11 | **Rust:** interactive live-dash panel (session/GPU/health/inference+lanes/perf/logs) | ✅ done |
| 12 | **Rust:** deep board panel (header/progress/compute/worker/tasks/events) | ✅ done |
| 13 | **Rust:** sidebar items + `start_refresh` 5s loops + `apply_*` methods | ✅ done |
| 14 | `cargo check` (0 warn) + `cargo build` + xvfb smoke | ⏳ in progress |
| 15 | Commit dashboards; push `ui/gpui-dashboards`; PR → main | ⬜ pending |

## Wire contract (Go → Rust) — do NOT re-derive
- Both commands emit **one JSON document on stdout**, keys **camelCase** (`sessionId`,
  `instanceId`, `laneEvents`, `nInferKV`, …). Rust models use `#[serde(rename_all="camelCase")]`
  + `#[serde(default)]` on every field so missing/extra keys never fail.
- **`time.Duration` marshals as an integer count of NANOSECONDS** (not seconds, not a string).
  Rust: `i64` + `dashboard::ns_to_secs(ns)` to display. Verified: 90m → `5400000000000`.
- **`time.Time` marshals as RFC3339**; the zero time is `"0001-01-01T00:00:00Z"` (treat as unset).
- The Go `Model` also carries TUI-only fields (`view`, `width`, `height`, `modal`, `noColor`) —
  serde ignores unknown fields, so the Rust structs can omit them.
- Interactive top-level: `noSession, session, health, gpu, inference, perf, logs, laneEvents,
  notice, error`. Deep top-level: `sessionId, mission, phase, branch, baseCommit, coordinator,
  workerName, startedAt, deadline, landBefore, tasks, events, compute, worker, canLand, latest,
  landedAt, landing*, error, notice`.
- On-box (production) deep dash: `--json` is threaded through the SSH route
  (`deepProductionDashboardCommand` / `runRemoteDeepDashboard` both take `jsonOut bool`).

## API facts learned (gpui-kit 0.7.3) — do NOT re-derive
- **`IconName` is the 106-icon DEFAULT subset** (`gpui-kit-assets/default-icons.txt`), NOT the
  full 1830 catalog. Valid icons used: `Bot` (Deep Work), `Play` (Deep Run),
  `SquareTerminal` (Interactive), `Cpu` (Live Dashboard), `LayoutDashboard` (Deep Board).
  `Workflow`/`Rocket`/`Terminal` do NOT exist here.
- **`Sidebar` implements `Styled`** → size it with `.w(px(...))` (there is NO `.width()`).
- **Fractional width** → `.w(relative(f32))`. NOT `style_with(...)` / `percentage(...)`.
- **`w_N` helpers** exist for `0,2,4,6,8,10,12,16,20,24,32` + `w_full`/`w_1`/`w_1_2`.
  `w_18` does NOT exist → use `w_16` + `.truncate()`.
- **`SidebarMenuItem::suffix`** is `Fn(&mut Window,&mut App)->E + 'static` → owns captured locals
  via `move`; clone owned values per call. It cannot borrow `self`.
- **`cx.listener`** closures borrow `this: &mut Self` — keep them in the same method scope.
- **`grid_cols` takes a `u16` column COUNT, not a CSS template** — for variable-width table rows
  use `h_flex` + `.w(relative(f32))` per cell instead (header + data share the same fractions).
- **Go `json` struct tags**: a shared tag string (`a,b,c`) on a multi-field declaration is IGNORED
  (fields drop out of JSON entirely). Each field needs its OWN `json:"name"` tag.
- **Module-level `#![allow(dead_code)]`** is valid on a non-crate module (used in `dashboard.rs`
  because wire-contract fields are intentionally present even when not yet rendered).

## Notes
- gpui docs: https://docs.rs/gpui/latest/gpui/
- **gpui-kit docs (user-saved, authoritative for components): https://gpui-kit.com/docs/**
- gpui-component `Sidebar` lives at `component::sidebar::{Sidebar, SidebarMenu, SidebarMenuItem}`.
- Dashboard data path: `dashboard::load_interactive()` / `load_deep()` → `interactive::run_stint`
  (shells out to the `stint` binary, resolves it like the other commands).
