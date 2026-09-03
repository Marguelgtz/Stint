package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

// --- fakes for the remote (Hermes-on-box) seam -----------------------------

// fakeRemote is a remoteCmd that answers from canned behavior keyed on the
// assembled box command line. It records every command so tests can assert the
// exact remote invocations (hermes, verify, git) the coordinator issued.
type fakeRemote struct {
	calls []string
}

func (f *fakeRemote) run(ctx context.Context, cmd string) (string, error) {
	f.calls = append(f.calls, cmd)
	switch {
	case strings.Contains(cmd, "hermes chat"):
		// The on-box Hermes run: agent text, then the exit-code marker.
		return "worker: wrote the S0 spec and fixtures\n" + hermesExitMarker + "0", nil
	case strings.Contains(cmd, "sh -c 'true'"):
		return "PASS\n", nil
	case strings.Contains(cmd, "rev-parse HEAD"):
		return "base123\n", nil
	case strings.Contains(cmd, "status --porcelain"):
		return "", nil
	case strings.Contains(cmd, "log --oneline"):
		return "", nil
	case strings.Contains(cmd, "diff --stat"):
		return "", nil
	default:
		return "", nil
	}
}

// stubGit is a gitOps that returns canned values (no real git), so a remote
// coordinator run can be exercised end-to-end over the fake box.
type stubGit struct{}

func (stubGit) repoHead(dir string) (string, error)                  { return "base123", nil }
func (stubGit) headCommit(dir string) (string, error)                { return "base123", nil }
func (stubGit) cleanTracked(dir string) (bool, string)               { return true, "" }
func (stubGit) logOneline(dir string, n int) (string, error)         { return "", nil }
func (stubGit) statusShort(dir string) (string, error)               { return "", nil }
func (stubGit) diffStat(dir, base string) (string, error)            { return "", nil }
func (stubGit) worktreeAdd(repo, worktree, branch string) error      { return nil }
func (stubGit) branchExists(repo, branch string) bool                { return true }
func (stubGit) worktreeUsable(worktree string) bool                  { return true }
func (stubGit) worktreeReattach(repo, worktree, branch string) error { return nil }
func (stubGit) commitAll(dir, message string) (string, error)        { return "ok", nil }

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"plain":         `'plain'`,
		"it's":          `'it'\''s'`,
		"a b  c":        `'a b  c'`,
		`": $(x) 'y' "`: `'": $(x) '\''y'\'' "'`,
		"":              "''",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHermesExecutorSuccess(t *testing.T) {
	fr := &fakeRemote{}
	e := newHermesExecutor(fr.run)
	res, err := e.run(context.Background(), execInput{
		workdir: "/root/repo/.stint-deep/x", prompt: "resume the mission",
		timeout: 300 * time.Second, model: "qwen3.8-27b",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.completed || res.exitCode != 0 {
		t.Errorf("completed=%v exit=%d, want completed/0", res.completed, res.exitCode)
	}
	if res.finishReason != "completed" {
		t.Errorf("finishReason=%q, want completed", res.finishReason)
	}
	if !strings.Contains(res.outputText, "wrote the S0 spec") {
		t.Errorf("outputText lost the agent text: %q", res.outputText)
	}
	if strings.Contains(res.outputText, hermesExitMarker) {
		t.Errorf("outputText still carries the exit marker: %q", res.outputText)
	}
	// The box command must stage the prompt (base64), cd into the worktree,
	// and run headless Hermes pointed at the model.
	if len(fr.calls) != 1 {
		t.Fatalf("expected 1 remote call, got %d", len(fr.calls))
	}
	line := fr.calls[0]
	for _, want := range []string{
		"base64 -d", "cd '/root/repo/.stint-deep/x'",
		"hermes chat --query-file", "--oneshot", "--provider custom -m 'qwen3.8-27b'",
		hermesExitMarker,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("box line missing %q:\n%s", want, line)
		}
	}
}

func TestHermesExecutorNonZeroExit(t *testing.T) {
	fr := &fakeRemote{}
	e := newHermesExecutor(func(ctx context.Context, cmd string) (string, error) {
		fr.calls = append(fr.calls, cmd)
		return "worker: ran out of budget\n" + hermesExitMarker + "124", nil
	})
	res, err := e.run(context.Background(), execInput{workdir: "/wt", prompt: "p", timeout: 60 * time.Second})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.completed {
		t.Errorf("a non-zero exit must not be completed (finish=%q)", res.finishReason)
	}
	if res.exitCode != 124 {
		t.Errorf("exitCode=%d, want 124 (timeout)", res.exitCode)
	}
}

func TestHermesExecutorSSHFailure(t *testing.T) {
	e := newHermesExecutor(func(ctx context.Context, cmd string) (string, error) {
		return "", context.DeadlineExceeded
	})
	res, err := e.run(context.Background(), execInput{workdir: "/wt", prompt: "p", timeout: 60 * time.Second})
	if err == nil {
		t.Fatalf("expected an error when the SSH channel fails, got nil (res=%+v)", res)
	}
	if res.exitCode != -1 {
		t.Errorf("exitCode=%d, want -1 on SSH failure", res.exitCode)
	}
}

func TestRunVerifyCmdRemote(t *testing.T) {
	fr := &fakeRemote{}
	out, ok, err := runVerifyCmdRemote(context.Background(), fr.run, "true", "/root/repo/.stint-deep/x")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Errorf("verify of `true` reported fail (out=%q)", out)
	}
	if len(fr.calls) != 1 || !strings.HasPrefix(fr.calls[0], "cd '/root/repo/.stint-deep/x' && sh -c 'true'") {
		t.Errorf("unexpected remote verify line: %q", fr.calls)
	}
	// A failing verify (exit non-zero) must be reported as fail.
	frailing := func(ctx context.Context, cmd string) (string, error) {
		return "boom", errors.New("command exited 1")
	}
	if _, ok, _ := runVerifyCmdRemote(context.Background(), frailing, "bash scripts/verify-cp1", "/wt"); ok {
		t.Errorf("a failing verify command was reported as passing")
	}
}

// A coordinator wired with the remote (Hermes) executor + remote verify + a
// stub remote git must run a full loop on the "box": each task's Hermes
// invocation runs over the box channel, acceptance is decided by the remote
// verify command, a checkpoint commit is issued over the box, and the session
// lands. No local cline, no local verify, no local git is involved.
func TestDeepLoopRemoteHermesEndToEnd(t *testing.T) {
	stateDir := t.TempDir()
	fr := &fakeRemote{}
	sessionID := "20260903-000000"
	mission := deep.Mission{
		Name: "remote mission", Objective: "run on the box",
		Success: []string{"the box does the work"}, Constraints: []string{"stay in the box worktree"},
		Tasks: []deep.Task{
			{ID: "T-001", Objective: "task one", Verify: "true", Status: deep.StatusQueued},
			{ID: "T-002", Objective: "task two", Verify: "true", Status: deep.StatusQueued},
		},
	}
	state := deep.NewState(sessionID, mission, "/root/repo", "/root/repo/.stint-deep/"+sessionID,
		time.Date(2026, 9, 3, 17, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 3, 16, 50, 0, 0, time.UTC), 3,
		time.Date(2026, 9, 3, 16, 0, 0, 0, time.UTC))
	state.BaseCommit = "base123"
	state.Verify = "" // per-task commands drive acceptance; no mission-level final check
	if err := state.SaveDir(stateDir); err != nil {
		t.Fatal(err)
	}
	statePtr := &state
	remoteVerify := func(ctx context.Context, command, workdir string) (string, bool, error) {
		return runVerifyCmdRemote(ctx, fr.run, command, workdir)
	}
	coord := &deepCoordinator{
		stateDir:    stateDir,
		state:       statePtr,
		execCfg:     execInput{provider: "openai-compatible", model: "qwen3.8-27b"},
		executor:    newHermesExecutor(fr.run),
		now:         func() time.Time { return time.Date(2026, 9, 3, 16, 0, 0, 0, time.UTC) },
		taskTimeout: time.Minute,
		verify:      remoteVerify,
		finalVerify: func(ctx context.Context, command string) (string, bool, error) {
			return runVerifyCmdRemote(ctx, fr.run, command, state.WorktreePath)
		},
		worktreeWrite: func(path string, data []byte) error {
			return writeRemoteFile(fr.run, path, data)
		},
		logf: func(string, ...any) {},
		out:  io.Discard,
		git:  stubGit{},
	}
	if err := coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if statePtr.Phase != deep.PhaseLanded {
		t.Errorf("phase=%s, want landed", statePtr.Phase)
	}
	for i, task := range statePtr.Tasks {
		if task.Status != deep.StatusVerified {
			t.Errorf("task %d = %s, want verified (remote verify of its own command)", i, task.Status)
		}
	}
	// Two Hermes invocations (one per task) plus the per-task verify runs,
	// all over the box channel.
	hermesCalls := 0
	verifyCalls := 0
	for _, c := range fr.calls {
		if strings.Contains(c, "hermes chat") {
			hermesCalls++
		}
		if strings.Contains(c, "sh -c 'true'") {
			verifyCalls++
		}
	}
	if hermesCalls != 2 {
		t.Errorf("hermes box invocations = %d, want 2 (one per task)", hermesCalls)
	}
	if verifyCalls < 2 {
		t.Errorf("remote verify runs = %d, want >= 2 (each task's own command)", verifyCalls)
	}
	if statePtr.HandoffPath == "" {
		t.Errorf("handoff path not recorded")
	}
	// The worktree handoff file must have been written OVER THE BOX
	// (base64 over SSH), so the on-box branch's final commit includes it.
	handoffWrite := 0
	for _, c := range fr.calls {
		if strings.Contains(c, "DEEP_WORK_HANDOFF.md") {
			handoffWrite++
		}
	}
	if handoffWrite < 1 {
		t.Errorf("landing handoff was not written over the box channel; calls:\n%s", strings.Join(fr.calls, "\n"))
	}
}
