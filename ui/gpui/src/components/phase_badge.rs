use gpui_kit::prelude::*;
use gpui_kit::*;

/// Colored badge for a run/task phase name.
pub fn phase_badge(phase: &str) -> impl IntoElement {
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
