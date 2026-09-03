package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func testResumeState(deadline time.Time) *deep.DeepState {
	return &deep.DeepState{
		SessionID:    "20260903-120000",
		MissionName:  "resume fixture",
		RepoPath:     "/repo",
		WorktreePath: "/repo/.stint-deep/x",
		Branch:       "stint/deep-x",
		Deadline:     deadline,
		Phase:        deep.PhaseExecuting,
	}
}

// A crash or lapsed machine may only tighten the budget while it is still in
// the future; once it lapsed, the fresh compute deadline becomes the budget.
func TestReanchorDeadline(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	t.Run("tightens to the compute deadline", func(t *testing.T) {
		st := testResumeState(now.Add(3 * time.Hour))
		reset, err := reanchorDeadline(st, now.Add(2*time.Hour), now)
		if err != nil || reset {
			t.Fatalf("reanchor: err=%v reset=%v", err, reset)
		}
		if !st.Deadline.Equal(now.Add(2 * time.Hour)) {
			t.Errorf("deadline = %s, want min(old, compute)", st.Deadline)
		}
	})
	t.Run("keeps the earlier deep deadline", func(t *testing.T) {
		st := testResumeState(now.Add(2 * time.Hour))
		reset, err := reanchorDeadline(st, now.Add(4*time.Hour), now)
		if err != nil || reset {
			t.Fatalf("reanchor: err=%v reset=%v", err, reset)
		}
		if !st.Deadline.Equal(now.Add(2 * time.Hour)) {
			t.Errorf("deadline = %s, want the original deep deadline", st.Deadline)
		}
	})
	t.Run("lapsed deadline adopts the fresh compute deadline", func(t *testing.T) {
		st := testResumeState(now.Add(-1 * time.Hour))
		reset, err := reanchorDeadline(st, now.Add(4*time.Hour), now)
		if err != nil || !reset {
			t.Fatalf("reanchor: err=%v reset=%v, want reset", err, reset)
		}
		if !st.Deadline.Equal(now.Add(4 * time.Hour)) {
			t.Errorf("deadline = %s, want the compute session deadline", st.Deadline)
		}
		if st.LandBefore.After(st.Deadline) || st.LandBefore.Before(now) {
			t.Errorf("landBefore = %s must sit between now and the deadline", st.LandBefore)
		}
	})
	t.Run("both lapsed is a fresh start, not a resume", func(t *testing.T) {
		st := testResumeState(now.Add(-2 * time.Hour))
		_, err := reanchorDeadline(st, now.Add(-time.Hour), now)
		if err == nil || !strings.Contains(err.Error(), "fresh session") {
			t.Fatalf("err = %v, want the fresh-session guidance", err)
		}
	})
}

func TestResolveExecSettings(t *testing.T) {
	on := true
	t.Run("persisted settings survive resume", func(t *testing.T) {
		st := &deep.DeepState{Exec: &deep.ExecSettings{
			AutoApprove: false, Provider: "openai-compatible", Model: "qwen3.8-27b",
			ClineConfig: "/cfg", TaskTimeoutSec: 900,
		}}
		got := resolveExecSettings(st, execOverrides{})
		if got.AutoApprove || got.Model != "qwen3.8-27b" || got.ClineConfig != "/cfg" || got.TaskTimeoutSec != 900 {
			t.Errorf("got = %+v", got)
		}
	})
	t.Run("flags override persisted settings", func(t *testing.T) {
		st := &deep.DeepState{Exec: &deep.ExecSettings{AutoApprove: true, Provider: "openai-compatible", Model: "m1", TaskTimeoutSec: 600}}
		got := resolveExecSettings(st, execOverrides{autoApprove: &on, provider: "p2", clineConfig: "/c2", taskTimeout: 5 * time.Minute})
		if !got.AutoApprove || got.Provider != "p2" || got.ClineConfig != "/c2" || got.TaskTimeoutSec != 300 {
			t.Errorf("got = %+v", got)
		}
		if got.Model != "m1" {
			t.Errorf("model = %q, want the persisted model (flags never clear it)", got.Model)
		}
	})
	t.Run("legacy sessions fall back to deny-by-default start-time defaults", func(t *testing.T) {
		got := resolveExecSettings(&deep.DeepState{}, execOverrides{})
		if got.AutoApprove || got.Provider != "openai-compatible" || got.TaskTimeoutSec != 600 || got.Model != "" {
			t.Errorf("got = %+v, want auto-approval OFF (safety default)", got)
		}
	})
	t.Run("command policy is persisted state, not an override", func(t *testing.T) {
		st := &deep.DeepState{Exec: &deep.ExecSettings{AutoApprove: false, AllowedCommands: []string{"go test", "git status"}}}
		got := resolveExecSettings(st, execOverrides{})
		if got.AutoApprove || len(got.AllowedCommands) != 2 || got.AllowedCommands[0] != "go test" || got.AllowedCommands[1] != "git status" {
			t.Errorf("got = %+v, want the persisted allow-list intact", got)
		}
	})
}

func TestAssertNoLiveCoordinator(t *testing.T) {
	dir := t.TempDir()
	if pid, err := assertNoLiveCoordinator(dir, "s1"); err != nil || pid != 0 {
		t.Fatalf("no pid file: pid=%d err=%v, want none", pid, err)
	}
	if err := deep.WriteCoordinatorPid(dir, "s1", os.Getpid()); err != nil {
		t.Fatal(err)
	}
	pid, err := assertNoLiveCoordinator(dir, "s1")
	if err == nil || !strings.Contains(err.Error(), "already running") || pid != os.Getpid() {
		t.Fatalf("live coordinator: pid=%d err=%v, want refusal with pid %d", pid, err, os.Getpid())
	}
	if err := deep.WriteCoordinatorPid(dir, "s1", 999999999); err != nil {
		t.Fatal(err)
	}
	if pid, err := assertNoLiveCoordinator(dir, "s1"); err != nil || pid != 0 {
		t.Fatalf("stale pid: pid=%d err=%v, want tolerated", pid, err)
	}
	if _, err := os.Stat(deep.CoordinatorPidFile(dir, "s1")); !os.IsNotExist(err) {
		t.Errorf("stale pid file must be cleared, stat err=%v", err)
	}
}

// A crash mid-task leaves the durable state with the task active; the
// recovered coordinator must continue from that exact point — same branch,
// same worktree, verified work not redone.
func TestDeepResumeContinuesCrashedSession(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks[0].Status = deep.StatusActive
	env.state.Tasks[0].Attempts = 1
	if err := env.state.SaveDir(env.coord.stateDir); err != nil {
		t.Fatalf("save crashed state: %v", err)
	}
	// The crashed process left a stale pid file behind.
	if err := deep.WriteCoordinatorPid(env.coord.stateDir, env.state.SessionID, 999999999); err != nil {
		t.Fatal(err)
	}
	// The recovered coordinator reconstructs from durable state.
	fresh, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	env.state = &fresh
	env.coord.state = &fresh

	before := env.fake.calls
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if env.fake.calls != before+2 {
		t.Errorf("executor called %d times after resume, want 2 (task A continuation + task B)", env.fake.calls-before)
	}
	if env.state.Tasks[0].Status != deep.StatusVerified || env.state.Tasks[0].Attempts != 2 {
		t.Errorf("task A = %+v, want verified on attempt 2", env.state.Tasks[0])
	}
	if env.state.Tasks[1].Status != deep.StatusVerified {
		t.Errorf("task B = %s, want verified", env.state.Tasks[1].Status)
	}
	if env.state.Phase != deep.PhaseLanded {
		t.Errorf("phase = %s, want landed", env.state.Phase)
	}
}

// A crash (or `git worktree remove`) can lose the worktree directory; the
// branch still holds every checkpoint commit, so re-attaching recovers the
// work intact.
func TestDeepResumeReattachesLostWorktree(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	if err := os.WriteFile(env.wt+"/note.txt", []byte("checkpoint\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := env.coord.git.commitAll(env.wt, "deep: test checkpoint"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	baseHead, err := env.coord.git.headCommit(env.wt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(env.wt); err != nil {
		t.Fatal(err)
	}
	if err := ensureDeepWorktree(env.coord.git, false, env.state); err != nil {
		t.Fatalf("ensureDeepWorktree: %v", err)
	}
	head, err := env.coord.git.headCommit(env.wt)
	if err != nil {
		t.Fatalf("head after re-attach: %v", err)
	}
	if head != baseHead {
		t.Errorf("head = %s, want %s (checkpoint commits must survive re-attach)", head, baseHead)
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if env.state.Phase != deep.PhaseLanded {
		t.Errorf("phase = %s, want landed", env.state.Phase)
	}
}

func TestDeepResumeRefusesWhenBranchIsGone(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	if err := os.RemoveAll(env.wt); err != nil {
		t.Fatal(err)
	}
	// Prune drops the stale worktree entry so the branch can be deleted.
	if out, err := exec.Command("git", "-C", env.repo, "worktree", "prune").CombinedOutput(); err != nil {
		t.Fatalf("prune: %v (%s)", err, out)
	}
	if out, err := exec.Command("git", "-C", env.repo, "branch", "-D", env.state.Branch).CombinedOutput(); err != nil {
		t.Fatalf("delete branch: %v (%s)", err, out)
	}
	err := ensureDeepWorktree(env.coord.git, false, env.state)
	if err == nil || !strings.Contains(err.Error(), "fresh session") {
		t.Fatalf("err = %v, want the fresh-session guidance (a resume cannot invent a branch)", err)
	}
}

// A landed or stopped session is a pause, not a verdict: resuming it
// continues the remaining tasks in the same worktree and branch, and work
// already verified (and checkpointed) is never redone.
func TestDeepResumeRevivesLandedSession(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	stopperState, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	stopper := &deepCoordinator{
		stateDir:    env.coord.stateDir,
		state:       &stopperState,
		taskTimeout: time.Minute,
		verify:      env.coord.verify,
		now:         func() time.Time { return env.clock.now },
		logf:        func(string, ...any) {},
		out:         io.Discard,
		git:         env.coord.git,
	}
	// `stint deep stop` lands the session while task B's invocation is in
	// flight; task A already verified and checkpointed before that.
	calls := 0
	env.fake.after = func() {
		calls++
		if calls == 2 {
			_ = stopper.land(context.Background(), "stopped by user")
		}
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	fresh, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Phase != deep.PhaseLanded {
		t.Fatalf("phase = %s, want landed before the revive", fresh.Phase)
	}
	if fresh.Tasks[0].Status != deep.StatusVerified || fresh.Tasks[0].Attempts != 1 {
		t.Fatalf("task A = %+v, want verified on attempt 1", fresh.Tasks[0])
	}
	if fresh.Tasks[1].Status != deep.StatusIncomplete {
		t.Fatalf("task B = %s, want incomplete (parked by the stop)", fresh.Tasks[1].Status)
	}

	// Revive: what `stint deep resume` does to the durable state.
	fresh.Phase = deep.PhaseExecuting
	fresh.LandedAt = nil
	if err := fresh.SaveDir(env.coord.stateDir); err != nil {
		t.Fatal(err)
	}
	env.state = &fresh
	env.coord.state = &fresh
	env.fake.after = nil

	before := env.fake.calls
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("revived run: %v", err)
	}
	if env.fake.calls != before+1 {
		t.Errorf("executor called %d times on the revive, want 1 (task B only; A was already verified)", env.fake.calls-before)
	}
	if env.state.Tasks[0].Status != deep.StatusVerified || env.state.Tasks[0].Attempts != 1 {
		t.Errorf("task A = %+v, want still verified from attempt 1 (never redone)", env.state.Tasks[0])
	}
	if env.state.Tasks[1].Status != deep.StatusVerified {
		t.Errorf("task B = %s, want verified", env.state.Tasks[1].Status)
	}
	if env.state.Phase != deep.PhaseLanded {
		t.Errorf("phase = %s, want landed", env.state.Phase)
	}
}
