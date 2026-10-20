use std::env;
use std::fs;
use std::path::{Component, Path, PathBuf};

use serde::Deserialize;

#[derive(Clone, Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DeepState {
    #[serde(default)]
    pub session_id: String,
    #[serde(default)]
    pub mission_name: String,
    #[serde(default)]
    pub objective: String,
    #[serde(default)]
    pub branch: String,
    #[serde(default)]
    pub phase: String,
    #[serde(default)]
    pub deadline: Option<String>,
    #[serde(default)]
    pub handoff_path: Option<String>,
    #[serde(default)]
    pub tasks: Vec<Task>,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Task {
    #[serde(default)]
    pub id: String,
    #[serde(default)]
    pub objective: String,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub attempts: u32,
}

pub fn load_latest() -> Result<DeepState, String> {
    let state_home = env::var_os("XDG_STATE_HOME")
        .map(PathBuf::from)
        .or_else(|| env::var_os("HOME").map(|home| PathBuf::from(home).join(".local/state")))
        .ok_or_else(|| "HOME and XDG_STATE_HOME are unset".to_string())?;
    let deep_dir = state_home.join("stint/deep");
    let latest_path = deep_dir.join("latest");
    let latest = fs::read_to_string(&latest_path)
        .map_err(|err| format!("reading {}: {err}", latest_path.display()))?;
    let session_id = latest.trim();

    if session_id.is_empty()
        || Path::new(session_id).components().count() != 1
        || !Path::new(session_id)
            .components()
            .all(|part| matches!(part, Component::Normal(_)))
    {
        return Err(format!("invalid session id in {}", latest_path.display()));
    }

    let state_path = deep_dir.join(session_id).join("deep.json");
    let content = fs::read_to_string(&state_path)
        .map_err(|err| format!("reading {}: {err}", state_path.display()))?;
    let state: DeepState = serde_json::from_str(&content)
        .map_err(|err| format!("parsing {}: {err}", state_path.display()))?;
    if state.session_id != session_id {
        return Err(format!("session id mismatch in {}", state_path.display()));
    }
    Ok(state)
}
