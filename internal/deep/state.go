package deep

import (
	"path/filepath"
	"time"
)

// Phase is the Deep Work session phase.
type Phase string

const (
	PhaseExecuting Phase = "executing"
	PhaseLanding   Phase = "landing"
	PhaseLanded    Phase = "landed"
	PhaseStopped   Phase = "stopped"
)

// DeepState is the durable truth of one Deep Work session. It lives in
// <stateDir>/deep/<sessionID>/deep.json and is written atomically (mode 0600)
// on every transition, following the session.json convention. All essential
// state is local: compute may die, this file and the git worktree do not.
type DeepState struct {
	SessionID      string     `json:"sessionId"`
	MissionName    string     `json:"missionName"`
	Objective      string     `json:"objective"`
	Success        []string   `json:"success,omitempty"`
	Constraints    []string   `json:"constraints,omitempty"`
	Verify         string     `json:"verify,omitempty"`
	RepoPath       string     `json:"repoPath"`
	WorktreePath   string     `json:"worktreePath"`
	Branch         string     `json:"branch"`
	BaseCommit     string     `json:"baseCommit,omitempty"`
	Tasks          []Task     `json:"tasks"`
	Phase          Phase      `json:"phase"`
	Deadline       time.Time  `json:"deadline"`
	LandBefore     time.Time  `json:"landBefore"`
	LandedAt       *time.Time `json:"landedAt,omitempty"`
	HandoffPath    string     `json:"handoffPath,omitempty"`
	TaskAttemptCap int        `json:"taskAttemptCap"`
	StartedAt      time.Time  `json:"startedAt"`
	UpdatedAt      time.Time  `json:"updatedAt,omitempty"`
}

// DeepDir is the state directory for one session.
func DeepDir(stateDir, sessionID string) string {
	return filepath.Join(stateDir, "deep", sessionID)
}

// LatestFile records the most recent session ID so status/stop can find it.
func LatestFile(stateDir string) string {
	return filepath.Join(stateDir, "deep", "latest")
}

// NewSessionID is stable enough for branches and directories:
// YYYYMMDD-HHMMSS.
func NewSessionID(now time.Time) string {
	return now.UTC().Format("20060102-150405")
}

// BranchName derives the Deep Work branch for a session.
func BranchName(sessionID string) string {
	return "stint/deep-" + sessionID
}

// NewState builds the initial state for a parsed mission.
func NewState(sessionID string, mission Mission, repoPath, worktreePath string, deadline time.Time, landBefore time.Time, taskAttemptCap int, now time.Time) DeepState {
	return DeepState{
		SessionID:      sessionID,
		MissionName:    mission.Name,
		Objective:      mission.Objective,
		Success:        mission.Success,
		Constraints:    mission.Constraints,
		Verify:         mission.Verify,
		RepoPath:       repoPath,
		WorktreePath:   worktreePath,
		Branch:         BranchName(sessionID),
		Tasks:          mission.Tasks,
		Phase:          PhaseExecuting,
		Deadline:       deadline.UTC(),
		LandBefore:     landBefore.UTC(),
		TaskAttemptCap: taskAttemptCap,
		StartedAt:      now.UTC(),
	}
}
