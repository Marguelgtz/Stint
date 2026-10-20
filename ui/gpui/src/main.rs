mod state;
mod interactive;

use std::time::Duration;

use gpui_kit::component::{h_flex, v_flex, Theme, ThemeMode, TitleBar};
use gpui_kit::prelude::*;
use gpui_kit::*;
use interactive::{DeadlinePreview, Snapshot, StartPreview};
use state::{DeepState, Task};

actions!(stint_ui, [Quit]);

fn main() {
    gpui_kit::application().with_assets(gpui_kit::assets::Assets).run(|cx| {
        gpui_kit::init(cx);
        cx.bind_keys([
            #[cfg(target_os = "macos")]
            KeyBinding::new("cmd-q", Quit, None),
            #[cfg(not(target_os = "macos"))]
            KeyBinding::new("alt-f4", Quit, None),
        ]);
        cx.on_action(|_: &Quit, cx: &mut App| cx.quit());

        let options = WindowOptions {
            window_bounds: Some(WindowBounds::centered(size(px(1040.), px(720.)), cx)),
            ..TitleBar::window_options()
        };

        gpui_kit::open_window(options, cx, |window, cx| {
            window.activate_window();
            window.set_window_title("Stint");
            Theme::change(ThemeMode::Dark, Some(window), cx);
            cx.new(|cx| {
                let mut view = RootView::new();
                view.start_refresh(cx);
                view
            })
        })
        .expect("failed to open window");
    });
}

struct RootView {
    state: Option<DeepState>,
    refresh_error: Option<String>,
    selected_task: Option<String>,
    interactive_tab: bool,
    interactive_status: Option<Snapshot>,
    interactive_error: Option<String>,
    selected_hours: f64,
    pending_action: Option<PendingAction>,
    operation_busy: bool,
    operation_label: Option<String>,
    operation_message: Option<(bool, String)>,
}

enum PendingAction {
    Start(StartPreview),
    Deadline { duration: String, preview: DeadlinePreview },
    Resume(i64),
    Destroy(i64),
}

impl RootView {
    fn new() -> Self {
        let mut view = Self {
            state: None,
            refresh_error: None,
            selected_task: None,
            interactive_tab: false,
            interactive_status: None,
            interactive_error: None,
            selected_hours: 1.0,
            pending_action: None,
            operation_busy: false,
            operation_label: None,
            operation_message: None,
        };
        view.apply_load(state::load_latest());
        view
    }

    fn start_refresh(&mut self, cx: &mut Context<Self>) {
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| loop {
            smol::Timer::after(Duration::from_secs(2)).await;
            let result = smol::unblock(state::load_latest).await;
            if view
                .update(cx, |this, cx| {
                    this.apply_load(result);
                    cx.notify();
                })
                .is_err()
            {
                break;
            }
        })
        .detach();

        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let mut refresh_tick = 0u8;
            loop {
                smol::Timer::after(Duration::from_secs(3)).await;
                refresh_tick = refresh_tick.wrapping_add(1);
                let refresh = refresh_tick >= 4;
                if refresh {
                    refresh_tick = 0;
                }
                let result = smol::unblock(move || interactive::load_status(refresh)).await;
                if view
                    .update(cx, |this, cx| {
                        match result {
                            Ok(snapshot) => {
                                this.interactive_status = Some(snapshot);
                                this.interactive_error = None;
                            }
                            Err(error) => this.interactive_error = Some(error),
                        }
                        cx.notify();
                    })
                    .is_err()
                {
                    break;
                }
            }
        })
        .detach();
    }

    fn request_start_preview(&mut self, cx: &mut Context<Self>) {
        if self.operation_busy {
            return;
        }
        self.pending_action = None;
        self.operation_message = None;
        self.operation_busy = true;
        self.operation_label = Some("Searching current interactive offers…".to_string());
        let hours = self.selected_hours;
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = smol::unblock(move || interactive::preview_start(hours)).await;
            let _ = view.update(cx, |this, cx| {
                this.operation_busy = false;
                this.operation_label = None;
                match result {
                    Ok(preview) => this.pending_action = Some(PendingAction::Start(preview)),
                    Err(error) => this.operation_message = Some((false, error)),
                }
                cx.notify();
            });
        })
        .detach();
    }

    fn request_deadline_preview(&mut self, direction: &'static str, duration: &'static str, cx: &mut Context<Self>) {
        if self.operation_busy {
            return;
        }
        self.pending_action = None;
        self.operation_message = None;
        self.operation_busy = true;
        self.operation_label = Some("Preparing deadline preview…".to_string());
        let direction = direction.to_string();
        let duration = duration.to_string();
        let pending_duration = duration.clone();
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = smol::unblock(move || interactive::preview_deadline(&direction, &duration)).await;
            let _ = view.update(cx, |this, cx| {
                this.operation_busy = false;
                this.operation_label = None;
                match result {
                    Ok(preview) => {
                        this.pending_action = Some(PendingAction::Deadline { duration: pending_duration, preview });
                    }
                    Err(error) => this.operation_message = Some((false, error)),
                }
                cx.notify();
            });
        })
        .detach();
    }

    fn confirm_pending_action(&mut self, cx: &mut Context<Self>) {
        let Some(action) = self.pending_action.take() else {
            return;
        };
        self.operation_message = None;
        match action {
            PendingAction::Start(preview) => self.run_operation(
                "Starting interactive session…",
                move || interactive::start(&preview),
                cx,
            ),
            PendingAction::Deadline { duration, preview } => {
                let direction = preview.direction.clone();
                self.run_operation("Updating session deadline…", move || {
                    interactive::change_deadline(&direction, &duration, &preview)
                }, cx);
            }
            PendingAction::Resume(instance_id) => self.run_operation(
                "Resuming the saved instance…",
                move || interactive::resume(instance_id),
                cx,
            ),
            PendingAction::Destroy(instance_id) => self.run_operation(
                "Destroying the interactive instance…",
                move || interactive::destroy(instance_id),
                cx,
            ),
        }
    }

    fn run_operation(
        &mut self,
        label: &'static str,
        operation: impl FnOnce() -> Result<String, String> + Send + 'static,
        cx: &mut Context<Self>,
    ) {
        if self.operation_busy {
            return;
        }
        self.operation_busy = true;
        self.operation_label = Some(label.to_string());
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = smol::unblock(operation).await;
            let _ = view.update(cx, |this, cx| {
                this.operation_busy = false;
                this.operation_label = None;
                match result {
                    Ok(output) => {
                        let message = if output.is_empty() { "Stint command completed".to_string() } else { output };
                        this.operation_message = Some((true, truncate(&message, 600)));
                    }
                    Err(error) => this.operation_message = Some((false, error)),
                }
                cx.notify();
            });
        })
        .detach();
    }

    fn cancel_pending_action(&mut self, cx: &mut Context<Self>) {
        self.pending_action = None;
        cx.notify();
    }

    fn apply_load(&mut self, result: Result<DeepState, String>) {
        match result {
            Ok(state) => {
                if !state
                    .tasks
                    .iter()
                    .any(|task| Some(&task.id) == self.selected_task.as_ref())
                {
                    self.selected_task = state.tasks.first().map(|task| task.id.clone());
                }
                self.state = Some(state);
                self.refresh_error = None;
            }
            Err(error) => self.refresh_error = Some(error),
        }
    }

    fn header(&self) -> impl IntoElement {
        h_flex()
            .items_center()
            .gap_3()
            .child(
                div()
                    .text_lg()
                    .font_weight(FontWeight::BOLD)
                    .child("Stint"),
            )
            .child(
                div()
                    .px_2()
                    .py_1()
                    .rounded_md()
                    .bg(rgb(0x252a34))
                    .text_sm()
                    .child(if self.interactive_tab { "interactive controls" } else { "Deep Work read only" }),
            )
            .child(
                div()
                    .ml_auto()
                    .text_sm()
                    .text_color(rgb(0x8993a4))
                    .child(if self.interactive_tab { "Status every 3s · telemetry every 12s" } else { "Auto-refresh every 2 seconds" }),
            )
    }

    fn navigation(&self, cx: &mut Context<Self>) -> impl IntoElement {
        h_flex()
            .items_center()
            .gap_2()
            .child(
                div()
                    .id("tab-deep-work")
                    .px_3()
                    .py_2()
                    .rounded_md()
                    .cursor_pointer()
                    .bg(if self.interactive_tab { rgb(0x20252e) } else { rgb(0x354256) })
                    .text_sm()
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.interactive_tab = false;
                        cx.notify();
                    }))
                    .child("Deep Work"),
            )
            .child(
                div()
                    .id("tab-interactive")
                    .px_3()
                    .py_2()
                    .rounded_md()
                    .cursor_pointer()
                    .bg(if self.interactive_tab { rgb(0x354256) } else { rgb(0x20252e) })
                    .text_sm()
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.interactive_tab = true;
                        cx.notify();
                    }))
                    .child("Interactive"),
            )
    }

    fn interactive_panel(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let mut panel = v_flex()
            .id("interactive-content")
            .overflow_y_scroll()
            .w_full()
            .flex_1()
            .gap_4()
            .child(
                h_flex()
                    .items_center()
                    .child(
                        div()
                            .text_xl()
                            .font_weight(FontWeight::BOLD)
                            .child("Interactive session"),
                    )
                    .child(
                        div()
                            .ml_auto()
                            .text_sm()
                            .text_color(rgb(0x8993a4))
                            .child(if self.operation_busy {
                                self.operation_label.clone().unwrap_or_else(|| "Working…".to_string())
                            } else {
                                "Status refreshes every 3s · passive telemetry every 12s".to_string()
                            }),
                    ),
            );

        if let Some((success, message)) = &self.operation_message {
            let color = if *success { rgb(0x67c587) } else { rgb(0xe58c8c) };
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(color.opacity(0.12))
                    .text_sm()
                    .text_color(color)
                    .child(message.clone()),
            );
        }

        if let Some(snapshot) = self.interactive_status.as_ref().filter(|snapshot| snapshot.active) {
            panel = panel.child(self.interactive_session_card(snapshot));
            panel = panel.child(self.interactive_controls(snapshot, cx));
        } else {
            panel = panel.child(self.interactive_empty_state(cx));
        }

        if let Some(error) = &self.interactive_error {
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x3a2927))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(format!("Status unavailable: {error}")),
            );
        }

        if let Some(pending) = &self.pending_action {
            panel = panel.child(self.pending_confirmation(pending, cx));
        }

        panel
    }

    fn interactive_empty_state(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let mut card = v_flex()
            .w_full()
            .gap_4()
            .p_5()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                div()
                    .text_base()
                    .font_weight(FontWeight::BOLD)
                    .child("No active interactive session"),
            )
            .child(
                div()
                    .text_sm()
                    .text_color(rgb(0x9ba6b8))
                    .child("Review live GPU offers before renting. Stint will enforce the interactive profile’s hourly and total cost ceilings."),
            )
            .child(
                h_flex()
                    .items_center()
                    .gap_2()
                    .child(div().text_sm().text_color(rgb(0x8993a4)).child("Duration"))
                    .children([1.0_f64, 2.0, 4.0].into_iter().map(|hours| {
                        let selected = (self.selected_hours - hours).abs() < 0.001;
                        div()
                            .id(format!("start-duration-{hours:.0}"))
                            .px_3()
                            .py_2()
                            .rounded_md()
                            .cursor_pointer()
                            .bg(if selected { rgb(0x354256) } else { rgb(0x20252e) })
                            .text_sm()
                            .on_click(cx.listener(move |this, _, _, cx| {
                                this.selected_hours = hours;
                                this.pending_action = None;
                                cx.notify();
                            }))
                            .child(format!("{hours:.0}h"))
                    }))
                    .child(
                        div()
                            .id("start-review-offers")
                            .ml_auto()
                            .px_4()
                            .py_2()
                            .rounded_md()
                            .cursor_pointer()
                            .bg(rgb(0x35624e))
                            .font_weight(FontWeight::BOLD)
                            .text_sm()
                            .on_click(cx.listener(|this, _, _, cx| this.request_start_preview(cx)))
                            .child("Review offers"),
                    ),
            );

        if self.interactive_status.is_none() && self.interactive_error.is_none() {
            card = card.child(
                div()
                    .text_sm()
                    .text_color(rgb(0x8993a4))
                    .child("Checking local Stint session state…"),
            );
        }
        card
    }

    fn interactive_session_card(&self, snapshot: &Snapshot) -> impl IntoElement {
        let status_color = match snapshot.session.status.as_str() {
            "READY" => rgb(0x67c587),
            "RECOVERABLE" => rgb(0xe6ad69),
            "RENTING" | "BOOTING" | "SSH_CONNECTING" | "SSH_READY" | "RUNTIME_BOOTSTRAP"
            | "RUNTIME_READY" | "MODEL_STARTING" | "MODEL_STARTED" | "MODEL_LOADING" => {
                rgb(0x70a4e5)
            }
            _ => rgb(0x8993a4),
        };
        let remaining = format_duration(snapshot.time.remaining_seconds);
        let status_health = if snapshot.session.status == "READY" && snapshot.health.endpoint.refreshed {
            if snapshot.health.endpoint.healthy {
                format!("Healthy · {:.0} ms", snapshot.health.endpoint.latency_milliseconds)
            } else if !snapshot.health.endpoint.meta.error.is_empty() {
                format!("Degraded · {}", truncate(&snapshot.health.endpoint.meta.error, 90))
            } else {
                "Endpoint degraded".to_string()
            }
        } else if snapshot.session.status == "READY" {
            "Health not checked yet".to_string()
        } else {
            "Waiting for READY".to_string()
        };
        let mut card = v_flex()
            .w_full()
            .gap_4()
            .p_5()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .gap_3()
                    .child(
                        div()
                            .text_base()
                            .font_weight(FontWeight::BOLD)
                            .child(format!("Instance {}", snapshot.session.instance_id)),
                    )
                    .child(status_pill(&snapshot.session.status, status_color))
                    .child(
                        div()
                            .ml_auto()
                            .text_sm()
                            .text_color(if status_health.starts_with("Healthy") { rgb(0x67c587) } else { rgb(0xe6ad69) })
                            .child(status_health),
                    ),
            )
            .child(
                h_flex()
                    .gap_5()
                    .child(metric("GPU", present(&snapshot.session.gpu_model)))
                    .child(metric("Runtime", present(&snapshot.session.runtime)))
                    .child(metric("Model", present(&snapshot.session.model)))
                    .child(metric("Context", format!("{} tokens", snapshot.session.context_tokens))),
            )
            .child(
                h_flex()
                    .gap_5()
                    .child(metric("Remaining", remaining))
                    .child(metric("Hourly", format!("${:.3}/hr", snapshot.cost.hourly_usd)))
                    .child(metric("Spent", format!("${:.2}", snapshot.cost.estimated_spent_usd)))
                    .child(metric("Scheduled", format!("${:.2}", snapshot.cost.scheduled_usd))),
            )
            .child(detail_field("Deadline", present(&snapshot.time.deadline)))
            .child(detail_field("Local endpoint", "http://127.0.0.1:8409/v1".to_string()));

        card = card.child(detail_field(
            "Lifecycle services",
            format!(
                "Tunnel {} · watchdog {} · SSH {} · remote runtime {}",
                health_label(snapshot.health.tunnel.running),
                health_label(snapshot.health.watchdog.running),
                health_label(snapshot.health.runtime.ssh),
                if snapshot.health.runtime.refreshed {
                    if snapshot.health.runtime.running { "running" } else { "not detected" }
                } else {
                    "not checked"
                },
            ),
        ));
        if snapshot.health.runtime.refreshed && !snapshot.health.runtime.meta.error.is_empty() {
            card = card.child(detail_field("Runtime check", snapshot.health.runtime.meta.error.clone()));
        }
        if !snapshot.collected_at.is_empty() {
            card = card.child(detail_field("Snapshot updated", snapshot.collected_at.clone()));
        }

        if !snapshot.session.checkpoint.is_empty() {
            card = card.child(detail_field("Checkpoint", snapshot.session.checkpoint.clone()));
        }
        if !snapshot.session.last_error.is_empty() {
            card = card.child(
                div()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x3a2927))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(format!("Startup issue: {}", snapshot.session.last_error)),
            );
        }
        if snapshot.staleness.deadline_stale && !snapshot.staleness.warning.is_empty() {
            card = card.child(
                div()
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(snapshot.staleness.warning.clone()),
            );
        }
        if snapshot.session.status == "READY" {
            let traffic = if snapshot.inference.available {
                format!(
                    "{} processing · {} lanes · {:.1} decode tok/s · {:.1} prefill tok/s",
                    snapshot.inference.processing,
                    snapshot.inference.agents,
                    snapshot.inference.decode_tokens_sec,
                    snapshot.inference.prefill_tokens_sec,
                )
            } else if !snapshot.inference.unavailable_reason.is_empty() {
                snapshot.inference.unavailable_reason.clone()
            } else {
                "Live traffic has not been sampled yet".to_string()
            };
            card = card.child(detail_field("Live inference (passive)", traffic));
        }
        card
    }

    fn interactive_controls(&self, snapshot: &Snapshot, cx: &mut Context<Self>) -> impl IntoElement {
        let mut row = h_flex().w_full().items_center().gap_2();
        if snapshot.session.status == "RECOVERABLE" && !snapshot.time.expired && !self.operation_busy {
            let instance_id = snapshot.session.instance_id;
            row = row.child(control_button("Resume existing instance", "#35624e", cx.listener(move |this, _, _, cx| {
                this.pending_action = Some(PendingAction::Resume(instance_id));
                cx.notify();
            })));
        }
        if snapshot.session.status == "READY" && !snapshot.time.expired && !self.operation_busy {
            row = row
                .child(control_button("Extend 30m", "#34445e", cx.listener(|this, _, _, cx| this.request_deadline_preview("extend", "30m", cx))))
                .child(control_button("Shorten 15m", "#4b3e39", cx.listener(|this, _, _, cx| this.request_deadline_preview("shorten", "15m", cx))));
        }
        if snapshot.session.status != "STOPPED" && !self.operation_busy {
            let instance_id = snapshot.session.instance_id;
            row = row.child(control_button("End session…", "#633d3b", cx.listener(move |this, _, _, cx| {
                this.pending_action = Some(PendingAction::Destroy(instance_id));
                cx.notify();
            })));
        }
        if self.operation_busy {
            row = row.child(
                div()
                    .text_sm()
                    .text_color(rgb(0x8993a4))
                    .child(self.operation_label.clone().unwrap_or_else(|| "Working…".to_string())),
            );
        }
        row
    }

    fn pending_confirmation(&self, pending: &PendingAction, cx: &mut Context<Self>) -> impl IntoElement {
        let mut card = v_flex()
            .w_full()
            .gap_3()
            .p_4()
            .border_1()
            .border_color(rgb(0x7c684b))
            .rounded_lg()
            .bg(rgb(0x27231e));
        match pending {
            PendingAction::Start(preview) => {
                card = card
                    .child(div().font_weight(FontWeight::BOLD).child("Confirm GPU rental"))
                    .child(detail_field("Selected offer", format!("{} · {}", preview.selected.gpu_model, preview.selected.location)))
                    .child(detail_field("Price and duration", format!("${:.3}/hr · {:.0}h · estimated ${:.2}", preview.selected.hourly_usd, preview.hours, preview.selected.estimated_session_usd)))
                    .child(detail_field("Runtime", format!("{} · {} context tokens", preview.selected.runtime, preview.selected.context_tokens)))
                    .child(detail_field("Maximum session cost", format!("${:.2}", preview.session_max_cost_usd)))
                    .child(detail_field("Offer ceiling and retries", format!("${:.3}/hr · up to {} reviewed offer(s)", preview.max_hourly_usd, preview.fallback_attempts)))
                    .child(detail_field("Model", preview.model.clone()))
                    .child(detail_field("Endpoint", preview.endpoint.clone()));
                let fallbacks: Vec<String> = preview
                    .candidates
                    .iter()
                    .skip(1)
                    .map(|candidate| format!("{} · {} · ${:.3}/hr · ${:.2} total{}", candidate.gpu_model, candidate.location, candidate.hourly_usd, candidate.estimated_session_usd, if candidate.within_cost_ceiling { "" } else { " · over cap, will be skipped" }))
                    .collect();
                if !fallbacks.is_empty() {
                    card = card.child(detail_field("Possible retries", fallbacks.join("; ")));
                }
                card = card.child(div().text_sm().text_color(rgb(0xe6ad69)).child("Confirming allows Stint to rent the selected offer and try listed eligible fallbacks. Startup failures after SSH may preserve a paid instance for Resume."));
            }
            PendingAction::Deadline { duration, preview } => {
                card = card
                    .child(div().font_weight(FontWeight::BOLD).child(format!("Confirm {} session", preview.direction)))
                    .child(detail_field("Current remaining", format_duration(preview.current_remaining_seconds)))
                    .child(detail_field("Change", format!("{} {}", if preview.direction == "extend" { "+" } else { "−" }, duration)))
                    .child(detail_field("Current deadline", preview.previous_deadline.clone()))
                    .child(detail_field("New deadline", preview.new_deadline.clone()))
                    .child(detail_field("Projected session cost", format!("${:.2} / ${:.2} ceiling", preview.projected_usd, preview.session_ceiling_usd)))
                    .child(detail_field("Scheduled cost before change", format!("${:.2}", preview.current_scheduled_usd)))
                    .child(detail_field("Deadline adjustment", format_duration(preview.delta_seconds)))
                    .child(detail_field("Cost change", format!("${:+.2}", preview.exposure_delta_usd)));
            }
            PendingAction::Resume(instance_id) => {
                card = card
                    .child(div().font_weight(FontWeight::BOLD).child("Resume saved session?"))
                    .child(div().text_sm().child(format!("This reattaches to instance {instance_id}; it will not rent replacement compute.")));
            }
            PendingAction::Destroy(instance_id) => {
                card = card
                    .child(div().font_weight(FontWeight::BOLD).text_color(rgb(0xffb2a8)).child("End interactive session?"))
                    .child(div().text_sm().child(format!("This immediately destroys Vast instance {instance_id}.")));
            }
        }
        card.child(
            h_flex()
                .gap_2()
                .child(control_button("Confirm", "#35624e", cx.listener(|this, _, _, cx| this.confirm_pending_action(cx))))
                .child(control_button("Cancel", "#303640", cx.listener(|this, _, _, cx| this.cancel_pending_action(cx)))),
        )
    }

    fn session_overview(&self) -> impl IntoElement {
        let mut panel = v_flex()
            .w_full()
            .gap_3()
            .p_5()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24));

        match &self.state {
            Some(state) => {
                panel = panel
                    .child(
                        h_flex()
                            .items_center()
                            .gap_3()
                            .child(
                                div()
                                    .text_base()
                                    .font_weight(FontWeight::BOLD)
                                    .child(state.mission_name.clone()),
                            )
                            .child(phase_badge(&state.phase)),
                    )
                    .child(
                        div()
                            .text_sm()
                            .text_color(rgb(0xb4bbc7))
                            .child(state.objective.clone()),
                    )
                    .child(
                        h_flex()
                            .gap_5()
                            .text_sm()
                            .text_color(rgb(0x8e97a7))
                            .child(format!("Session {}", state.session_id))
                            .child(format!("{} tasks", state.tasks.len()))
                            .child(format!("Branch {}", state.branch))
                            .child(format!("Deadline {}", state.deadline.as_deref().unwrap_or("—"))),
                    );
                if let Some(handoff_path) = &state.handoff_path {
                    panel = panel.child(
                        div()
                            .text_sm()
                            .text_color(rgb(0x8e97a7))
                            .child(format!("Handoff {handoff_path}")),
                    );
                }
                if let Some(error) = &self.refresh_error {
                    panel = panel.child(
                        div()
                            .text_sm()
                            .text_color(rgb(0xe6ad69))
                            .child(format!("Refresh issue: {error}")),
                    );
                }
            }
            None => {
                panel = panel
                    .child(
                        div()
                            .text_base()
                            .font_weight(FontWeight::BOLD)
                            .child("No Deep Work session loaded"),
                    )
                    .child(
                        div()
                            .text_sm()
                            .text_color(rgb(0xe58c8c))
                            .child(
                                self.refresh_error
                                    .clone()
                                    .unwrap_or_else(|| "Waiting for session state".to_string()),
                            ),
                    )
                    .child(
                        div()
                            .text_sm()
                            .text_color(rgb(0x8e97a7))
                            .child("Reads the latest session from ~/.local/state/stint/deep/latest."),
                    );
            }
        }

        panel
    }

    fn work_area(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
        h_flex()
            .w_full()
            .flex_1()
            .gap_4()
            .child(self.task_list(cx))
            .child(self.task_detail())
    }

    fn task_list(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let mut panel = v_flex()
            .w(px(390.))
            .h_full()
            .gap_2()
            .p_3()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .id("task-list")
            .overflow_y_scroll();

        panel = panel.child(
            h_flex()
                .items_center()
                .pb_2()
                .child(
                    div()
                        .font_weight(FontWeight::BOLD)
                        .child("Tasks"),
                )
                .child(
                    div()
                        .ml_auto()
                        .text_sm()
                        .text_color(rgb(0x8993a4))
                        .child("Select a task for details"),
                ),
        );

        if let Some(state) = &self.state {
            panel = panel.children(state.tasks.iter().map(|task| {
                let task_id = task.id.clone();
                let is_selected = self.selected_task.as_deref() == Some(task.id.as_str());
                h_flex()
                    .id(format!("task-{}", task.id))
                    .items_center()
                    .gap_2()
                    .p_3()
                    .rounded_md()
                    .when(is_selected, |row| row.bg(rgb(0x2b3543)))
                    .when(!is_selected, |row| row.bg(rgb(0x20252e)))
                    .on_click(cx.listener(move |this, _, _, cx| {
                        this.selected_task = Some(task_id.clone());
                        cx.notify();
                    }))
                    .child(
                        div()
                            .w_16()
                            .text_sm()
                            .text_color(rgb(0x9ba6b8))
                            .child(task.id.clone()),
                    )
                    .child(
                        div()
                            .flex_1()
                            .text_sm()
                            .child(truncate(&task.objective, 76)),
                    )
                    .child(phase_badge(&task.status))
            }));
        } else {
            panel = panel.child(
                div()
                    .p_3()
                    .text_sm()
                    .text_color(rgb(0x8993a4))
                    .child("Waiting for task data"),
            );
        }

        panel
    }

    fn task_detail(&self) -> impl IntoElement {
        let mut panel = v_flex()
            .flex_1()
            .h_full()
            .gap_3()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .id("task-detail")
            .overflow_y_scroll();

        match self.selected_task() {
            Some(task) => {
                panel = panel
                    .child(
                        h_flex()
                            .items_center()
                            .gap_3()
                            .child(
                                div()
                                    .text_base()
                                    .font_weight(FontWeight::BOLD)
                                    .child(task.id.clone()),
                            )
                            .child(phase_badge(&task.status))
                            .child(
                                div()
                                    .ml_auto()
                                    .text_sm()
                                    .text_color(rgb(0x9ba6b8))
                                    .child(format!("{} attempts", task.attempts)),
                            ),
                    )
                    .child(detail_field("Objective", task.objective.clone()));

                if !task.acceptance.is_empty() {
                    panel = panel.child(detail_field("Acceptance", task.acceptance.clone()));
                }
                let verify = if task.verification_command.is_empty() {
                    task.verify.clone()
                } else {
                    task.verification_command.clone()
                };
                if !verify.is_empty() {
                    panel = panel.child(detail_field("Verification command", verify));
                }
                if !task.verification_result.is_empty() {
                    panel = panel.child(detail_field(
                        "Verification result",
                        task.verification_result.clone(),
                    ));
                }
                if !task.blocker.is_empty() {
                    panel = panel.child(detail_field("Blocker", task.blocker.clone()));
                }
                if !task.last_result.is_empty() {
                    panel = panel.child(detail_field(
                        "Last result",
                        truncate(&task.last_result, 900),
                    ));
                }
                if !task.verification_output.is_empty() {
                    panel = panel.child(detail_field(
                        "Verification output",
                        truncate(&task.verification_output, 900),
                    ));
                }
            }
            None => {
                panel = panel.child(
                    div()
                        .text_sm()
                        .text_color(rgb(0x8993a4))
                        .child("Select a task to inspect its details."),
                );
            }
        }

        panel
    }

    fn selected_task(&self) -> Option<&Task> {
        let state = self.state.as_ref()?;
        let selected = self.selected_task.as_deref()?;
        state.tasks.iter().find(|task| task.id == selected)
    }
}

impl Render for RootView {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let mut page = v_flex()
            .w_full()
            .h_full()
            .gap_4()
            .p_6()
            .bg(rgb(0x111318))
            .text_color(rgb(0xe7e9ee))
            .child(self.header())
            .child(self.navigation(cx));
        if self.interactive_tab {
            page = page.child(self.interactive_panel(cx));
        } else {
            page = page
                .child(self.session_overview())
                .child(self.work_area(cx));
        }
        page
    }
}

fn detail_field(label: &str, value: String) -> impl IntoElement {
    v_flex()
        .gap_1()
        .child(
            div()
                .text_sm()
                .text_color(rgb(0x91a0b5))
                .child(label.to_string()),
        )
        .child(div().text_sm().child(value))
}

fn phase_badge(phase: &str) -> impl IntoElement {
    let color = match phase {
        "executing" | "verified" => rgb(0x67c587),
        "active" | "attempting" => rgb(0x70a4e5),
        "landing" | "incomplete" | "needs_human" => rgb(0xe6ad69),
        "landed" => rgb(0x65b7b0),
        "blocked" => rgb(0xe58c8c),
        "stopped" => rgb(0x8993a4),
        _ => rgb(0x8993a4),
    };
    let label = if phase.is_empty() {
        "unknown".to_string()
    } else {
        phase.to_string()
    };
    div()
        .px_2()
        .py_1()
        .rounded_md()
        .bg(color.opacity(0.16))
        .text_color(color)
        .text_sm()
        .child(label)
}

fn status_pill(label: &str, color: Rgba) -> impl IntoElement {
    div()
        .px_2()
        .py_1()
        .rounded_md()
        .bg(color.opacity(0.16))
        .text_color(color)
        .text_sm()
        .child(label.to_string())
}

fn metric(label: &str, value: String) -> impl IntoElement {
    v_flex()
        .flex_1()
        .gap_1()
        .child(div().text_sm().text_color(rgb(0x91a0b5)).child(label.to_string()))
        .child(div().text_sm().child(value))
}

fn control_button(
    label: &str,
    color: &str,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let color = u32::from_str_radix(color.trim_start_matches('#'), 16).unwrap_or(0x303640);
    div()
        .id(label.to_string())
        .px_4()
        .py_2()
        .rounded_md()
        .cursor_pointer()
        .bg(rgb(color))
        .text_sm()
        .on_click(on_click)
        .child(label.to_string())
}

fn present(value: &str) -> String {
    if value.is_empty() { "—".to_string() } else { value.to_string() }
}

fn health_label(running: bool) -> &'static str {
    if running { "running" } else { "not running" }
}

fn format_duration(seconds: f64) -> String {
    if !seconds.is_finite() || seconds <= 0.0 {
        return "expired".to_string();
    }
    let total_minutes = (seconds / 60.0).ceil() as u64;
    let hours = total_minutes / 60;
    let minutes = total_minutes % 60;
    if hours > 0 {
        format!("{hours}h {minutes:02}m")
    } else {
        format!("{minutes}m")
    }
}

fn truncate(value: &str, max_chars: usize) -> String {
    let value = value.replace('\n', " ");
    if value.chars().count() <= max_chars {
        return value;
    }
    format!("{}…", value.chars().take(max_chars).collect::<String>())
}
