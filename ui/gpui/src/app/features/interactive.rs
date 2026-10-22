//! Interactive destination: session status card, compute controls,
//! start/deadline previews and pending-action confirmations.
use gpui_kit::component::{h_flex, v_flex};
use gpui_kit::prelude::*;
use gpui_kit::*;

use crate::app::{PendingAction, RootView, format_duration, present, truncate};
use crate::components::{control_button, detail_field, metric, status_pill};
use crate::services::interactive::{self, Snapshot};
impl RootView {
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

    pub(crate) fn interactive_panel(&self, cx: &mut Context<Self>) -> impl IntoElement {
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
}

fn health_label(running: bool) -> &'static str {
    if running { "running" } else { "not running" }
}
