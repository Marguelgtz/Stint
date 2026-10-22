//! Data access for the GPUI app: local state loading, Stint CLI invocations,
//! and JSON-boundary models. The Go CLI remains the source of truth; this
//! layer only reads files, runs commands, and deserializes their output.
//!
//! Rendering code never calls these modules directly through re-exports of
//! internals — page code imports the specific module it uses.

pub mod dashboard;
pub mod deep_run;
pub mod interactive;
pub mod state;
