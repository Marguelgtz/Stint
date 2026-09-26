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
	SessionID                      string               `json:"sessionId"`
	MissionName                    string               `json:"missionName"`
	Objective                      string               `json:"objective"`
	Success                        []string             `json:"success,omitempty"`
	Constraints                    []string             `json:"constraints,omitempty"`
	Verify                         string               `json:"verify,omitempty"`
	GitHub                         GitHubPolicy         `json:"github"`
	RepoPath                       string               `json:"repoPath"`
	WorktreePath                   string               `json:"worktreePath"`
	Branch                         string               `json:"branch"`
	BaseCommit                     string               `json:"baseCommit,omitempty"`
	Tasks                          []Task               `json:"tasks"`
	Phase                          Phase                `json:"phase"`
	Deadline                       time.Time            `json:"deadline"`
	LandBefore                     time.Time            `json:"landBefore"`
	ComputeBinding                 *ComputeBinding      `json:"computeBinding,omitempty"`
	LandedAt                       *time.Time           `json:"landedAt,omitempty"`
	HandoffPath                    string               `json:"handoffPath,omitempty"`
	LandingReason                  string               `json:"landingReason,omitempty"`
	LandingCommit                  string               `json:"landingCommit,omitempty"`
	LandingCheckpointTreeSHA       string               `json:"landingCheckpointTreeSha,omitempty"`
	LandingVerify                  string               `json:"landingVerify,omitempty"`
	LandingVerifyDone              bool                 `json:"landingVerifyDone,omitempty"`
	LandingVerificationSubject     *VerificationSubject `json:"landingVerificationSubject,omitempty"`
	LandingVerificationBookkeeping map[string]string    `json:"landingVerificationBookkeeping,omitempty"`
	LandingHandoff                 string               `json:"landingHandoff,omitempty"`
	// ExecutionQuiescenceUnconfirmed blocks verification/landing when an
	// executor may still have writers in the worktree. The active task owner
	// clears it only after that invocation has returned with quiescence known.
	ExecutionQuiescenceUnconfirmed bool            `json:"executionQuiescenceUnconfirmed,omitempty"`
	ExecutionQuiescenceTaskID      string          `json:"executionQuiescenceTaskId,omitempty"`
	PreviousLandings               []LandingRecord `json:"previousLandings,omitempty"`
	// MissionOutcome is separate from Phase: landed means the coordinator
	// reached a recoverable boundary, while this records deterministic mission
	// completion evidence. Empty legacy terminal values remain unknown.
	MissionOutcome MissionOutcome `json:"missionOutcome,omitempty"`
	// LandingVerificationOutcome preserves the typed mission verifier result
	// across a crash between final verification and terminal state persistence.
	LandingVerificationOutcome VerificationOutcome `json:"landingVerificationOutcome,omitempty"`
	TaskAttemptCap             int                 `json:"taskAttemptCap"`
	Exec                       *ExecSettings       `json:"exec,omitempty"`
	StartedAt                  time.Time           `json:"startedAt"`
	UpdatedAt                  time.Time           `json:"updatedAt,omitempty"`
}

// LandingRecord preserves the identity and terminal reason of an earlier
// landing when an operator deliberately resumes the same Deep Work session.
type LandingRecord struct {
	At                  time.Time            `json:"at"`
	Reason              string               `json:"reason"`
	Commit              string               `json:"commit,omitempty"`
	CheckpointTreeSHA   string               `json:"checkpointTreeSha,omitempty"`
	Verification        string               `json:"verification,omitempty"`
	MissionOutcome      MissionOutcome       `json:"missionOutcome,omitempty"`
	VerificationOutcome VerificationOutcome  `json:"verificationOutcome,omitempty"`
	VerificationSubject *VerificationSubject `json:"verificationSubject,omitempty"`
	HandoffSHA256       string               `json:"handoffSha256,omitempty"`
}

// ExecSettings are the per-session coding-agent invocation settings,
// persisted at start so `stint deep resume` can reconstruct the same
// invocations without a live endpoint or operator memory. Nil on sessions
// started before the field existed; resume falls back to current Hermes
// defaults. AllowedCommands is advisory policy included in the worker prompt;
// Hermes does not enforce it.
// Worker selects the execution target: "hermes" (Hermes plus file/shell work on the
// compute box through the launcher's SSH seam), or "hermes-onbox" (the
// coordinator and Hermes are co-located on the compute box with no SSH loopback).
type ExecSettings struct {
	Worker          string   `json:"worker,omitempty"`
	Provider        string   `json:"provider,omitempty"`
	Model           string   `json:"model,omitempty"`
	Reasoning       string   `json:"reasoning,omitempty"`
	ActionPlanPath  string   `json:"actionPlanPath,omitempty"`
	TaskTimeoutSec  int      `json:"taskTimeoutSec,omitempty"`
	AllowedCommands []string `json:"allowedCommands,omitempty"`
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
		GitHub:         mission.GitHub,
		RepoPath:       repoPath,
		WorktreePath:   worktreePath,
		Branch:         BranchName(sessionID),
		Tasks:          mission.Tasks,
		Phase:          PhaseExecuting,
		MissionOutcome: MissionOutcomePending,
		Deadline:       deadline.UTC(),
		LandBefore:     landBefore.UTC(),
		TaskAttemptCap: taskAttemptCap,
		StartedAt:      now.UTC(),
	}
}
