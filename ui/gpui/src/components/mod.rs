//! Small reusable UI helpers that do not depend on `RootView` state.
//!
//! One file per genuinely reusable component; feature-specific markup stays
//! in `crate::app::features`.

pub mod control_button;
pub mod detail_field;
pub mod metric;
pub mod phase_badge;
pub mod status_pill;

pub use control_button::control_button;
pub use detail_field::detail_field;
pub use metric::metric;
pub use phase_badge::phase_badge;
pub use status_pill::status_pill;
