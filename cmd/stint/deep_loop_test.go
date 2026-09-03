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
		verify:      func(_ context.Context, _, _ string) (string, bool, error) { return "", false, nil },
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

// The live-run calibration gap: a mission-level verify command that is
// broader than any one task's scope parks correctly scoped tasks. A per-task
// verify command closes it: the task is checked by its own command while the
// over-broad mission command still applies to tasks without one.
func TestDeepLoopPerTaskVerifyOverridesMissionVerify(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Verify = "over-broad mission command"
	env.state.Tasks[0].Verify = "scoped task command"
	if err := env.state.SaveDir(env.coord.stateDir); err != nil {
		t.Fatal(err)
	}
	env.coord.verify = func(_ context.Context, command, _ string) (string, bool, error) {
		if strings.Contains(command, "scoped") {
			return "task check ok", true, nil
		}
		return "", false, nil
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	a, b := env.state.Tasks[0], env.state.Tasks[1]
	if a.Status != deep.StatusVerified || a.Attempts != 1 {
		t.Errorf("task A = %+v, want verified on attempt 1 by its own command", a)
	}
	// Task B has no per-task command: the over-broad mission command fails
	// every attempt until the cap, then parks — and that no longer blocks A.
	if b.Status != deep.StatusBlocked || b.Attempts != 3 || !strings.Contains(b.Blocker, "verification") {
		t.Errorf("task B = %+v, want blocked after the cap under the mission command", b)
	}
	if env.state.Phase != deep.PhaseLanded {
		t.Errorf("phase = %s, want landed", env.state.Phase)
	}
	if env.fake.calls != 4 {
		t.Errorf("executor called %d times, want 4 (A x1, B x3)", env.fake.calls)
	}
}

// With no mission-level command, a per-task command verifies its task and
// command-less tasks fall back to completed-invocation acceptance; the
// handoff must keep the two truths apart.
func TestDeepLoopPerTaskVerifyWithoutMissionVerify(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks[0].Verify = "scoped task command"
	if err := env.state.SaveDir(env.coord.stateDir); err != nil {
		t.Fatal(err)
	}
	env.coord.verify = func(_ context.Context, command, _ string) (string, bool, error) {
		if strings.Contains(command, "scoped") {
			return "task check ok", true, nil
		}
		return "", false, nil
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if env.state.Tasks[0].Status != deep.StatusVerified {
		t.Errorf("task A = %s, want verified by its own command", env.state.Tasks[0].Status)
	}
	if env.state.Tasks[1].Status != deep.StatusVerified {
		t.Errorf("task B = %s, want verified (clean completion, no command)", env.state.Tasks[1].Status)
	}
	data, err := os.ReadFile(env.state.HandoffPath)
	if err != nil {
		t.Fatalf("handoff missing: %v", err)
	}
	handoff := string(data)
	if !strings.Contains(handoff, "task verify passed (`scoped task command`)") {
		t.Errorf("handoff missing the task-verify evidence label:\n%s", handoff)
	}
	if !strings.Contains(handoff, "worker reported completion (no verify command defined)") {
		t.Errorf("handoff missing the worker-report label for the command-less task:\n%s", handoff)
	}
}

// The incident log is the audit trail: a full run must record the
// invocations, each verification run (naming the command it ran), and the
// session end — all machine-readable.
func TestDeepLoopRecordsIncidents(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Verify = "true" // a silently-passing command
	if err := env.state.SaveDir(env.coord.stateDir); err != nil {
		t.Fatal(err)
	}
	env.coord.verify = func(_ context.Context, command, _ string) (string, bool, error) {
		return "", true, nil
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	incs, err := deep.ReadIncidents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	var verifyDetails []string
	for _, in := range incs {
		seen[in.Kind]++
		if in.Kind == deep.IncidentVerifyRun {
			verifyDetails = append(verifyDetails, in.Detail)
		}
	}
	if seen[deep.IncidentExecutorInvoke] == 0 {
		t.Error("no executor-invoke incidents: every bounded invocation must be recorded")
	}
	if seen[deep.IncidentVerifyRun] == 0 {
		t.Error("no verify-run incidents: the verification command is the acceptance surface")
	}
	if seen[deep.IncidentLanded] == 0 {
		t.Error("no landed incident: the session end must be recorded")
	}
	for _, d := range verifyDetails {
		if !strings.Contains(d, "command=`true`") || !strings.Contains(d, "result=pass") {
			t.Errorf("verify incident does not name the command and result: %q", d)
		}
	}
}

// A verification command that hangs must be bounded: the coordinator kills
// it at the bound and continues (recorded as a failed run) instead of
// stalling the whole session.
func TestDeepLoopVerifyBounded(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	// "true" is instant in the REAL final verification at landing; the
	// coordinator's own verify seam is faked to hang, which is what is
	// under test (a hung per-attempt verify must not stall the session).
	env.state.Verify = "true"
	if err := env.state.SaveDir(env.coord.stateDir); err != nil {
		t.Fatal(err)
	}
	env.coord.verifyTimeout = 30 * time.Millisecond
	env.coord.verify = func(ctx context.Context, command, _ string) (string, bool, error) {
		<-ctx.Done() // emulate a hung command killed by the bound
		return "", false, nil
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if env.state.Tasks[0].Status != deep.StatusBlocked {
		t.Errorf("task A = %s, want blocked (a hung verify can never pass)", env.state.Tasks[0].Status)
	}
	incs, err := deep.ReadIncidents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	fails := 0
	for _, in := range incs {
		if in.Kind == deep.IncidentVerifyRun && strings.Contains(in.Detail, "result=fail") {
			fails++
		}
	}
	if fails == 0 {
		t.Error("no failed verify-run incidents: the bound kill must be recorded")
	}
}

// The session's command policy must reach the worker: the reconstructed
// prompt names the allow-list and its enforcement mode.
func TestDeepLoopPolicyInPrompt(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.coord.execCfg.allowedCommands = []string{"go test", "git status"}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if env.fake.calls < 1 {
		t.Fatal("no executor invocations")
	}
	prompt := env.fake.prompts[0]
	for _, want := range []string{"COMMAND POLICY", "- go test", "- git status", "auto-approval is OFF"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
