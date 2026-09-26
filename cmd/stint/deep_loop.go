package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

const (
	defaultTaskVerifyReserve = 3 * time.Minute
	coordinatorReserve       = 30 * time.Second
	minimumUsefulTaskWindow  = 5 * time.Minute
)

// deepCoordinator is the Slice-1 Deep Work loop: select a task, invoke the
// coding-agent executor in the isolated worktree, decide acceptance from
// repository evidence, persist state, repeat until landing. The coordinator
// is a plain foreground process: the machine must stay awake (D-3), and the
// existing compute watchdog remains the hard-deadline authority.
type deepCoordinator struct {
	stateDir    string
	state       *deep.DeepState
	execCfg     execInput // session-wide invocation settings (workdir/prompt set per task)
	executor    executor
	now         func() time.Time
	taskTimeout time.Duration
	// verifyTimeout bounds one verification command run by the coordinator;
	// a hung verify must not stall the loop (zero = the 3 m built-in bound).
	verifyTimeout time.Duration
	// verify runs one verification command in the worktree; the coordinator
	// decides which command (the task's own, else the mission's) to hand it.
	verify func(ctx context.Context, command, workdir string) verificationResult
	// finalVerify runs the mission-level command at landing; it may differ
	// from verify when the worktree is remote (on the compute box). Nil
	// skips the landing check.
	finalVerify func(ctx context.Context, command string) verificationResult
	// worktreeWrite writes a file into the session worktree. It differs
	// when the worktree is remote (on the compute box): the landing
	// handoff write must reach the box, not the operator's machine. Nil
	// uses the local os.WriteFile.
	worktreeWrite func(path string, data []byte) error
	logf          func(format string, args ...any)
	out           io.Writer
	git           gitOps
	persist       func(string, deep.DeepState) error
	// completeLanding is the durable landing-transition seam. Production uses
	// the RunEvent-backed operation; tests inject the crash boundary after Git
	// has created the checkpoint but before its lifecycle event is recorded.
	completeLanding func(string, *deep.DeepState, string, string, time.Time) error
}

// execInputFor builds the per-task invocation from the session-wide config.
func (c *deepCoordinator) execInputFor(t deep.Task, timeout time.Duration) execInput {
	in := c.execCfg
	in.workdir = c.state.WorktreePath
	in.timeout = timeout
	mission := c.mission()
	in.prompt = deep.BuildTaskPromptWithActionPlan(mission, t, t.Attempts, c.repoSummary(), c.execCfg.actionPlan)
	if t.Reasoning != "" {
		in.reasoning = t.Reasoning
	}
	// The session's command policy is part of the reconstructed context: the
	// worker must know exactly which commands it may run and what will
	// happen to the rest.
	if sec := deep.CommandPolicySection(in.allowedCommands); sec != "" {
		in.prompt += sec
	}
	return in
}

func (c *deepCoordinator) mission() deep.Mission {
	return deep.Mission{
		Name:        c.state.MissionName,
		Objective:   c.state.Objective,
		Success:     c.state.Success,
		Constraints: c.state.Constraints,
	}
}

func (c *deepCoordinator) save() error {
	persist := c.persist
	if persist == nil {
		persist = func(dir string, state deep.DeepState) error { return state.SaveDir(dir) }
	}
	if err := persist(c.stateDir, *c.state); err != nil {
		c.logf("WARNING: persist state: %v", err)
		c.incident(deep.IncidentStateSave, "", err.Error())
		return err
	}
	return nil
}

// incident records one safety-relevant event in the session's incident log
// (and mirrors it to coordinator.log). Best effort: auditing must never fail
// the run.
func (c *deepCoordinator) incident(kind, taskID, detail string) {
	deep.AppendIncident(c.stateDir, *c.state, kind, taskID, detail)
	c.logf("incident %s %s: %s", kind, taskID, detail)
}

// policySummary is the compact command-policy description used in incidents.
func policySummary(in execInput) string {
	if len(in.allowedCommands) == 0 {
		return "allow=<none> (Hermes command execution is not restricted by Stint)"
	}
	return fmt.Sprintf("reasoning=%s advisory-allow=[%s]", in.reasoning, strings.Join(in.allowedCommands, ", "))
}

// stillExecuting re-reads the durable phase. An external `stint deep stop`
// (or a recovered coordinator) may have landed the session while this
// process was mid-task; persisting the in-memory state would resurrect it,
// so every save is gated on this check.
func (c *deepCoordinator) stillExecuting() (bool, error) {
	fresh, err := deep.LoadState(c.stateDir, c.state.SessionID)
	if err != nil {
		return false, err
	}
	return fresh.Phase == deep.PhaseExecuting, nil
}

// afterInvocationState refreshes a concurrently landed session. When the
// operator requested a stop during this task, the landing path leaves a
// durable quiescence flag; this invocation's owner may clear it only after
// the executor returned with its process group known to be quiescent.
func (c *deepCoordinator) afterInvocationState(taskID string) (bool, error) {
	fresh, err := deep.LoadState(c.stateDir, c.state.SessionID)
	if err != nil {
		return false, err
	}
	if fresh.Phase == deep.PhaseExecuting {
		return true, nil
	}
	*c.state = fresh
	changed := false
	for i := range c.state.Tasks {
		if c.state.Tasks[i].ID == taskID && c.state.Tasks[i].Status == deep.StatusActive {
			c.state.Tasks[i].Status = deep.StatusIncomplete
			c.state.Tasks[i].Blocker = "stopped mid-task (" + c.state.LandingReason + ")"
			changed = true
		}
	}
	if fresh.ExecutionQuiescenceUnconfirmed && fresh.ExecutionQuiescenceTaskID == taskID {
		c.state.ExecutionQuiescenceUnconfirmed = false
		c.state.ExecutionQuiescenceTaskID = ""
		changed = true
	}
	if changed {
		if err := c.save(); err != nil {
			return false, fmt.Errorf("persist executor quiescence after external landing: %w", err)
		}
	}
	return false, nil
}

func (c *deepCoordinator) persistUnquiescedExecutor(taskID string, result execResult, execErr error) error {
	fresh, err := deep.LoadState(c.stateDir, c.state.SessionID)
	if err != nil {
		return fmt.Errorf("read durable state after unconfirmed executor quiescence: %w", err)
	}
	*c.state = fresh
	for i := range c.state.Tasks {
		if c.state.Tasks[i].ID != taskID {
			continue
		}
		task := &c.state.Tasks[i]
		task.Status = deep.StatusNeedsHuman
		task.Blocker = "executor process quiescence is unconfirmed; verification and further work are stopped"
		task.ExecutionError = execErr.Error()
		task.LastResult = result.summary() + " | executor error: " + execErr.Error()
		task.VerificationCommand = ""
		task.VerificationResult = verificationResult{Outcome: verificationExecutionErr, Error: "not run because executor writers may still be active"}.Summary()
		task.VerificationOutput = ""
		task.VerificationSubject = nil
		task.VerificationBookkeeping = nil
		task.CheckpointCommit = ""
		task.CheckpointTreeSHA = ""
		task.VerifiedAt = nil
		c.state.ExecutionQuiescenceUnconfirmed = true
		c.state.ExecutionQuiescenceTaskID = taskID
		if err := c.save(); err != nil {
			return fmt.Errorf("persist unconfirmed executor quiescence for task %s: %w", taskID, err)
		}
		return fmt.Errorf("task %s cannot be verified because %w", taskID, execErr)
	}
	return fmt.Errorf("task %s disappeared from durable state after execution", taskID)
}

func (c *deepCoordinator) persistUnquiescedVerifier(taskID, command string, result verificationResult) error {
	fresh, err := deep.LoadState(c.stateDir, c.state.SessionID)
	if err != nil {
		return fmt.Errorf("read durable state after unconfirmed verifier quiescence: %w", err)
	}
	*c.state = fresh
	for i := range c.state.Tasks {
		if c.state.Tasks[i].ID != taskID {
			continue
		}
		task := &c.state.Tasks[i]
		task.Status = deep.StatusNeedsHuman
		task.Blocker = "verifier process quiescence is unconfirmed; checkpointing and further work are stopped"
		task.VerificationCommand = command
		task.VerificationResult = result.Summary()
		task.VerificationOutput = strings.TrimSpace(tailLine(result.Output, 3))
		task.VerificationSubject = nil
		task.VerificationBookkeeping = nil
		task.CheckpointCommit = ""
		task.CheckpointTreeSHA = ""
		task.VerifiedAt = nil
		c.state.ExecutionQuiescenceUnconfirmed = true
		c.state.ExecutionQuiescenceTaskID = taskID
		if err := c.save(); err != nil {
			return fmt.Errorf("persist unconfirmed verifier quiescence for task %s: %w", taskID, err)
		}
		return fmt.Errorf("task %s cannot be checkpointed because verifier process quiescence is unconfirmed", taskID)
	}
	return fmt.Errorf("task %s disappeared from durable state after verification", taskID)
}

// repoSummary is the durable git truth folded into reconstructed task
// context so a fresh invocation can continue without conversational memory.
func (c *deepCoordinator) repoSummary() deep.RepoSummary {
	wt := c.state.WorktreePath
	head, _ := c.git.headCommit(wt)
	log, _ := c.git.logOneline(wt, 5)
	status, _ := c.git.statusShort(wt)
	s := deep.RepoSummary{Branch: c.state.Branch, HeadCommit: head, RecentLog: log, Changed: status}
	if c.state.BaseCommit != "" && head != "" && head != c.state.BaseCommit {
		if stat, err := c.git.diffStat(wt, c.state.BaseCommit); err == nil {
			s.DiffStat = stat
		}
	}
	return s
}

// runTask invokes the executor once for the task and transitions its state
// from repository evidence, never from the worker's word alone.
func (c *deepCoordinator) runTask(ctx context.Context, idx int, now time.Time) error {
	executing, err := c.stillExecuting()
	if err != nil {
		return fmt.Errorf("read durable Deep Work state before task: %w", err)
	}
	if !executing {
		return nil
	}
	t := &c.state.Tasks[idx]
	if prerequisite := c.failedPrerequisite(*t); prerequisite != "" {
		t.Status = deep.StatusBlocked
		t.Blocker = prerequisite
		t.LastResult = "not run: prerequisite was not verified"
		if err := c.save(); err != nil {
			return fmt.Errorf("persist blocked dependent task %s: %w", t.ID, err)
		}
		c.logf("task %s BLOCKED: %s", t.ID, prerequisite)
		return nil
	}
	effectiveTimeout, timeoutDecision := c.effectiveTaskTimeout(now, *t)
	if effectiveTimeout <= 0 {
		return fmt.Errorf("task %s has no useful executor window: %s", t.ID, timeoutDecision)
	}
	t.Status = deep.StatusActive
	// A previous timeout-window deferral is no longer the current blocker once
	// a new invocation starts. If this attempt fails at the attempt cap, its
	// own executor/verification evidence must determine the final blocker.
	t.Blocker = ""
	t.Attempts++
	t.ConfiguredTimeoutSec = int(c.taskTimeout.Seconds())
	t.EffectiveTimeoutSec = int(effectiveTimeout.Seconds())
	t.TimeoutDecision = timeoutDecision
	// Evidence from an earlier attempt must never be reused by a new
	// verification/checkpoint cycle.
	t.VerificationSubject = nil
	t.VerificationBookkeeping = nil
	t.CheckpointCommit = ""
	t.CheckpointTreeSHA = ""
	t.VerifiedAt = nil
	if err := c.save(); err != nil {
		return fmt.Errorf("persist active task %s before invoking Hermes: %w", t.ID, err)
	}
	c.logf("task %s attempt %d: invoking executor (configured maximum %s, effective timeout %s; %s)", t.ID, t.Attempts, c.taskTimeout, effectiveTimeout, timeoutDecision)

	tc, cancel := context.WithTimeout(ctx, effectiveTimeout)
	defer cancel()
	c.incident(deep.IncidentExecutorInvoke, t.ID,
		fmt.Sprintf("attempt %d %s (configured maximum %s; effective timeout %s; %s)", t.Attempts, policySummary(c.execCfg), c.taskTimeout, effectiveTimeout, timeoutDecision))
	res, execErr := c.executor.run(tc, c.execInputFor(*t, effectiveTimeout))
	if execErr != nil {
		c.logf("task %s: executor error: %v", t.ID, execErr)
		c.incident(deep.IncidentExecutorError, t.ID, execErr.Error())
	}
	c.logf("task %s attempt %d result: %s", t.ID, t.Attempts, res.summary())
	if errors.Is(execErr, errExecutorQuiescenceUnconfirmed) {
		return c.persistUnquiescedExecutor(t.ID, res, execErr)
	}
	continuing, err := c.afterInvocationState(t.ID)
	if err != nil {
		return fmt.Errorf("refresh durable state after task %s executor: %w", t.ID, err)
	}
	if !continuing {
		c.logf("task %s: executor quiesced after an external landing request; verification is deferred to resumed landing", t.ID)
		return nil
	}

	// Capture the exact Git-visible worktree state after the executor returns
	// and immediately before verification. Stint bookkeeping that is not
	// already tracked or staged is fingerprinted separately from the product
	// tree by the Git backend.
	subject, subjectErr := c.git.verificationSubject(c.state.WorktreePath, c.verificationBookkeepingPaths())
	if subjectErr != nil {
		c.logf("task %s: capture verification subject: %v", t.ID, subjectErr)
		c.incident(deep.IncidentCheckpointFail, t.ID, "could not capture verification subject: "+subjectErr.Error())
	} else {
		t.VerificationSubject = &subject.Subject
		t.VerificationBookkeeping = subject.Bookkeeping
	}
	continuing, err = c.afterInvocationState(t.ID)
	if err != nil {
		return fmt.Errorf("refresh durable state before task %s verification: %w", t.ID, err)
	}
	if !continuing {
		c.logf("task %s: external landing began before verification; verification is deferred", t.ID)
		return nil
	}

	verifyResult, verifyCmd := c.accept(ctx, t)
	if verifyCmd != "" {
		c.incident(deep.IncidentVerifyRun, t.ID, verifyResult.IncidentDetail())
	}
	t.LastResult = res.summary()
	t.ExecutionError = ""
	if execErr != nil {
		t.ExecutionError = execErr.Error()
		t.LastResult += " | executor error: " + execErr.Error()
	}
	t.VerificationCommand = verifyCmd
	t.VerificationOutput = strings.TrimSpace(tailLine(verifyResult.Output, 3))
	t.VerificationResult = verifyResult.Summary()
	if verifyResult.QuiescenceUnconfirmed {
		return c.persistUnquiescedVerifier(t.ID, verifyCmd, verifyResult)
	}

	executionSucceeded := execErr == nil && res.completed && res.exitCode == 0

	switch {
	case executionSucceeded && verifyResult.Passed():
		if subjectErr != nil {
			t.Status = deep.StatusNeedsHuman
			t.Blocker = "verification passed, but the exact repository subject could not be captured: " + subjectErr.Error()
			if saveErr := c.save(); saveErr != nil {
				return fmt.Errorf("persist missing verification subject for task %s: %w", t.ID, saveErr)
			}
			return fmt.Errorf("verification subject unavailable for task %s: %w", t.ID, subjectErr)
		}
		currentSubject, err := c.git.verificationSubject(c.state.WorktreePath, c.verificationBookkeepingPaths())
		if err == nil && !sameVerificationSnapshot(subject, currentSubject) {
			err = fmt.Errorf("repository changed after verification; verified subject %s/%s no longer matches %s/%s", subject.Subject.HeadCommit, subject.Subject.TreeSHA, currentSubject.Subject.HeadCommit, currentSubject.Subject.TreeSHA)
		}
		if err != nil {
			t.Status = deep.StatusNeedsHuman
			t.Blocker = "verification evidence was invalidated before checkpoint creation: " + err.Error()
			c.incident(deep.IncidentCheckpointFail, t.ID, t.Blocker)
			if saveErr := c.save(); saveErr != nil {
				return fmt.Errorf("persist invalidated verification for task %s: %w", t.ID, saveErr)
			}
			return fmt.Errorf("verification subject changed for task %s: %w", t.ID, err)
		}
		head, tree, err := c.git.checkpointSubject(c.state.WorktreePath, taskCheckpointMessage(*t), subject)
		if err != nil {
			c.logf("checkpoint for %s failed: %v", t.ID, err)
			c.incident(deep.IncidentCheckpointFail, t.ID, "verified subject could not be checkpointed exactly: "+err.Error())
			t.Status = deep.StatusNeedsHuman
			t.Blocker = "verification passed, but an exact-state checkpoint could not be created: " + err.Error()
			if saveErr := c.save(); saveErr != nil {
				return fmt.Errorf("persist checkpoint failure for task %s: %w", t.ID, saveErr)
			}
			return fmt.Errorf("exact-state checkpoint failed for task %s: %w", t.ID, err)
		} else {
			head, tree = strings.TrimSpace(head), strings.TrimSpace(tree)
			if head == "" || tree == "" || tree != subject.Subject.TreeSHA {
				err := fmt.Errorf("checkpoint identity does not match verified tree")
				t.Status = deep.StatusNeedsHuman
				t.Blocker = "verification passed, but checkpoint identity does not match its subject"
				c.incident(deep.IncidentCheckpointFail, t.ID, err.Error())
				if saveErr := c.save(); saveErr != nil {
					return fmt.Errorf("persist invalid-checkpoint state for task %s: %w", t.ID, saveErr)
				}
				return fmt.Errorf("checkpoint identity unavailable for task %s", t.ID)
			}
			t.CheckpointCommit = head
			t.CheckpointTreeSHA = tree
			ts := c.now()
			t.Status = deep.StatusVerified
			t.VerifiedAt = &ts
			t.Blocker = ""
		}
		c.logf("task %s VERIFIED", t.ID)
	case verifyCmd == "" && executionSucceeded:
		t.Status = deep.StatusNeedsHuman
		t.Blocker = "worker reported completion, but no independent verification command is defined"
	case t.Attempts < c.state.TaskAttemptCap && c.hasUsefulTaskWindow(c.now(), *t):
		t.Status = deep.StatusIncomplete
		t.Blocker = ""
		c.logf("task %s INCOMPLETE (attempt %d/%d): will reconstruct context and continue",
			t.ID, t.Attempts, c.state.TaskAttemptCap)
	default:
		t.Status = deep.StatusBlocked
		if t.Blocker == "" {
			t.Blocker = blockReason(res, execErr, verifyResult)
		}
		c.logf("task %s BLOCKED: %s", t.ID, t.Blocker)
	}
	// The invocation and verify span the longest gap of the session: an
	// external stop or landing must win, or this process's save would
	// resurrect a stopped session from stale in-memory state.
	executing, err = c.afterInvocationState(t.ID)
	if err != nil {
		return fmt.Errorf("read durable Deep Work state after task %s: %w", t.ID, err)
	}
	if executing {
		if err := c.save(); err != nil {
			return fmt.Errorf("persist task %s outcome: %w", t.ID, err)
		}
	} else {
		c.logf("task %s: session stopped during the invocation; outcome not persisted", t.ID)
		c.incident(deep.IncidentExternalStop, t.ID, "outcome of the in-flight attempt was not persisted")
	}
	return nil
}

// accept checks repository evidence for the attempt: the task's own
// verification command when it defines one, else the mission-level command
// (a per-task command is the precision step for missions whose single
// command is broader than any one task's scope). With no command at all,
// the worker's completion is recorded but the handoff marks the result
// unverified. The verification run is bounded: a hung command must not
// stall the coordinator. It returns the command it used so the caller can
// record it in the incident log.
func (c *deepCoordinator) accept(ctx context.Context, t *deep.Task) (verificationResult, string) {
	command := t.Verify
	if command == "" {
		command = c.state.Verify
	}
	if command == "" {
		return verificationResult{Outcome: verificationNotRun}, ""
	}
	bound := c.verifyTimeout
	if bound <= 0 {
		bound = defaultTaskVerifyReserve
	}
	vctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	result := c.verify(vctx, command, c.state.WorktreePath)
	if vctx.Err() != nil && result.Outcome != verificationInvalid {
		if vctx.Err() == context.DeadlineExceeded {
			result.Outcome = verificationTimedOut
		} else {
			result.Outcome = verificationCanceled
		}
		result.Error = vctx.Err().Error()
		result.CompletedAt = time.Now().UTC()
	}
	return result, command
}

func blockReason(res execResult, execErr error, verification verificationResult) string {
	if execErr != nil {
		return "executor failed: " + execErr.Error()
	}
	if !res.completed || res.exitCode != 0 {
		return fmt.Sprintf("executor did not complete successfully (exit %d): %s", res.exitCode, tailLine(res.stderrTail, 2))
	}
	if !verification.Passed() {
		if verification.Error != "" {
			return verification.Summary()
		}
		if verification.Output != "" {
			return verification.Summary() + ": " + strings.TrimSpace(tailLine(verification.Output, 2))
		}
		return verification.Summary()
	}
	if res.exitCode != 0 {
		return fmt.Sprintf("invocation failed (exit %d): %s", res.exitCode, tailLine(res.stderrTail, 2))
	}
	return "invocation did not complete: " + res.finishReason
}

// effectiveTaskTimeout treats the configured timeout as a maximum. It reserves
// bounded time for task verification and coordinator checkpoint work before
// shortening the invocation to the remaining window.
func (c *deepCoordinator) effectiveTaskTimeout(now time.Time, task deep.Task) (time.Duration, string) {
	maximum := c.taskTimeout
	if maximum <= 0 {
		return 0, "refused: configured task timeout is not positive"
	}
	verifyReserve := time.Duration(0)
	if task.Verify != "" || c.state.Verify != "" {
		verifyReserve = c.verifyTimeout
		if verifyReserve <= 0 {
			verifyReserve = defaultTaskVerifyReserve
		}
	}
	remaining := c.state.LandBefore.Sub(now)
	usable := remaining - verifyReserve - coordinatorReserve
	minimum := minDuration(maximum, minimumUsefulTaskWindow)
	reserveLabel := "coordinator checkpoint work"
	if verifyReserve > 0 {
		reserveLabel = "verification and coordinator work"
	}
	if usable < minimum {
		return 0, fmt.Sprintf("deferred: %s remains before landing cutoff; %s is reserved for %s, leaving less than the %s minimum useful invocation window (configured maximum %s)", remaining.Round(time.Second), (verifyReserve + coordinatorReserve).Round(time.Second), reserveLabel, minimum.Round(time.Second), maximum.Round(time.Second))
	}
	if usable < maximum {
		return usable, fmt.Sprintf("shortened from configured maximum %s to preserve %s for %s", maximum.Round(time.Second), (verifyReserve + coordinatorReserve).Round(time.Second), reserveLabel)
	}
	return maximum, "started at configured maximum"
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func (c *deepCoordinator) failedPrerequisite(task deep.Task) string {
	for _, prerequisite := range task.DependsOn {
		status := deep.Status("")
		for _, candidate := range c.state.Tasks {
			if candidate.ID == prerequisite {
				status = candidate.Status
				break
			}
		}
		if status != deep.StatusVerified {
			statusLabel := string(status)
			if statusLabel == "" {
				statusLabel = "missing"
			}
			return fmt.Sprintf("not run: prerequisite %s has status %s; implementation review requires a verified implementation", prerequisite, statusLabel)
		}
	}
	return ""
}

func (c *deepCoordinator) hasUsefulTaskWindow(now time.Time, task deep.Task) bool {
	timeout, _ := c.effectiveTaskTimeout(now, task)
	return timeout > 0
}

// selectTask returns the first task that still has useful work: queued
// tasks and retryable incomplete tasks in list order; parked (terminal)
// tasks are skipped.
func (c *deepCoordinator) selectTask() (int, bool) {
	for i := range c.state.Tasks {
		if !c.state.Tasks[i].Status.Terminal() {
			return i, true
		}
	}
	return 0, false
}

func (c *deepCoordinator) run(ctx context.Context) error {
	for {
		// Disk state is the truth: an external `stint deep stop` (or a
		// recovered coordinator) changes the phase here.
		fresh, err := deep.LoadState(c.stateDir, c.state.SessionID)
		if err != nil {
			return fmt.Errorf("read durable Deep Work state: %w", err)
		}
		if fresh.ExecutionQuiescenceUnconfirmed {
			*c.state = fresh
			return fmt.Errorf("Deep Work is blocked because executor writers may still be active (task %s)", fresh.ExecutionQuiescenceTaskID)
		}
		if fresh.Phase != deep.PhaseExecuting {
			if fresh.Phase == deep.PhaseLanding {
				*c.state = fresh
				return c.land(ctx, "resuming interrupted landing")
			}
			c.logf("state phase is %s; stopping loop", fresh.Phase)
			c.incident(deep.IncidentExternalStop, "", "phase "+string(fresh.Phase))
			return nil
		}

		now := c.now()
		if !now.Before(c.state.LandBefore) {
			return c.land(ctx, "landing window reached")
		}
		idx, ok := c.selectTask()
		if !ok {
			return c.land(ctx, "no safe useful work remaining")
		}
		effective, decision := c.effectiveTaskTimeout(now, c.state.Tasks[idx])
		if effective <= 0 {
			t := &c.state.Tasks[idx]
			if t.Attempts > 0 {
				t.Status = deep.StatusIncomplete
			} else {
				t.Status = deep.StatusQueued
			}
			t.ConfiguredTimeoutSec = int(c.taskTimeout.Seconds())
			t.EffectiveTimeoutSec = 0
			t.TimeoutDecision = decision
			t.Blocker = decision
			if err := c.save(); err != nil {
				return fmt.Errorf("persist task %s before landing: %w", t.ID, err)
			}
			return c.land(ctx, "insufficient useful task window before landing cutoff")
		}
		if err := c.runTask(ctx, idx, now); err != nil {
			return err
		}
	}
}
