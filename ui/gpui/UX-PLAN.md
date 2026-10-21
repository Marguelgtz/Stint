# GPUI UX — Living Action Plan

> Living document. Update status as work lands. Owner: Hermes. Branch: `ui/gpui-interactive-controls`.
> Last touched: 2026-10-20

## Objective
Make the Stint GPUI app a calm, glanceable **operator console** for Deep Work / Deep Run /
Interactive sessions. Start with a left side nav, then de-duplicate shared UI so the three
panels share one design language. No repeated layout logic.

## Principles
- **Monitor/Operate** surface: dense, at-a-glance, no marketing chrome.
- **DRY**: one `Sidebar`, shared row/card/badge/progress helpers reused across panels.
- **Honest state**: refresh failures and stale data must be visible, never silent.
- **Safe actions**: confirm stop/resume; disable invalid actions; history is read-only.

## Goals
1. Left side nav (collapsible) for Deep Work / Deep Run / Interactive. ✅
2. Deep Run rebuilt as a clear step flow (select → validate → provision → launch). ✅
3. Run monitor with history list + detail, task progress bar, stale-data warning. ✅
4. Deep Work panel shows verified/total progress at a glance. ✅
5. Shared components: sidebar item, status badge, progress bar, section header. ✅
6. Compiles clean (`cargo check` green). ✅
7. Manual render check of all 3 tabs (headless xvfb). ✅ (ran 25s, no panic; only benign DRI3 fallback warning)

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

## API facts learned (gpui-kit 0.7.3) — do NOT re-derive
- **`IconName` is the 106-icon DEFAULT subset** (`gpui-kit-assets/default-icons.txt`), NOT the
  full 1830 catalog. Valid sidebar icons used: `Bot` (Deep Work), `Play` (Deep Run),
  `SquareTerminal` (Interactive). `Workflow`/`Rocket`/`Terminal` do NOT exist here.
- **`Sidebar` implements `Styled`** → size it with `.w(px(...))` (there is NO `.width()`).
- **Fractional width** → `.w(relative(f32))` (e.g. progress bar). NOT `style_with(...)` /
  `percentage(...)`.
- **`w_N` helpers** exist for `0,2,4,6,8,10,12,16,20,24,32` + `w_full`/`w_1`/`w_1_2` etc.
  `w_18` does NOT exist → use `w_16` + `.truncate()`.
- **`SidebarMenuItem::suffix`** is `Fn(&mut Window,&mut App)->E + 'static` → it OWNS captured
  locals via `move`; clone owned values per call (e.g. `run_count.clone()`). It cannot borrow
  `self` (not 'static).
- **`cx.listener`** closures borrow `this: &mut Self` — keep them in the same method scope.

## Notes
- gpui docs: https://docs.rs/gpui/latest/gpui/
- **gpui-kit docs (user-saved, authoritative for components): https://gpui-kit.com/docs/**
- gpui-component `Sidebar` lives at `component::sidebar::{Sidebar, SidebarMenu, SidebarMenuItem}`.
