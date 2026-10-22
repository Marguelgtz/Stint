use gpui_kit::prelude::*;
use gpui_kit::*;

/// Small colored status pill; the caller picks the label and color.
pub fn status_pill(label: &str, color: Rgba) -> impl IntoElement {
    div()
        .px_2()
        .py_1()
        .rounded_md()
        .bg(color.opacity(0.16))
        .text_color(color)
        .text_sm()
        .child(label.to_string())
}
