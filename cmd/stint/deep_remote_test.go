package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
		return "PASS\n" + verifyExitMarker + "0\n", nil
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
func (stubGit) headSubject(dir string) (string, error)               { return "baseline", nil }
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
		"mktemp /tmp/stint-deep-prompt.XXXXXX", "base64 -d", "cd '/root/repo/.stint-deep/x'",
		"hermes chat --query-file", "--oneshot --provider custom -m 'qwen3.8-27b'", "trap 'rm -f",
		hermesExitMarker,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("box line missing %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, " -Q") {
		t.Errorf("remote Hermes invocation uses -Q quiet mode, which can disrupt tool calls:\n%s", line)
	}
}

func TestHermesExecutorReasoningProviderTemplate(t *testing.T) {
	fr := &fakeRemote{}
	e := newHermesExecutor(fr.run)
	_, err := e.run(context.Background(), execInput{
		workdir: "/wt", prompt: "plan", timeout: time.Minute,
		provider: "custom:qwen-stint-{reasoning}", model: "qwen3.8-27b", reasoning: "xhigh",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("remote calls = %d, want 1", len(fr.calls))
	}
	line := fr.calls[0]
	for _, want := range []string{"custom:qwen-stint-xhigh", "--reasoning 'xhigh'"} {
		if !strings.Contains(line, want) {
			t.Errorf("box line missing %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "{reasoning}") {
		t.Errorf("provider template was not resolved:\n%s", line)
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

func TestLocalHermesExecutorSuccess(t *testing.T) {
	dir := t.TempDir()
	hermes := dir + "/hermes"
	captured := filepath.Join(dir, "captured-prompt-path")
	script := "#!/bin/sh\ntest \"$2\" = --query-file || exit 11\n" +
		"test \"$(cat \"$3\")\" = 'continue locally' || exit 12\n" +
		"printf '%s' \"$3\" > " + shellQuote(captured) + "\nprintf 'on-box worker output\\n'\n"
	if err := os.WriteFile(hermes, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	e := newLocalHermesExecutor(hermes)
	res, err := e.run(context.Background(), execInput{
		workdir: dir, prompt: "continue locally", timeout: time.Minute,
		provider: "custom:qwen-stint-{reasoning}", model: "qwen3.8-27b", reasoning: "medium",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.completed || res.exitCode != 0 {
		t.Fatalf("completed=%v exit=%d, want completed/0", res.completed, res.exitCode)
	}
	if !strings.Contains(res.outputText, "on-box worker output") {
		t.Errorf("outputText = %q", res.outputText)
	}
	promptPathBytes, err := os.ReadFile(captured)
	if err != nil {
		t.Fatalf("read captured prompt path: %v", err)
	}
	promptPath := strings.TrimSpace(string(promptPathBytes))
	if filepath.Dir(promptPath) == dir {
		t.Errorf("prompt was staged in the target worktree: %q", promptPath)
	}
	if _, err := os.Stat(promptPath); !os.IsNotExist(err) {
		t.Errorf("temporary prompt was not removed after Hermes exited: stat err=%v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".stint-hermes-prompt-") {
			t.Fatalf("temporary prompt was captured in the target worktree: %s", entry.Name())
		}
	}
}

func TestLocalHermesExecutorFailure(t *testing.T) {
	dir := t.TempDir()
	hermes := dir + "/hermes"
	if err := os.WriteFile(hermes, []byte("#!/bin/sh\necho failed >&2\nexit 7\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	res, err := newLocalHermesExecutor(hermes).run(context.Background(), execInput{
		workdir: dir, prompt: "fail", timeout: time.Minute,
	})
	if err == nil {
		t.Fatal("expected a local Hermes error")
	}
	if res.exitCode != 7 || res.completed {
		t.Errorf("result = %+v, want exit 7 and incomplete", res)
	}
	if !strings.Contains(res.stderrTail, "failed") {
		t.Errorf("stderr tail = %q", res.stderrTail)
	}
}

func TestRunVerifyCmdRemote(t *testing.T) {
	fr := &fakeRemote{}
	passed := runVerifyCmdRemote(context.Background(), fr.run, "true", "/root/repo/.stint-deep/x")
	if !passed.Passed() || passed.ExitCode != 0 || !strings.Contains(passed.Output, "PASS") {
		t.Errorf("verify of `true` result = %+v", passed)
	}
	if len(fr.calls) != 1 || !strings.HasPrefix(fr.calls[0], "cd '/root/repo/.stint-deep/x' ||") || !strings.Contains(fr.calls[0], "sh -c 'true'") {
		t.Errorf("unexpected remote verify line: %q", fr.calls)
	}
	// A failing verifier keeps its process exit distinct from SSH transport.
	failing := func(ctx context.Context, cmd string) (string, error) {
		return "boom\n" + verifyExitMarker + "7\n", nil
	}
	failed := runVerifyCmdRemote(context.Background(), failing, "bash scripts/verify-cp1", "/wt")
	if failed.Outcome != verificationFailed || !failed.HasExitCode || failed.ExitCode != 7 || !strings.Contains(failed.Output, "boom") {
		t.Errorf("nonzero verifier result = %+v, want failed exit 7 with output", failed)
	}
	transport := func(ctx context.Context, cmd string) (string, error) {
		return "ssh disconnected", errors.New("connection lost")
	}
	transportResult := runVerifyCmdRemote(context.Background(), transport, "true", "/wt")
	if transportResult.Outcome != verificationExecutionErr || !strings.Contains(transportResult.Error, "connection lost") {
		t.Errorf("transport result = %+v, want execution_error", transportResult)
	}
	markerPlusTransportError := func(ctx context.Context, cmd string) (string, error) {
		return "verifier output\n" + verifyExitMarker + "0\n", errors.New("connection lost after output")
	}
	masked := runVerifyCmdRemote(context.Background(), markerPlusTransportError, "true", "/wt")
	if masked.Outcome != verificationExecutionErr || masked.Passed() {
		t.Errorf("transport error was masked by exit marker: %+v", masked)
	}
	before := len(fr.calls)
	invalid := runVerifyCmdRemote(context.Background(), fr.run, "`echo wrapped`", "/wt")
	if invalid.Outcome != verificationInvalid || len(fr.calls) != before {
		t.Errorf("invalid remote command result = %+v; remote calls %d => %d", invalid, before, len(fr.calls))
	}
	realRemote := func(ctx context.Context, command string) (string, error) {
		out, err := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
		return string(out), err
	}
	realFailure := runVerifyCmdRemote(context.Background(), realRemote, "printf remote-output; exit 7", t.TempDir())
	if realFailure.Outcome != verificationFailed || !realFailure.HasExitCode || realFailure.ExitCode != 7 || !strings.Contains(realFailure.Output, "remote-output") {
		t.Errorf("executed remote wrapper result = %+v, want command exit 7 and output", realFailure)
	}
	markerOutput := runVerifyCmdRemote(context.Background(), realRemote, "printf '%s\\n' '__STINT_VERIFY_SETUP__=42'; true", t.TempDir())
	if !markerOutput.Passed() || !strings.Contains(markerOutput.Output, verifySetupMarker+"42") {
		t.Errorf("verifier output collided with setup marker framing: %+v", markerOutput)
	}
	setupFailure := runVerifyCmdRemote(context.Background(), realRemote, "true", filepath.Join(t.TempDir(), "missing-worktree"))
	if setupFailure.Outcome != verificationExecutionErr || !strings.Contains(setupFailure.Error, "could not enter verifier worktree") {
		t.Errorf("remote worktree setup result = %+v, want execution_error", setupFailure)
	}
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	remoteTimeout := runVerifyCmdRemote(timeoutCtx, func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}, "true", "/wt")
	if remoteTimeout.Outcome != verificationTimedOut {
		t.Errorf("remote timeout result = %+v, want timed_out", remoteTimeout)
	}
}

func TestTakeTrailingVerifierMarkerRejectsEmbeddedFrame(t *testing.T) {
	output := "ordinary output\n" + verifyExitMarker + "0\ntrailing verifier output\n"
	if _, _, ok := takeTrailingVerifierMarker(output, verifyExitMarker); ok {
		t.Fatalf("accepted non-trailing verifier marker in %q", output)
	}
	code, body, ok := takeTrailingVerifierMarker("ordinary output\n"+verifyExitMarker+"7\n", verifyExitMarker)
	if !ok || code != 7 || body != "ordinary output" {
		t.Fatalf("trailing marker = (%d, %q, %t), want (7, ordinary output, true)", code, body, ok)
	}
}

func TestVerifyCommandValidationIsSeparateFromRuntimePreflight(t *testing.T) {
	mission := deep.Mission{Verify: "`pnpm test`", Tasks: []deep.Task{{ID: "T-17", Verify: "test -f result.txt"}}}
	if err := validateMissionVerifyCommands(mission); err == nil || !strings.Contains(err.Error(), "mission verification command is invalid") {
		t.Fatalf("pure command validation error = %v, want attributable invalid mission command", err)
	}
	remoteCalls := 0
	err := preflightRemoteVerifyTools(deep.Mission{Verify: "go test ./..."}, func(context.Context, string) (string, error) {
		remoteCalls++
		return "", nil
	})
	if err != nil {
		t.Fatalf("runtime preflight unexpectedly failed: %v", err)
	}
	if remoteCalls != 1 {
		t.Fatalf("remote availability preflight calls = %d, want one independent tool lookup", remoteCalls)
	}

	taskMission := deep.Mission{Tasks: []deep.Task{{ID: "T-17", Verify: "```pnpm test```"}}}
	if err := validateMissionVerifyCommands(taskMission); err == nil || !strings.Contains(err.Error(), "task T-17") {
		t.Fatalf("task preflight error = %v, want task attribution", err)
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
	remoteVerify := func(ctx context.Context, command, workdir string) verificationResult {
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
		finalVerify: func(ctx context.Context, command string) verificationResult {
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
