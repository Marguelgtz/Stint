//! Deep Run destination: run creation flow, live monitor, history and
//! per-run detail with lifecycle controls (stop/resume).

use gpui_kit::component::{h_flex, v_flex};
use gpui_kit::prelude::*;
use gpui_kit::*;

use crate::app::{RootView, truncate};
use crate::components::phase_badge;
use crate::services::deep_run::{self, RunRecord};
use crate::services::interactive::{self, StartPreview};

macro_rules! action_button {
    ($label:expr, $handler:expr) => {
        div().id(concat!("action-", $label)).px_3().py_2().rounded_md().cursor_pointer().bg(rgb(0x2b3543)).text_sm()
            .on_click($handler).child($label)
    };
}

impl RootView {
    fn pick_path(&mut self, mission: bool, cx: &mut Context<Self>) {
        let receiver = cx.prompt_for_paths(PathPromptOptions {
            files: mission,
            directories: !mission,
            multiple: false,
            prompt: Some(if mission { "Choose a Deep Work mission".into() } else { "Choose a clean target repository".into() }),
        });
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = receiver.await;
            let _ = view.update(cx, |this, cx| {
                if let Ok(Ok(Some(paths))) = result {
                    if let Some(path) = paths.first() {
                        if mission { this.mission_path = path.display().to_string(); this.mission_preview = None; }
                        else { this.repo_path = path.display().to_string(); this.mission_preview = None; }
                    }
                }
                cx.notify();
            });
        }).detach();
    }

    fn preview_mission(&mut self, cx: &mut Context<Self>) {
        let mission = self.mission_path.clone();
        let repo = self.repo_path.clone();
        self.run_busy = true;
        self.run_message = None;
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = smol::unblock(move || deep_run::preview(&mission, &repo)).await;
            let _ = view.update(cx, |this, cx| {
                this.run_busy = false;
                match result { Ok(preview) => { this.mission_preview = Some(preview); this.run_message = Some((true, "Mission and repository validated".to_string())); }, Err(error) => { this.mission_preview = None; this.run_message = Some((false, error)); } }
                cx.notify();
            });
        }).detach();
    }

    fn request_deep_start(&mut self, cx: &mut Context<Self>) {
        if self.run_busy || self.mission_preview.is_none() { return; }
        self.run_busy = true;
        self.run_message = Some((true, "Checking compute session and offers…".to_string()));
        let hours = self.run_hours;
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = match smol::unblock(move || interactive::load_status(false)).await {
                Ok(snapshot) if snapshot.active && snapshot.session.status == "READY" => Ok((true, None)),
                _ => smol::unblock(move || interactive::preview_start(hours)).await.map(|preview| (false, Some(preview))),
            };
            let _ = view.update(cx, |this, cx| {
                this.run_busy = false;
                match result {
                    Ok((true, _)) => this.pending_run = Some(StartPreview { hours, session_max_cost_usd: 0.0, max_hourly_usd: 0.0, model: String::new(), endpoint: String::new(), fallback_attempts: 0, selected: interactive::StartCandidate { offer_id: "existing-ready-session".into(), gpu_model: String::new(), location: String::new(), hourly_usd: 0.0, estimated_session_usd: 0.0, runtime: String::new(), context_tokens: 0, within_cost_ceiling: true }, candidates: vec![] }),
                    Ok((false, Some(preview))) => this.pending_run = Some(preview),
                    _ => this.run_message = Some((false, result.err().unwrap_or_else(|| "Unable to preview compute offer".into()))),
                }
                cx.notify();
            });
        }).detach();
    }

    fn confirm_deep_start(&mut self, cx: &mut Context<Self>) {
        let Some(review) = self.pending_run.take() else { return; };
        let mission = self.mission_path.clone();
        let repo = self.repo_path.clone();
        let provision = review.selected.offer_id != "existing-ready-session";
        self.run_busy = true;
        self.run_message = Some((true, "Starting Deep Work…".to_string()));
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = smol::unblock(move || {
                if provision { interactive::start(&review)?; }
                deep_run::start(&mission, &repo, if provision { Some(review.hours) } else { None })
            }).await;
            let _ = view.update(cx, |this, cx| {
                this.run_busy = false;
                match result { Ok(output) => { this.run_message = Some((true, if output.is_empty() { "Deep Work launch accepted".into() } else { output })); }, Err(error) => this.run_message = Some((false, error)) }
                cx.notify();
            });
        }).detach();
    }

    fn run_lifecycle_action(&mut self, resume: Option<String>, cx: &mut Context<Self>) {
        let action = resume.clone();
        self.confirm_stop = false;
        self.confirm_resume = None;
        self.run_busy = true;
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = smol::unblock(move || match action { Some(id) => deep_run::resume(&id), None => deep_run::stop() }).await;
            let _ = view.update(cx, |this, cx| { this.run_busy = false; this.run_message = Some(match result { Ok(output) => (true, output), Err(error) => (false, error) }); cx.notify(); });
        }).detach();
    }

    fn launch_step(&self) -> u8 {
        if self.mission_path.is_empty() || self.repo_path.is_empty() {
            0
        } else if self.mission_preview.is_none() {
            1
        } else if self.pending_run.is_some() {
            2
        } else {
            3
        }
    }

    fn step_indicator(&self, index: u8, label: &str) -> impl IntoElement {
        let current = self.launch_step();
        let (circle, text) = if current < index {
            (rgb(0x3a4150), rgb(0x7d8698))
        } else if current == index {
            (rgb(0x70a4e5), rgb(0xdbe6f5))
        } else {
            (rgb(0x3d6a52), rgb(0x8fd0aa))
        };
        let mark = if current > index { "✓".to_string() } else { index.to_string() };
        h_flex()
            .items_center()
            .gap_2()
            .child(
                div()
                    .w_6()
                    .h_6()
                    .rounded_full()
                    .flex_shrink_0()
                    .border_1()
                    .border_color(circle)
                    .text_sm()
                    .text_color(circle)
                    .child(mark),
            )
            .child(div().text_sm().text_color(text).child(label.to_string()))
    }

    pub(crate) fn progress_bar(&self, verified: usize, total: usize) -> impl IntoElement {
        let fraction = if total > 0 {
            (verified as f64 / total as f64).clamp(0.0, 1.0)
        } else {
            0.0
        };
        h_flex()
            .items_center()
            .w_full()
            .gap_3()
            .child(
                div()
                    .flex_1()
                    .h_2()
                    .rounded_full()
                    .overflow_hidden()
                    .bg(rgb(0x232936))
                    .child(
                        div()
                            .h_full()
                            .rounded_full()
                            .bg(rgb(0x67c587))
                            .w(relative(fraction as f32)),
                    ),
            )
            .child(
                div()
                    .text_sm()
                    .text_color(rgb(0x9ba6b8))
                    .child(format!("{verified}/{total} verified")),
            )
    }

    pub(crate) fn deep_run_panel(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
        let mut panel = v_flex()
            .id("deep-run-content")
            .overflow_y_scroll()
            .w_full()
            .flex_1()
            .min_h_0()
            .gap_4()
            .child(
                h_flex()
                    .items_center()
                    .child(
                        div()
                            .text_xl()
                            .font_weight(FontWeight::BOLD)
                            .child("Create and monitor Deep Work"),
                    )
                    .child(
                        div()
                            .ml_auto()
                            .text_sm()
                            .text_color(if self.run_busy { rgb(0xe6ad69) } else { rgb(0x8993a4) })
                            .child(if self.run_busy {
                                "Working…".to_string()
                            } else {
                                "Refreshes every 5s".to_string()
                            }),
                    ),
            );

        let launch = self.launch_flow(cx);
        panel = panel.child(launch);
        panel.child(self.run_monitor(cx))
    }

    fn launch_flow(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
        let mut card = v_flex()
            .w_full()
            .flex_shrink_0()
            .gap_4()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .gap_5()
                    .child(self.step_indicator(0, "Mission & repo"))
                    .child(self.step_indicator(1, "Validate"))
                    .child(self.step_indicator(2, "Compute & launch")),
            );

        let mut step = v_flex().gap_3().p_3().rounded_md().bg(rgb(0x20252e));
        step = step.child(
            h_flex()
                .items_center()
                .gap_2()
                .child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .text_sm()
                        .text_color(rgb(0x9ba6b8))
                        .child(if self.mission_path.is_empty() {
                            "Choose a mission Markdown file".to_string()
                        } else {
                            self.mission_path.clone()
                        }),
                )
                .child(action_button!(
                    "Choose mission",
                    cx.listener(|this, _, _, cx| this.pick_path(true, cx))
                )),
        );
        step = step.child(
            h_flex()
                .items_center()
                .gap_2()
                .child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .text_sm()
                        .text_color(rgb(0x9ba6b8))
                        .child(if self.repo_path.is_empty() {
                            "Choose a clean target repository".to_string()
                        } else {
                            self.repo_path.clone()
                        }),
                )
                .child(action_button!(
                    "Choose repository",
                    cx.listener(|this, _, _, cx| this.pick_path(false, cx))
                )),
        );
        step = step.child(
            h_flex()
                .items_center()
                .gap_2()
                .child(div().text_sm().text_color(rgb(0x8993a4)).child("If new compute is needed"))
                .children([1.0_f64, 2.0, 4.0].into_iter().map(|hours| {
                    let selected = (self.run_hours - hours).abs() < 0.001;
                    div()
                        .id(format!("deep-hours-{hours:.0}"))
                        .px_3()
                        .py_2()
                        .rounded_md()
                        .cursor_pointer()
                        .bg(if selected { rgb(0x354256) } else { rgb(0x20252e) })
                        .text_sm()
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.run_hours = hours;
                            cx.notify();
                        }))
                        .child(format!("{hours:.0}h"))
                }))
                .child(action_button!(
                    "Validate and preview",
                    cx.listener(|this, _, _, cx| this.preview_mission(cx))
                )),
        );
        card = card.child(step);

        if let Some(preview) = &self.mission_preview {
            card = card.child(
                v_flex()
                    .gap_1()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x20252e))
                    .child(
                        div()
                            .font_weight(FontWeight::BOLD)
                            .child(format!("{} · {} tasks", preview.name, preview.tasks.len())),
                    )
                    .child(div().text_sm().child(preview.objective.clone()))
                    .children(
                        preview
                            .tasks
                            .iter()
                            .map(|task| div().text_sm().text_color(rgb(0x9ba6b8)).child(format!("{} · {}", task.id, task.objective))),
                    )
                    .when(!preview.success.is_empty(), |card| {
                        card.child(div().text_sm().child(format!("Success: {}", preview.success.join(" · "))))
                    })
                    .when(!preview.constraints.is_empty(), |card| {
                        card.child(div().text_sm().child(format!("Constraints: {}", preview.constraints.join(" · "))))
                    })
                    .child(div().text_sm().text_color(rgb(0x8993a4)).child(truncate(&preview.content, 5000))),
            );
            card = card.child(action_button!(
                "Review and start",
                cx.listener(|this, _, _, cx| this.request_deep_start(cx))
            ));
        }

        if let Some(review) = &self.pending_run {
            let provision = review.selected.offer_id != "existing-ready-session";
            card = card.child(
                v_flex()
                    .gap_2()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x252a34))
                    .child(
                        div()
                            .font_weight(FontWeight::BOLD)
                            .child(if provision {
                                "Confirm compute rental"
                            } else {
                                "Confirm Deep Work start"
                            }),
                    )
                    .child(
                        div().text_sm().child(if provision {
                            format!(
                                "{} · {} · {}h · ${:.2}/hour · estimated ${:.2}",
                                review.selected.gpu_model,
                                review.selected.runtime,
                                review.hours,
                                review.selected.hourly_usd,
                                review.selected.estimated_session_usd
                            )
                        } else {
                            "Reuse the existing READY compute session. No new rental will be requested.".into()
                        }),
                    )
                    .child(
                        h_flex()
                            .gap_2()
                            .child(action_button!(
                                "Confirm",
                                cx.listener(|this, _, _, cx| this.confirm_deep_start(cx))
                            ))
                            .child(action_button!(
                                "Cancel",
                                cx.listener(|this, _, _, cx| {
                                    this.pending_run = None;
                                    cx.notify();
                                })
                            )),
                    ),
            );
        }

        if let Some((success, message)) = &self.run_message {
            card = card.child(
                div()
                    .p_2()
                    .rounded_md()
                    .bg(if *success { rgb(0x1d3429) } else { rgb(0x3a2927) })
                    .text_sm()
                    .text_color(if *success { rgb(0x67c587) } else { rgb(0xe58c8c) })
                    .child(message.clone()),
            );
        }
        card
    }

    fn run_monitor(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
        let mut container = h_flex()
            .id("run-monitor")
            .w_full()
            .flex_1()
            .min_h_0()
            .gap_4();

        let list = if self.run_history.is_empty() && self.run_history_error.is_none() {
            v_flex()
                .w(px(300.))
                .flex_shrink_0()
                .h_full()
                .p_4()
                .border_1()
                .border_color(rgb(0x2d333e))
                .rounded_lg()
                .bg(rgb(0x191d24))
                .child(div().text_sm().text_color(rgb(0x8993a4)).child("No Deep Work runs yet. Launch one above."))
                .into_any_element()
        } else if let Some(error) = &self.run_history_error {
            v_flex()
                .w(px(300.))
                .flex_shrink_0()
                .h_full()
                .p_4()
                .border_1()
                .border_color(rgb(0x2d333e))
                .rounded_lg()
                .bg(rgb(0x191d24))
                .child(div().font_weight(FontWeight::BOLD).child("History"))
                .child(
                    div()
                        .text_sm()
                        .text_color(rgb(0xe6ad69))
                        .child(format!("History unavailable: {error}")),
                )
                .into_any_element()
        } else {
            v_flex()
                .w(px(300.))
                .flex_shrink_0()
                .h_full()
                .p_3()
                .border_1()
                .border_color(rgb(0x2d333e))
                .rounded_lg()
                .bg(rgb(0x191d24))
                .child(div().font_weight(FontWeight::BOLD).child("Runs"))
                .child(
                    div()
                        .id("run-history-list")
                        .overflow_y_scroll()
                        .flex_1()
                        .min_h_0()
                        .children(
                            self.run_history
                                .iter()
                                .map(|record| self.run_history_row(record, cx)),
                        ),
                )
                .into_any_element()
        };
        container = container.child(list);

        let detail = if let Some(record) = self
            .run_history
            .iter()
            .find(|r| Some(r.state.session_id.as_str()) == self.selected_run.as_deref())
        {
            self.run_detail(record, cx).into_any_element()
        } else {
            v_flex()
                .flex_1()
                .min_w_0()
                .h_full()
                .p_4()
                .border_1()
                .border_color(rgb(0x2d333e))
                .rounded_lg()
                .bg(rgb(0x191d24))
                .child(div().text_sm().text_color(rgb(0x8993a4)).child("Select a run to inspect its progress."))
                .into_any_element()
        };
        container.child(detail)
    }

    fn run_history_row(&self, record: &RunRecord, cx: &mut Context<Self>) -> impl IntoElement {
        let id = record.state.session_id.clone();
        let selected = self.selected_run.as_deref() == Some(id.as_str());
        let verified = record.state.tasks.iter().filter(|t| t.status == "verified").count();
        h_flex()
            .id(format!("run-history-{id}"))
            .items_center()
            .gap_2()
            .p_3()
            .mb_1()
            .rounded_md()
            .cursor_pointer()
            .bg(if selected { rgb(0x2b3543) } else { rgb(0x20252e) })
            .on_click(cx.listener(move |this, _, _, cx| {
                this.selected_run = Some(id.clone());
                cx.notify();
            }))
            .child(
                v_flex()
                    .flex_1()
                    .min_w_0()
                    .gap_1()
                    .child(
                        h_flex()
                            .items_center()
                            .gap_1()
                            .child(
                                div()
                                    .flex_1()
                                    .min_w_0()
                                    .text_sm()
                                    .child(truncate(&record.state.mission_name, 28)),
                            )
                            .when(record.latest, |row| {
                                row.child(
                                    div()
                                        .flex_shrink_0()
                                        .px_2()
                                        .py_1()
                                        .rounded_md()
                                        .bg(rgb(0x1d3429))
                                        .text_sm()
                                        .text_color(rgb(0x67c587))
                                        .child("latest"),
                                )
                            }),
                    )
                    .child(
                        div()
                            .text_sm()
                            .text_color(rgb(0x8993a4))
                            .child(format!("{} verified of {}", verified, record.state.tasks.len())),
                    ),
            )
            .child(phase_badge(&record.state.phase))
    }

    fn run_detail(&self, record: &RunRecord, cx: &mut Context<Self>) -> impl IntoElement {
        let terminal = record.state.phase == "landed" || record.state.phase == "stopped";
        let verified = record.state.tasks.iter().filter(|t| t.status == "verified").count();
        let mut card = v_flex()
            .flex_1()
            .min_w_0()
            .h_full()
            .gap_3()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .gap_2()
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .text_base()
                            .font_weight(FontWeight::BOLD)
                            .child(truncate(&record.state.mission_name, 40)),
                    )
                    .child(phase_badge(&record.state.phase)),
            )
            .child(
                div()
                    .text_sm()
                    .text_color(rgb(0x9ba6b8))
                    .child(format!(
                        "Session {} · deadline {} · coordinator {}",
                        record.state.session_id,
                        record.state.deadline.as_deref().unwrap_or("unknown"),
                        if record.coordinator_alive { "running" } else { "not running" }
                    )),
            );

        card = card.child(self.progress_bar(verified, record.state.tasks.len()));

        if record.latest && !terminal {
            card = card.child(
                h_flex()
                    .gap_2()
                    .child(action_button!(
                        "Stop run",
                        cx.listener(|this, _, _, cx| {
                            this.confirm_stop = true;
                            cx.notify();
                        })
                    ))
                    .when(!record.coordinator_alive, |row| {
                        let id = record.state.session_id.clone();
                        row.child(action_button!(
                            "Resume",
                            cx.listener(move |this, _, _, cx| {
                                this.confirm_resume = Some(id.clone());
                                cx.notify();
                            })
                        ))
                    }),
            );
        } else if !record.latest {
            card = card.child(
                div()
                    .text_sm()
                    .text_color(rgb(0x8993a4))
                    .child("Historical run — read only. Controls act on the latest run only."),
            );
        }

        if self.confirm_stop && record.latest {
            card = card.child(
                h_flex()
                    .items_center()
                    .gap_2()
                    .child(div().text_sm().child("Stop this Deep Work run and land its current state?"))
                    .child(action_button!(
                        "Confirm stop",
                        cx.listener(|this, _, _, cx| this.run_lifecycle_action(None, cx))
                    ))
                    .child(action_button!(
                        "Cancel",
                        cx.listener(|this, _, _, cx| {
                            this.confirm_stop = false;
                            cx.notify();
                        })
                    )),
            );
        }
        if let Some(id) = &self.confirm_resume {
            let selected_id = id.clone();
            card = card.child(
                h_flex()
                    .items_center()
                    .gap_2()
                    .child(div().text_sm().child(format!("Resume {selected_id}?")))
                    .child(action_button!(
                        "Confirm resume",
                        cx.listener(move |this, _, _, cx| {
                            this.run_lifecycle_action(Some(selected_id.clone()), cx)
                        })
                    ))
                    .child(action_button!(
                        "Cancel",
                        cx.listener(|this, _, _, cx| {
                            this.confirm_resume = None;
                            cx.notify();
                        })
                    )),
            );
        }

        let tasks = record.state.tasks.clone();
        let incidents = record.incidents.clone();
        let body = v_flex()
            .id("run-detail-body")
            .flex_1()
            .min_h_0()
            .overflow_y_scroll()
            .gap_1()
            .child(div().text_sm().font_weight(FontWeight::BOLD).child("Tasks"))
            .children(
                tasks.iter().map(|task| {
                    h_flex()
                        .items_start()
                        .gap_2()
                        .child(
                            div()
                                .w_16()
                                .flex_shrink_0()
                                .text_sm()
                                .text_color(rgb(0x9ba6b8))
                                .truncate()
                                .child(task.id.clone()),
                        )
                        .child(
                            h_flex()
                                .items_start()
                                .gap_2()
                                .child(
                                    div()
                                        .flex_1()
                                        .min_w_0()
                                        .text_sm()
                                        .child(truncate(&task.objective, 90)),
                                )
                                .when(!task.blocker.is_empty(), |row| {
                                    row.child(
                                        div()
                                            .flex_shrink_0()
                                            .px_2()
                                            .py_1()
                                            .rounded_md()
                                            .bg(rgb(0x3a2927))
                                            .text_sm()
                                            .text_color(rgb(0xe6ad69))
                                            .child(truncate(&task.blocker, 40)),
                                    )
                                }),
                        )
                        .child(phase_badge(&task.status))
                }),
            )
            .when(!incidents.is_empty(), |body| {
                body.child(div().pt_2().text_sm().font_weight(FontWeight::BOLD).child("Recent incidents")).children(
                    incidents.iter().rev().take(6).map(|incident| {
                        div()
                            .text_sm()
                            .text_color(rgb(0x8993a4))
                            .child(format!("{} · {} {} {}", incident.time, incident.kind, incident.task, incident.detail))
                    }),
                )
            });
        card.child(body)
    }
}
