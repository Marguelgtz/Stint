package deep

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TaskCheckpoint is the durable link between a passed task verifier and the
// Git checkpoint that records the same product tree. It records operational
// evidence only; it does not define Objective C acceptance.
type TaskCheckpoint struct {
	// TaskID is the stable Objective / Work Unit identity, not an atomic action ID.
	TaskID              string              `json:"taskId"`
	Attempt             int                 `json:"attempt"`
	ExecutorRunID       string              `json:"executorRunId"`
	VerificationRunID   string              `json:"verificationRunId"`
	VerificationSubject VerificationSubject `json:"verificationSubject"`
	Commit              string              `json:"commit"`
	TreeSHA             string              `json:"treeSha"`
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
	id := taskCheckpointEventID(checkpoint.VerificationRunID)
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

func validateTaskCheckpoint(checkpoint TaskCheckpoint) error {
	if checkpoint.TaskID == "" || len(checkpoint.TaskID) > 128 || strings.ContainsAny(checkpoint.TaskID, "\x00\r\n") ||
		checkpoint.Attempt < 1 || checkpoint.Attempt > 1_000_000 ||
		checkpoint.ExecutorRunID == "" || len(checkpoint.ExecutorRunID) > 128 || strings.ContainsAny(checkpoint.ExecutorRunID, "\x00\r\n") ||
		checkpoint.VerificationRunID == "" || len(checkpoint.VerificationRunID) != 32 ||
		checkpoint.Commit == "" || len(checkpoint.Commit) > 128 || strings.ContainsAny(checkpoint.Commit, "\x00\r\n") ||
		checkpoint.TreeSHA == "" || len(checkpoint.TreeSHA) > 128 || strings.ContainsAny(checkpoint.TreeSHA, "\x00\r\n") ||
		checkpoint.VerificationSubject.HeadCommit == "" || len(checkpoint.VerificationSubject.HeadCommit) > 128 ||
		checkpoint.VerificationSubject.TreeSHA == "" || len(checkpoint.VerificationSubject.TreeSHA) > 128 ||
		checkpoint.TreeSHA != checkpoint.VerificationSubject.TreeSHA {
		return errors.New("task checkpoint identity or verified tree is invalid")
	}
	if _, err := hex.DecodeString(checkpoint.VerificationRunID); err != nil {
		return errors.New("task checkpoint verification identity is not hexadecimal")
	}
	return nil
}

func applyTaskCheckpoint(state *DeepState, checkpoint TaskCheckpoint, at time.Time) error {
	task, ok := findTask(state, checkpoint.TaskID)
	if !ok || task.Attempts != checkpoint.Attempt || task.Status != StatusActive ||
		task.ExecutorRunID != checkpoint.ExecutorRunID || task.VerificationRunID != checkpoint.VerificationRunID ||
		task.VerificationOutcome != VerificationPassed || task.VerificationSubject == nil ||
		*task.VerificationSubject != checkpoint.VerificationSubject {
		return errors.New("task checkpoint does not match a passed verifier for the active executor attempt")
	}
	task.Status = StatusVerified
	task.Blocker = ""
	task.CheckpointCommit = checkpoint.Commit
	task.CheckpointTreeSHA = checkpoint.TreeSHA
	verifiedAt := at.UTC()
	task.VerifiedAt = &verifiedAt
	task.ExecutorRunProcessed = true
	return nil
}

func taskCheckpointProjectionMatches(state DeepState, checkpoint TaskCheckpoint, at time.Time) bool {
	task, ok := findTask(&state, checkpoint.TaskID)
	return ok && task.Status == StatusVerified && task.Attempts == checkpoint.Attempt &&
		task.ExecutorRunID == checkpoint.ExecutorRunID && task.VerificationRunID == checkpoint.VerificationRunID &&
		task.VerificationOutcome == VerificationPassed && task.VerificationSubject != nil &&
		*task.VerificationSubject == checkpoint.VerificationSubject && task.CheckpointCommit == checkpoint.Commit &&
		task.CheckpointTreeSHA == checkpoint.TreeSHA && task.VerifiedAt != nil && task.VerifiedAt.Equal(at)
}

func validateTaskCheckpointProjection(state DeepState, events []RunEvent) error {
	latest := make(map[string]RunEvent)
	for _, event := range events {
		if event.Type == RunEventTaskCheckpointCreated && event.TaskCheckpoint != nil {
			latest[event.TaskCheckpoint.TaskID] = event
		}
	}
	for _, event := range latest {
		if event.TaskCheckpoint == nil || !taskCheckpointProjectionMatches(state, *event.TaskCheckpoint, event.OccurredAt) {
			return errors.New("deep.json task checkpoint contradicts canonical checkpoint history")
		}
	}
	return nil
}

func validateTaskCheckpointEventTransition(prior []RunEvent, event RunEvent) error {
	if event.Type != RunEventTaskCheckpointCreated {
		return nil
	}
	checkpoint := event.TaskCheckpoint
	if checkpoint == nil {
		return errors.New("task-checkpoint event has no checkpoint record")
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
	if verification == nil || verification.Purpose != VerificationPurposeTask || verification.Outcome != VerificationPassed ||
		verification.TaskID != checkpoint.TaskID || verification.Attempt != checkpoint.Attempt ||
		verification.Subject != checkpoint.VerificationSubject || verification.SubjectAfter == nil ||
		*verification.SubjectAfter != checkpoint.VerificationSubject {
		return errors.New("task checkpoint does not reference a passed verifier for its exact subject")
	}
	return nil
}
