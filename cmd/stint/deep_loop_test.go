package main

import (
	"context"
	"errors"
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
	calls    int
	prompts  []string
	timeouts []time.Duration
	// script maps call number (1-based) to the result for that invocation.
	script    map[int]execResult
	scriptErr map[int]error
	// after (optional) runs after each invocation — tests use it to
	// advance the fake clock mid-run.
	after func()
}

func (f *fakeExecutor) run(_ context.Context, in execInput) (execResult, error) {
	f.calls++
	f.prompts = append(f.prompts, in.prompt)
	f.timeouts = append(f.timeouts, in.timeout)
	r, ok := f.script[f.calls]
	if !ok {
		r = completedResult()
	}
	// Simulate worker output so checkpoint commits have a real diff.
	if in.workdir != "" {
		_ = os.WriteFile(filepath.Join(in.workdir, fmt.Sprintf("work-%d.txt", f.calls)),
			[]byte(fmt.Sprintf("attempt %d", f.calls)), 0o644)
		verifyMarker := filepath.Join(in.workdir, ".stint-verified")
		_ = os.Remove(verifyMarker)
		if r.completed && r.exitCode == 0 {
			_ = os.WriteFile(verifyMarker, []byte("verified"), 0o644)
		}
	}
	if f.after != nil {
		f.after()
	}
	return r, f.scriptErr[f.calls]
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
		Verify:      "test -f .stint-verified",
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
		verify:      runVerifyCmd,
		logf:        func(string, ...any) {},
		out:         io.Discard,
		git:         g,
	}
	return &testEnv{coord: coord, state: statePtr, fake: fake, clock: clk, repo: repo, wt: wt}
}

func completedResult() execResult {
	return execResult{exitCode: 0, completed: true, finishReason: "completed"}
}

func TestOnBoxHandoffUsesOnBoxResumePathAndShowsEarlierLanding(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	state := deep.DeepState{
		SessionID: "20260924-120000", MissionName: "resume fixture", Objective: "continue work",
		Phase: deep.PhaseLanded, Branch: "stint/deep-20260924-120000", StartedAt: at.Add(-time.Hour), Deadline: at.Add(time.Hour), Exec: &deep.ExecSettings{Worker: workerHermesOnBox},
		Tasks:            []deep.Task{{ID: "T-001", Objective: "finish implementation", Status: deep.StatusQueued}},
		PreviousLandings: []deep.LandingRecord{{At: at.Add(-time.Hour), Reason: "time budget exhausted", Commit: strings.Repeat("a", 40), HandoffSHA256: "digest"}},
	}
	handoff := buildHandoff(state, "later landing", at, "passed", deep.RepoSummary{})
	if !strings.Contains(handoff, "`stint deep onbox --resume` in the on-box supervisor context") || strings.Contains(handoff, "`stint deep resume`") {
		t.Fatalf("on-box handoff recommends the wrong resume path:\n%s", handoff)
	}
	if !strings.Contains(handoff, "Earlier landing epochs") || !strings.Contains(handoff, "time budget exhausted") || !strings.Contains(handoff, strings.Repeat("a", 40)) {
		t.Fatalf("earlier landing identity was not retained:\n%s", handoff)
	}
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
	if !strings.Contains(env.fake.prompts[1], "PREVIOUS EXECUTOR RESULT") {
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

// A worker may commit its own verified changes before returning. The coordinator
// must add a distinct acceptance marker and record that exact SHA so every task
// can become one unambiguous layer in the on-box PR stack.
func TestVerifiedTaskRecordsCoordinatorCheckpointAfterWorkerCommit(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	var workerHead string
	env.fake.after = func() {
		for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", "worker checkpoint"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = env.wt
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v (%s)", args, err, out)
			}
		}
		cmd := exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = env.wt
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git rev-parse HEAD: %v (%s)", err, out)
		}
		workerHead = strings.TrimSpace(string(out))
	}

	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	checkpoint := env.state.Tasks[0].CheckpointCommit
	if checkpoint == "" || checkpoint == workerHead {
		t.Fatalf("checkpointCommit = %q, want a coordinator commit after worker HEAD %q", checkpoint, workerHead)
	}
	persisted, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatalf("load persisted state: %v", err)
	}
	if got := persisted.Tasks[0].CheckpointCommit; got != checkpoint {
		t.Fatalf("persisted checkpointCommit = %q, want %q", got, checkpoint)
	}
	cmd := exec.Command("git", "show", "-s", "--format=%P%x00%s", checkpoint)
	cmd.Dir = env.wt
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect checkpoint commit: %v (%s)", err, out)
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\x00", 2)
	if len(parts) != 2 || parts[0] != workerHead || parts[1] != "deep: "+env.state.SessionID+" T-001 verified" {
		t.Fatalf("checkpoint marker = %q, want parent %s and coordinator subject", strings.TrimSpace(string(out)), workerHead)
	}
	finalHead, err := env.coord.git.headCommit(env.wt)
	if err != nil {
		t.Fatalf("final HEAD: %v", err)
	}
	if finalHead == checkpoint {
		t.Fatal("landing handoff should be a later commit than the task checkpoint")
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

func TestDeepLoopRetryUsesCurrentFailureAfterPriorDeferral(t *testing.T) {
	env := newTestEnv(t, map[int]execResult{1: failedResult()}, 1)
	task := &env.state.Tasks[0]
	task.Status = deep.StatusIncomplete
	task.Blocker = "deferred: configured timeout did not fit before landing"
	env.fake.scriptErr = map[int]error{1: errors.New("context deadline exceeded")}
	if err := env.state.SaveDir(env.coord.stateDir); err != nil {
		t.Fatal(err)
	}

	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("runTask: %v", err)
	}
	got := env.state.Tasks[0]
	if got.Status != deep.StatusBlocked {
		t.Fatalf("task status = %s, want blocked", got.Status)
	}
	if !strings.Contains(got.Blocker, "executor failed: context deadline exceeded") {
		t.Fatalf("task blocker = %q, want current executor failure", got.Blocker)
	}
	if strings.Contains(got.Blocker, "deferred:") {
		t.Fatalf("task blocker retained stale deferral reason: %q", got.Blocker)
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
	if env.state.Tasks[0].Status != deep.StatusQueued {
		t.Errorf("task A = %s, want queued (deferred without an invocation)", env.state.Tasks[0].Status)
	}
	if !strings.Contains(env.state.Tasks[0].Blocker, "deferred:") {
		t.Errorf("task defer reason = %q, want explicit budget explanation", env.state.Tasks[0].Blocker)
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

func TestDeepLandingResumesAfterWorktreeHandoffFailure(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	verifyCalls := 0
	env.coord.finalVerify = func(context.Context, string) (string, bool, error) {
		verifyCalls++
		return "checks passed", true, nil
	}
	env.coord.worktreeWrite = func(string, []byte) error { return errors.New("box write failed") }
	if err := env.coord.land(context.Background(), "test landing"); err == nil {
		t.Fatal("land succeeded after worktree handoff write failed")
	}
	fresh, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Phase != deep.PhaseLanding || !fresh.LandingVerifyDone || fresh.LandingHandoff == "" {
		t.Fatalf("durable state after failed landing = phase %s verifyDone=%t handoff=%t, want resumable landing", fresh.Phase, fresh.LandingVerifyDone, fresh.LandingHandoff != "")
	}
	env.coord.worktreeWrite = nil
	if err := env.coord.land(context.Background(), "retry reason must not replace durable reason"); err != nil {
		t.Fatalf("resume landing: %v", err)
	}
	if verifyCalls != 1 {
		t.Errorf("final verification ran %d times, want one persisted result", verifyCalls)
	}
	if env.state.Phase != deep.PhaseLanded || env.state.LandingCommit == "" {
		t.Fatalf("state after resumed landing = %s checkpoint=%q", env.state.Phase, env.state.LandingCommit)
	}
	head, err := env.coord.git.headCommit(env.wt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(head) != env.state.LandingCommit {
		t.Errorf("durable landing checkpoint = %q, worktree HEAD = %q", env.state.LandingCommit, head)
	}
}

func TestDeepLandingReusesCheckpointAfterFinalStateSaveFailure(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.coord.persist = func(dir string, state deep.DeepState) error {
		if state.Phase == deep.PhaseLanded {
			return errors.New("state disk unavailable")
		}
		return state.SaveDir(dir)
	}
	if err := env.coord.land(context.Background(), "test landing"); err == nil {
		t.Fatal("land succeeded after final state persistence failed")
	}
	fresh, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Phase != deep.PhaseLanding || fresh.LandingCommit != "" {
		t.Fatalf("durable state after final-save failure = phase %s checkpoint=%q", fresh.Phase, fresh.LandingCommit)
	}
	firstHead, err := env.coord.git.headCommit(env.wt)
	if err != nil {
		t.Fatal(err)
	}
	env.coord.persist = nil
	if err := env.coord.land(context.Background(), "retry landing"); err != nil {
		t.Fatalf("resume landing: %v", err)
	}
	secondHead, err := env.coord.git.headCommit(env.wt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(firstHead) != strings.TrimSpace(secondHead) {
		t.Errorf("recovery added another commit: first HEAD %q, second %q", firstHead, secondHead)
	}
	if env.state.LandingCommit != strings.TrimSpace(firstHead) {
		t.Errorf("recorded landing SHA = %q, want %q", env.state.LandingCommit, firstHead)
	}
}

func TestDeepLoopDoesNotInvokeWorkerWhenActiveStateCannotPersist(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.coord.persist = func(string, deep.DeepState) error { return errors.New("disk full") }
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err == nil {
		t.Fatal("runTask succeeded after active state persistence failed")
	}
	if env.fake.calls != 0 {
		t.Errorf("executor ran %d times after active state could not be persisted", env.fake.calls)
	}
}

type failingCommitGit struct {
	gitOps
	err error
}

func (g failingCommitGit) commitAll(string, string) (string, error) { return "", g.err }

func TestDeepLoopDoesNotVerifyTaskWhenCheckpointFails(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	env.coord.git = failingCommitGit{gitOps: env.coord.git, err: errors.New("git commit failed")}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err == nil {
		t.Fatal("runTask succeeded after task checkpoint failed")
	}
	if env.state.Tasks[0].Status != deep.StatusNeedsHuman || env.state.Tasks[0].CheckpointCommit != "" {
		t.Fatalf("task after checkpoint failure = status %s checkpoint=%q", env.state.Tasks[0].Status, env.state.Tasks[0].CheckpointCommit)
	}
	fresh, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Tasks[0].Status != deep.StatusNeedsHuman || fresh.Tasks[0].CheckpointCommit != "" {
		t.Errorf("persisted task after checkpoint failure = status %s checkpoint=%q", fresh.Tasks[0].Status, fresh.Tasks[0].CheckpointCommit)
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

// With no mission-level command, a per-task command verifies its task while a
// command-less task is parked for human review even when Hermes exits cleanly.
func TestDeepLoopPerTaskVerifyWithoutMissionVerify(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Verify = ""
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
	if env.state.Tasks[1].Status != deep.StatusNeedsHuman {
		t.Errorf("task B = %s, want needs_human (no independent command)", env.state.Tasks[1].Status)
	}
	data, err := os.ReadFile(env.state.HandoffPath)
	if err != nil {
		t.Fatalf("handoff missing: %v", err)
	}
	handoff := string(data)
	if !strings.Contains(handoff, "executor completed; repository verification passed (`scoped task command`)") {
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
		if in.Kind == deep.IncidentVerifyRun && (strings.Contains(in.Detail, "result=fail") || strings.Contains(in.Detail, "result=error:")) {
			fails++
		}
	}
	if fails == 0 {
		t.Error("no failed verify-run incidents: the bound kill must be recorded")
	}
}

func TestExecutorFailureCannotBeAcceptedByPassingVerification(t *testing.T) {
	env := newTestEnv(t, nil, 1)
	env.state.Tasks = env.state.Tasks[:1]
	env.fake.scriptErr = map[int]error{1: context.DeadlineExceeded}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	task := env.state.Tasks[0]
	if task.Status == deep.StatusVerified {
		t.Fatalf("task was verified after executor timeout: %+v", task)
	}
	if task.Status != deep.StatusBlocked {
		t.Fatalf("task status = %s, want blocked after the only failed attempt", task.Status)
	}
	if task.VerificationResult != "repository verification passed" {
		t.Fatalf("verification evidence = %q, want independent pass", task.VerificationResult)
	}
	if !strings.Contains(task.ExecutionError, "deadline exceeded") || !strings.Contains(task.LastResult, "executor error: context deadline exceeded") {
		t.Fatalf("executor failure evidence was not retained: %+v", task)
	}
	if task.CheckpointCommit != "" || task.VerifiedAt != nil {
		t.Fatalf("failed executor received acceptance identity: %+v", task)
	}
	fresh, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Tasks[0].Status == deep.StatusVerified || fresh.Tasks[0].VerificationResult != "repository verification passed" || fresh.Tasks[0].ExecutionError == "" {
		t.Fatalf("durable task evidence contradicts executor outcome: %+v", fresh.Tasks[0])
	}
}

func TestTimeoutShortensToFitBeforeLandingReserve(t *testing.T) {
	env := newTestEnv(t, nil, 1)
	env.state.Tasks = env.state.Tasks[:1]
	env.coord.taskTimeout = 15 * time.Minute
	env.state.LandBefore = env.clock.now.Add(13*time.Minute + 39*time.Second)
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if env.fake.calls != 1 {
		t.Fatalf("executor calls = %d, want one shortened invocation", env.fake.calls)
	}
	want := 10*time.Minute + 9*time.Second
	if env.fake.timeouts[0] != want {
		t.Fatalf("effective executor timeout = %s, want %s", env.fake.timeouts[0], want)
	}
	task := env.state.Tasks[0]
	if task.ConfiguredTimeoutSec != 900 || task.EffectiveTimeoutSec != int(want.Seconds()) || !strings.Contains(task.TimeoutDecision, "shortened") {
		t.Fatalf("durable timeout decision = %+v", task)
	}
}

func TestNoVerifierTaskDoesNotReserveUnusedVerificationTime(t *testing.T) {
	env := newTestEnv(t, nil, 1)
	env.coord.taskTimeout = time.Minute
	env.state.Verify = ""
	env.state.Tasks = []deep.Task{{ID: "NO-VERIFY-001", Objective: "do useful work", Status: deep.StatusQueued}}
	env.state.LandBefore = env.clock.now.Add(2 * time.Minute)
	if err := env.state.SaveDir(env.coord.stateDir); err != nil {
		t.Fatal(err)
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if env.fake.calls != 1 || env.fake.timeouts[0] != time.Minute {
		t.Fatalf("no-verifier invocation calls/timeouts = %d/%v, want one 1-minute invocation", env.fake.calls, env.fake.timeouts)
	}
	if env.state.Tasks[0].Status != deep.StatusNeedsHuman {
		t.Fatalf("no-verifier task status = %s, want needs_human", env.state.Tasks[0].Status)
	}
}

func TestLegacyVerifiedTaskWithoutVerifierKeepsWorkerOnlyEvidence(t *testing.T) {
	state := deep.DeepState{Verify: "", Tasks: []deep.Task{{ID: "LEGACY-001", Objective: "old task", Status: deep.StatusVerified, LastResult: `finish="completed"`}}}
	evidence := taskHandoffEvidence(state, state.Tasks[0])
	if !strings.Contains(evidence, "legacy verified state; no independent verifier recorded") || strings.Contains(evidence, "repository verification passed") {
		t.Fatalf("legacy no-verifier evidence = %q", evidence)
	}
}

func TestReviewTaskRequiresVerifiedPrerequisite(t *testing.T) {
	env := newTestEnv(t, nil, 2)
	env.state.Tasks = []deep.Task{
		{ID: "IMPLEMENT-001", Objective: "implement change", Status: deep.StatusBlocked},
		{ID: "REVIEW-001", Objective: "review implementation", Status: deep.StatusQueued, DependsOn: []string{"IMPLEMENT-001"}},
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	review := env.state.Tasks[1]
	if review.Status != deep.StatusBlocked || review.Attempts != 0 || !strings.Contains(review.Blocker, "prerequisite IMPLEMENT-001 has status blocked") {
		t.Fatalf("review outcome = %+v, want blocked without execution", review)
	}
	if env.fake.calls != 0 {
		t.Fatalf("executor calls = %d, want no review invocation", env.fake.calls)
	}
}

// The session's command guidance must reach the worker without claiming Stint
// enforces Hermes tool execution.
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
	for _, want := range []string{"COMMAND GUIDANCE (ADVISORY ONLY", "- go test", "- git status", "Stint does not enforce this list"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
