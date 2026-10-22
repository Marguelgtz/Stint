mod app;
mod components;
mod navigation;
mod services;

use gpui_kit::component::{Theme, ThemeMode, TitleBar};
use gpui_kit::prelude::*;
use gpui_kit::*;

use crate::app::RootView;

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
