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

// Status is a work unit's lifecycle state in the legacy coordinator model.
type Status string

const (
	StatusQueued     Status = "queued"
	StatusActive     Status = "active"
	StatusVerified   Status = "verified" // legacy terminal state; not Objective C acceptance
	StatusIncomplete Status = "incomplete"
	StatusBlocked    Status = "blocked"
	StatusNeedsHuman Status = "needs_human"
	StatusDropped    Status = "dropped"
)

// Terminal reports the legacy coordinator scheduling rule: terminal statuses
// are not selected for execution again. Objective C must preserve this behavior
// for legacy missions; the new contract must model acceptance and execution
// eligibility separately instead of inferring either from this predicate.
// Blocked and needs_human tasks stay parked; they surface in the handoff.
func (s Status) Terminal() bool {
	switch s {
	case StatusVerified, StatusBlocked, StatusNeedsHuman, StatusDropped:
		return true
	}
	return false
}

func (s Status) String() string { return string(s) }

// RepositoryChangeExpectation declares whether a new-contract Work Unit is
// required, permitted, or forbidden to change the Git-visible product tree.
// The comparison is against the Work Unit's durable first-executor baseline,
// not against each individual attempt.
type RepositoryChangeExpectation string

const (
	RepositoryChangeRequired  RepositoryChangeExpectation = "required"
	RepositoryChangeOptional  RepositoryChangeExpectation = "optional"
	RepositoryChangeForbidden RepositoryChangeExpectation = "forbidden"
)

// AcceptanceOutcome is the deterministic decision for the current Work Unit
// contract evaluation. It is separate from both StatusVerified and the
// verifier's typed outcome.
type AcceptanceOutcome string

const (
	AcceptanceNotEvaluated AcceptanceOutcome = "not_evaluated"
	AcceptanceAccepted     AcceptanceOutcome = "accepted"
	AcceptanceNotSatisfied AcceptanceOutcome = "not_satisfied"
	AcceptanceUnresolved   AcceptanceOutcome = "unresolved"
)

type AcceptanceCheckOutcome string

const (
	AcceptanceCheckStarted      AcceptanceCheckOutcome = "started"
	AcceptanceCheckPassed       AcceptanceCheckOutcome = "passed"
	AcceptanceCheckFailed       AcceptanceCheckOutcome = "failed"
	AcceptanceCheckTimedOut     AcceptanceCheckOutcome = "timed_out"
	AcceptanceCheckExecutionErr AcceptanceCheckOutcome = "execution_error"
	AcceptanceCheckCanceled     AcceptanceCheckOutcome = "canceled"
	AcceptanceCheckUnknown      AcceptanceCheckOutcome = "unknown"
)

// Task is the current compatibility model for one bounded Deep Work work unit
// (called an Objective in product discussions). A work unit may involve many
// actions; it is not an atomic action. Current mission IDs come from the
// mission, with coordinator-added entries identified by Source. Verify is a
// deterministic evidence command for this work unit: when set, the coordinator
// runs it instead of the mission-level ## Verification command after each
// attempt. Passing it does not by itself establish Objective C acceptance.
type Task struct {
	// ID is the stable Objective / Work Unit identity used by the current run
	// records. A future action layer can add finer attribution without changing it.
	ID        string `json:"id"`
	Objective string `json:"objective"`
	// Acceptance is narrative intent included in the executor prompt. It is not
	// a deterministic acceptance outcome or evidence record.
	Acceptance string `json:"acceptance,omitempty"`
	Verify     string `json:"verify,omitempty"`
	// AcceptanceCheck is deterministic, Objective-specific evidence. It is
	// intentionally distinct from Verify, which checks general repository
	// health. These fields are effective only under an explicit mission
	// acceptance contract version.
	RepositoryChange RepositoryChangeExpectation `json:"repositoryChange,omitempty"`
	AcceptanceCheck  string                      `json:"acceptanceCheck,omitempty"`
	// These fields are the current projection of append-only AcceptanceRun
	// facts. The journal remains authoritative and may contain older checkpoint
	// and acceptance generations for this same Work Unit.
	AcceptanceOutcome           AcceptanceOutcome      `json:"acceptanceOutcome,omitempty"`
	AcceptanceRunID             string                 `json:"acceptanceRunId,omitempty"`
	AcceptanceCheckOutcome      AcceptanceCheckOutcome `json:"acceptanceCheckOutcome,omitempty"`
	AcceptanceReason            string                 `json:"acceptanceReason,omitempty"`
	AcceptanceSubject           *VerificationSubject   `json:"acceptanceSubject,omitempty"`
	AcceptanceCheckpointEventID string                 `json:"acceptanceCheckpointEventId,omitempty"`
	AcceptanceCheckpointCommit  string                 `json:"acceptanceCheckpointCommit,omitempty"`
	AcceptanceCheckpointTreeSHA string                 `json:"acceptanceCheckpointTreeSha,omitempty"`
	AcceptanceOutput            string                 `json:"acceptanceOutput,omitempty"`
	Reasoning                   string                 `json:"reasoning,omitempty"`
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
	ExecutionError      string              `json:"executionError,omitempty"`
	VerificationCommand string              `json:"verificationCommand,omitempty"`
	VerificationOutcome VerificationOutcome `json:"verificationOutcome,omitempty"`
	VerificationResult  string              `json:"verificationResult,omitempty"`
	VerificationOutput  string              `json:"verificationOutput,omitempty"`
	// VerificationRunID identifies the canonical verifier invocation in a
	// journaled run. It remains separate from task acceptance and checkpointing.
	VerificationRunID    string     `json:"verificationRunId,omitempty"`
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
	// recorded for this task's passed verification. Checkpoint identity is
	// separate from the later deterministic task acceptance contract. The
	// commit may be an existing worker commit when it already represents the
	// verified tree; no empty marker commit is needed.
	CheckpointCommit  string `json:"checkpointCommit,omitempty"`
	CheckpointTreeSHA string `json:"checkpointTreeSha,omitempty"`
	Source            string `json:"source,omitempty"`
}
