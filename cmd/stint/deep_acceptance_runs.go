package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

var errAcceptanceCheckpointChanged = errors.New("acceptance checkpoint is no longer the current repository subject")
var errAcceptanceWindowUnavailable = errors.New("insufficient landing window for the acceptance-check")

// runJournaledAcceptanceCheck evaluates one Objective-specific command only
// after the task checkpoint exists. The command is bound to that checkpoint,
// and the repository subject is captured again after the command is quiescent.
func (c *deepCoordinator) runJournaledAcceptanceCheck(ctx context.Context, taskID string, checkpoint deep.TaskCheckpoint, checkpointEventID string) error {
	acceptanceStarted := time.Now()
	defer func() { c.recordTiming("acceptance.total", taskID, 0, acceptanceStarted) }()
	task := c.taskByID(taskID)
	if task == nil {
		return fmt.Errorf("acceptance Work Unit %s disappeared", taskID)
	}
	if !task.IsAcceptanceContractTask() || c.state.AcceptanceContractVersion != deep.DeterministicAcceptanceContractVersion {
		return fmt.Errorf("task %s is not governed by the deterministic acceptance contract", taskID)
	}
	if strings.TrimSpace(task.AcceptanceCheck) == "" {
		return fmt.Errorf("task %s has no configured objective-specific acceptance-check", taskID)
	}
	if err := deep.ValidateVerifyCommand(task.AcceptanceCheck); err != nil {
		return fmt.Errorf("task %s acceptance-check command is invalid: %w", taskID, err)
	}

	before, err := c.captureVerificationSubject("acceptance.pre_subject_capture", c.state.WorktreePath, c.verificationBookkeepingPaths(), taskID, checkpoint.Attempt)
	if err != nil {
		return fmt.Errorf("capture repository subject before acceptance-check for task %s: %w", taskID, err)
	}
	checkpointSubject := deep.TaskCheckpointSubject(checkpoint)
	if before.Subject != checkpointSubject {
		return fmt.Errorf("%w: checkpoint %s/%s; current %s/%s", errAcceptanceCheckpointChanged,
			checkpointSubject.HeadCommit, checkpointSubject.TreeSHA,
			before.Subject.HeadCommit, before.Subject.TreeSHA)
	}
	baseline, found, err := deep.LoadTaskRepositoryBaseline(c.stateDir, c.state.SessionID, taskID)
	if err != nil {
		return fmt.Errorf("load first executor repository baseline for task %s: %w", taskID, err)
	}
	if !found {
		return fmt.Errorf("task %s has no durable first-executor repository baseline", taskID)
	}

	bound := c.effectiveAcceptanceTimeout(c.now())
	if bound <= 0 {
		return errAcceptanceWindowUnavailable
	}
	now := c.now().UTC()
	remaining := max(0, int(c.state.Deadline.Sub(now).Seconds()))
	runID, err := deep.NewAcceptanceRunID()
	if err != nil {
		return err
	}
	run := deep.AcceptanceRun{
		ID: runID, TaskID: taskID, Attempt: checkpoint.Attempt,
		ContractSHA256:   c.state.AcceptanceContractSHA256,
		CommandSHA256:    deep.AcceptanceCommandIdentity(task.AcceptanceCheck),
		RepositoryChange: task.RepositoryChange, RepositoryBaseline: baseline,
		SubjectBefore: checkpointSubject, CheckpointEventID: checkpointEventID,
		Checkpoint: checkpoint, Runtime: acceptanceRuntimeFromVerification(c.verificationRuntime()),
		StartedAt: now, TimeoutSeconds: int(bound.Seconds()), RemainingDeadlineSeconds: remaining,
	}
	run, err = deep.BeginAcceptanceRun(c.stateDir, c.state, run)
	if err != nil {
		return fmt.Errorf("persist acceptance-check start for task %s: %w", taskID, err)
	}

	vctx, cancel := context.WithTimeout(ctx, bound)
	result := verificationResult{Outcome: verificationExecutionErr, Error: "acceptance-check runner is unavailable"}
	acceptanceCommandStarted := time.Now()
	if c.verify != nil {
		result = c.verify(vctx, task.AcceptanceCheck, c.state.WorktreePath)
	}
	c.recordTiming("acceptance.command", taskID, checkpoint.Attempt, acceptanceCommandStarted)
	contextErr := vctx.Err()
	cancel()
	if contextErr != nil && result.Outcome != verificationInvalid {
		if errors.Is(contextErr, context.DeadlineExceeded) {
			result.Outcome = verificationTimedOut
		} else {
			result.Outcome = verificationCanceled
		}
		result.Error = contextErr.Error()
	}
	if result.Command == "" {
		result.Command = task.AcceptanceCheck
	} else if result.Command != task.AcceptanceCheck {
		result.Outcome = verificationExecutionErr
		result.HasExitCode = false
		result.ExitCode = 0
		result.Error = "acceptance-check runner reported a command different from the configured contract"
	}
	if result.CompletedAt.IsZero() || result.CompletedAt.Before(run.StartedAt) {
		result.CompletedAt = c.now().UTC()
	}
	if result.CompletedAt.Before(run.StartedAt) {
		result.CompletedAt = run.StartedAt
	}
	if result.Outcome == verificationPassed && !result.HasExitCode {
		result.HasExitCode, result.ExitCode = true, 0
	}
	run.CheckOutcome, run.HasExitCode, run.ExitCode, run.Error = acceptanceCheckFacts(result)
	run.EndedAt = result.CompletedAt.UTC()
	run.QuiescenceUnconfirmed = result.QuiescenceUnconfirmed
	run.DurationMilliseconds = max(0, run.EndedAt.Sub(run.StartedAt).Milliseconds())
	run.Error = boundedExecutionFact(run.Error, 512)
	if !result.QuiescenceUnconfirmed {
		after, captureErr := c.captureVerificationSubject("acceptance.post_subject_capture", c.state.WorktreePath, c.verificationBookkeepingPaths(), taskID, checkpoint.Attempt)
		if captureErr != nil {
			run.SubjectAfterError = boundedExecutionFact(captureErr.Error(), 512)
		} else {
			run.SubjectAfter = &after.Subject
		}
	} else {
		run.SubjectAfterError = "post-acceptance subject unavailable because process quiescence is unconfirmed"
	}
	output := boundedVerifierOutput(result.Output)
	if ref, persistErr := deep.PersistAcceptanceOutput(c.stateDir, c.state.SessionID, run.ID, []byte(output)); persistErr != nil {
		return fmt.Errorf("persist bounded acceptance-check output for task %s: %w", taskID, persistErr)
	} else if ref != "" {
		run.ArtifactRefs = []string{ref}
	}
	if run.CheckOutcome == deep.AcceptanceCheckFailed {
		run.Reason = fmt.Sprintf("objective-specific acceptance-check failed with exit code %d", run.ExitCode)
	} else if run.CheckOutcome != deep.AcceptanceCheckPassed {
		run.Reason = "objective-specific acceptance-check did not produce a conclusive pass: " + run.Error
	} else if run.SubjectAfterError != "" || run.SubjectAfter == nil || *run.SubjectAfter != checkpointSubject {
		run.Reason = "repository subject changed or became unavailable while the acceptance-check ran"
	} else if !acceptanceRepositoryChangeSatisfied(task.RepositoryChange, baseline, checkpointSubject) {
		run.Reason = fmt.Sprintf("acceptance-check passed, but repository-change expectation %q was not satisfied", task.RepositoryChange)
	}

	fresh, err := deep.LoadState(c.stateDir, c.state.SessionID)
	if err != nil {
		return fmt.Errorf("refresh state before persisting acceptance-check result: %w", err)
	}
	*c.state = fresh
	if err := deep.CompleteAcceptanceRun(c.stateDir, c.state, run); err != nil {
		return fmt.Errorf("persist acceptance-check result for task %s: %w", taskID, err)
	}
	if result.QuiescenceUnconfirmed {
		return fmt.Errorf("task %s acceptance-check process quiescence is unconfirmed; further execution is stopped", taskID)
	}
	return nil
}

func (c *deepCoordinator) effectiveAcceptanceTimeout(now time.Time) time.Duration {
	maximum := c.verifyTimeout
	if maximum <= 0 {
		maximum = defaultTaskVerifyReserve
	}
	usable := c.state.LandBefore.Sub(now) - coordinatorReserve
	if usable <= 0 {
		return 0
	}
	return minDuration(maximum, usable)
}

// pendingAcceptanceCheckpoint reports whether a v2 checkpoint can be checked
// without another executor attempt. A stale checkpoint falls back to normal
// execution only while the Work Unit still has retry budget.
func (c *deepCoordinator) pendingAcceptanceCheckpoint(task deep.Task) (deep.TaskCheckpoint, string, bool, error) {
	if c.state.AcceptanceContractVersion != deep.DeterministicAcceptanceContractVersion || !task.IsAcceptanceContractTask() ||
		(task.Status != deep.StatusVerified && task.Status != deep.StatusCheckpointed) ||
		(task.AcceptanceOutcome != "" && task.AcceptanceOutcome != deep.AcceptanceNotEvaluated) {
		return deep.TaskCheckpoint{}, "", false, nil
	}
	checkpoint, eventID, found, err := deep.LoadTaskCheckpoint(c.stateDir, c.state.SessionID, task.ID)
	if err != nil {
		return deep.TaskCheckpoint{}, "", false, err
	}
	if !found || checkpoint.Attempt != task.Attempts || checkpoint.Commit != task.CheckpointCommit ||
		checkpoint.TreeSHA != task.CheckpointTreeSHA {
		return deep.TaskCheckpoint{}, "", false, nil
	}
	current, err := c.captureVerificationSubject("acceptance.pending_checkpoint_subject_capture", c.state.WorktreePath, c.verificationBookkeepingPaths(), task.ID, task.Attempts)
	if err != nil {
		return deep.TaskCheckpoint{}, "", false, fmt.Errorf("capture current repository subject for task %s acceptance: %w", task.ID, err)
	}
	if current.Subject != deep.TaskCheckpointSubject(checkpoint) {
		return deep.TaskCheckpoint{}, "", false, nil
	}
	return checkpoint, eventID, true, nil
}

func (c *deepCoordinator) taskByID(taskID string) *deep.Task {
	for i := range c.state.Tasks {
		if c.state.Tasks[i].ID == taskID {
			return &c.state.Tasks[i]
		}
	}
	return nil
}

func acceptanceRuntimeFromVerification(runtime deep.VerificationRuntime) deep.AcceptanceRuntime {
	return deep.AcceptanceRuntime{
		Worker: runtime.Worker, Location: runtime.Location, Shell: runtime.Shell,
		Protocol: runtime.Protocol, ComputeProvider: runtime.ComputeProvider,
		ComputeInstance: runtime.ComputeInstance,
	}
}

func acceptanceCheckFacts(result verificationResult) (deep.AcceptanceCheckOutcome, bool, int, string) {
	switch result.Outcome {
	case verificationPassed:
		if result.QuiescenceUnconfirmed {
			return deep.AcceptanceCheckExecutionErr, false, 0, "acceptance-check returned a pass without confirmed process quiescence"
		}
		if result.HasExitCode && result.ExitCode != 0 {
			return deep.AcceptanceCheckExecutionErr, false, 0, "acceptance-check reported pass with a nonzero exit code"
		}
		return deep.AcceptanceCheckPassed, true, 0, ""
	case verificationFailed:
		if result.HasExitCode && result.ExitCode != 0 && !result.QuiescenceUnconfirmed {
			return deep.AcceptanceCheckFailed, true, result.ExitCode, result.Error
		}
		return deep.AcceptanceCheckExecutionErr, false, 0, "acceptance-check failure did not include a conclusive nonzero exit code"
	case verificationTimedOut:
		errorText := result.Error
		if errorText == "" {
			errorText = "acceptance-check timed out"
		}
		return deep.AcceptanceCheckTimedOut, false, 0, errorText
	case verificationCanceled:
		errorText := result.Error
		if errorText == "" {
			errorText = "acceptance-check canceled"
		}
		return deep.AcceptanceCheckCanceled, false, 0, errorText
	default:
		errorText := result.Error
		if errorText == "" {
			errorText = result.Summary()
		}
		return deep.AcceptanceCheckExecutionErr, false, 0, errorText
	}
}

func acceptanceRepositoryChangeSatisfied(expectation deep.RepositoryChangeExpectation, baseline, checkpoint deep.VerificationSubject) bool {
	changed := baseline.TreeSHA != checkpoint.TreeSHA
	switch expectation {
	case deep.RepositoryChangeRequired:
		return changed
	case deep.RepositoryChangeOptional:
		return true
	case deep.RepositoryChangeForbidden:
		return !changed
	default:
		return false
	}
}
