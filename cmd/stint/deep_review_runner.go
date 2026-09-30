package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func (c *deepCoordinator) runSemanticReview(ctx context.Context, taskID string) error {
	reviewTotalStarted := time.Now()
	defer func() { c.recordTiming("objective_review.total", taskID, 0, reviewTotalStarted) }()
	task, ok := findCoordinatorTask(c.state.Tasks, taskID)
	if !ok || !pendingSemanticReview(*task, *c.state) {
		return fmt.Errorf("task %s has no pending semantic review", taskID)
	}
	checkpoint, checkpointEventID, found, err := deep.LoadTaskCheckpoint(c.stateDir, c.state.SessionID, taskID)
	if err != nil {
		return fmt.Errorf("load semantic review checkpoint for task %s: %w", taskID, err)
	}
	if !found || checkpointEventID != task.AcceptanceCheckpointEventID || checkpoint.Commit != task.AcceptanceCheckpointCommit ||
		checkpoint.TreeSHA != task.AcceptanceCheckpointTreeSHA || checkpoint.TaskID != task.ID || checkpoint.Attempt != task.Attempts {
		return fmt.Errorf("task %s accepted evidence does not identify its latest durable checkpoint", taskID)
	}
	acceptance, found, err := deep.LoadAcceptanceRun(c.stateDir, c.state.SessionID, task.AcceptanceRunID)
	if err != nil {
		return fmt.Errorf("load deterministic acceptance record for task %s: %w", taskID, err)
	}
	if !found || acceptance.Decision != deep.AcceptanceAccepted || acceptance.CheckOutcome != deep.AcceptanceCheckPassed ||
		acceptance.CheckpointEventID != checkpointEventID || acceptance.Checkpoint.Commit != checkpoint.Commit ||
		acceptance.Checkpoint.TreeSHA != checkpoint.TreeSHA {
		return fmt.Errorf("task %s has no canonical accepted decision for the checkpoint selected for semantic review", taskID)
	}
	preflightTimeout := c.effectiveReviewTimeout(c.now())
	preflightCtx := ctx
	cancelPreflight := func() {}
	if preflightTimeout > 0 {
		preflightCtx, cancelPreflight = context.WithTimeout(ctx, preflightTimeout)
	}
	defer cancelPreflight()

	baseline, hasBaseline, baselineErr := deep.LoadTaskRepositoryBaseline(c.stateDir, c.state.SessionID, taskID)
	var preflightErr error
	if preflightTimeout <= 0 {
		preflightErr = errors.New("insufficient safe time remains for semantic review")
	} else if baselineErr != nil {
		preflightErr = fmt.Errorf("load Objective repository baseline: %w", baselineErr)
	} else if !hasBaseline {
		preflightErr = errors.New("Objective repository baseline is unavailable in canonical executor history")
	}
	var prompt, contextSHA string
	if preflightErr == nil && preflightCtx.Err() != nil {
		preflightErr = preflightCtx.Err()
	}
	if preflightErr == nil {
		before, captureErr := c.captureVerificationSubject("objective_review.pre_subject_capture", c.state.WorktreePath, c.verificationBookkeepingPaths(), taskID, task.Attempts)
		if captureErr != nil {
			preflightErr = fmt.Errorf("capture repository subject before semantic review: %w", captureErr)
		} else if before.Subject.HeadCommit != checkpoint.Commit || before.Subject.TreeSHA != checkpoint.TreeSHA {
			preflightErr = fmt.Errorf("current product subject %s/%s differs from accepted checkpoint %s/%s",
				before.Subject.HeadCommit, before.Subject.TreeSHA, checkpoint.Commit, checkpoint.TreeSHA)
		}
	}
	if preflightErr == nil && preflightCtx.Err() != nil {
		preflightErr = preflightCtx.Err()
	}
	if preflightErr == nil {
		var diff string
		var oversized bool
		diff, oversized, err = c.git.reviewDiff(preflightCtx, c.state.WorktreePath, baseline.TreeSHA, checkpoint.TreeSHA, semanticReviewDiffLimit)
		if err != nil {
			preflightErr = fmt.Errorf("capture bounded Objective diff: %w", err)
		} else if oversized {
			preflightErr = fmt.Errorf("Objective diff exceeds the %d-byte semantic review limit", semanticReviewDiffLimit)
		} else {
			prompt, contextSHA, preflightErr = semanticReviewPrompt(*c.state, *task, baseline, checkpoint, diff)
		}
	}
	if preflightErr == nil && preflightCtx.Err() != nil {
		preflightErr = preflightCtx.Err()
	}
	if contextSHA == "" {
		contextHash := sha256.Sum256([]byte(fmt.Sprintf("review-context-unavailable:%s:%s:%s", taskID, checkpoint.TreeSHA, safeReviewReason(preflightErr))))
		contextSHA = hex.EncodeToString(contextHash[:])
	}
	if preflightErr == nil {
		after, captureErr := c.captureVerificationSubject("objective_review.post_subject_capture", c.state.WorktreePath, c.verificationBookkeepingPaths(), taskID, task.Attempts)
		if captureErr != nil {
			preflightErr = fmt.Errorf("re-capture repository subject before semantic review: %w", captureErr)
		} else if after.Subject.HeadCommit != checkpoint.Commit || after.Subject.TreeSHA != checkpoint.TreeSHA {
			preflightErr = fmt.Errorf("product subject changed while preparing semantic review (checkpoint %s/%s, current %s/%s)",
				checkpoint.Commit, checkpoint.TreeSHA, after.Subject.HeadCommit, after.Subject.TreeSHA)
		}
	}
	if preflightErr == nil && preflightCtx.Err() != nil {
		preflightErr = preflightCtx.Err()
	}

	id, err := deep.NewReviewCycleID()
	if err != nil {
		return err
	}
	reasoning := c.execCfg.reasoning
	if task.Reasoning != "" {
		reasoning = task.Reasoning
	}
	provider := resolveHermesProvider(c.execCfg.provider, reasoning)
	startedAt := c.now().UTC()
	timeout := c.effectiveReviewTimeout(startedAt)
	remainingDeadline := c.state.LandBefore.Sub(startedAt)
	if remainingDeadline < 0 {
		remainingDeadline = 0
	}
	cycle := deep.ReviewCycle{
		ID: id, TaskID: taskID, Attempt: checkpoint.Attempt,
		PolicySHA256: c.state.SemanticReviewContractSHA256, ContextSHA256: contextSHA,
		CheckpointEventID: checkpointEventID, Checkpoint: checkpoint,
		Reviewer: "hermes-safe-no-tools-v1", Provider: provider, Model: c.execCfg.model,
		StartedAt: startedAt, TimeoutMilliseconds: timeout.Milliseconds(),
		RemainingDeadlineMilliseconds: remainingDeadline.Milliseconds(),
	}
	cycle, err = deep.BeginReviewCycle(c.stateDir, c.state, cycle)
	if err != nil {
		return fmt.Errorf("persist semantic review start for task %s: %w", taskID, err)
	}
	c.incident(deep.IncidentExecutorInvoke, taskID, fmt.Sprintf("semantic review %s of checkpoint %s/%s", cycle.ID, checkpoint.Commit, checkpoint.TreeSHA))

	if preflightErr != nil {
		cycle.Outcome = deep.ReviewOutcomeUnresolved
		cycle.Reason = safeReviewReason(preflightErr)
		cycle.EndedAt = c.now().UTC()
		if err := deep.CompleteReviewCycle(c.stateDir, c.state, cycle); err != nil {
			return fmt.Errorf("persist unresolved semantic review for task %s: %w", taskID, err)
		}
		c.logf("task %s semantic review unresolved: %s", taskID, cycle.Reason)
		return nil
	}

	if timeout <= 0 {
		cycle.Outcome = deep.ReviewOutcomeUnresolved
		cycle.Reason = "insufficient safe time remains for semantic review"
		cycle.EndedAt = c.now().UTC()
		if err := deep.CompleteReviewCycle(c.stateDir, c.state, cycle); err != nil {
			return fmt.Errorf("persist deferred semantic review for task %s: %w", taskID, err)
		}
		return nil
	}
	reviewCtx, cancelReview := context.WithTimeout(ctx, timeout)
	defer cancelReview()
	in := c.execCfg
	in.prompt = prompt
	in.timeout = timeout
	if deadline, ok := reviewCtx.Deadline(); ok {
		in.timeout = minDuration(in.timeout, time.Until(deadline))
	}
	if in.timeout <= 0 {
		cycle.Outcome = deep.ReviewOutcomeUnresolved
		cycle.Reason = "insufficient safe time remains for semantic review"
		cycle.EndedAt = c.now().UTC()
		if err := deep.CompleteReviewCycle(c.stateDir, c.state, cycle); err != nil {
			return fmt.Errorf("persist deferred semantic review for task %s: %w", taskID, err)
		}
		return nil
	}
	in.semanticReviewer = true
	in.allowedCommands = nil
	in.actionPlan = ""
	in.executorRunID = ""
	in.stateDir = ""
	in.sessionID = ""
	if c.state.Exec != nil && c.state.Exec.Worker == workerHermes {
		in.workdir = "/tmp"
	} else {
		reviewWorkdir, err := os.MkdirTemp("", "stint-semantic-review-")
		if err != nil {
			cycle.Outcome = deep.ReviewOutcomeExecutionError
			cycle.Reason = safeReviewReason(fmt.Errorf("create isolated semantic review directory: %w", err))
			cycle.EndedAt = c.now().UTC()
			if persistErr := deep.CompleteReviewCycle(c.stateDir, c.state, cycle); persistErr != nil {
				return fmt.Errorf("persist semantic review setup failure: %w", persistErr)
			}
			return nil
		}
		defer os.RemoveAll(reviewWorkdir)
		in.workdir = reviewWorkdir
	}
	in.provider = provider
	in.model = c.execCfg.model
	in.reasoning = reasoning
	reviewStarted := time.Now()
	result, executionErr := c.executor.run(reviewCtx, in)
	c.recordTimingWithPacket("objective_review.invocation", taskID, task.Attempts, reviewStarted, len(prompt))
	if result.timedOut && executionErr == nil {
		executionErr = context.DeadlineExceeded
	}
	cycle.EndedAt = c.now().UTC()
	if cycle.EndedAt.Before(cycle.StartedAt) {
		cycle.EndedAt = cycle.StartedAt
	}
	cycle.DurationMilliseconds = cycle.EndedAt.Sub(cycle.StartedAt).Milliseconds()
	switch {
	case errors.Is(executionErr, errExecutorQuiescenceUnconfirmed):
		cycle.Outcome = deep.ReviewOutcomeUnknown
		cycle.QuiescenceUnconfirmed = true
		cycle.Reason = "review invocation may still be active; process quiescence could not be confirmed"
	case result.timedOut || errors.Is(executionErr, context.DeadlineExceeded):
		cycle.Outcome = deep.ReviewOutcomeTimedOut
		cycle.Reason = "semantic review exceeded its configured time limit"
	case executionErr != nil:
		cycle.Outcome = deep.ReviewOutcomeExecutionError
		cycle.Reason = safeReviewReason(executionErr)
	case !result.completed || result.exitCode != 0:
		cycle.Outcome = deep.ReviewOutcomeExecutionError
		cycle.Reason = fmt.Sprintf("semantic reviewer exited without a successful result (exit %d)", result.exitCode)
	default:
		outcome, reason, findings, parseErr := parseSemanticReviewResponse(result.outputText)
		if parseErr != nil {
			cycle.Outcome = deep.ReviewOutcomeUnresolved
			cycle.Reason = "semantic reviewer output did not match the strict structured-result protocol"
		} else {
			cycle.Outcome, cycle.Reason, cycle.Findings = outcome, reason, findings
		}
	}
	if !cycle.QuiescenceUnconfirmed {
		after, captureErr := c.captureVerificationSubject("objective_review.result_subject_capture", c.state.WorktreePath, c.verificationBookkeepingPaths(), taskID, task.Attempts)
		if captureErr != nil || after.Subject.HeadCommit != checkpoint.Commit || after.Subject.TreeSHA != checkpoint.TreeSHA {
			cycle.Outcome = deep.ReviewOutcomeUnresolved
			cycle.QuiescenceUnconfirmed = false
			cycle.Reason = "product repository subject changed or became unavailable during semantic review"
		}
	}
	if err := deep.ValidateReviewCycleResult(cycle); err != nil {
		cycle.Outcome = deep.ReviewOutcomeUnresolved
		cycle.Reason = "semantic reviewer result did not satisfy the bounded structured-evidence contract"
		cycle.QuiescenceUnconfirmed = false
		cycle.Findings = nil
	}
	if err := deep.CompleteReviewCycle(c.stateDir, c.state, cycle); err != nil {
		return fmt.Errorf("persist semantic review result for task %s: %w", taskID, err)
	}
	c.incident(deep.IncidentVerifyRun, taskID, fmt.Sprintf("semantic review %s outcome=%s findings=%d", cycle.ID, cycle.Outcome, len(cycle.Findings)))
	c.logf("task %s semantic review %s (%d findings)", taskID, cycle.Outcome, len(cycle.Findings))
	return nil
}

func findCoordinatorTask(tasks []deep.Task, id string) (*deep.Task, bool) {
	for i := range tasks {
		if tasks[i].ID == id {
			return &tasks[i], true
		}
	}
	return nil, false
}

func safeReviewReason(err error) string {
	if err == nil {
		return "semantic review could not establish a usable evidence packet"
	}
	value := strings.Map(func(r rune) rune {
		if r == 0 || r == '\r' || r == '\n' || r == '\t' {
			return ' '
		}
		return r
	}, err.Error())
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 480 {
		for len(value) > 480 {
			_, size := utf8.DecodeLastRuneInString(value)
			value = value[:len(value)-size]
		}
	}
	if value == "" {
		return "semantic review could not establish a usable evidence packet"
	}
	return value
}
