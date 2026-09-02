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
	verify      func(ctx context.Context, workdir string) (string, bool, error)
	logf        func(format string, args ...any)
	out         io.Writer
	git         *gitRunner
}

// execInputFor builds the per-task invocation from the session-wide config.
func (c *deepCoordinator) execInputFor(t deep.Task) execInput {
	in := c.execCfg
	in.workdir = c.state.WorktreePath
	in.timeout = c.taskTimeout
	mission := c.mission()
	in.prompt = deep.BuildTaskPrompt(mission, t, t.Attempts, c.repoSummary())
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
	}
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
	c.save()
	c.logf("task %s attempt %d: invoking executor (timeout %s)", t.ID, t.Attempts, c.taskTimeout)

	tc, cancel := context.WithTimeout(ctx, c.taskTimeout)
	defer cancel()
	res, err := c.executor.run(tc, c.execInputFor(*t))
	if err != nil {
		c.logf("task %s: executor error: %v", t.ID, err)
	}
	c.logf("task %s attempt %d result: %s", t.ID, t.Attempts, res.summary())

	verified, verifyOut := c.accept(t, res)
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
	c.save()
}

// accept checks the mission's verification command (when the mission defines
// one). Without a verify command the worker's completion is recorded but the
// handoff marks the result unverified.
func (c *deepCoordinator) accept(t *deep.Task, res execResult) (bool, string) {
	if c.state.Verify == "" {
		return res.completed && res.exitCode == 0, ""
	}
	out, ok, _ := c.verify(context.Background(), c.state.WorktreePath)
	return ok, out
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
