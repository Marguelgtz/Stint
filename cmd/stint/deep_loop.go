package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
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
	verify func(ctx context.Context, command, workdir string) (string, bool, error)
	logf   func(format string, args ...any)
	out    io.Writer
	git    *gitRunner
}

// execInputFor builds the per-task invocation from the session-wide config.
func (c *deepCoordinator) execInputFor(t deep.Task) execInput {
	in := c.execCfg
	in.workdir = c.state.WorktreePath
	in.timeout = c.taskTimeout
	mission := c.mission()
	in.prompt = deep.BuildTaskPrompt(mission, t, t.Attempts, c.repoSummary())
	// The session's command policy is part of the reconstructed context: the
	// worker must know exactly which commands it may run and what will
	// happen to the rest.
	if sec := deep.CommandPolicySection(in.allowedCommands, in.autoApprove); sec != "" {
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

func (c *deepCoordinator) save() {
	if err := c.state.SaveDir(c.stateDir); err != nil {
		c.logf("WARNING: persist state: %v", err)
		c.incident(deep.IncidentStateSave, "", err.Error())
	}
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
		return fmt.Sprintf("autoApprove=%t allow=<none>", in.autoApprove)
	}
	return fmt.Sprintf("autoApprove=%t allow=[%s]", in.autoApprove, strings.Join(in.allowedCommands, ", "))
}

// stillExecuting re-reads the durable phase. An external `stint deep stop`
// (or a recovered coordinator) may have landed the session while this
// process was mid-task; persisting the in-memory state would resurrect it,
// so every save is gated on this check.
func (c *deepCoordinator) stillExecuting() bool {
	fresh, err := deep.LoadState(c.stateDir, c.state.SessionID)
	if err != nil {
		return true // unreadable state: don't fail the run on a bookkeeping read
	}
	return fresh.Phase == deep.PhaseExecuting
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
func (c *deepCoordinator) runTask(ctx context.Context, idx int, now time.Time) {
	t := &c.state.Tasks[idx]
	t.Status = deep.StatusActive
	t.Attempts++
	if c.stillExecuting() {
		c.save()
	}
	c.logf("task %s attempt %d: invoking executor (timeout %s)", t.ID, t.Attempts, c.taskTimeout)

	tc, cancel := context.WithTimeout(ctx, c.taskTimeout)
	defer cancel()
	c.incident(deep.IncidentExecutorInvoke, t.ID,
		fmt.Sprintf("attempt %d %s (timeout %s)", t.Attempts, policySummary(c.execCfg), c.taskTimeout))
	res, err := c.executor.run(tc, c.execInputFor(*t))
	if err != nil {
		c.logf("task %s: executor error: %v", t.ID, err)
		c.incident(deep.IncidentExecutorError, t.ID, err.Error())
	}
	c.logf("task %s attempt %d result: %s", t.ID, t.Attempts, res.summary())

	verified, verifyOut, verifyCmd := c.accept(t, res)
	if verifyCmd != "" {
		if verified {
			c.incident(deep.IncidentVerifyRun, t.ID, "command=`"+verifyCmd+"` result=pass")
		} else {
			c.incident(deep.IncidentVerifyRun, t.ID, "command=`"+verifyCmd+"` result=fail")
		}
	}
	t.LastResult = res.summary()
	if verifyOut != "" {
		t.LastResult += " | verify: " + strings.TrimSpace(tailLine(verifyOut, 2))
	}

	switch {
	case verified:
		ts := c.now()
		t.Status = deep.StatusVerified
		t.VerifiedAt = &ts
		t.Blocker = ""
		if msg, err := c.git.commitAll(c.state.WorktreePath, fmt.Sprintf("deep: %s %s verified", c.state.SessionID, t.ID)); err != nil {
			c.logf("checkpoint commit for %s: %v (%s)", t.ID, err, msg)
			c.incident(deep.IncidentCheckpointFail, t.ID, "checkpoint commit failed: "+err.Error())
		}
		c.logf("task %s VERIFIED", t.ID)
	case t.Attempts < c.state.TaskAttemptCap && now.Add(c.taskTimeout).Before(c.state.LandBefore):
		t.Status = deep.StatusIncomplete
		c.logf("task %s INCOMPLETE (attempt %d/%d): will reconstruct context and continue",
			t.ID, t.Attempts, c.state.TaskAttemptCap)
	default:
		t.Status = deep.StatusBlocked
		if t.Blocker == "" {
			t.Blocker = blockReason(res, verified, verifyOut)
		}
		c.logf("task %s BLOCKED: %s", t.ID, t.Blocker)
	}
	// The invocation and verify span the longest gap of the session: an
	// external stop or landing must win, or this process's save would
	// resurrect a stopped session from stale in-memory state.
	if c.stillExecuting() {
		c.save()
	} else {
		c.logf("task %s: session stopped during the invocation; outcome not persisted", t.ID)
		c.incident(deep.IncidentExternalStop, t.ID, "outcome of the in-flight attempt was not persisted")
	}
}

// accept checks repository evidence for the attempt: the task's own
// verification command when it defines one, else the mission-level command
// (a per-task command is the precision step for missions whose single
// command is broader than any one task's scope). With no command at all,
// the worker's completion is recorded but the handoff marks the result
// unverified. The verification run is bounded: a hung command must not
// stall the coordinator. It returns the command it used so the caller can
// record it in the incident log.
func (c *deepCoordinator) accept(t *deep.Task, res execResult) (bool, string, string) {
	command := t.Verify
	if command == "" {
		command = c.state.Verify
	}
	if command == "" {
		return res.completed && res.exitCode == 0, "", ""
	}
	bound := c.verifyTimeout
	if bound <= 0 {
		bound = 3 * time.Minute
	}
	vctx, cancel := context.WithTimeout(context.Background(), bound)
	defer cancel()
	out, ok, _ := c.verify(vctx, command, c.state.WorktreePath)
	return ok, out, command
}

func blockReason(res execResult, verified bool, verifyOut string) string {
	if !verified && res.completed {
		if verifyOut != "" {
			return "verification failed: " + strings.TrimSpace(tailLine(verifyOut, 2))
		}
		return "verification did not pass"
	}
	if res.exitCode != 0 {
		return fmt.Sprintf("invocation failed (exit %d): %s", res.exitCode, tailLine(res.stderrTail, 2))
	}
	return "invocation did not complete: " + res.finishReason
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
		if fresh, err := deep.LoadState(c.stateDir, c.state.SessionID); err == nil &&
			fresh.Phase != deep.PhaseExecuting {
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
		if !now.Add(c.taskTimeout).Before(c.state.LandBefore) {
			t := &c.state.Tasks[idx]
			t.Status = deep.StatusBlocked
			t.Blocker = "not started: insufficient time before landing window"
			c.save()
			return c.land(ctx, "time budget exhausted")
		}
		c.runTask(ctx, idx, now)
	}
}
