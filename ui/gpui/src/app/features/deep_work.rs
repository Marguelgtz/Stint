//! Deep Work destination: session overview, task list and task detail.
//! Rendered by `RootView::render` (see `app.rs`) for `Panel::DeepWork`.

use gpui_kit::component::{h_flex, v_flex};
use gpui_kit::prelude::*;
use gpui_kit::*;

use crate::app::{RootView, truncate};
use crate::components::{detail_field, phase_badge};
use crate::services::state::Task;

impl RootView {
    pub(crate) fn session_overview(&self) -> impl IntoElement {
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

    pub(crate) fn work_area(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
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
