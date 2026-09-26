package deep

import (
	"fmt"
	"os"
	"time"
)

// readAll is a seam over os.ReadFile so tests can supply mission content
// without touching the filesystem when the parser is exercised directly.
func readAll(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read mission %s: %w", path, err)
	}
	return string(data), nil
}

// Status is a task's lifecycle state.
type Status string

const (
	StatusQueued     Status = "queued"
	StatusActive     Status = "active"
	StatusVerified   Status = "verified"
	StatusIncomplete Status = "incomplete"
	StatusBlocked    Status = "blocked"
	StatusNeedsHuman Status = "needs_human"
	StatusDropped    Status = "dropped"
)

// Terminal reports whether a status will not be selected for execution again.
// Blocked and needs_human tasks stay parked; they surface in the handoff.
func (s Status) Terminal() bool {
	switch s {
	case StatusVerified, StatusBlocked, StatusNeedsHuman, StatusDropped:
		return true
	}
	return false
}

func (s Status) String() string { return string(s) }

// Task is one unit of Deep Work. IDs come from the mission (or from
// coordinator discovery, marked via Source). Verify is the task's own
// acceptance command (a per-task precision step over the mission-level
// command): when set, the coordinator runs it — instead of the mission's
// ## Verification command — after each attempt of this task.
type Task struct {
	ID         string `json:"id"`
	Objective  string `json:"objective"`
	Acceptance string `json:"acceptance,omitempty"`
	Verify     string `json:"verify,omitempty"`
	Reasoning  string `json:"reasoning,omitempty"`
	// DependsOn names earlier tasks that must be verified before this task can
	// run. This supports review tasks that inspect completed implementations.
	DependsOn  []string `json:"dependsOn,omitempty"`
	Status     Status   `json:"status"`
	Attempts   int      `json:"attempts"`
	Blocker    string   `json:"blocker,omitempty"`
	LastResult string   `json:"lastResult,omitempty"`
	// ExecutorRunID identifies the latest canonical executor invocation for
	// journaled runs. It is separate from verification and task acceptance.
	ExecutorRunID string `json:"executorRunId,omitempty"`
	// ExecutorRunProcessed records that the coordinator has applied the latest
	// executor result to this task's current verification/checkpoint cycle.
	ExecutorRunProcessed bool `json:"executorRunProcessed,omitempty"`
	// Attempt evidence is kept independently: a passing repository command is
	// diagnostic evidence, not proof that a failed executor completed the task.
	ExecutionError       string     `json:"executionError,omitempty"`
	VerificationCommand  string     `json:"verificationCommand,omitempty"`
	VerificationResult   string     `json:"verificationResult,omitempty"`
	VerificationOutput   string     `json:"verificationOutput,omitempty"`
	ConfiguredTimeoutSec int        `json:"configuredTimeoutSec,omitempty"`
	EffectiveTimeoutSec  int        `json:"effectiveTimeoutSec,omitempty"`
	TimeoutDecision      string     `json:"timeoutDecision,omitempty"`
	Findings             []string   `json:"findings,omitempty"`
	VerifiedAt           *time.Time `json:"verifiedAt,omitempty"`
	// VerificationSubject records the product Git state exercised by the
	// verifier. Stint-owned worktree bookkeeping is identified separately in
	// VerificationBookkeeping and is never folded into the product tree by
	// default.
	VerificationSubject     *VerificationSubject `json:"verificationSubject,omitempty"`
	VerificationBookkeeping map[string]string    `json:"verificationBookkeeping,omitempty"`
	// CheckpointCommit and CheckpointTreeSHA identify the repository state
	// accepted for this task. The commit may be an existing worker commit when
	// it already represents the verified tree; no empty marker commit is needed.
	CheckpointCommit  string `json:"checkpointCommit,omitempty"`
	CheckpointTreeSHA string `json:"checkpointTreeSha,omitempty"`
	Source            string `json:"source,omitempty"`
}
