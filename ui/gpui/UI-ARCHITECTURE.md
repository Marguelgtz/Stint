# Stint GPUI UI — Architecture

Orientation guide for humans and agents working on the GPUI desktop UI in
`ui/gpui`. Read this before touching `src/`. It describes how the code is
organized and where new work belongs. It is not a design document: the
application's look, destinations, interactions, refresh cadences, and
operational safeguards are fixed.

## Layering

```text
Go CLI (source of truth)
  │  JSON over `stint dashboard --json`, `stint deep dash --json`,
  │  `stint interactive …`, `stint deep start|stop|resume|status`,
  │  and ~/.local/state/stint/deep/latest
  ▼
services/      data access: run commands, read files, deserialize JSON models
  ▼
app.rs         one RootView owns ALL shared state; refresh loops; root layout
  ▼
app/features/  page rendering + handlers as `impl RootView` extensions
  ▼
components/    small reusable UI helpers with no RootView state
```

## Source tree

```text
ui/gpui/src/
├── main.rs                 startup only: GPUI + GPUI Kit init, key bindings,
│                           window (1040×720, title "Stint", dark theme),
│                           RootView::new() + start_refresh()
├── app.rs                  RootView struct + shared fields, PendingAction,
│                           new()/start_refresh(), apply_*() result handlers,
│                           sidebar() (GPUI Kit Sidebar), compute_footer(),
│                           Render impl (page selection + root layout),
│                           shared formatting helpers (present, truncate,
│                           format_duration); health_label lives in
│                           interactive.rs (its only user)
├── app/
│   └── features/
│       ├── mod.rs          declares the four feature modules
│       ├── deep_work.rs    Deep Work: session header, task list, task detail
│       ├── deep_run.rs     Deep Run: launch flow, run monitor/history,
│                           run detail, lifecycle actions, action_button!
│       ├── interactive.rs  Interactive: session card, controls, pending
│                           confirmation, operations, deadline flow
│       └── dashboards.rs   Live Dashboard + Deep Board panels
├── navigation.rs           Panel enum (the five destinations)
├── components/
│   ├── mod.rs              declares + re-exports the component API
│   ├── detail_field.rs     label/value detail row
│   ├── phase_badge.rs      colored task-phase badge
│   ├── status_pill.rs      caller-colored status pill
│   ├── metric.rs           label/value metric cell
│   └── control_button.rs   hex-colored click control (interactive controls)
└── services/
    ├── mod.rs              declares + documents the service modules
    ├── state.rs            local deep-work state + task data models
    ├── interactive.rs      CLI invocation, interactive status snapshot,
    │                       start/stop/resume/extend/shorten/deadline ops,
    │                       deadline + start preview models
    ├── deep_run.rs         run history, mission preview, deep start,
    │                       stop/resume lifecycle operations
    └── dashboard.rs        `stint dashboard --json` / `stint deep dash --json`
                            retrieval + Go-compatible models
```

## Ownership and state

- **One `RootView`, one instance.** All page state (session snapshot,
  selected task, interactive session, pending action, run history, mission
  form fields, dashboards) is a field on `RootView`. Feature modules do not
  own state; they extend `RootView` with methods and mutate the shared
  fields. Do not introduce per-page view entities.
- **Refresh loops live in `app.rs::start_refresh`**: deep state (5 s),
  interactive status (3 s, heavy refresh every 4th tick), run history
  (5 s, initial + periodic), interactive dashboard (5 s), deep dashboard
  (5 s). Each applies results through an `apply_*` method that preserves
  selection when possible. Do not move loops into features or change
  intervals.
- **Services are render-free.** They only execute the existing CLI
  commands / read the existing state file and parse into models. The Go
  CLI remains the source of truth; never duplicate domain logic in Rust or
  change command arguments or JSON handling.

## Navigation

`navigation.rs` holds the `Panel` enum with the five destinations in
fixed order: Deep Work, Deep Run, Interactive, Live Dashboard, Deep Board.
The sidebar is rendered with GPUI Kit's `Sidebar` / `SidebarMenu` /
`SidebarMenuItem` in `app.rs::sidebar()` — labels, icons, active states,
the Deep Run run-count suffix, and the compute footer stay there. Do not
add another sidebar component.

## Rules for future work

- **New rendering for an existing destination** → add an `impl RootView`
  method to that destination's feature module. Cross-feature calls need
  `pub(in crate::app)` visibility (see `progress_bar`, used by both Deep
  Run and Deep Board).
- **New genuinely reusable UI helper** (no RootView state, used by more
  than one feature) → new file in `components/`, declared in
  `components/mod.rs` with a re-export. One-off markup stays in the
  feature module.
- **New data operation** → `services/`, mirroring how the Go CLI exposes
  it; add the model there, keep parsing mechanical.
- **Colors stay inline hex literals**, bit-identical to the pre-refactor
  values. There is no theme module; if a color is reused, leave the
  literals inline. Never change a value to "normalize" similar colors.
- **`mod.rs` files are entry points**: they declare child modules and
  re-export the intended API. No application logic in `mod.rs`.
- **Behavior preservation**: this codebase is refactored for structure,
  not behavior. Moving code must not change text, colors, spacing,
  command arguments, timers, confirmations, or error handling.

## Validation

From `ui/gpui`: `cargo check`, `cargo test`, `cargo build`. All three must
pass with zero warnings. Do not run `cargo fmt`: the pre-refactor code
itself is not rustfmt-clean (the original `main.rs` carries 102 rustfmt
hunks), so reformatting would silently alter code the refactor moved
verbatim. Keep the original formatting of moved code.

The app launches with the Go `stint` CLI on PATH;
`stint dashboard --json` / `stint deep dash --json` / interactive status
drive the panels.
