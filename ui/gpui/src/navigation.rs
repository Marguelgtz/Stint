//! Navigation destinations shared across the app.
//!
//! The sidebar itself is rendered with the GPUI Kit `Sidebar` components
//! (see `RootView::sidebar` in `crate::app`); this module only owns the
//! destination definitions. Labels, icons, order, and active states live
//! with the sidebar rendering.

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Panel {
    DeepWork,
    DeepRun,
    Interactive,
    InteractiveDash,
    DeepBoard,
}
