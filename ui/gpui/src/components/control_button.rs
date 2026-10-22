use gpui_kit::prelude::*;
use gpui_kit::*;

/// A click control with a hex background color string.
pub fn control_button(
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
