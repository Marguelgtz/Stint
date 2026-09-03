package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

// --- fakes ----------------------------------------------------------------

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

type fakeExecutor struct {
	calls   int
	prompts []string
	// script maps call number (1-based) to the result for that invocation.
	script map[int]execResult
	// after (optional) runs after each invocation — tests use it to
	// advance the fake clock mid-run.
	after func()
}

func (f *fakeExecutor) run(_ context.Context, in execInput) (execResult, error) {
	f.calls++
	f.prompts = append(f.prompts, in.prompt)
	// Simulate worker output so checkpoint commits have a real diff.
	if in.workdir != "" {
		_ = os.WriteFile(filepath.Join(in.workdir, fmt.Sprintf("work-%d.txt", f.calls)),
			[]byte(fmt.Sprintf("attempt %d", f.calls)), 0o644)
	}
	if f.after != nil {
		f.after()
	}
	if r, ok := f.script[f.calls]; ok {
		return r, nil
	}
	return execResult{exitCode: 0, completed: true, finishReason: "completed"}, nil
}

// newTestRepo initializes a git repo with one commit and returns its path.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@stint.local")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-m", "baseline")
	return dir
}

type testEnv struct {
	coord *deepCoordinator
	state *deep.DeepState
	fake  *fakeExecutor
	clock *fakeClock
	repo  string
	wt    string
}

// newTestEnv assembles a coordinator over a real temporary git repo with a
// real worktree (the workspace model under test) and fake executor/clock.
func newTestEnv(t *testing.T, script map[int]execResult, maxAttempts int) *testEnv {
	t.Helper()
	repo := newTestRepo(t)
	base, _ := newGitRunner().repoHead(repo)
	mission := deep.Mission{
		Name: "fixture mission", Objective: "ship the fixture",
		Success:     []string{"the fixture ships"},
		Constraints: []string{"stay in the worktree"},
		Tasks: []deep.Task{
			{ID: "T-001", Objective: "add helper.go", Status: deep.StatusQueued},
			{ID: "T-002", Objective: "add helper_test.go", Status: deep.StatusQueued},
		},
	}
	stateDir := t.TempDir()
	landBefore := time.Date(2026, 2, 9, 17, 0, 0, 0, time.UTC)
	now := time.Date(2026, 2, 9, 16, 0, 0, 0, time.UTC)
	sessionID := "20260209T160000-testsess"
	wt := filepath.Join(repo, ".stint-deep", "testsession")
	g := newGitRunner()
	// The branch must match the one recorded in the state: resume's
	// worktree re-attach relies on that pair.
	if err := g.worktreeAdd(repo, wt, deep.BranchName(sessionID)); err != nil {
		t.Fatalf("worktree: %v", err)
	}
	state := deep.NewState(sessionID, mission, repo, wt,
		landBefore.Add(time.Hour), landBefore, maxAttempts, now)
	state.BaseCommit = base
	state.Verify = "" // acceptance is driven by the fake executor's result
	if err := state.SaveDir(stateDir); err != nil {
		t.Fatalf("save state: %v", err)
	}
	statePtr := &state
	clk := &fakeClock{now: now}
	fake := &fakeExecutor{script: script}
	coord := &deepCoordinator{
		stateDir:    stateDir,
		state:       statePtr,
		execCfg:     execInput{provider: "openai-compatible", model: "test-model"},
		executor:    fake,
		now:         func() time.Time { return clk.now },
		taskTimeout: time.Minute,
		verify:      func(_ context.Context, _ string) (string, bool, error) { return "", false, nil },
		logf:        func(string, ...any) {},
		out:         io.Discard,
		git:         g,
	}
	return &testEnv{coord: coord, state: statePtr, fake: fake, clock: clk, repo: repo, wt: wt}
}

func completedResult() execResult {
	return execResult{exitCode: 0, completed: true, finishReason: "completed", iterations: 1}
}

func failedResult() execResult {
	return execResult{exitCode: 1, completed: false, finishReason: "error"}
}

// --- scenarios --------------------------------------------------------------

// Gate C at the unit level: attempt 1 leaves the task incomplete; the next
// fresh invocation reconstructs context from durable state + git and
// completes the task — no conversational memory, no user "continue".
func TestDeepLoopContinuation(t *testing.T) {
	env := newTestEnv(t, map[int]execResult{1: failedResult()}, 3)
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if env.state.Phase != deep.PhaseLanded {
		t.Errorf("phase = %s, want landed", env.state.Phase)
	}
	a := env.state.Tasks[0]
	if a.Status != deep.StatusVerified || a.Attempts != 2 {
		t.Errorf("task A = %s attempts=%d, want verified/2", a.Status, a.Attempts)
	}
	if b := env.state.Tasks[1]; b.Status != deep.StatusVerified {
		t.Errorf("task B = %s, want verified", b.Status)
	}
	// The reconstructed prompt for attempt 2 must carry task context.
	if got := env.fake.prompts[1]; !strings.Contains(got, "add helper.go") {
		t.Errorf("attempt-2 prompt lost the task objective:\n%s", got)
	}
	if !strings.Contains(env.fake.prompts[1], "stint/deep-20260209T160000-testsess") {
		t.Errorf("attempt-2 prompt lost the branch context:\n%s", env.fake.prompts[1])
	}
	if !strings.Contains(env.fake.prompts[1], "PREVIOUS ATTEMPT RESULT") {
		t.Errorf("attempt-2 prompt lost the previous attempt evidence:\n%s", env.fake.prompts[1])
	}
	if env.state.HandoffPath == "" {
		t.Errorf("handoff path not recorded")
	}
	// The worktree must contain checkpoint commits beyond the baseline.
	head, err := env.coord.git.headCommit(env.wt)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head == env.state.BaseCommit {
		t.Errorf("no checkpoint commits landed in the worktree")
	}
}

// Gate D at the unit level: a task that cannot reach verification is parked
// as blocked after its attempt cap and the coordinator continues with the
// next useful work; the handoff is truthful about the split.
func TestDeepLoopParkAndContinue(t *testing.T) {
	env := newTestEnv(t, map[int]execResult{1: failedResult(), 2: failedResult()}, 2)
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	a, b := env.state.Tasks[0], env.state.Tasks[1]
	if a.Status != deep.StatusBlocked || a.Attempts != 2 || a.Blocker == "" {
		t.Errorf("task A = %s attempts=%d blocker=%q, want blocked/2/non-empty", a.Status, a.Attempts, a.Blocker)
	}
	if b.Status != deep.StatusVerified {
		t.Errorf("task B = %s, want verified", b.Status)
	}
	if env.state.Phase != deep.PhaseLanded {
		t.Errorf("phase = %s, want landed", env.state.Phase)
	}
	data, err := os.ReadFile(env.state.HandoffPath)
	if err != nil {
		t.Fatalf("handoff missing: %v", err)
	}
	handoff := string(data)
	if !strings.Contains(handoff, "blocked") || !strings.Contains(handoff, "Remaining work") {
		t.Errorf("handoff does not report the blocked task honestly:\n%s", handoff)
	}
}

// The landing window: once the clock passes landBefore mid-session, the
// coordinator stops starting new work and lands truthfully, leaving later
// tasks queued for an honest handoff.
func TestDeepLoopLandsOnClock(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.fake.after = func() { env.clock.advance(71 * time.Minute) }
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if env.state.Phase != deep.PhaseLanded {
		t.Errorf("phase = %s, want landed after clock advanced", env.state.Phase)
	}
	if b := env.state.Tasks[1]; b.Status != deep.StatusQueued {
		t.Errorf("task B = %s, want queued (never started past the landing window)", b.Status)
	}
	if env.fake.calls != 1 {
		t.Errorf("executor called %d times, want 1", env.fake.calls)
	}
}

// Time-budget parking: with a task timeout that would overrun the landing
// window, a queued task is parked instead of started.
func TestDeepLoopParksWhenBudgetExhausted(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.clock.advance(59*time.Minute + 30*time.Second) // now+taskTimeout would pass landBefore
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if env.state.Tasks[0].Status != deep.StatusBlocked {
		t.Errorf("task A = %s, want blocked (not started)", env.state.Tasks[0].Status)
	}
	if env.fake.calls != 0 {
		t.Errorf("executor called %d times, want 0", env.fake.calls)
	}
	if env.state.Phase != deep.PhaseLanded {
		t.Errorf("phase = %s, want landed", env.state.Phase)
	}
}

// External stop: a second process lands the session; the running coordinator
// must observe the phase change and exit without further invocations.
func TestDeepLoopStopsOnExternalLanding(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.clock.advance(59 * time.Minute)
	stopper := &deepCoordinator{
		stateDir:    env.coord.stateDir,
		state:       env.state, // same durable state
		taskTimeout: time.Minute,
		verify:      env.coord.verify,
		now:         func() time.Time { return env.clock.now },
		logf:        func(string, ...any) {},
		out:         io.Discard,
		git:         env.coord.git,
	}
	if err := stopper.land(context.Background(), "stopped by user"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	before := env.fake.calls
	if fresh, err := deep.LoadState(env.coord.stateDir, env.state.SessionID); err == nil {
		env.coord.state = &fresh
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if env.fake.calls != before {
		t.Errorf("executor ran %d more times after external landing", env.fake.calls-before)
	}
}

func TestRunVerifyCmd(t *testing.T) {
	dir := t.TempDir()
	if out, ok, err := runVerifyCmd(context.Background(), "echo verified", dir); err != nil || !ok || !strings.Contains(out, "verified") {
		t.Errorf("pass case: ok=%v err=%v out=%q", ok, err, out)
	}
	if _, ok, _ := runVerifyCmd(context.Background(), "exit 3", dir); ok {
		t.Errorf("exit 3 reported as passing")
	}
}

func TestLandingDeadline(t *testing.T) {
	now := time.Date(2026, 2, 9, 16, 0, 0, 0, time.UTC)
	long := landingDeadline(now.Add(2*time.Hour), now)
	if got := now.Add(2 * time.Hour).Sub(long); got != 10*time.Minute {
		t.Errorf("2h window lands %s before deadline, want 10m", got)
	}
	short := landingDeadline(now.Add(20*time.Minute), now)
	if got := now.Add(20 * time.Minute).Sub(short); got != 5*time.Minute {
		t.Errorf("20m window lands %s before deadline, want 5m (quarter)", got)
	}
}

// A silent passing verification command (test/grep -q) must be reported as
// passed in the handoff, not as "did not run" (live-run finding, F-DW-010).
func TestDeepLandingSilentVerifyReported(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Verify = "true" // silent passing command
	if err := env.state.SaveDir(env.coord.stateDir); err != nil {
		t.Fatal(err) // landing works from durable state only
	}
	if err := env.coord.land(context.Background(), "test landing"); err != nil {
		t.Fatalf("land: %v", err)
	}
	if env.state.Phase != deep.PhaseLanded {
		t.Errorf("phase = %s, want landed", env.state.Phase)
	}
	data, err := os.ReadFile(env.state.HandoffPath)
	if err != nil {
		t.Fatalf("handoff missing: %v", err)
	}
	handoff := string(data)
	if strings.Contains(handoff, "did not run") {
		t.Errorf("silent passing verify misreported as not run:\n%s", handoff)
	}
	if !strings.Contains(handoff, "passed") {
		t.Errorf("handoff does not report the passing verify:\n%s", handoff)
	}
}

// An external stop that lands the session while a task's invocation is in
// flight must win: the in-flight task's saves may not resurrect the stopped
// session from stale in-memory state (the zombie-proof invariant behind
// `stint deep resume`).
func TestDeepLoopExternalStopBeatsInFlightTask(t *testing.T) {
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
	env.fake.after = func() {
		_ = stopper.land(context.Background(), "stopped by user")
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	fresh, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Phase != deep.PhaseLanded {
		t.Errorf("phase = %s, want landed (an in-flight save must not resurrect the session)", fresh.Phase)
	}
	if env.fake.calls != 1 {
		t.Errorf("executor called %d times, want 1 (nothing runs after the stop)", env.fake.calls)
	}
	if fresh.Tasks[0].Status == deep.StatusVerified {
		t.Errorf("in-flight outcome was persisted after the stop: %s", fresh.Tasks[0].Status)
	}
}
