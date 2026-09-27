package deep

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TaskCheckpoint records one exact product-tree checkpoint. Legacy and
// verified-basis records link a passed task verifier; version 2 missions with
// no generic verifier can instead checkpoint a successful executor result.
// The checkpoint remains operational evidence, not acceptance.
type TaskCheckpoint struct {
	// TaskID is the stable Objective / Work Unit identity, not an atomic action ID.
	TaskID              string              `json:"taskId"`
	Attempt             int                 `json:"attempt"`
	ExecutorRunID       string              `json:"executorRunId"`
	VerificationRunID   string              `json:"verificationRunId,omitempty"`
	Basis               TaskCheckpointBasis `json:"basis,omitempty"`
	VerificationSubject VerificationSubject `json:"verificationSubject"`
	Commit              string              `json:"commit"`
	TreeSHA             string              `json:"treeSha"`
}

type TaskCheckpointBasis string

const (
	TaskCheckpointBasisVerification TaskCheckpointBasis = "verification"
	TaskCheckpointBasisExecutor     TaskCheckpointBasis = "executor_result"
)

// TaskCheckpointSubject is the exact Git-visible identity acceptance checks
// exercise after the checkpoint operation. VerificationSubject on the record
// remains the pre-checkpoint subject that the executor/verifier exercised;
// Commit and TreeSHA identify the resulting checkpoint HEAD and tree.
func TaskCheckpointSubject(checkpoint TaskCheckpoint) VerificationSubject {
	return VerificationSubject{HeadCommit: checkpoint.Commit, TreeSHA: checkpoint.TreeSHA}
}

// RecordTaskCheckpoint appends the checkpoint fact after the Git operation
// has completed. A repeated call with the same durable fact is idempotent,
// including after the event was written but its projection update failed.
func RecordTaskCheckpoint(stateDir string, state *DeepState, checkpoint TaskCheckpoint, at time.Time) error {
	if state == nil || state.RunID == "" || state.ExecutionEpochID == "" || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return errors.New("task checkpoint requires a journaled run epoch")
	}
	if at.IsZero() {
		return errors.New("task checkpoint requires a timestamp")
	}
	if err := validateTaskCheckpoint(checkpoint); err != nil {
		return err
	}
	if checkpoint.Basis == TaskCheckpointBasisExecutor {
		task, ok := findTask(state, checkpoint.TaskID)
		if state.AcceptanceContractVersion != DeterministicAcceptanceContractVersion || state.Verify != "" || !ok ||
			!isAcceptanceContractTask(*task) || task.Verify != "" {
			return errors.New("executor-result checkpoint basis requires a version 2 Work Unit with no generic verifier")
		}
	}
	id := taskCheckpointRecordEventID(checkpoint)
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.EventID != id {
			continue
		}
		if event.Type != RunEventTaskCheckpointCreated || event.TaskCheckpoint == nil || *event.TaskCheckpoint != checkpoint {
			return errors.New("task checkpoint identity already has different journal facts")
		}
		fresh, err := LoadState(stateDir, state.SessionID)
		if err != nil {
			return fmt.Errorf("reload durable task checkpoint projection: %w", err)
		}
		*state = fresh
		return nil
	}
	projected := *state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if err := applyTaskCheckpoint(&projected, checkpoint, at); err != nil {
		return err
	}
	event := RunEvent{
		EventID: id, RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: at.UTC(), Actor: "deep-coordinator", Type: RunEventTaskCheckpointCreated,
		FromPhase: state.Phase, ToPhase: state.Phase, TaskCheckpoint: &checkpoint,
		TaskSummary: summarizeRunTasks(projected.Tasks),
	}
	return appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked)
}

func taskCheckpointEventID(verificationRunID string) string {
	return "task-checkpoint/" + verificationRunID + "/created"
}

func taskCheckpointRecordEventID(checkpoint TaskCheckpoint) string {
	if checkpoint.VerificationRunID != "" {
		return taskCheckpointEventID(checkpoint.VerificationRunID)
	}
	return "task-checkpoint/executor/" + checkpoint.ExecutorRunID + "/created"
}

func validateTaskCheckpoint(checkpoint TaskCheckpoint) error {
	if checkpoint.TaskID == "" || len(checkpoint.TaskID) > 128 || strings.ContainsAny(checkpoint.TaskID, "\x00\r\n") ||
		checkpoint.Attempt < 1 || checkpoint.Attempt > 1_000_000 ||
		checkpoint.ExecutorRunID == "" || len(checkpoint.ExecutorRunID) > 128 || strings.ContainsAny(checkpoint.ExecutorRunID, "\x00\r\n") ||
		checkpoint.Commit == "" || len(checkpoint.Commit) > 128 || strings.ContainsAny(checkpoint.Commit, "\x00\r\n") ||
		checkpoint.TreeSHA == "" || len(checkpoint.TreeSHA) > 128 || strings.ContainsAny(checkpoint.TreeSHA, "\x00\r\n") ||
		checkpoint.VerificationSubject.HeadCommit == "" || len(checkpoint.VerificationSubject.HeadCommit) > 128 ||
		checkpoint.VerificationSubject.TreeSHA == "" || len(checkpoint.VerificationSubject.TreeSHA) > 128 ||
		checkpoint.TreeSHA != checkpoint.VerificationSubject.TreeSHA {
		return errors.New("task checkpoint identity or verified tree is invalid")
	}
	switch checkpoint.Basis {
	case "", TaskCheckpointBasisVerification:
		if checkpoint.VerificationRunID == "" || len(checkpoint.VerificationRunID) != 32 {
			return errors.New("verification-basis task checkpoint requires a verifier identity")
		}
		if _, err := hex.DecodeString(checkpoint.VerificationRunID); err != nil {
			return errors.New("task checkpoint verification identity is not hexadecimal")
		}
	case TaskCheckpointBasisExecutor:
		if checkpoint.VerificationRunID != "" {
			return errors.New("executor-result task checkpoint cannot contain a verifier identity")
		}
	default:
		return errors.New("task checkpoint has an unknown evidence basis")
	}
	return nil
}

func applyTaskCheckpoint(state *DeepState, checkpoint TaskCheckpoint, at time.Time) error {
	task, ok := findTask(state, checkpoint.TaskID)
	if !ok || task.Attempts != checkpoint.Attempt || task.Status != StatusActive ||
		task.ExecutorRunID != checkpoint.ExecutorRunID || task.VerificationRunID != checkpoint.VerificationRunID ||
		task.VerificationSubject == nil || *task.VerificationSubject != checkpoint.VerificationSubject {
		return errors.New("task checkpoint does not match the active executor attempt and exact repository subject")
	}
	if checkpoint.Basis == TaskCheckpointBasisExecutor {
		if state.AcceptanceContractVersion != DeterministicAcceptanceContractVersion || state.Verify != "" || task.Verify != "" ||
			!isAcceptanceContractTask(*task) || task.VerificationOutcome != VerificationNotRun {
			return errors.New("executor-result checkpoint basis is not allowed with a generic verifier")
		}
		task.Status = StatusCheckpointed
		task.VerifiedAt = nil
	} else {
		if task.VerificationOutcome != VerificationPassed {
			return errors.New("verification-basis task checkpoint does not reference a passed verifier")
		}
		task.Status = StatusVerified
		verifiedAt := at.UTC()
		task.VerifiedAt = &verifiedAt
	}
	task.Blocker = ""
	task.CheckpointCommit = checkpoint.Commit
	task.CheckpointTreeSHA = checkpoint.TreeSHA
	task.ExecutorRunProcessed = true
	return nil
}

func taskCheckpointProjectionMatches(state DeepState, checkpoint TaskCheckpoint, at time.Time) bool {
	task, ok := findTask(&state, checkpoint.TaskID)
	if !ok {
		return false
	}
	if checkpoint.Basis == TaskCheckpointBasisExecutor &&
		(state.AcceptanceContractVersion != DeterministicAcceptanceContractVersion || state.Verify != "" || task.Verify != "" ||
			!isAcceptanceContractTask(*task)) {
		return false
	}
	wantStatus, wantVerifierOutcome := StatusVerified, VerificationPassed
	verifiedAtMatches := task.VerifiedAt != nil && task.VerifiedAt.Equal(at)
	if checkpoint.Basis == TaskCheckpointBasisExecutor {
		wantStatus, wantVerifierOutcome = StatusCheckpointed, VerificationNotRun
		verifiedAtMatches = task.VerifiedAt == nil
	}
	statusMatches := task.Status == wantStatus
	if !statusMatches && state.AcceptanceContractVersion == DeterministicAcceptanceContractVersion &&
		task.AcceptanceCheckpointEventID == taskCheckpointRecordEventID(checkpoint) &&
		task.AcceptanceCheckpointCommit == checkpoint.Commit && task.AcceptanceCheckpointTreeSHA == checkpoint.TreeSHA &&
		task.AcceptanceSubject != nil && *task.AcceptanceSubject == TaskCheckpointSubject(checkpoint) {
		switch task.AcceptanceOutcome {
		case AcceptanceAccepted:
			statusMatches = task.Status == StatusAccepted
		case AcceptanceNotSatisfied:
			statusMatches = task.Status == StatusIncomplete
		case AcceptanceUnresolved:
			statusMatches = task.Status == StatusNeedsHuman
		}
	}
	return statusMatches && task.Attempts == checkpoint.Attempt &&
		task.ExecutorRunID == checkpoint.ExecutorRunID && task.VerificationRunID == checkpoint.VerificationRunID &&
		task.VerificationOutcome == wantVerifierOutcome && task.VerificationSubject != nil &&
		*task.VerificationSubject == checkpoint.VerificationSubject && task.CheckpointCommit == checkpoint.Commit &&
		task.CheckpointTreeSHA == checkpoint.TreeSHA && verifiedAtMatches
}

func validateTaskCheckpointProjection(state DeepState, events []RunEvent) error {
	latestStart := make(map[string]uint64)
	latestCheckpoint := make(map[string]RunEvent)
	for _, event := range events {
		if event.Type == RunEventExecutorStarted && event.ExecutorRun != nil {
			latestStart[event.ExecutorRun.TaskID] = event.Sequence
		}
		if event.Type == RunEventTaskCheckpointCreated && event.TaskCheckpoint != nil {
			latestCheckpoint[event.TaskCheckpoint.TaskID] = event
		}
	}
	for taskID, event := range latestCheckpoint {
		// A later executor start supersedes the summary pointer to the prior
		// checkpoint, but the checkpoint remains in append-only history.
		if event.Sequence < latestStart[taskID] {
			continue
		}
		if event.TaskCheckpoint == nil || !taskCheckpointProjectionMatches(state, *event.TaskCheckpoint, event.OccurredAt) {
			return errors.New("deep.json task checkpoint contradicts canonical checkpoint history")
		}
	}
	return nil
}

// LoadTaskCheckpoint returns the latest durable checkpoint for a stable
// Objective / Work Unit identity. Callers still need to compare it with the
// current repository subject before reusing it as acceptance evidence.
func LoadTaskCheckpoint(stateDir, sessionID, taskID string) (TaskCheckpoint, string, bool, error) {
	events, err := ReadRunEvents(stateDir, sessionID)
	if err != nil {
		return TaskCheckpoint{}, "", false, err
	}
	var checkpoint TaskCheckpoint
	var eventID string
	found := false
	for _, event := range events {
		if event.Type == RunEventTaskCheckpointCreated && event.TaskCheckpoint != nil && event.TaskCheckpoint.TaskID == taskID {
			checkpoint, eventID, found = *event.TaskCheckpoint, event.EventID, true
		}
	}
	return checkpoint, eventID, found, nil
}

func validateTaskCheckpointEventTransition(prior []RunEvent, event RunEvent) error {
	if event.Type != RunEventTaskCheckpointCreated {
		return nil
	}
	checkpoint := event.TaskCheckpoint
	if checkpoint == nil {
		return errors.New("task-checkpoint event has no checkpoint record")
	}
	if event.EventID != taskCheckpointRecordEventID(*checkpoint) {
		return errors.New("task-checkpoint event identity does not match its checkpoint provenance")
	}
	var executor *ExecutorRun
	var verification *VerificationRun
	for _, old := range prior {
		if old.ExecutorRun != nil && (old.Type == RunEventExecutorResult || old.Type == RunEventExecutorReconciled) && old.ExecutorRun.ID == checkpoint.ExecutorRunID {
			value := *old.ExecutorRun
			executor = &value
		}
		if old.VerificationRun != nil && old.Type == RunEventVerificationResult && old.VerificationRun.ID == checkpoint.VerificationRunID {
			value := *old.VerificationRun
			verification = &value
		}
		if old.Type == RunEventTaskCheckpointCreated && old.TaskCheckpoint != nil &&
			old.TaskCheckpoint.TaskID == checkpoint.TaskID && old.TaskCheckpoint.Attempt == checkpoint.Attempt {
			return errors.New("task attempt already has a checkpoint event")
		}
	}
	if executor == nil || executor.Outcome != ExecutorOutcomeSucceeded || executor.TaskID != checkpoint.TaskID || executor.Attempt != checkpoint.Attempt {
		return errors.New("task checkpoint does not reference a successful executor result")
	}
	if checkpoint.Basis == TaskCheckpointBasisExecutor {
		if executor.RepositoryAfter == nil || executor.RepositoryAfterError != "" ||
			*executor.RepositoryAfter != checkpoint.VerificationSubject {
			return errors.New("executor-result checkpoint does not match the successful executor's exact repository subject")
		}
		return nil
	}
	if verification == nil || verification.Purpose != VerificationPurposeTask || verification.Outcome != VerificationPassed ||
		verification.TaskID != checkpoint.TaskID || verification.Attempt != checkpoint.Attempt ||
		verification.Subject != checkpoint.VerificationSubject || verification.SubjectAfter == nil ||
		*verification.SubjectAfter != checkpoint.VerificationSubject {
		return errors.New("task checkpoint does not reference a passed verifier for its exact subject")
	}
	return nil
}
