package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Marguelgtz/Stint/internal/deep"
)

const (
	defaultTaskVerifyReserve = 3 * time.Minute
	defaultMissionVerifyTime = 10 * time.Minute
	coordinatorReserve       = 30 * time.Second
	minimumUsefulTaskWindow  = 5 * time.Minute
)

// deepCoordinator selects a bounded work unit, invokes the coding-agent
// executor in the isolated worktree, records verification, checkpoint, and
// versioned Objective-acceptance evidence, and repeats until landing. The
// coordinator is a plain foreground process: the machine
// must stay awake (D-3), and the existing compute watchdog remains the
// hard-deadline authority.
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
	// reviewTimeout bounds one fresh-context semantic review (zero = the
	// built-in three-minute limit).
	reviewTimeout time.Duration
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
	persist       func(string, *deep.DeepState) error
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
	in.provider = resolveHermesProvider(in.provider, in.reasoning)
	// The session's command policy is part of the reconstructed context: the
	// worker must know exactly which commands it may run and what will
	// happen to the rest.
	if sec := deep.CommandPolicySection(in.allowedCommands); sec != "" {
		in.prompt += sec
	}
	return in
}

func (c *deepCoordinator) executorRuntimeFor(t deep.Task) deep.ExecutorRuntime {
	reasoning := c.execCfg.reasoning
	if t.Reasoning != "" {
		reasoning = t.Reasoning
	}
	runtime := deep.ExecutorRuntime{
		Provider: resolveHermesProvider(c.execCfg.provider, reasoning),
		Model:    c.execCfg.model, Reasoning: reasoning,
	}
	if c.state.Exec != nil {
		runtime.Worker = c.state.Exec.Worker
	}
	return runtime
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
		persist = func(dir string, state *deep.DeepState) error { return state.SaveDir(dir) }
	}
	if err := persist(c.stateDir, c.state); err != nil {
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
		if c.state.RunEventSchemaVersion == deep.RunEventSchemaVersion && task.VerificationRunID != "" {
			// The verification.result event already projected this exact failure
			// and hard block. Do not rewrite its canonical evidence from a second
			// deep.json path.
			return fmt.Errorf("task %s cannot be checkpointed because verifier process quiescence is unconfirmed", taskID)
		}
		task.Status = deep.StatusNeedsHuman
		task.Blocker = "verifier process quiescence is unconfirmed; checkpointing and further work are stopped"
		task.VerificationCommand = command
		task.VerificationOutcome = result.Outcome
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

// runTask advances one task, invoking the executor when useful work remains,
// and transitions state from durable repository and acceptance evidence.
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
		t.LastResult = prerequisite
		if err := c.save(); err != nil {
			return fmt.Errorf("persist blocked dependent task %s: %w", t.ID, err)
		}
		c.logf("task %s BLOCKED: %s", t.ID, prerequisite)
		return nil
	}
	journaled := c.state.RunEventSchemaVersion == deep.RunEventSchemaVersion
	if c.pendingSemanticReview(*t) {
		return c.runSemanticReview(ctx, t.ID)
	}
	if journaled && c.state.AcceptanceContractVersion == deep.DeterministicAcceptanceContractVersion &&
		t.IsAcceptanceContractTask() && (t.Status == deep.StatusVerified || t.Status == deep.StatusCheckpointed) &&
		(t.AcceptanceOutcome == "" || t.AcceptanceOutcome == deep.AcceptanceNotEvaluated) {
		checkpoint, checkpointEventID, found, loadErr := deep.LoadTaskCheckpoint(c.stateDir, c.state.SessionID, t.ID)
		if loadErr != nil {
			return fmt.Errorf("load durable checkpoint for task %s acceptance: %w", t.ID, loadErr)
		}
		if found && checkpoint.Attempt == t.Attempts && checkpoint.Commit == t.CheckpointCommit &&
			checkpoint.TreeSHA == t.CheckpointTreeSHA {
			err := c.runJournaledAcceptanceCheck(ctx, t.ID, checkpoint, checkpointEventID)
			if errors.Is(err, errAcceptanceCheckpointChanged) {
				c.logf("task %s: prior checkpoint subject changed before acceptance; continuing the Work Unit with a new executor attempt", t.ID)
			} else if errors.Is(err, errAcceptanceWindowUnavailable) {
				return nil
			} else {
				return err
			}
			if err == nil {
				task := c.state.Tasks[idx]
				if task.AcceptanceOutcome == deep.AcceptanceAccepted ||
					(c.state.TaskAttemptCap > 0 && task.Attempts >= c.state.TaskAttemptCap) {
					return nil
				}
			}
		}
	}
	var (
		res              execResult
		execErr          error
		subject          verificationSnapshot
		subjectErr       error
		executorRun      deep.ExecutorRun
		resultRecorded   bool
		continuing       bool
		effectiveTimeout time.Duration
		timeoutDecision  string
	)

	// A durable result whose task transition was interrupted is reused to
	// continue verification. This closes the crash window without launching a
	// duplicate executor invocation.
	if journaled && t.ExecutorRunID != "" && !t.ExecutorRunProcessed {
		var found bool
		executorRun, found, err = deep.LoadExecutorRun(c.stateDir, c.state.SessionID, t.ExecutorRunID)
		if err != nil {
			return fmt.Errorf("load durable executor result for task %s: %w", t.ID, err)
		}
		if !found || executorRun.Outcome == deep.ExecutorOutcomeStarted {
			return fmt.Errorf("task %s has an unmatched executor invocation; recovery must establish quiescence before retry", t.ID)
		}
		if executorRun.Outcome == deep.ExecutorOutcomeUnknown || executorRun.Outcome == deep.ExecutorOutcomeQuiescenceUnconfirmed {
			return fmt.Errorf("task %s executor outcome or process quiescence is unresolved", t.ID)
		}
		res = execResult{
			exitCode: executorRun.ExitCode, completed: executorRun.Completed,
			finishReason: executorRun.FinishReason, duration: time.Duration(executorRun.DurationMilliseconds) * time.Millisecond,
		}
		if executorRun.Error != "" {
			execErr = errors.New(executorRun.Error)
		}
		continuing, err = c.afterInvocationState(t.ID)
		if err != nil {
			return fmt.Errorf("refresh durable state before resuming task %s: %w", t.ID, err)
		}
		if !continuing {
			return nil
		}
		subject, subjectErr = c.captureVerificationSubject("executor.recovery_subject_capture", c.state.WorktreePath, c.verificationBookkeepingPaths(), t.ID, t.Attempts)
		if subjectErr == nil {
			switch {
			case executorRun.RepositoryAfterError != "":
				subjectErr = fmt.Errorf("executor result did not capture a stable repository state: %s", executorRun.RepositoryAfterError)
			case executorRun.RepositoryAfter == nil:
				subjectErr = errors.New("executor result has no post-run repository identity")
			case subject.Subject.TreeSHA != executorRun.RepositoryAfter.TreeSHA:
				subjectErr = fmt.Errorf("product tree changed after executor result: recorded tree %s, current tree %s", executorRun.RepositoryAfter.TreeSHA, subject.Subject.TreeSHA)
			}
		}
		if subjectErr != nil {
			t = &c.state.Tasks[idx]
			t.Status = deep.StatusNeedsHuman
			t.Blocker = "executor result no longer identifies the current repository state: " + subjectErr.Error()
			t.ExecutorRunProcessed = true
			if err := c.save(); err != nil {
				return fmt.Errorf("persist invalidated executor result for task %s: %w", t.ID, err)
			}
			return fmt.Errorf("task %s cannot verify a changed or unidentified executor result: %w", t.ID, subjectErr)
		}
		t = &c.state.Tasks[idx]
		t.VerificationSubject = &subject.Subject
		if journaled {
			t.VerificationBookkeeping = landingVerificationBookkeeping(subject.Bookkeeping)
		} else {
			t.VerificationBookkeeping = subject.Bookkeeping
		}
		resultRecorded = true
	} else {
		effectiveTimeout, timeoutDecision = c.effectiveTaskTimeout(now, *t)
		if effectiveTimeout <= 0 {
			return fmt.Errorf("task %s has no useful executor window: %s", t.ID, timeoutDecision)
		}
		attempt := t.Attempts + 1
		// Build retry context before BeginExecutorRun clears the current
		// attempt's projected results. The previous verification and acceptance
		// failures are diagnostics for the fresh worker, not current evidence.
		promptTask := *t
		promptTask.Attempts = attempt
		input := c.execInputFor(promptTask, effectiveTimeout)
		if journaled {
			before, err := c.captureVerificationSubject("executor.pre_subject_capture", c.state.WorktreePath, c.verificationBookkeepingPaths(), t.ID, attempt)
			if err != nil {
				return fmt.Errorf("capture repository state before executor for task %s: %w", t.ID, err)
			}
			runID, err := deep.NewExecutorRunID()
			if err != nil {
				return err
			}
			startedAt := c.now().UTC()
			runtime := c.executorRuntimeFor(*t)
			executorRun = deep.ExecutorRun{
				ID: runID, TaskID: t.ID, Attempt: attempt, StartedAt: startedAt,
				ConfiguredTimeoutSeconds: int(c.taskTimeout.Seconds()), EffectiveTimeoutSeconds: int(effectiveTimeout.Seconds()),
				RemainingDeadlineSeconds: max(0, int(c.state.Deadline.Sub(startedAt).Seconds())),
				TimeoutDecision:          timeoutDecision, Runtime: runtime, RepositoryBefore: &before.Subject,
			}
			executorRun, err = deep.BeginExecutorRun(c.stateDir, c.state, executorRun)
			if err != nil {
				return fmt.Errorf("persist executor start for task %s before launch: %w", t.ID, err)
			}
			t = &c.state.Tasks[idx]
		} else {
			t.Status = deep.StatusActive
			t.Blocker = ""
			t.Attempts = attempt
			t.ConfiguredTimeoutSec = int(c.taskTimeout.Seconds())
			t.EffectiveTimeoutSec = int(effectiveTimeout.Seconds())
			t.TimeoutDecision = timeoutDecision
			t.ExecutorRunID = ""
			t.ExecutorRunProcessed = false
			t.VerificationSubject = nil
			t.VerificationBookkeeping = nil
			t.CheckpointCommit = ""
			t.CheckpointTreeSHA = ""
			t.VerifiedAt = nil
			if err := c.save(); err != nil {
				return fmt.Errorf("persist active task %s before invoking Hermes: %w", t.ID, err)
			}
		}
		c.logf("task %s attempt %d: invoking executor (configured maximum %s, effective timeout %s; %s)", t.ID, t.Attempts, c.taskTimeout, effectiveTimeout, timeoutDecision)

		tc, cancel := context.WithTimeout(ctx, effectiveTimeout)
		defer cancel()
		c.incident(deep.IncidentExecutorInvoke, t.ID,
			fmt.Sprintf("attempt %d %s (configured maximum %s; effective timeout %s; %s)", t.Attempts, policySummary(c.execCfg), c.taskTimeout, effectiveTimeout, timeoutDecision))
		if journaled {
			input.stateDir = c.stateDir
			input.sessionID = c.state.SessionID
			input.executorRunID = executorRun.ID
		}
		executorStarted := time.Now()
		res, execErr = c.executor.run(tc, input)
		c.recordTiming("executor.exit_to_coordinator_return", t.ID, t.Attempts, executorStarted)
		c.recordProviderRuntime(t.ID, t.Attempts, res.duration)
		if res.exitToQuiescence > 0 {
			c.recordMeasuredTiming("executor.exit_to_quiescence", t.ID, t.Attempts, res.exitToQuiescence, "supervisor-monotonic")
		}
		if res.timedOut && execErr == nil {
			execErr = context.DeadlineExceeded
		}
		if execErr == nil && tc.Err() != nil {
			execErr = tc.Err()
		}
		if injected, faultErr := qualificationFaultAfterReceipt(c, *t, executorRun, res, execErr); faultErr != nil {
			return fmt.Errorf("qualification receipt fault: %w", faultErr)
		} else if injected {
			os.Exit(qualificationFaultExitCode)
		}
		if execErr != nil {
			c.logf("task %s: executor error: %v", t.ID, execErr)
			c.incident(deep.IncidentExecutorError, t.ID, execErr.Error())
		}
		c.logf("task %s attempt %d result: %s", t.ID, t.Attempts, res.summary())
		if errors.Is(execErr, errExecutorQuiescenceUnconfirmed) {
			if !journaled {
				return c.persistUnquiescedExecutor(t.ID, res, execErr)
			}
			fresh, err := deep.LoadState(c.stateDir, c.state.SessionID)
			if err != nil {
				return fmt.Errorf("refresh state after unconfirmed executor quiescence: %w", err)
			}
			*c.state = fresh
			executorRun.Outcome = deep.ExecutorOutcomeQuiescenceUnconfirmed
			if !res.endedAt.IsZero() {
				executorRun.EndedAt = res.endedAt.UTC()
				executorRun.EndTimeSource = executorResultEndTimeSource(res)
			}
			executorRun.ExitCode = res.exitCode
			executorRun.Completed = res.completed
			executorRun.FinishReason = boundedExecutionFact(res.finishReason, 512)
			executorRun.Error = boundedExecutionFact(execErr.Error(), 512)
			executorRun.ResultSummary = executorResultSummary(res)
			executorRun.DurationMilliseconds = max(0, res.duration.Milliseconds())
			if err := deep.CompleteExecutorRun(c.stateDir, c.state, executorRun, c.now().UTC()); err != nil {
				return fmt.Errorf("persist unconfirmed executor result for task %s: %w", t.ID, err)
			}
			return fmt.Errorf("task %s cannot be verified because executor process quiescence is unconfirmed", t.ID)
		}
		continuing, err = c.afterInvocationState(t.ID)
		if err != nil {
			return fmt.Errorf("refresh durable state after task %s executor: %w", t.ID, err)
		}
		// Capture the exact Git-visible state after the executor is quiescent.
		// Stint bookkeeping is recorded separately by the Git backend.
		subject, subjectErr = c.captureVerificationSubject("executor.post_subject_capture", c.state.WorktreePath, c.verificationBookkeepingPaths(), t.ID, t.Attempts)
		if subjectErr != nil {
			c.logf("task %s: capture verification subject: %v", t.ID, subjectErr)
			c.incident(deep.IncidentCheckpointFail, t.ID, "could not capture verification subject: "+subjectErr.Error())
		} else {
			if res.repositoryAfterError != "" {
				subjectErr = fmt.Errorf("executor supervisor could not capture its Git-visible result: %s", res.repositoryAfterError)
			} else if res.repositoryAfter != nil &&
				(res.repositoryAfter.HeadCommit != subject.Subject.HeadCommit || res.repositoryAfter.TreeSHA != subject.Subject.TreeSHA) {
				subjectErr = fmt.Errorf("Git-visible repository state changed after the executor supervisor captured it (supervisor %s/%s; coordinator %s/%s)",
					res.repositoryAfter.HeadCommit, res.repositoryAfter.TreeSHA, subject.Subject.HeadCommit, subject.Subject.TreeSHA)
			}
			t = &c.state.Tasks[idx]
			t.VerificationSubject = &subject.Subject
			t.VerificationBookkeeping = subject.Bookkeeping
		}
		if journaled {
			switch {
			case res.timedOut || errors.Is(execErr, context.DeadlineExceeded):
				executorRun.Outcome = deep.ExecutorOutcomeTimedOut
				executorRun.Error = boundedExecutionFact(execErr.Error(), 512)
			case errors.Is(execErr, context.Canceled):
				executorRun.Outcome = deep.ExecutorOutcomeCanceled
				executorRun.Error = boundedExecutionFact(execErr.Error(), 512)
			case execErr == nil && res.completed && res.exitCode == 0:
				executorRun.Outcome = deep.ExecutorOutcomeSucceeded
			default:
				executorRun.Outcome = deep.ExecutorOutcomeFailed
			}
			executorRun.EndedAt = executorResultEndedAt(res, c.now())
			executorRun.EndTimeSource = executorResultEndTimeSource(res)
			executorRun.ExitCode = res.exitCode
			executorRun.Completed = res.completed
			executorRun.FinishReason = boundedExecutionFact(res.finishReason, 512)
			if execErr != nil {
				executorRun.Error = boundedExecutionFact(execErr.Error(), 512)
			}
			executorRun.ResultSummary = executorResultSummary(res)
			executorRun.DurationMilliseconds = max(0, res.duration.Milliseconds())
			if res.repositoryAfter != nil {
				executorRun.RepositoryAfter = res.repositoryAfter
				executorRun.RepositoryAfterObservedAt = res.repositoryAfterObservedAt
			} else if subjectErr == nil {
				executorRun.RepositoryAfter = &subject.Subject
				executorRun.RepositoryAfterObservedAt = c.now().UTC()
			}
			if subjectErr != nil {
				executorRun.RepositoryAfterError = boundedExecutionFact(subjectErr.Error(), 512)
			}
			if err := deep.CompleteExecutorRun(c.stateDir, c.state, executorRun, c.now().UTC()); err != nil {
				return fmt.Errorf("persist executor result for task %s before verification: %w", t.ID, err)
			}
			resultRecorded = true
			t = &c.state.Tasks[idx]
		}
		if !continuing {
			c.logf("task %s: executor quiesced after an external landing request; verification is deferred to resumed landing", t.ID)
			return nil
		}
	}

	if !continuing {
		return nil
	}
	t = &c.state.Tasks[idx]
	continuing, err = c.afterInvocationState(t.ID)
	if err != nil {
		return fmt.Errorf("refresh durable state before task %s verification: %w", t.ID, err)
	}
	if !continuing {
		c.logf("task %s: external landing began before verification; verification is deferred", t.ID)
		return nil
	}
	if journaled && resultRecorded && executorRun.RepositoryAfterError != "" {
		t = &c.state.Tasks[idx]
		t.Status = deep.StatusNeedsHuman
		t.Blocker = "executor result is not bound to a stable Git-visible repository state: " + executorRun.RepositoryAfterError
		t.ExecutorRunProcessed = true
		if err := c.save(); err != nil {
			return fmt.Errorf("persist task %s repository-result conflict: %w", t.ID, err)
		}
		return fmt.Errorf("task %s cannot be verified because its executor repository result is not stable", t.ID)
	}

	verifyResult, verifyCmd, verificationStarted, verifyErr := c.verifyTaskAttempt(ctx, t, subject, journaled)
	if verifyErr != nil {
		if errors.Is(verifyErr, errVerificationSubjectChanged) && !verificationStarted {
			t = &c.state.Tasks[idx]
			t.Status = deep.StatusNeedsHuman
			t.Blocker = "repository changed between executor completion and task verification; the executor result cannot be verified against the current tree"
			if resultRecorded {
				t.ExecutorRunProcessed = true
			}
			if saveErr := c.save(); saveErr != nil {
				return fmt.Errorf("persist task %s verification-subject conflict: %w", t.ID, saveErr)
			}
		}
		return fmt.Errorf("task %s verification could not be durably completed: %w", t.ID, verifyErr)
	}
	if journaled {
		t = &c.state.Tasks[idx]
	}
	if verifyCmd != "" {
		c.incident(deep.IncidentVerifyRun, t.ID, verifyResult.IncidentDetail())
	}
	resultSummary := res.summary()
	if resultRecorded && executorRun.ResultSummary != "" {
		resultSummary = executorRun.ResultSummary
	}
	t.LastResult = resultSummary
	t.ExecutionError = ""
	if execErr != nil {
		executionError := execErr.Error()
		if resultRecorded {
			executionError = executorRun.Error
		}
		t.ExecutionError = executionError
		t.LastResult += " | executor error: " + executionError
	}
	t.VerificationCommand = verifyCmd
	t.VerificationOutcome = verifyResult.Outcome
	t.VerificationOutput = strings.TrimSpace(tailLine(verifyResult.Output, 3))
	if journaled && verifyCmd != "" {
		t.VerificationOutput = deep.VerificationOutputExcerpt(verifyResult.Output)
	}
	t.VerificationResult = verifyResult.Summary()
	markExecutorResultProcessed := func() {
		if resultRecorded {
			t.ExecutorRunProcessed = true
		}
	}
	if verifyResult.QuiescenceUnconfirmed {
		markExecutorResultProcessed()
		return c.persistUnquiescedVerifier(t.ID, verifyCmd, verifyResult)
	}
	continuing, err = c.afterInvocationState(t.ID)
	if err != nil {
		return fmt.Errorf("refresh durable state after task %s verification: %w", t.ID, err)
	}
	if !continuing {
		t = &c.state.Tasks[idx]
		// A stop that raced with verification leaves the exact result available
		// for a later epoch, but this attempt does not checkpoint or accept it.
		t.LastResult = resultSummary
		t.ExecutionError = ""
		if execErr != nil {
			if resultRecorded {
				t.ExecutionError = executorRun.Error
			} else {
				t.ExecutionError = execErr.Error()
			}
		}
		t.VerificationCommand = verifyCmd
		t.VerificationOutcome = verifyResult.Outcome
		if journaled && verifyCmd != "" {
			t.VerificationOutput = deep.VerificationOutputExcerpt(verifyResult.Output)
		} else {
			t.VerificationOutput = strings.TrimSpace(tailLine(verifyResult.Output, 3))
		}
		t.VerificationResult = verifyResult.Summary()
		if err := c.save(); err != nil {
			return fmt.Errorf("persist task %s verification before landing: %w", t.ID, err)
		}
		return nil
	}
	t = &c.state.Tasks[idx]

	executionSucceeded := execErr == nil && res.completed && res.exitCode == 0

	acceptanceContractTask := journaled && c.state.AcceptanceContractVersion == deep.DeterministicAcceptanceContractVersion && t.IsAcceptanceContractTask()
	switch {
	case executionSucceeded && (verifyResult.Passed() || (verifyCmd == "" && acceptanceContractTask)):
		if journaled && verifyCmd != "" {
			durableRun, found, loadErr := deep.LoadVerificationRun(c.stateDir, c.state.SessionID, t.VerificationRunID)
			if loadErr != nil {
				return fmt.Errorf("load canonical verifier evidence for task %s: %w", t.ID, loadErr)
			}
			if !found || durableRun.Outcome != deep.VerificationPassed || !verificationRunMatchesSnapshot(durableRun, subject) {
				t.Status = deep.StatusNeedsHuman
				t.Blocker = "verification passed, but its durable result does not prove a stable exact repository subject"
				markExecutorResultProcessed()
				if saveErr := c.save(); saveErr != nil {
					return fmt.Errorf("persist unbound verification result for task %s: %w", t.ID, saveErr)
				}
				return fmt.Errorf("task %s verification result is not bound to a stable exact subject", t.ID)
			}
		}
		if subjectErr != nil {
			t.Status = deep.StatusNeedsHuman
			t.Blocker = "verification passed, but the exact repository subject could not be captured: " + subjectErr.Error()
			markExecutorResultProcessed()
			if saveErr := c.save(); saveErr != nil {
				return fmt.Errorf("persist missing verification subject for task %s: %w", t.ID, saveErr)
			}
			return fmt.Errorf("verification subject unavailable for task %s: %w", t.ID, subjectErr)
		}
		currentSubject, err := c.captureVerificationSubject("checkpoint.pre_capture", c.state.WorktreePath, c.verificationBookkeepingPaths(), t.ID, t.Attempts)
		if err == nil && !sameVerificationSnapshot(subject, currentSubject) {
			err = fmt.Errorf("repository changed after verification; verified subject %s/%s no longer matches %s/%s", subject.Subject.HeadCommit, subject.Subject.TreeSHA, currentSubject.Subject.HeadCommit, currentSubject.Subject.TreeSHA)
		}
		if err != nil {
			t.Status = deep.StatusNeedsHuman
			t.Blocker = "verification evidence was invalidated before checkpoint creation: " + err.Error()
			c.incident(deep.IncidentCheckpointFail, t.ID, t.Blocker)
			markExecutorResultProcessed()
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
			markExecutorResultProcessed()
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
				markExecutorResultProcessed()
				if saveErr := c.save(); saveErr != nil {
					return fmt.Errorf("persist invalid-checkpoint state for task %s: %w", t.ID, saveErr)
				}
				return fmt.Errorf("checkpoint identity unavailable for task %s", t.ID)
			}
			ts := c.now().UTC()
			if journaled && (verifyCmd != "" || acceptanceContractTask) {
				checkpoint := deep.TaskCheckpoint{
					TaskID: t.ID, Attempt: t.Attempts, ExecutorRunID: t.ExecutorRunID,
					VerificationRunID: t.VerificationRunID, VerificationSubject: subject.Subject,
					Commit: head, TreeSHA: tree,
				}
				if verifyCmd == "" {
					checkpoint.Basis = deep.TaskCheckpointBasisExecutor
					checkpoint.VerificationRunID = ""
				}
				if err := deep.RecordTaskCheckpoint(c.stateDir, c.state, checkpoint, ts); err != nil {
					return fmt.Errorf("journal exact task checkpoint for %s: %w", t.ID, err)
				}
				t = &c.state.Tasks[idx]
				if acceptanceContractTask {
					stored, eventID, found, loadErr := deep.LoadTaskCheckpoint(c.stateDir, c.state.SessionID, t.ID)
					if loadErr != nil || !found {
						if loadErr == nil {
							loadErr = errors.New("checkpoint event is missing after persistence")
						}
						return fmt.Errorf("load exact checkpoint for task %s acceptance: %w", t.ID, loadErr)
					}
					if err := c.runJournaledAcceptanceCheck(ctx, t.ID, stored, eventID); err != nil {
						if errors.Is(err, errAcceptanceCheckpointChanged) {
							c.logf("task %s: checkpoint subject changed before acceptance; a later executor attempt must establish a current subject", t.ID)
							return nil
						}
						if errors.Is(err, errAcceptanceWindowUnavailable) {
							return nil
						}
						return err
					}
					t = &c.state.Tasks[idx]
				}
			} else {
				t.CheckpointCommit = head
				t.CheckpointTreeSHA = tree
				t.Status = deep.StatusVerified
				t.VerifiedAt = &ts
				t.Blocker = ""
			}
		}
		if acceptanceContractTask {
			c.logf("task %s checkpointed; acceptance=%s", t.ID, t.AcceptanceOutcome)
		} else {
			c.logf("task %s VERIFIED", t.ID)
		}
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
	markExecutorResultProcessed()
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

func executorResultEndedAt(result execResult, fallback time.Time) time.Time {
	if !result.endedAt.IsZero() {
		return result.endedAt.UTC()
	}
	return fallback.UTC()
}

func executorResultEndTimeSource(result execResult) deep.ExecutorEndTimeSource {
	if result.endedAtSource != "" {
		return result.endedAtSource
	}
	return deep.ExecutorEndTimeCoordinator
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

func boundedExecutionFact(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	for len(value) > limit {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}

func executorResultSummary(result execResult) string {
	return fmt.Sprintf("exit=%d finish=%q in %s", result.exitCode, result.finishReason, result.duration.Round(time.Second))
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
// bounded time for task verification, Objective acceptance, and coordinator
// checkpoint work before shortening the invocation to the remaining window.
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
	acceptanceReserve := time.Duration(0)
	if c.state.AcceptanceContractVersion == deep.DeterministicAcceptanceContractVersion && task.IsAcceptanceContractTask() {
		acceptanceReserve = c.verifyTimeout
		if acceptanceReserve <= 0 {
			acceptanceReserve = defaultTaskVerifyReserve
		}
	}
	reviewReserve := time.Duration(0)
	if deep.HasSemanticReviewContract(c.state.SemanticReviewContractVersion) && task.IsAcceptanceContractTask() {
		reviewReserve = c.effectiveReviewLimit()
	}
	remaining := c.state.LandBefore.Sub(now)
	usable := remaining - verifyReserve - acceptanceReserve - reviewReserve - coordinatorReserve
	minimum := minDuration(maximum, minimumUsefulTaskWindow)
	reserveLabel := "coordinator checkpoint work"
	if verifyReserve > 0 && acceptanceReserve > 0 && reviewReserve > 0 {
		reserveLabel = "verification, acceptance-check, semantic review, and coordinator work"
	} else if verifyReserve > 0 && acceptanceReserve > 0 {
		reserveLabel = "verification, acceptance-check, and coordinator work"
	} else if reviewReserve > 0 && verifyReserve > 0 {
		reserveLabel = "verification, semantic review, and coordinator work"
	} else if reviewReserve > 0 && acceptanceReserve > 0 {
		reserveLabel = "acceptance-check, semantic review, and coordinator work"
	} else if reviewReserve > 0 {
		reserveLabel = "semantic review and coordinator work"
	} else if verifyReserve > 0 {
		reserveLabel = "verification and coordinator work"
	} else if acceptanceReserve > 0 {
		reserveLabel = "acceptance-check and coordinator work"
	}
	if usable < minimum {
		return 0, fmt.Sprintf("deferred: %s remains before landing cutoff; %s is reserved for %s, leaving less than the %s minimum useful invocation window (configured maximum %s)", remaining.Round(time.Second), (verifyReserve + acceptanceReserve + reviewReserve + coordinatorReserve).Round(time.Second), reserveLabel, minimum.Round(time.Second), maximum.Round(time.Second))
	}
	if usable < maximum {
		return usable, fmt.Sprintf("shortened from configured maximum %s to preserve %s for %s", maximum.Round(time.Second), (verifyReserve + acceptanceReserve + reviewReserve + coordinatorReserve).Round(time.Second), reserveLabel)
	}
	return maximum, "started at configured maximum"
}

func (c *deepCoordinator) effectiveReviewLimit() time.Duration {
	if c.reviewTimeout > 0 {
		return c.reviewTimeout
	}
	return defaultTaskVerifyReserve
}

func (c *deepCoordinator) effectiveReviewTimeout(now time.Time) time.Duration {
	remaining := c.state.LandBefore.Sub(now) - coordinatorReserve
	if remaining <= 0 {
		return 0
	}
	return minDuration(c.effectiveReviewLimit(), remaining)
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func (c *deepCoordinator) failedPrerequisite(task deep.Task) string {
	for _, prerequisite := range task.DependsOn {
		var required *deep.Task
		for _, candidate := range c.state.Tasks {
			if candidate.ID == prerequisite {
				required = &candidate
				break
			}
		}
		status := deep.Status("")
		satisfied := false
		requires := "verified"
		if required != nil {
			status = required.Status
			satisfied = status == deep.StatusVerified
			if c.state.AcceptanceContractVersion == deep.DeterministicAcceptanceContractVersion && required.IsAcceptanceContractTask() {
				requires = "accepted"
				satisfied = status == deep.StatusAccepted && required.AcceptanceOutcome == deep.AcceptanceAccepted
				if deep.HasSemanticReviewContract(c.state.SemanticReviewContractVersion) {
					requires = "accepted and clear-reviewed"
					satisfied = satisfied && deep.TaskHasSatisfiedReviewGate(required.ID, c.state.Tasks)
				}
			}
		}
		if !satisfied {
			statusLabel := string(status)
			if statusLabel == "" {
				statusLabel = "missing"
			}
			return fmt.Sprintf("not run: prerequisite %s has status %s; this Work Unit requires it to be %s", prerequisite, statusLabel, requires)
		}
	}
	return ""
}

func (c *deepCoordinator) hasUsefulTaskWindow(now time.Time, task deep.Task) bool {
	timeout, _ := c.effectiveTaskTimeout(now, task)
	return timeout > 0
}

// selectTask returns the first task with useful executor or pending acceptance
// work in list order. Contract-terminal tasks and attempts beyond the cap are
// skipped, except that a current pending checkpoint may still be accepted.
func (c *deepCoordinator) selectTask() (int, bool) {
	for i := range c.state.Tasks {
		task := c.state.Tasks[i]
		pendingReview := c.pendingSemanticReview(task)
		if task.TerminalInContract(c.state.AcceptanceContractVersion) && !pendingReview {
			continue
		}
		if c.state.AcceptanceContractVersion == deep.DeterministicAcceptanceContractVersion &&
			deep.HasSemanticReviewContract(c.state.SemanticReviewContractVersion) && task.IsAcceptanceContractTask() &&
			c.failedPrerequisite(task) != "" {
			// Keep a dependent queued while a prerequisite has repair findings.
			// Dynamic repair Work Units may resolve it later in this same epoch.
			continue
		}
		if c.state.TaskAttemptCap > 0 && task.Attempts >= c.state.TaskAttemptCap &&
			!taskHasPendingAcceptanceProjection(task, c.state.AcceptanceContractVersion) && !pendingReview {
			continue
		}
		return i, true
	}
	return 0, false
}

func (c *deepCoordinator) pendingSemanticReview(task deep.Task) bool {
	state := *c.state
	if !deep.HasSemanticReviewContract(state.SemanticReviewContractVersion) ||
		state.AcceptanceContractVersion != deep.DeterministicAcceptanceContractVersion || !task.IsAcceptanceContractTask() ||
		task.Status != deep.StatusAccepted || task.AcceptanceOutcome != deep.AcceptanceAccepted ||
		task.AcceptanceCheckOutcome != deep.AcceptanceCheckPassed || task.AcceptanceCheckpointEventID == "" {
		return false
	}
	if task.ReviewCycleID == "" || task.ReviewCheckpointEventID != task.AcceptanceCheckpointEventID {
		return true
	}
	if task.ReviewOutcome != deep.ReviewOutcomeUnresolved {
		return false
	}
	if task.ReviewReason == semanticReviewProtocolFailureReason {
		return true
	}
	// A deliberate resume can retry a transport failure after runtime repair,
	// while retaining the accepted checkpoint and all failed review facts.
	// The format retry stays bounded within each execution epoch.
	if task.ReviewReason != semanticReviewProtocolRetryExhaustedReason {
		return false
	}
	cycle, found, err := deep.LoadReviewCycle(c.stateDir, state.SessionID, task.ReviewCycleID)
	return err == nil && found && cycle.CheckpointEventID == task.AcceptanceCheckpointEventID &&
		cycle.StartedInEpochID != state.ExecutionEpochID
}

func taskHasPendingAcceptanceProjection(task deep.Task, contractVersion int) bool {
	return contractVersion == deep.DeterministicAcceptanceContractVersion && task.IsAcceptanceContractTask() &&
		(task.Status == deep.StatusVerified || task.Status == deep.StatusCheckpointed) &&
		(task.AcceptanceOutcome == "" || task.AcceptanceOutcome == deep.AcceptanceNotEvaluated)
}

func (c *deepCoordinator) run(ctx context.Context) error {
	for {
		// Disk state is the truth: an external `stint deep stop` (or a
		// recovered coordinator) changes the phase here.
		fresh, err := deep.LoadState(c.stateDir, c.state.SessionID)
		if err != nil {
			return fmt.Errorf("read durable Deep Work state: %w", err)
		}
		unmatchedExecutor, err := deep.LoadUnmatchedExecutorRun(c.stateDir, fresh.SessionID)
		if err != nil {
			return fmt.Errorf("load unmatched executor invocation: %w", err)
		}
		if unmatchedExecutor != nil {
			receiptFound := false
			if reader, ok := c.executor.(executorReceiptReader); ok {
				receipt, found, receiptErr := reader.loadExecutorReceipt(ctx, c.stateDir, fresh.SessionID, unmatchedExecutor.ID)
				if receiptErr != nil {
					if _, recoverErr := deep.RecoverUnmatchedExecutorRun(c.stateDir, &fresh, c.now()); recoverErr != nil {
						return fmt.Errorf("record unresolved executor recovery after receipt lookup failed: %w", recoverErr)
					}
					*c.state = fresh
					return fmt.Errorf("Deep Work is blocked: executor %s for task %s has no readable completion receipt; process quiescence is unknown: %w", unmatchedExecutor.ID, unmatchedExecutor.TaskID, receiptErr)
				}
				if found {
					var repositoryAtRecovery *deep.VerificationSubject
					repositoryAtRecoveryError := ""
					snapshot, captureErr := c.captureVerificationSubject("executor.reconciliation_subject_capture", fresh.WorktreePath, c.verificationBookkeepingPaths(), unmatchedExecutor.TaskID, unmatchedExecutor.Attempt)
					if captureErr != nil {
						repositoryAtRecoveryError = boundedExecutionFact(captureErr.Error(), 512)
					} else {
						subject := snapshot.Subject
						repositoryAtRecovery = &subject
					}
					reconciliationStarted := time.Now()
					reconciled, err := deep.ReconcileExecutorRunReceipt(c.stateDir, &fresh, receipt, repositoryAtRecovery, repositoryAtRecoveryError, c.now().UTC())
					c.recordTiming("executor.receipt_reconciliation", unmatchedExecutor.TaskID, unmatchedExecutor.Attempt, reconciliationStarted)
					if err != nil {
						return fmt.Errorf("reconcile executor completion receipt: %w", err)
					}
					*c.state = fresh
					if reconciled.RepositoryAfter == nil || reconciled.RepositoryAfterError != "" ||
						reconciled.RepositoryAtRecovery == nil || reconciled.RepositoryAtRecoveryError != "" ||
						reconciled.RepositoryAfter.HeadCommit != reconciled.RepositoryAtRecovery.HeadCommit ||
						reconciled.RepositoryAfter.TreeSHA != reconciled.RepositoryAtRecovery.TreeSHA {
						return fmt.Errorf("Deep Work is blocked: executor %s result repository state changed or is unidentified; verification and retries are stopped", unmatchedExecutor.ID)
					}
					receiptFound = true
				}
			}
			if !receiptFound {
				if _, err := deep.RecoverUnmatchedExecutorRun(c.stateDir, &fresh, c.now()); err != nil {
					return fmt.Errorf("record unmatched executor recovery: %w", err)
				}
				*c.state = fresh
				return fmt.Errorf("Deep Work is blocked: executor %s for task %s has no durable completion receipt and process quiescence is unknown", unmatchedExecutor.ID, unmatchedExecutor.TaskID)
			}
		}
		if unmatched, err := deep.RecoverUnmatchedVerificationRun(c.stateDir, &fresh, c.now()); err != nil {
			return fmt.Errorf("recover unmatched verification invocation: %w", err)
		} else if unmatched != nil {
			*c.state = fresh
			return fmt.Errorf("Deep Work is blocked: verification %s has no durable result and process quiescence is unknown", unmatched.ID)
		}
		if unmatched, err := deep.RecoverUnmatchedAcceptanceRun(c.stateDir, &fresh, c.now()); err != nil {
			return fmt.Errorf("recover unmatched acceptance-check invocation: %w", err)
		} else if unmatched != nil {
			*c.state = fresh
			return fmt.Errorf("Deep Work is blocked: acceptance-check %s for task %s has no durable result and process quiescence is unknown", unmatched.ID, unmatched.TaskID)
		}
		if fresh.RunEventSchemaVersion == deep.RunEventSchemaVersion {
			if unmatched, err := deep.RecoverUnmatchedReviewCycle(c.stateDir, &fresh, c.now()); err != nil {
				return fmt.Errorf("recover unmatched semantic review: %w", err)
			} else if unmatched != nil {
				*c.state = fresh
				return fmt.Errorf("Deep Work is blocked: semantic review %s for task %s has no durable result and reviewer process quiescence is unknown", unmatched.ID, unmatched.TaskID)
			}
			if unmatched, err := deep.RecoverUnmatchedMissionReviewCycle(c.stateDir, &fresh, c.now()); err != nil {
				return fmt.Errorf("recover unmatched mission semantic review: %w", err)
			} else if unmatched != nil {
				*c.state = fresh
				return fmt.Errorf("Deep Work is blocked: mission semantic review %s has no durable result and reviewer process quiescence is unknown", unmatched.ID)
			}
		}
		if fresh.ExecutionQuiescenceUnconfirmed {
			*c.state = fresh
			return fmt.Errorf("Deep Work is blocked because executor writers may still be active or verifier writers may still be active (task %s)", fresh.ExecutionQuiescenceTaskID)
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
		if err := deep.ReconcileReviewRepairs(c.stateDir, c.state, c.now().UTC()); err != nil {
			return fmt.Errorf("reconcile semantic review repair Work Units: %w", err)
		}

		now := c.now()
		if !now.Before(c.state.LandBefore) {
			return c.land(ctx, "landing window reached")
		}
		idx, ok := c.selectTask()
		if !ok {
			return c.land(ctx, "no safe useful work remaining")
		}
		task := c.state.Tasks[idx]
		if c.pendingSemanticReview(task) {
			if c.effectiveReviewTimeout(now) <= 0 {
				return c.land(ctx, "insufficient semantic review window before landing cutoff")
			}
			if err := c.runTask(ctx, idx, now); err != nil {
				return err
			}
			continue
		}
		if taskHasPendingAcceptanceProjection(task, c.state.AcceptanceContractVersion) {
			_, _, pending, err := c.pendingAcceptanceCheckpoint(task)
			if err != nil {
				return err
			}
			if pending {
				if c.effectiveAcceptanceTimeout(now) <= 0 {
					return c.land(ctx, "insufficient acceptance-check window before landing cutoff")
				}
				if err := c.runTask(ctx, idx, now); err != nil {
					return err
				}
				continue
			}
		}
		if c.state.TaskAttemptCap > 0 && task.Attempts >= c.state.TaskAttemptCap {
			return c.land(ctx, "Work Unit attempt cap reached with deterministic acceptance incomplete")
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
