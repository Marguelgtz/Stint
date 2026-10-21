use std::process::Command;
use std::path::PathBuf;

use serde::{Deserialize, Serialize};

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Snapshot {
    #[serde(default)]
    pub active: bool,
    #[serde(default)]
    pub collected_at: String,
    #[serde(default)]
    pub session: Session,
    #[serde(default)]
    pub time: SessionTime,
    #[serde(default)]
    pub cost: Cost,
    #[serde(default)]
    pub health: Health,
    #[serde(default)]
    pub inference: Inference,
    #[serde(default)]
    pub staleness: Staleness,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Session {
    #[serde(default)]
    pub instance_id: i64,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub gpu_model: String,
    #[serde(default)]
    pub runtime: String,
    #[serde(default)]
    pub model: String,
    #[serde(default)]
    pub context_tokens: u32,
    #[serde(default)]
    pub checkpoint: String,
    #[serde(default)]
    pub last_error: String,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SessionTime {
    #[serde(default)]
    pub deadline: String,
    #[serde(default)]
    pub remaining_seconds: f64,
    #[serde(default)]
    pub expired: bool,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Cost {
    #[serde(default)]
    pub hourly_usd: f64,
    #[serde(default)]
    pub estimated_spent_usd: f64,
    #[serde(default)]
    pub scheduled_usd: f64,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Health {
    #[serde(default)]
    pub tunnel: ProcessHealth,
    #[serde(default)]
    pub watchdog: ProcessHealth,
    #[serde(default)]
    pub endpoint: EndpointHealth,
    #[serde(default)]
    pub runtime: RuntimeHealth,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ProcessHealth {
    #[serde(default)]
    pub running: bool,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct EndpointHealth {
    #[serde(default)]
    pub refreshed: bool,
    #[serde(default)]
    pub healthy: bool,
    #[serde(default)]
    pub latency_milliseconds: f64,
    #[serde(default)]
    pub meta: SampleMeta,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RuntimeHealth {
    #[serde(default)]
    pub refreshed: bool,
    #[serde(default)]
    pub ssh: bool,
    #[serde(default)]
    pub running: bool,
    #[serde(default)]
    pub meta: SampleMeta,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SampleMeta {
    #[serde(default)]
    pub error: String,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Inference {
    #[serde(default)]
    pub available: bool,
    #[serde(default)]
    pub processing: u32,
    #[serde(default)]
    pub agents: u32,
    #[serde(default)]
    pub decode_tokens_sec: f64,
    #[serde(default)]
    pub prefill_tokens_sec: f64,
    #[serde(default)]
    pub unavailable_reason: String,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Staleness {
    #[serde(default)]
    pub deadline_stale: bool,
    #[serde(default)]
    pub warning: String,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct StartPreview {
    pub hours: f64,
    pub session_max_cost_usd: f64,
    pub max_hourly_usd: f64,
    pub model: String,
    pub endpoint: String,
    pub fallback_attempts: usize,
    pub selected: StartCandidate,
    #[serde(default)]
    pub candidates: Vec<StartCandidate>,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct StartCandidate {
    pub offer_id: String,
    pub gpu_model: String,
    pub location: String,
    pub hourly_usd: f64,
    pub estimated_session_usd: f64,
    pub runtime: String,
    pub context_tokens: u32,
    pub within_cost_ceiling: bool,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DeadlinePreview {
    pub instance_id: i64,
    pub direction: String,
    pub delta_seconds: f64,
    pub current_remaining_seconds: f64,
    pub previous_deadline: String,
    pub new_deadline: String,
    pub current_scheduled_usd: f64,
    pub projected_usd: f64,
    pub exposure_delta_usd: f64,
    pub session_ceiling_usd: f64,
}

pub fn load_status(refresh: bool) -> Result<Snapshot, String> {
    let mut args = vec!["status", "--json"];
    if refresh {
        args.push("--refresh");
    }
    let output = run_stint(&args)?;
    serde_json::from_str(&output).map_err(|error| format!("parse Stint status JSON: {error}"))
}

pub fn preview_start(hours: f64) -> Result<StartPreview, String> {
    let output = run_stint(&[
        "start",
        "interactive",
        "--hours",
        &hours.to_string(),
        "--ui-preview-json",
    ])?;
    serde_json::from_str(&output).map_err(|error| format!("parse start preview JSON: {error}"))
}

pub fn start(preview: &StartPreview) -> Result<String, String> {
    let candidates = serde_json::to_string(&preview.candidates)
        .map_err(|error| format!("encode reviewed start candidates: {error}"))?;
    run_stint(&[
        "start",
        "interactive",
        "--hours",
        &preview.hours.to_string(),
        "--expected-offer-id",
        &preview.selected.offer_id,
        "--expected-hourly-usd",
        &preview.selected.hourly_usd.to_string(),
        "--expected-gpu-model",
        &preview.selected.gpu_model,
        "--expected-location",
        &preview.selected.location,
        "--expected-candidates-json",
        &candidates,
        "--yes",
    ])
}

pub fn preview_deadline(direction: &str, duration: &str) -> Result<DeadlinePreview, String> {
    let output = run_stint(&[direction, duration, "--preview-json"])?;
    serde_json::from_str(&output).map_err(|error| format!("parse deadline preview JSON: {error}"))
}

pub fn change_deadline(
    direction: &str,
    duration: &str,
    preview: &DeadlinePreview,
) -> Result<String, String> {
    run_stint(&[
        direction,
        duration,
        "--yes",
        "--expect-instance",
        &preview.instance_id.to_string(),
        "--expect-deadline",
        &preview.previous_deadline,
    ])
}

pub fn resume(instance_id: i64) -> Result<String, String> {
    run_stint(&["resume", "--expect-instance", &instance_id.to_string()])
}

pub fn destroy(instance_id: i64) -> Result<String, String> {
    run_stint(&[
        "down",
        "--yes",
        "--expect-instance",
        &instance_id.to_string(),
    ])
}

pub fn run_stint(args: &[&str]) -> Result<String, String> {
	let binary = std::env::var_os("STINT_BIN").unwrap_or_else(|| {
		let workspace_binary = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../stint");
		if workspace_binary.is_file() {
			workspace_binary.into_os_string()
		} else {
			"stint".into()
		}
	});
    let output = Command::new(binary)
        .args(args)
        .output()
        .map_err(|error| format!("run Stint command: {error}; set STINT_BIN to the stint executable"))?;
    let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
    if output.status.success() {
        Ok(stdout)
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        Err(if stderr.is_empty() {
            format!("Stint command exited with {}", output.status)
        } else {
            stderr
        })
    }
}
