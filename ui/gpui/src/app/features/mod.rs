//! Page-specific rendering and handlers for the five navigation
//! destinations. Each module extends the shared `RootView` (defined in
//! `crate::app`) with the methods its destination needs; the central root
//! view keeps ownership of all shared state.

pub mod dashboards;
pub mod deep_run;
pub mod deep_work;
pub mod interactive;
