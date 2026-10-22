//! Live Dashboard and Deep Board destinations: read-only views over
//! `stint dashboard` and `stint deep dash` models, refreshed in
//! `RootView::start_refresh`.
use gpui_kit::component::{h_flex, v_flex};
use gpui_kit::prelude::*;
use gpui_kit::*;

use crate::app::{RootView, format_duration, present, truncate};
use crate::components::{detail_field, metric, phase_badge, status_pill};
use crate::services::dashboard;
impl RootView {
    pub(crate) fn interactive_dash_panel(&self) -> impl IntoElement {
        let mut panel = v_flex()
            .id("interactive-dash-content")
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
                            .child("Interactive dashboard"),
                    )
                    .child(
                        div()
                            .ml_auto()
                            .text_sm()
                            .text_color(rgb(0x8993a4))
                            .child("Live compute · GPU · inference · refreshes every 5s"),
                    ),
            );

        if let Some(error) = &self.interactive_dash_error {
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x3a2927))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(format!("Dashboard unavailable: {error}")),
            );
        }

        let Some(model) = self.interactive_dash.as_ref() else {
            panel = panel.child(
                v_flex()
                    .w_full()
                    .p_5()
                    .border_1()
                    .border_color(rgb(0x2d333e))
                    .rounded_lg()
                    .bg(rgb(0x191d24))
                    .child(div().text_sm().text_color(rgb(0x8993a4)).child("Loading dashboard…")),
            );
            return panel;
        };

        if !model.error.is_empty() {
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x3a2927))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(model.error.clone()),
            );
        }

        // Session header card.
        let status = model.session.status.clone();
        let status_color = match status.as_str() {
            "READY" => rgb(0x67c587),
            "RECOVERABLE" => rgb(0xe6ad69),
            "" => rgb(0x8993a4),
            _ => rgb(0x70a4e5),
        };
        let header = v_flex()
            .w_full()
            .gap_3()
            .p_4()
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
                            .child(if model.no_session || status.is_empty() {
                                "No compute session".to_string()
                            } else {
                                format!("Instance #{} · {}", model.session.instance_id, model.session.gpu_model)
                            }),
                    )
                    .when(!status.is_empty() && !model.no_session, |h| {
                        h.child(status_pill(&status, status_color))
                    }),
            );
        let mut header = header;
        if !model.no_session && !status.is_empty() {
            header = header
                .child(
                    h_flex()
                        .gap_5()
                        .child(metric("Model", present(&model.session.model)))
                        .child(metric("Runtime", present(&model.session.runtime)))
                        .child(metric("Context", format!("{} tok", model.session.context)))
                        .child(metric("Rate", format!("${:.3}/hr", model.session.rate)))
                        .child(metric("Spent est.", format!("${:.2}", model.session.spent)))
                        .child(metric("Exposure", format!("${:.2}", model.session.exposure))),
                )
                .child(
                    h_flex()
                        .gap_5()
                        .child(metric("Elapsed", format_duration(dashboard::ns_to_secs(model.session.elapsed) as f64)))
                        .child(metric("Remaining", format_duration(dashboard::ns_to_secs(model.session.remaining) as f64)))
                        .child(metric("Scheduled", format_duration(dashboard::ns_to_secs(model.session.scheduled) as f64)))
                        .child(detail_field("Deadline", present(&model.session.deadline))),
                );
        }
        panel = panel.child(header);

        // GPU + health row.
        let gpu = &model.gpu;
        let health = &model.health;
        let gpu_card = v_flex()
            .flex_1()
            .gap_2()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .child(div().text_sm().font_weight(FontWeight::BOLD).child("GPU"))
                    .child(
                        div()
                            .ml_auto()
                            .child(status_pill(if gpu.available { "available" } else { "n/a" }, if gpu.available { rgb(0x67c587) } else { rgb(0x8993a4) })),
                    ),
            )
            .child(
                h_flex()
                    .gap_4()
                    .child(metric("Util", present(&gpu.utilization)))
                    .child(metric("VRAM", present(&gpu.vram)))
                    .child(metric("Power", present(&gpu.power)))
                    .child(metric("Temp", present(&gpu.temperature))),
            )
            .when(!gpu.error.is_empty(), |c| c.child(div().text_sm().text_color(rgb(0xe6ad69)).child(gpu.error.clone())));
        let health_card = v_flex()
            .flex_1()
            .gap_2()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(div().text_sm().font_weight(FontWeight::BOLD).child("Health"))
            .child(detail_field("Endpoint", present(&health.endpoint)))
            .child(detail_field("Tunnel", present(&health.tunnel)))
            .child(detail_field("Runtime", present(&health.runtime)))
            .child(detail_field("Watchdog", present(&health.watchdog)))
            .child(detail_field("SSH", present(&health.ssh)));
        panel = panel.child(h_flex().w_full().gap_4().child(gpu_card).child(health_card));

        // Inference card with lane table.
        let inf = &model.inference;
        let mut inf_card = v_flex()
            .w_full()
            .gap_3()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .child(div().text_sm().font_weight(FontWeight::BOLD).child("Inference"))
                    .when(inf.stale, |h| h.child(div().ml_auto().child(status_pill("stale", rgb(0xe6ad69)))))
                    .when(inf.stale && !inf.sample_age.is_empty(), |h| {
                        h.child(div().ml_auto().text_sm().text_color(rgb(0x8993a4)).child(inf.sample_age.clone()))
                    }),
            )
            .child(
                h_flex()
                    .gap_5()
                    .child(metric("Decode", present(&inf.decode)))
                    .child(metric(if inf.prefill_label.is_empty() { "Prefill" } else { &inf.prefill_label }, present(&inf.prefill)))
                    .child(metric("Queue", present(&inf.queue)))
                    .child(metric("Cache reuse", present(&inf.cache_reuse)))
                    .child(metric("Speculative", present(&inf.speculative)))
                    .child(metric("Lanes", present(&inf.lanes))),
            );
        if !inf.lane_rows.is_empty() {
            let lane_row = |cells: Vec<(String, bool)>| {
                h_flex()
                    .w_full()
                    .gap_2()
                    .text_sm()
                    .children(
                        cells
                            .into_iter()
                            .zip([0.12_f32, 0.12, 0.34, 0.21, 0.21].iter().copied())
                            .map(|((text, muted), frac)| {
                                div()
                                    .w(relative(frac))
                                    .min_w_0()
                                    .truncate()
                                    .text_color(if muted { rgb(0x91a0b5) } else { rgb(0x9ba6b8) })
                                    .child(text)
                            }),
                    )
            };
            inf_card = inf_card.child(
                v_flex()
                    .gap_1()
                    .child(lane_row(vec![
                        ("Lane".to_string(), true),
                        ("Depth".to_string(), true),
                        ("Status".to_string(), true),
                        ("Cache".to_string(), true),
                        ("Decode".to_string(), true),
                    ]))
                    .children(inf.lane_rows.iter().map(|lane| {
                        lane_row(vec![
                            (format!("#{}", lane.id), false),
                            (format!("d{}", lane.depth), false),
                            (lane.status.clone(), false),
                            (lane.cache.clone(), false),
                            (lane.decode.clone(), false),
                        ])
                    })),
            );
        }
        panel = panel.child(inf_card);

        // Performance card.
        let perf = &model.perf;
        let perf_card = v_flex()
            .w_full()
            .gap_2()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .child(div().text_sm().font_weight(FontWeight::BOLD).child("Performance"))
                    .when(perf.benchmarking, |h| h.child(div().ml_auto().child(status_pill("benchmarking", rgb(0x70a4e5)))))
                    .when(!perf.benchmarking && perf.age.is_empty(), |h| {
                        h.child(div().ml_auto().text_sm().text_color(rgb(0x8993a4)).child("no sample yet"))
                    }),
            )
            .child(
                h_flex()
                    .gap_5()
                    .child(metric("TTFT", present(&perf.ttft)))
                    .child(metric("Total", present(&perf.total)))
                    .child(metric("Decode", present(&perf.decode)))
                    .child(metric("Prompt tok", perf.prompt_tokens.to_string()))
                    .child(metric("Completion tok", perf.completion_tokens.to_string()))
                    .child(metric("Age", present(&perf.age))),
            )
            .when(!perf.error.is_empty(), |c| c.child(div().text_sm().text_color(rgb(0xe6ad69)).child(perf.error.clone())));
        panel = panel.child(perf_card);

        // Logs + lane events.
        if !model.logs.is_empty() || !model.lane_events.is_empty() {
            let mut logs_card = v_flex()
                .w_full()
                .gap_2()
                .p_4()
                .border_1()
                .border_color(rgb(0x2d333e))
                .rounded_lg()
                .bg(rgb(0x111318))
                .child(div().text_sm().font_weight(FontWeight::BOLD).child("Logs & lane events"));
            for line in model.lane_events.iter().chain(model.logs.iter()) {
                logs_card = logs_card.child(div().text_sm().text_color(rgb(0x9ba6b8)).child(line.clone()));
            }
            panel = panel.child(logs_card);
        }

        if !model.notice.is_empty() {
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x27231e))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(model.notice.clone()),
            );
        }

        panel
    }

    pub(crate) fn deep_board_panel(&self) -> impl IntoElement {
        let mut panel = v_flex()
            .id("deep-board-content")
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
                            .child("Deep Work board"),
                    )
                    .child(
                        div()
                            .ml_auto()
                            .text_sm()
                            .text_color(rgb(0x8993a4))
                            .child("Durable run progress · refreshes every 5s"),
                    ),
            );

        if let Some(error) = &self.deep_dash_error {
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x3a2927))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(format!("Deep board unavailable: {error}")),
            );
        }

        let Some(model) = self.deep_dash.as_ref() else {
            panel = panel.child(
                v_flex()
                    .w_full()
                    .p_5()
                    .border_1()
                    .border_color(rgb(0x2d333e))
                    .rounded_lg()
                    .bg(rgb(0x191d24))
                    .child(div().text_sm().text_color(rgb(0x8993a4)).child("Loading deep board…")),
            );
            return panel;
        };

        if !model.error.is_empty() {
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x3a2927))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(model.error.clone()),
            );
        }

        let empty_run = model.session_id.is_empty() && model.mission.is_empty();
        let verified = model.tasks.iter().filter(|t| t.status == "verified").count();

        // Header card.
        let mut header = v_flex()
            .w_full()
            .gap_3()
            .p_4()
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
                            .child(if empty_run { "No Deep Work run".to_string() } else { present(&model.mission) }),
                    )
                    .child(phase_badge(&model.phase))
                    .when(model.latest && !empty_run, |h| h.child(status_pill("latest", rgb(0x65b7b0))))
                    .when(model.can_land && !empty_run, |h| h.child(status_pill("can land", rgb(0x67c587)))),
            );
        if !empty_run {
            header = header
                .child(
                    h_flex()
                        .gap_5()
                        .child(detail_field("Session", present(&model.session_id)))
                        .child(detail_field("Branch", present(&model.branch)))
                        .child(detail_field("Base commit", present(&model.base_commit)))
                        .child(detail_field("Coordinator", present(&model.coordinator)))
                        .child(detail_field("Worker", present(&model.worker_name))),
                )
                .child(
                    h_flex()
                        .gap_5()
                        .child(detail_field("Started", present(&model.started_at)))
                        .child(detail_field("Deadline", present(&model.deadline)))
                        .child(detail_field("Land before", present(&model.land_before)))
                        .child(detail_field("Verify", present(&model.verify))),
                );
            if !model.landing_reason.is_empty() || !model.landing_commit.is_empty() {
                header = header.child(detail_field("Landing", format!("{} · {} · verify {}{}", model.landing_reason, model.landing_commit, if model.landing_verify_done { "done" } else { "pending" }, if model.landed_at.is_some() { " · landed" } else { "" })));
            }
            if !empty_run {
                header = header.child(self.progress_bar(verified, model.tasks.len()));
            }
        }
        panel = panel.child(header);

        // Compute + worker row.
        let compute = &model.compute;
        let worker = &model.worker;
        let compute_card = v_flex()
            .flex_1()
            .gap_2()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .child(div().text_sm().font_weight(FontWeight::BOLD).child("Compute"))
                    .child(
                        div()
                            .ml_auto()
                            .child(status_pill(if compute.available { "available" } else { "n/a" }, if compute.available { rgb(0x67c587) } else { rgb(0x8993a4) })),
                    ),
            )
            .child(detail_field("Status", present(&compute.status)))
            .child(detail_field("Endpoint", present(&compute.endpoint)))
            .child(detail_field("Runtime", present(&compute.runtime)))
            .child(detail_field("GPU", present(&compute.gpu)))
            .child(
                h_flex()
                    .gap_4()
                    .child(metric("Util", present(&compute.utilization)))
                    .child(metric("VRAM", present(&compute.vram)))
                    .child(metric("Lane", present(&compute.lane))),
            )
            .when(!compute.error.is_empty(), |c| c.child(div().text_sm().text_color(rgb(0xe6ad69)).child(compute.error.clone())));
        let worker_card = v_flex()
            .flex_1()
            .gap_2()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .child(div().text_sm().font_weight(FontWeight::BOLD).child("Worker"))
                    .child(
                        div()
                            .ml_auto()
                            .child(status_pill(
                                if worker.reachable { "reachable" } else if worker.observed { "observed" } else { "none" },
                                if worker.reachable { rgb(0x67c587) } else if worker.observed { rgb(0xe6ad69) } else { rgb(0x8993a4) },
                            )),
                    ),
            )
            .child(detail_field("Phase", present(&worker.latest_phase)))
            .child(detail_field("At", present(&worker.latest_at)))
            .child(
                h_flex()
                    .gap_4()
                    .child(metric("NInfer ctx", worker.n_infer_context.to_string()))
                    .child(metric("NInfer KV", worker.n_infer_kv.to_string()))
                    .child(metric("xHigh", worker.x_high_requests.to_string()))
                    .child(metric("Medium", worker.medium_requests.to_string())),
            )
            .child(
                h_flex()
                    .gap_4()
                    .child(metric("Compression", present(&worker.compression)))
                    .child(metric("Completed", worker.compression_completed.to_string()))
                    .child(metric("Failed", worker.compression_failed.to_string()))
                    .child(metric("Truncated", worker.truncated.to_string())),
            )
            .when(!worker.error.is_empty(), |c| c.child(div().text_sm().text_color(rgb(0xe6ad69)).child(worker.error.clone())))
            .when(!worker.hermes_log.is_empty(), |c| c.child(detail_field("Hermes log", truncate(&worker.hermes_log, 300))));
        panel = panel.child(h_flex().w_full().gap_4().child(compute_card).child(worker_card));

        // Tasks table.
        if !model.tasks.is_empty() {
            let tasks_card = v_flex()
                .w_full()
                .gap_2()
                .p_4()
                .border_1()
                .border_color(rgb(0x2d333e))
                .rounded_lg()
                .bg(rgb(0x191d24))
                .child(div().text_sm().font_weight(FontWeight::BOLD).child(format!("Tasks ({} verified of {})", verified, model.tasks.len())))
                .children(
                    model.tasks.iter().map(|task| {
                        h_flex()
                            .items_start()
                            .gap_2()
                            .child(div().w_16().flex_shrink_0().text_sm().text_color(rgb(0x9ba6b8)).truncate().child(task.id.clone()))
                            .child(div().flex_1().min_w_0().text_sm().child(truncate(&task.objective, 100)))
                            .when(!task.blocker.is_empty(), |r| r.child(div().flex_shrink_0().px_2().py_1().rounded_md().bg(rgb(0x3a2927)).text_sm().text_color(rgb(0xe6ad69)).child(truncate(&task.blocker, 40))))
                            .child(phase_badge(&task.status))
                    }),
                );
            panel = panel.child(tasks_card);
        }

        // Recent events.
        if !model.events.is_empty() {
            let mut events_card = v_flex()
                .w_full()
                .gap_1()
                .p_4()
                .border_1()
                .border_color(rgb(0x2d333e))
                .rounded_lg()
                .bg(rgb(0x111318))
                .child(div().text_sm().font_weight(FontWeight::BOLD).child("Recent events"));
            for event in model.events.iter().rev().take(10) {
                events_card = events_card.child(div().text_sm().text_color(rgb(0x8993a4)).child(format!("{} · {} {} {}", event.time, event.kind, event.task, event.detail)));
            }
            panel = panel.child(events_card);
        }

        if !model.notice.is_empty() {
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x27231e))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(model.notice.clone()),
            );
        }

        panel
    }
}
