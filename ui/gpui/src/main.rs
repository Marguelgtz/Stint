//! Stint Deep Work dashboard, built on GPUI (gpui-kit).
//!
//! Reads durable deep-work session state from `~/.local/state/stint/deep/`
//! and renders a live view of the mission, phase, and task list.

use gpui_kit::actions;
use gpui_kit::component::{h_flex, v_flex, Theme, ThemeMode, TitleBar};
use gpui_kit::*;

actions!(stint_ui, [Quit]);

fn main() {
    let app = gpui_kit::application().with_assets(gpui_kit::assets::Assets);

    app.run(move |cx| {
        gpui_kit::init(cx);

        cx.bind_keys([
            #[cfg(target_os = "macos")]
            KeyBinding::new("cmd-q", Quit, None),
            #[cfg(not(target_os = "macos"))]
            KeyBinding::new("alt-f4", Quit, None),
        ]);

        cx.on_action(|_: &Quit, cx: &mut gpui::App| cx.quit());

        let window_options = WindowOptions {
            window_bounds: Some(WindowBounds::centered(size(px(880.), px(620.)), cx)),
            ..TitleBar::window_options()
        };

        gpui_kit::open_window(window_options, cx, |window, cx| {
            window.activate_window();
            window.set_window_title("Stint Deep Work");

            Theme::change(ThemeMode::Dark, Some(window), cx);

            cx.new(|_cx| RootView)
        })
        .expect("failed to open window");
    });
}

struct RootView;

impl Render for RootView {
    fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
        v_flex()
            .w_full()
            .h_full()
            .child(
                h_flex()
                    .items_center()
                    .p_4()
                    .child("Stint Deep Work — bootstrapping")
                    .text_sm(),
            )
    }
}
