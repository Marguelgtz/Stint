mod state;

use std::time::Duration;

use gpui_kit::component::{h_flex, v_flex, Theme, ThemeMode, TitleBar};
use gpui_kit::prelude::*;
use gpui_kit::*;
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
            window.set_window_title("Stint Deep Work");
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
}

impl RootView {
    fn new() -> Self {
        let mut view = Self {
            state: None,
            refresh_error: None,
            selected_task: None,
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
                    .child("Stint Deep Work"),
            )
            .child(
                div()
                    .px_2()
                    .py_1()
                    .rounded_md()
                    .bg(rgb(0x252a34))
                    .text_sm()
                    .child("read only"),
            )
            .child(
                div()
                    .ml_auto()
                    .text_sm()
                    .text_color(rgb(0x8993a4))
                    .child("Auto-refresh every 2 seconds"),
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
        v_flex()
            .w_full()
            .h_full()
            .gap_4()
            .p_6()
            .bg(rgb(0x111318))
            .text_color(rgb(0xe7e9ee))
            .child(self.header())
            .child(self.session_overview())
            .child(self.work_area(cx))
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

fn truncate(value: &str, max_chars: usize) -> String {
    let value = value.replace('\n', " ");
    if value.chars().count() <= max_chars {
        return value;
    }
    format!("{}…", value.chars().take(max_chars).collect::<String>())
}
