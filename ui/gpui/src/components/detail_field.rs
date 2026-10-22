use gpui_kit::component::v_flex;
use gpui_kit::prelude::*;
use gpui_kit::*;

/// A two-line label/value field used throughout the panels.
pub fn detail_field(label: &str, value: String) -> impl IntoElement {
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
