//! GPUI-side models and fetch for the two CLI dashboards.
//!
//! The Go CLI is the single source of truth for what data a dashboard shows:
//! `stint dashboard --json` and `stint deep dash --json` marshal the exact
//! model the TUI renders. This module only deserializes that JSON into Rust
//! structs so the GPUI app can render the same figures natively. No projection
//! logic is duplicated here.
//!
//! Every field is part of the JSON wire contract even when the UI does not
//! render it yet, so the module is allowed to carry dead fields.

// The structs below mirror the Go JSON contract; fields present in the wire
// format but not yet rendered in the UI are intentional, not dead code.
#![allow(dead_code)]

use serde::Deserialize;

/// Fetch the interactive compute/GPU dashboard snapshot from the CLI.
pub fn load_interactive() -> Result<InteractiveModel, String> {
    let output = crate::interactive::run_stint(&["dashboard", "--json"])?;
    serde_json::from_str(&output).map_err(|e| format!("parse dashboard JSON: {e}"))
}

/// Fetch the Deep Work dashboard snapshot from the CLI (local or on-box).
pub fn load_deep() -> Result<DeepModel, String> {
    let output = crate::interactive::run_stint(&["deep", "dash", "--json"])?;
    serde_json::from_str(&output).map_err(|e| format!("parse deep dashboard JSON: {e}"))
}

/// Convert nanoseconds (Go time.Duration encoding) to whole seconds.
pub fn ns_to_secs(ns: i64) -> i64 {
    ns / 1_000_000_000
}

// ---------------------------------------------------------------------------
// Interactive dashboard
// ---------------------------------------------------------------------------

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct InteractiveModel {
    #[serde(default)]
    pub no_session: bool,
    #[serde(default)]
    pub session: InteractiveSession,
    #[serde(default)]
    pub health: InteractiveHealth,
    #[serde(default)]
    pub gpu: InteractiveGpu,
    #[serde(default)]
    pub inference: InteractiveInference,
    #[serde(default)]
    pub perf: InteractivePerf,
    #[serde(default)]
    pub logs: Vec<String>,
    #[serde(default)]
    pub lane_events: Vec<String>,
    #[serde(default)]
    pub notice: String,
    #[serde(default)]
    pub error: String,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct InteractiveSession {
    #[serde(default)]
    pub instance_id: i64,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub model: String,
    #[serde(default)]
    pub gpu_model: String,
    #[serde(default)]
    pub runtime: String,
    #[serde(default)]
    pub profile: String,
    #[serde(default)]
    pub context: i32,
    #[serde(default)]
    pub rate: f64,
    #[serde(default)]
    pub spent: f64,
    #[serde(default)]
    pub exposure: f64,
    /// RFC3339; "0001-01-01T00:00:00Z" when unset.
    #[serde(default)]
    pub started: String,
    #[serde(default)]
    pub deadline: String,
    /// Go time.Duration in nanoseconds.
    #[serde(default)]
    pub elapsed: i64,
    #[serde(default)]
    pub remaining: i64,
    #[serde(default)]
    pub scheduled: i64,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct InteractiveHealth {
    #[serde(default)]
    pub endpoint: String,
    #[serde(default)]
    pub tunnel: String,
    #[serde(default)]
    pub runtime: String,
    #[serde(default)]
    pub watchdog: String,
    #[serde(default)]
    pub ssh: String,
    #[serde(default)]
    pub refreshed: bool,
    #[serde(default)]
    pub refreshing: bool,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct InteractiveGpu {
    #[serde(default)]
    pub available: bool,
    #[serde(default)]
    pub utilization: String,
    #[serde(default)]
    pub vram: String,
    #[serde(default)]
    pub power: String,
    #[serde(default)]
    pub temperature: String,
    #[serde(default)]
    pub error: String,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct InteractiveInference {
    #[serde(default)]
    pub available: bool,
    #[serde(default)]
    pub refreshed: bool,
    #[serde(default)]
    pub agents: i32,
    #[serde(default)]
    pub depth: i32,
    #[serde(default)]
    pub context_capacity: i32,
    #[serde(default)]
    pub decode: String,
    #[serde(default)]
    pub prefill: String,
    #[serde(default)]
    pub prefill_label: String,
    #[serde(default)]
    pub queue: String,
    #[serde(default)]
    pub cache_reuse: String,
    #[serde(default)]
    pub speculative: String,
    #[serde(default)]
    pub lanes: String,
    #[serde(default)]
    pub error: String,
    #[serde(default)]
    pub lane_rows: Vec<InteractiveLane>,
    #[serde(default)]
    pub stale: bool,
    #[serde(default)]
    pub sample_age: String,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct InteractiveLane {
    #[serde(default)]
    pub id: i32,
    #[serde(default)]
    pub depth: i32,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub cache: String,
    #[serde(default)]
    pub decode: String,
    #[serde(default)]
    pub decode_scope: String,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct InteractivePerf {
    #[serde(default)]
    pub available: bool,
    #[serde(default)]
    pub benchmarking: bool,
    #[serde(default)]
    pub ttft: String,
    #[serde(default)]
    pub total: String,
    #[serde(default)]
    pub decode: String,
    #[serde(default)]
    pub age: String,
    #[serde(default)]
    pub error: String,
    #[serde(default)]
    pub prompt_tokens: i32,
    #[serde(default)]
    pub completion_tokens: i32,
}

// ---------------------------------------------------------------------------
// Deep Work dashboard
// ---------------------------------------------------------------------------

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DeepModel {
    #[serde(default)]
    pub session_id: String,
    #[serde(default)]
    pub mission: String,
    #[serde(default)]
    pub phase: String,
    #[serde(default)]
    pub base_commit: String,
    #[serde(default)]
    pub branch: String,
    #[serde(default)]
    pub verify: String,
    #[serde(default)]
    pub compute_binding: String,
    #[serde(default)]
    pub landing_reason: String,
    #[serde(default)]
    pub landing_commit: String,
    #[serde(default)]
    pub landing_verify: String,
    #[serde(default)]
    pub landing_handoff: String,
    #[serde(default)]
    pub landing_verify_done: bool,
    /// RFC3339 or null.
    #[serde(default)]
    pub landed_at: Option<String>,
    #[serde(default)]
    pub latest: bool,
    #[serde(default)]
    pub can_land: bool,
    #[serde(default)]
    pub worker_name: String,
    #[serde(default)]
    pub coordinator: String,
    #[serde(default)]
    pub started_at: String,
    #[serde(default)]
    pub deadline: String,
    #[serde(default)]
    pub land_before: String,
    #[serde(default)]
    pub now: String,
    #[serde(default)]
    pub tasks: Vec<DeepTask>,
    #[serde(default)]
    pub events: Vec<DeepEvent>,
    #[serde(default)]
    pub compute: DeepCompute,
    #[serde(default)]
    pub worker: DeepWorker,
    #[serde(default)]
    pub error: String,
    #[serde(default)]
    pub notice: String,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DeepTask {
    #[serde(default)]
    pub id: String,
    #[serde(default)]
    pub objective: String,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub last_result: String,
    #[serde(default)]
    pub blocker: String,
    #[serde(default)]
    pub verify: String,
    #[serde(default)]
    pub attempts: i32,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DeepEvent {
    #[serde(default)]
    pub time: String,
    #[serde(default)]
    pub kind: String,
    #[serde(default)]
    pub task: String,
    #[serde(default)]
    pub detail: String,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DeepCompute {
    #[serde(default)]
    pub available: bool,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub endpoint: String,
    #[serde(default)]
    pub runtime: String,
    #[serde(default)]
    pub gpu: String,
    #[serde(default)]
    pub utilization: String,
    #[serde(default)]
    pub vram: String,
    #[serde(default)]
    pub lane: String,
    #[serde(default)]
    pub error: String,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DeepWorker {
    #[serde(default)]
    pub observed: bool,
    #[serde(default)]
    pub reachable: bool,
    #[serde(default)]
    pub n_infer_context: i32,
    #[serde(default)]
    pub n_infer_kv: i32,
    #[serde(default)]
    pub x_high_requests: i32,
    #[serde(default)]
    pub medium_requests: i32,
    #[serde(default)]
    pub scope: String,
    #[serde(default)]
    pub latest_phase: String,
    #[serde(default)]
    pub latest_at: String,
    #[serde(default)]
    pub compression: String,
    #[serde(default)]
    pub compression_at: String,
    #[serde(default)]
    pub compression_completed: i32,
    #[serde(default)]
    pub compression_failed: i32,
    #[serde(default)]
    pub truncated: i32,
    #[serde(default)]
    pub error: String,
    #[serde(default)]
    pub hermes_log: String,
    #[serde(default)]
    pub hermes_log_at: String,
}
