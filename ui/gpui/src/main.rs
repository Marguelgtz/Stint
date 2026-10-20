mod state;

use gpui_kit::component::{h_flex, v_flex, Theme, ThemeMode, TitleBar};
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
use state::DeepState;

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
            window_bounds: Some(WindowBounds::centered(size(px(920.), px(640.)), cx)),
            ..TitleBar::window_options()
        };

        gpui_kit::open_window(options, cx, |window, cx| {
            window.activate_window();
            window.set_window_title("Stint Deep Work");
            Theme::change(ThemeMode::Dark, Some(window), cx);
            cx.new(|_| RootView {
                state: state::load_latest(),
            })
        })
        .expect("failed to open window");
    });
}

struct RootView {
    state: Result<DeepState, String>,
}

impl Render for RootView {
    fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
        v_flex()
            .w_full()
            .h_full()
            .gap_4()
            .p_6()
            .bg(rgb(0x111318))
            .text_color(rgb(0xe7e9ee))
            .child(self.header())
            .child(self.session_overview())
    }
}

impl RootView {
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
            Ok(state) => {
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
                    .child(div().text_sm().text_color(rgb(0xb4bbc7)).child(state.objective.clone()))
                    .child(
                        h_flex()
                            .gap_5()
                            .text_sm()
                            .text_color(rgb(0x8e97a7))
                            .child(format!("Session {}", state.session_id))
                            .child(format!("{} tasks", state.tasks.len()))
                            .child(format!("Branch {}", state.branch))
                            .child(format!("Deadline {}", state.deadline.as_deref().unwrap_or("—"))),
                    )
                    .when_some(state.handoff_path.as_ref(), |panel, handoff_path| {
                        panel.child(
                            div()
                                .text_sm()
                                .text_color(rgb(0x8e97a7))
                                .child(format!("Handoff {handoff_path}")),
                        )
                    })
                    .child(
                        v_flex()
                            .gap_1()
                            .pt_2()
                            .children(state.tasks.iter().map(|task| {
                                h_flex()
                                    .items_center()
                                    .gap_3()
                                    .p_3()
                                    .rounded_md()
                                    .bg(rgb(0x20252e))
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
                                            .child(task.objective.clone()),
                                    )
                                    .child(phase_badge(&task.status))
                                    .child(
                                        div()
                                            .text_sm()
                                            .text_color(rgb(0x9ba6b8))
                                            .child(format!("{} attempts", task.attempts)),
                                    )
                            })),
                    );
            }
            Err(error) => {
                panel = panel
                    .child(
                        div()
                            .text_base()
                            .font_weight(FontWeight::BOLD)
                            .child("No Deep Work session loaded"),
                    )
                    .child(div().text_sm().text_color(rgb(0xe58c8c)).child(error.clone()))
                    .child(
                        div()
                            .text_sm()
                            .text_color(rgb(0x8e97a7))
                            .child("The dashboard reads the latest session from ~/.local/state/stint/deep/latest."),
                    );
            }
        }

        panel
    }
}

fn phase_badge(phase: &str) -> impl IntoElement {
    let color = match phase {
        "executing" | "verified" => rgb(0x67c587),
        "active" | "attempting" => rgb(0x70a4e5),
        "landing" => rgb(0xe6ad69),
        "landed" => rgb(0x65b7b0),
        "blocked" => rgb(0xe58c8c),
        "incomplete" | "needs_human" => rgb(0xe6ad69),
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
