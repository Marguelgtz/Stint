use std::path::PathBuf;
use std::process::Command;

use serde::Deserialize;

use crate::services::{interactive, state::DeepState};

#[derive(Clone, Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MissionPreview {
    pub name: String,
    pub objective: String,
    #[serde(default)]
    pub tasks: Vec<MissionTask>,
    #[serde(default)]
    pub success: Vec<String>,
    #[serde(default)]
    pub constraints: Vec<String>,
    #[serde(default)]
    pub content: String,
}

#[derive(Clone, Debug, Deserialize)]
pub struct MissionTask {
    pub id: String,
    pub objective: String,
}

#[derive(Clone, Debug, Deserialize)]
pub struct RunRecord {
    pub state: DeepState,
    pub latest: bool,
    #[serde(default)]
    pub coordinator_alive: bool,
    #[serde(default)]
    pub incidents: Vec<Incident>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct Incident {
    pub time: String,
    pub kind: String,
    #[serde(default)]
    pub task: String,
    #[serde(default)]
    pub detail: String,
}

pub fn preview(mission: &str, repo: &str) -> Result<MissionPreview, String> {
    let output = interactive::run_stint(&["deep", "preview", "--mission", mission, "--repo", repo])?;
    serde_json::from_str(&output).map_err(|error| format!("parse mission preview: {error}"))
}

pub fn list() -> Result<Vec<RunRecord>, String> {
    let output = interactive::run_stint(&["deep", "list", "--json"])?;
    serde_json::from_str(&output).map_err(|error| format!("parse Deep Work history: {error}"))
}

pub fn start(mission: &str, repo: &str, hours: Option<f64>) -> Result<String, String> {
    let mut args = vec!["deep".to_string(), "start".into(), "--mission".into(), mission.into(), "--repo".into(), repo.into()];
    if let Some(hours) = hours { args.extend(["--hours".into(), hours.to_string()]); }
    run_owned(&args)
}

pub fn stop() -> Result<String, String> { interactive::run_stint(&["deep", "stop"]) }
pub fn resume(session_id: &str) -> Result<String, String> { interactive::run_stint(&["deep", "resume", "--session", session_id]) }

fn run_owned(args: &[String]) -> Result<String, String> {
    let binary = std::env::var_os("STINT_BIN").unwrap_or_else(|| {
        let workspace_binary = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../stint");
        if workspace_binary.is_file() { workspace_binary.into_os_string() } else { "stint".into() }
    });
    let output = Command::new(binary).args(args).output().map_err(|error| format!("run Stint command: {error}; set STINT_BIN to the stint executable"))?;
    let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
    if output.status.success() { Ok(stdout) } else {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        Err(if stderr.is_empty() { format!("Stint command exited with {}", output.status) } else { stderr })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const LIST_JSON: &str = r#"[
      {
        "state": {
          "sessionId": "20261010-120000",
          "missionName": "Synthetic smoke",
          "objective": "Verify list JSON shape",
          "branch": "stint/deep-20261010-120000",
          "phase": "executing",
          "deadline": "2026-10-10T14:00:00Z",
          "tasks": [
            {"id": "STINT-001", "objective": "Do thing A", "status": "verified", "attempts": 1},
            {"id": "STINT-003", "objective": "Do thing C", "status": "blocked", "attempts": 3, "blocker": "waiting on API"}
          ]
        },
        "latest": true,
        "coordinatorAlive": false,
        "incidents": [{"time": "2026-10-10T12:05:00Z", "kind": "taskFailed", "task": "STINT-002", "detail": "timeout"}]
      }
    ]"#;

    #[test]
    fn parses_list_records() {
        let records: Vec<RunRecord> = serde_json::from_str(LIST_JSON).expect("list JSON parses");
        assert_eq!(records.len(), 1);
        assert!(records[0].latest);
        assert_eq!(records[0].state.session_id, "20261010-120000");
        assert_eq!(records[0].state.phase, "executing");
        assert_eq!(records[0].state.tasks.len(), 2);
        assert_eq!(records[0].state.tasks[1].blocker, "waiting on API");
        assert_eq!(records[0].incidents.len(), 1);
        assert_eq!(records[0].incidents[0].kind, "taskFailed");
    }

    #[test]
    fn parses_mission_preview() {
        let json = r#"{"name":"M","objective":"O","success":["s1"],"constraints":["c1"],"tasks":[{"id":"STINT-001","objective":"T"}],"content":"raw"}"#;
        let preview: MissionPreview = serde_json::from_str(json).expect("preview JSON parses");
        assert_eq!(preview.name, "M");
        assert_eq!(preview.tasks.len(), 1);
        assert_eq!(preview.success, ["s1"]);
        assert_eq!(preview.content, "raw");
    }

    #[test]
    fn rejects_malformed_history() {
        assert!(serde_json::from_str::<Vec<RunRecord>>("{ not an array").is_err());
        assert!(serde_json::from_str::<Vec<RunRecord>>(r#"["nope"]"#).is_err());
    }
}
