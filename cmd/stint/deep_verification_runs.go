package main

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/Marguelgtz/Stint/internal/deep"
)

var errVerificationSubjectChanged = errors.New("repository changed before task verification began")

func (c *deepCoordinator) taskVerificationCommand(task *deep.Task) (string, string) {
	if task.Verify != "" {
		return task.Verify, "task"
	}
	if c.state.Verify != "" {
		return c.state.Verify, "mission"
	}
	return "", ""
}

func (c *deepCoordinator) verifyTaskAttempt(ctx context.Context, task *deep.Task, subject verificationSnapshot, journaled bool) (verificationResult, string, bool, error) {
	command, source := c.taskVerificationCommand(task)
	if command == "" {
		return verificationResult{Outcome: verificationNotRun}, "", false, nil
	}
	if !journaled {
		result, used := c.accept(ctx, task)
		return result, used, false, nil
	}
	current, err := c.git.verificationSubject(c.state.WorktreePath, c.verificationBookkeepingPaths())
	if err != nil {
		return verificationResult{}, command, false, fmt.Errorf("capture repository state immediately before task verification: %w", err)
	}
	if !sameVerificationSnapshot(subject, current) {
		return verificationResult{}, command, false, fmt.Errorf("%w: executor result subject %s/%s is now %s/%s",
			errVerificationSubjectChanged, subject.Subject.HeadCommit, subject.Subject.TreeSHA,
			current.Subject.HeadCommit, current.Subject.TreeSHA)
	}
	// If result persistence was interrupted after the verifier completed, use
	// that exact result to resume checkpointing instead of invoking it twice.
	if task.VerificationRunID != "" {
		previous, found, loadErr := deep.LoadVerificationRun(c.stateDir, c.state.SessionID, task.VerificationRunID)
		if loadErr != nil {
			return verificationResult{}, command, false, fmt.Errorf("load durable task verification result: %w", loadErr)
		}
		if found && previous.Outcome != deep.VerificationStarted && previous.Outcome != deep.VerificationUnknown &&
			previous.CommandSHA256 == deep.VerificationCommandIdentity(command) &&
			verificationRunMatchesSnapshot(previous, current) {
			output, outputErr := deep.ReadVerificationOutput(c.stateDir, c.state.SessionID, previous)
			if outputErr != nil {
				return verificationResult{}, command, false, fmt.Errorf("load durable task verification artifact: %w", outputErr)
			}
			return verificationResultFromRun(previous, command, output), command, false, nil
		}
		if found && previous.Outcome == deep.VerificationStarted {
			return verificationResult{}, command, false, fmt.Errorf("task %s has an unmatched verification invocation; recovery must establish process quiescence before retry", task.ID)
		}
		if found && previous.Outcome == deep.VerificationUnknown {
			return verificationResult{}, command, false, fmt.Errorf("task %s verification outcome or process quiescence is unresolved", task.ID)
		}
	}
	bound := c.verifyTimeout
	if bound <= 0 {
		bound = defaultTaskVerifyReserve
	}
	result, run, runErr := c.runJournaledVerification(ctx, deep.VerificationPurposeTask, task.ID, task.Attempts, source, command,
		current, bound, c.verify)
	return result, command, run.ID != "", runErr
}

func (c *deepCoordinator) verificationRuntime() deep.VerificationRuntime {
	worker := "unknown"
	if c.state.Exec != nil && c.state.Exec.Worker != "" {
		worker = c.state.Exec.Worker
	}
	location, protocol := "coordinator", "local-process-group-v1"
	if worker == workerHermes || worker == workerHermesOnBox {
		location = "compute"
	}
	if worker == workerHermes {
		protocol = "remote-process-group-v1"
	}
	return deep.VerificationRuntime{Worker: worker, Location: location, Shell: "sh", Protocol: protocol}
}

func (c *deepCoordinator) runJournaledVerification(
	ctx context.Context,
	purpose deep.VerificationPurpose,
	taskID string,
	attempt int,
	commandSource string,
	command string,
	before verificationSnapshot,
	bound time.Duration,
	invoke func(context.Context, string, string) verificationResult,
) (verificationResult, deep.VerificationRun, error) {
	if command == "" {
		return verificationResult{Outcome: verificationNotRun}, deep.VerificationRun{}, nil
	}
	if bound <= 0 {
		bound = 3 * time.Minute
	}
	startedAt := c.now().UTC()
	runID, err := deep.NewVerificationRunID()
	if err != nil {
		return verificationResult{}, deep.VerificationRun{}, err
	}
	runtime := c.verificationRuntime()
	bookkeepingBefore := cloneBookkeeping(landingVerificationBookkeeping(before.Bookkeeping))
	run := deep.VerificationRun{
		ID: runID, Purpose: purpose, TaskID: taskID, Attempt: attempt,
		CommandSource: commandSource, CommandSHA256: deep.VerificationCommandIdentity(command),
		Runtime: runtime, StartedAt: startedAt, TimeoutSeconds: int(bound.Seconds()),
		RemainingDeadlineSeconds: max(0, int(c.state.Deadline.Sub(startedAt).Seconds())),
		Subject:                  before.Subject, BookkeepingBefore: bookkeepingBefore,
	}
	run, err = deep.BeginVerificationRun(c.stateDir, c.state, run)
	if err != nil {
		return verificationResult{}, deep.VerificationRun{}, fmt.Errorf("persist verification start before command execution: %w", err)
	}
	vctx, cancel := context.WithTimeout(ctx, bound)
	result := invoke(vctx, command, c.state.WorktreePath)
	verifyContextErr := vctx.Err()
	cancel()
	if result.Outcome == verificationNotRun {
		result.Outcome = verificationExecutionErr
		result.Error = "verifier returned without an outcome"
	}
	if verifyContextErr != nil && result.Outcome != verificationInvalid {
		if errors.Is(verifyContextErr, context.DeadlineExceeded) {
			result.Outcome = verificationTimedOut
		} else {
			result.Outcome = verificationCanceled
		}
		result.Error = verifyContextErr.Error()
	}
	if result.Command == "" {
		result.Command = command
	}
	result.StartedAt = startedAt
	if result.CompletedAt.IsZero() || result.CompletedAt.Before(startedAt) || result.CompletedAt.Sub(startedAt) > bound+time.Minute {
		result.CompletedAt = c.now().UTC()
		if result.CompletedAt.Before(startedAt) {
			result.CompletedAt = startedAt
		}
	}
	if result.Outcome == verificationPassed && !result.HasExitCode {
		result.HasExitCode = true
		result.ExitCode = 0
	}
	result.Error = boundedExecutionFact(result.Error, 512)

	run.Outcome = result.Outcome
	run.EndedAt = result.CompletedAt.UTC()
	run.HasExitCode = result.HasExitCode
	run.ExitCode = result.ExitCode
	run.Error = result.Error
	run.QuiescenceUnconfirmed = result.QuiescenceUnconfirmed
	run.DurationMilliseconds = max(0, run.EndedAt.Sub(run.StartedAt).Milliseconds())
	result.Output = boundedVerifierOutput(result.Output)
	if result.QuiescenceUnconfirmed {
		run.SubjectAfterError = "post-verification subject unavailable because process quiescence is unconfirmed"
	} else {
		after, captureErr := c.git.verificationSubject(c.state.WorktreePath, c.verificationBookkeepingPaths())
		if captureErr != nil {
			run.SubjectAfterError = boundedExecutionFact(captureErr.Error(), 512)
		} else {
			run.SubjectAfter = &after.Subject
			run.BookkeepingAfter = cloneBookkeeping(landingVerificationBookkeeping(after.Bookkeeping))
		}
	}
	if ref, artifactErr := deep.PersistVerificationOutput(c.stateDir, c.state.SessionID, run.ID, []byte(result.Output)); artifactErr != nil {
		return result, run, artifactErr
	} else if ref != "" {
		run.ArtifactRefs = []string{ref}
	}

	// A concurrent stop can move task verification from executing into landing
	// while the verifier is still running. Refresh before appending its result
	// so the journal event follows the current phase and projection revision.
	fresh, err := deep.LoadState(c.stateDir, c.state.SessionID)
	if err != nil {
		return result, run, fmt.Errorf("refresh state before persisting verification result: %w", err)
	}
	*c.state = fresh
	if err := deep.CompleteVerificationRun(c.stateDir, c.state, run); err != nil {
		return result, run, fmt.Errorf("persist verification result: %w", err)
	}
	return result, run, nil
}

func boundedVerifierOutput(output string) string {
	if len(output) > 4096 {
		output = output[len(output)-4096:]
	}
	for !utf8.ValidString(output) && len(output) > 0 {
		output = output[1:]
	}
	return output
}

func cloneBookkeeping(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	copy := make(map[string]string, len(input))
	for key, value := range input {
		copy[key] = value
	}
	return copy
}

func verificationResultFromRun(run deep.VerificationRun, command, output string) verificationResult {
	return verificationResult{
		Command: command, Outcome: run.Outcome, ExitCode: run.ExitCode, HasExitCode: run.HasExitCode,
		StartedAt: run.StartedAt, CompletedAt: run.EndedAt, Output: output,
		Error: run.Error, QuiescenceUnconfirmed: run.QuiescenceUnconfirmed,
	}
}

func verificationRunMatchesSnapshot(run deep.VerificationRun, snapshot verificationSnapshot) bool {
	return run.Subject == snapshot.Subject && run.SubjectAfter != nil && *run.SubjectAfter == snapshot.Subject &&
		sameMetadata(run.BookkeepingBefore, landingVerificationBookkeeping(snapshot.Bookkeeping)) &&
		sameMetadata(run.BookkeepingAfter, landingVerificationBookkeeping(snapshot.Bookkeeping))
}
