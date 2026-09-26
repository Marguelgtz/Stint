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
	before   func(execInput)
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
	if f.before != nil {
		f.before(in)
	}
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
		PreviousLandings: []deep.LandingRecord{{At: at.Add(-time.Hour), Reason: "time budget exhausted", Commit: strings.Repeat("a", 40), MissionOutcome: deep.MissionOutcomeIncomplete, HandoffSHA256: "digest"}},
	}
	handoff := buildHandoff(state, "later landing", at, "passed", deep.RepoSummary{})
	if !strings.Contains(handoff, "`stint deep onbox --resume` in the on-box supervisor context") || strings.Contains(handoff, "`stint deep resume`") {
		t.Fatalf("on-box handoff recommends the wrong resume path:\n%s", handoff)
	}
	if !strings.Contains(handoff, "Earlier landing epochs") || !strings.Contains(handoff, "time budget exhausted") || !strings.Contains(handoff, "incomplete") || !strings.Contains(handoff, strings.Repeat("a", 40)) {
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
	if env.state.MissionOutcome != deep.MissionOutcomeSucceeded {
		t.Errorf("mission outcome = %s, want succeeded after bound task and final verification evidence", env.state.MissionOutcome)
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

// A worker may commit its own verified changes before returning. When that
// commit already represents the exact verified tree, the task checkpoint
// reuses it instead of creating an empty coordinator marker.
func TestVerifiedTaskReusesWorkerCommitWhenTreeMatches(t *testing.T) {
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
	if checkpoint == "" || checkpoint != workerHead {
		t.Fatalf("checkpointCommit = %q, want existing worker HEAD %q", checkpoint, workerHead)
	}
	persisted, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatalf("load persisted state: %v", err)
	}
	if got := persisted.Tasks[0].CheckpointCommit; got != checkpoint {
		t.Fatalf("persisted checkpointCommit = %q, want %q", got, checkpoint)
	}
	if persisted.Tasks[0].VerificationSubject == nil {
		t.Fatal("persisted task has no verification subject")
	}
	if persisted.Tasks[0].CheckpointTreeSHA == "" || persisted.Tasks[0].CheckpointTreeSHA != persisted.Tasks[0].VerificationSubject.TreeSHA {
		t.Fatalf("checkpoint tree %q does not match verification subject %+v", persisted.Tasks[0].CheckpointTreeSHA, persisted.Tasks[0].VerificationSubject)
	}
	cmd := exec.Command("git", "rev-parse", checkpoint+"^{tree}")
	cmd.Dir = env.wt
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect checkpoint tree: %v (%s)", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != persisted.Tasks[0].VerificationSubject.TreeSHA {
		t.Fatalf("checkpoint tree = %q, want verified tree %q", got, persisted.Tasks[0].VerificationSubject.TreeSHA)
	}
	finalHead, err := env.coord.git.headCommit(env.wt)
	if err != nil {
		t.Fatalf("final HEAD: %v", err)
	}
	if finalHead != checkpoint {
		t.Fatalf("landing added a marker-only commit %s after task checkpoint %s", finalHead, checkpoint)
	}
}

func TestNoOpCheckpointReusesHeadWithoutEmptyCommit(t *testing.T) {
	repo := newTestRepo(t)
	git := newGitRunner()
	before, err := git.repoHead(repo)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := git.verificationSubject(repo, nil)
	if err != nil {
		t.Fatalf("capture no-op subject: %v", err)
	}
	countBefore, err := exec.Command("git", "-C", repo, "rev-list", "--count", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, tree, err := git.checkpointSubject(repo, "deep(T-001): no-op checkpoint", subject)
	if err != nil {
		t.Fatalf("checkpoint no-op subject: %v", err)
	}
	if checkpoint != before || tree != subject.Subject.TreeSHA {
		t.Fatalf("no-op checkpoint = %q/%q, want existing HEAD/tree %q/%q", checkpoint, tree, before, subject.Subject.TreeSHA)
	}
	countAfter, err := exec.Command("git", "-C", repo, "rev-list", "--count", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(countAfter)) != strings.TrimSpace(string(countBefore)) {
		t.Fatalf("no-op task created a Git commit: count %s -> %s", strings.TrimSpace(string(countBefore)), strings.TrimSpace(string(countAfter)))
	}
}

func TestMutationAfterVerificationInvalidatesSubjectBeforeCheckpoint(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	env.coord.verify = func(_ context.Context, command, workdir string) verificationResult {
		if err := os.WriteFile(filepath.Join(workdir, "mutated-after-verifier.txt"), []byte("new state"), 0o644); err != nil {
			t.Fatalf("mutate repository after verifier: %v", err)
		}
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	err := env.coord.runTask(context.Background(), 0, env.clock.now)
	if err == nil || !strings.Contains(err.Error(), "verification subject changed") {
		t.Fatalf("runTask error = %v, want subject-mutation failure", err)
	}
	task := env.state.Tasks[0]
	if task.Status != deep.StatusNeedsHuman || task.CheckpointCommit != "" || task.CheckpointTreeSHA != "" {
		t.Fatalf("mutated verification accepted a checkpoint: status=%s commit=%q tree=%q", task.Status, task.CheckpointCommit, task.CheckpointTreeSHA)
	}
	if task.VerificationSubject == nil || !strings.Contains(task.VerificationResult, "verification passed") {
		t.Fatalf("did not preserve the original passing evidence and subject: %+v", task)
	}
	head, err := env.coord.git.headCommit(env.wt)
	if err != nil {
		t.Fatal(err)
	}
	if head != env.state.BaseCommit {
		t.Fatalf("mutated state received a checkpoint commit: HEAD=%s baseline=%s", head, env.state.BaseCommit)
	}
}

type mutateAtCheckpointGit struct {
	gitOps
	worktree string
	path     string
}

func (g mutateAtCheckpointGit) checkpointSubject(dir, message string, snapshot verificationSnapshot) (string, string, error) {
	if err := os.WriteFile(filepath.Join(g.worktree, g.path), []byte("mutated at checkpoint boundary\n"), 0o644); err != nil {
		return "", "", err
	}
	return g.gitOps.checkpointSubject(dir, message, snapshot)
}

func TestMutationAtCheckpointBoundaryCannotReuseVerifiedSubject(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	env.coord.git = mutateAtCheckpointGit{gitOps: env.coord.git, worktree: env.wt, path: "mutated-at-checkpoint.txt"}
	err := env.coord.runTask(context.Background(), 0, env.clock.now)
	if err == nil || !strings.Contains(err.Error(), "exact-state checkpoint failed") {
		t.Fatalf("runTask error = %v, want checkpoint-boundary mutation failure", err)
	}
	task := env.state.Tasks[0]
	if task.Status != deep.StatusNeedsHuman || task.CheckpointCommit != "" || task.CheckpointTreeSHA != "" {
		t.Fatalf("checkpoint-boundary mutation was accepted: status=%s commit=%q tree=%q", task.Status, task.CheckpointCommit, task.CheckpointTreeSHA)
	}
}

func TestHeadChangeAfterVerificationInvalidatesSubjectEvenWithSameTree(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	env.coord.verify = func(_ context.Context, command, workdir string) verificationResult {
		cmd := exec.Command("git", "add", "-A")
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("stage worktree after verifier: %v (%s)", err, out)
		}
		cmd = exec.Command("git", "commit", "-m", "external same-tree commit")
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("commit same tree after verifier: %v (%s)", err, out)
		}
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	err := env.coord.runTask(context.Background(), 0, env.clock.now)
	if err == nil || !strings.Contains(err.Error(), "verification subject changed") {
		t.Fatalf("runTask error = %v, want HEAD-identity mismatch", err)
	}
	task := env.state.Tasks[0]
	if task.Status != deep.StatusNeedsHuman || task.CheckpointCommit != "" || task.VerificationSubject == nil {
		t.Fatalf("same-tree HEAD change was accepted: %+v", task)
	}
}

func TestVerificationSubjectUsesWorktreeContentOverStagedBlob(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	env.fake.after = func() {
		readme := filepath.Join(env.wt, "README.md")
		if err := os.WriteFile(readme, []byte("staged version\n"), 0o644); err != nil {
			t.Fatalf("write staged README: %v", err)
		}
		cmd := exec.Command("git", "add", "README.md")
		cmd.Dir = env.wt
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("stage README: %v (%s)", err, out)
		}
		if err := os.WriteFile(readme, []byte("worktree version\n"), 0o644); err != nil {
			t.Fatalf("write unstaged README: %v", err)
		}
	}
	env.coord.verify = func(_ context.Context, command, workdir string) verificationResult {
		contents, err := os.ReadFile(filepath.Join(workdir, "README.md"))
		if err != nil || string(contents) != "worktree version\n" {
			t.Fatalf("verifier worktree README = %q, err=%v", contents, err)
		}
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("runTask: %v", err)
	}
	task := env.state.Tasks[0]
	if task.Status != deep.StatusVerified || task.VerificationSubject == nil || task.CheckpointTreeSHA != task.VerificationSubject.TreeSHA {
		t.Fatalf("tracked worktree change was not checkpointed exactly: %+v", task)
	}
	contents, err := exec.Command("git", "-C", env.wt, "show", task.CheckpointCommit+":README.md").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "worktree version\n" {
		t.Fatalf("checkpoint captured staged content %q, want verifier-visible worktree content", contents)
	}
	message, err := exec.Command("git", "-C", env.wt, "show", "-s", "--format=%s", task.CheckpointCommit).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(message)), "deep(T-001): add helper.go"; got != want {
		t.Fatalf("semantic checkpoint message = %q, want %q", got, want)
	}
}

func TestVerificationSubjectRejectsDirtySubmoduleWorktree(t *testing.T) {
	repo := newTestRepo(t)
	submodule := newTestRepo(t)
	cmd := exec.Command("git", "-C", repo, "-c", "protocol.file.allow=always", "submodule", "add", submodule, "deps/inner")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("add local submodule: %v (%s)", err, out)
	}
	cmd = exec.Command("git", "-C", repo, "add", "-A")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("stage superproject submodule: %v (%s)", err, out)
	}
	cmd = exec.Command("git", "-C", repo, "commit", "-m", "add submodule")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit superproject submodule: %v (%s)", err, out)
	}
	cleanSnapshot, err := newGitRunner().verificationSubject(repo, nil)
	if err != nil {
		t.Fatalf("capture clean initialized submodule: %v", err)
	}
	if cleanSnapshot.Subject.TreeSHA == "" {
		t.Fatal("clean submodule was missing from the product tree")
	}
	if err := os.WriteFile(filepath.Join(repo, "deps", "inner", "dirty.txt"), []byte("uncommitted nested input\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := newGitRunner().verificationSubject(repo, nil); err == nil || !strings.Contains(err.Error(), "dirty submodule") {
		t.Fatalf("dirty submodule subject error = %v, want fail-closed dirty-submodule error", err)
	}
}

func TestUntrackedActionPlanIsSeparateFromProductCheckpoint(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	const planPath = "plans/current.md"
	env.coord.execCfg.actionPlan = planPath
	env.fake.after = func() {
		if err := os.MkdirAll(filepath.Join(env.wt, "plans"), 0o755); err != nil {
			t.Fatalf("create plan directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(env.wt, planPath), []byte("run bookkeeping plan\n"), 0o644); err != nil {
			t.Fatalf("write action plan: %v", err)
		}
	}
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("runTask: %v", err)
	}
	task := env.state.Tasks[0]
	if task.Status != deep.StatusVerified || task.VerificationSubject == nil {
		t.Fatalf("task did not bind a verified subject: %+v", task)
	}
	if got := task.VerificationBookkeeping[planPath]; !strings.HasPrefix(got, "git-blob:") {
		t.Fatalf("action-plan identity = %q, want separate Git blob identity", got)
	}
	if task.CheckpointTreeSHA != task.VerificationSubject.TreeSHA {
		t.Fatalf("checkpoint tree %q differs from product subject %q", task.CheckpointTreeSHA, task.VerificationSubject.TreeSHA)
	}
	listed, err := exec.Command("git", "-C", env.wt, "ls-tree", "-r", "--name-only", task.CheckpointCommit, "--", planPath).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(listed)) != "" {
		t.Fatalf("untracked Stint action plan entered product checkpoint: %q", listed)
	}
	status, err := env.coord.git.statusShort(env.wt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "?? plans/") {
		t.Fatalf("bookkeeping file should remain outside the product checkpoint, status=%q", status)
	}
	if err := env.coord.land(context.Background(), "test landing"); err != nil {
		t.Fatalf("land: %v", err)
	}
	listed, err = exec.Command("git", "-C", env.wt, "ls-tree", "-r", "--name-only", env.state.LandingCommit, "--", planPath, deepWorktreeHandoff).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(listed)) != "" {
		t.Fatalf("Stint run bookkeeping entered landing product tree: %q", listed)
	}
}

func TestStagedActionPlanIsIncludedInProductCheckpoint(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	const planPath = "plans/current.md"
	env.coord.execCfg.actionPlan = planPath
	env.fake.after = func() {
		if err := os.MkdirAll(filepath.Join(env.wt, "plans"), 0o755); err != nil {
			t.Fatalf("create plan directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(env.wt, planPath), []byte("intentional mission output\n"), 0o644); err != nil {
			t.Fatalf("write action plan: %v", err)
		}
		cmd := exec.Command("git", "add", "--", planPath)
		cmd.Dir = env.wt
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("stage intentional action-plan output: %v (%s)", err, out)
		}
	}
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("runTask: %v", err)
	}
	task := env.state.Tasks[0]
	if task.Status != deep.StatusVerified || task.VerificationSubject == nil {
		t.Fatalf("task did not bind a verified subject: %+v", task)
	}
	if _, ok := task.VerificationBookkeeping[planPath]; ok {
		t.Fatalf("Git-visible action plan was incorrectly classified as bookkeeping: %+v", task.VerificationBookkeeping)
	}
	listed, err := exec.Command("git", "-C", env.wt, "ls-tree", "-r", "--name-only", task.CheckpointCommit, "--", planPath).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(listed)) != planPath {
		t.Fatalf("staged action-plan output missing from product checkpoint: %q", listed)
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
	if env.state.MissionOutcome != deep.MissionOutcomeIncomplete {
		t.Errorf("mission outcome = %s, want incomplete with a blocked task", env.state.MissionOutcome)
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
	fresh, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatalf("reload durable state: %v", err)
	}
	if fresh.Tasks[0].Blocker != got.Blocker || fresh.Tasks[0].Status != got.Status {
		t.Fatalf("durable task = %s blocker %q, want %s blocker %q", fresh.Tasks[0].Status, fresh.Tasks[0].Blocker, got.Status, got.Blocker)
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
	passed := runVerifyCmd(context.Background(), "echo verified", dir)
	if !passed.Passed() || !strings.Contains(passed.Output, "verified") || !passed.HasExitCode || passed.ExitCode != 0 {
		t.Errorf("pass case: result=%+v", passed)
	}
	failed := runVerifyCmd(context.Background(), "exit 3", dir)
	if failed.Outcome != verificationFailed || !failed.HasExitCode || failed.ExitCode != 3 {
		t.Errorf("exit 3 result = %+v, want failed with exit 3", failed)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	timedOut := runVerifyCmd(ctx, "sleep 1", dir)
	if timedOut.Outcome != verificationTimedOut {
		t.Errorf("timeout result = %+v, want timed_out", timedOut)
	}
	launchFailure := runVerifyCmd(context.Background(), "true", filepath.Join(dir, "missing-worktree"))
	if launchFailure.Outcome != verificationExecutionErr || launchFailure.Error == "" {
		t.Errorf("shell launch failure result = %+v, want execution_error with cause", launchFailure)
	}
	invalid := runVerifyCmd(context.Background(), "`touch sentinel`", dir)
	if invalid.Outcome != verificationInvalid {
		t.Errorf("Markdown-wrapped command result = %+v, want invalid_command", invalid)
	}
	if _, err := os.Stat(filepath.Join(dir, "sentinel")); !os.IsNotExist(err) {
		t.Errorf("invalid command executed before rejection; sentinel stat error = %v", err)
	}
	lateWriter := filepath.Join(dir, "late-verifier-write")
	command := "(sleep 0.2; printf late > " + shellQuote(lateWriter) + ") & true"
	quiesced := runVerifyCmd(context.Background(), command, dir)
	returnedAt := time.Now()
	if !quiesced.Passed() {
		t.Fatalf("verifier with background writer = %+v, want passed", quiesced)
	}
	assertNoDelayedMutationAfterReturn(t, lateWriter, returnedAt)
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
	env.coord.finalVerify = func(context.Context, string) verificationResult {
		verifyCalls++
		return verificationResult{Outcome: verificationPassed, Output: "checks passed"}
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

func TestDeepLandingRerunsFinalVerifierWhenSubjectChangedOnResume(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	verifyCalls := 0
	env.coord.finalVerify = func(context.Context, string) verificationResult {
		verifyCalls++
		return verificationResult{Outcome: verificationPassed, Output: "checks passed"}
	}
	env.coord.worktreeWrite = func(string, []byte) error { return errors.New("box write failed") }
	if err := env.coord.land(context.Background(), "test landing"); err == nil {
		t.Fatal("land succeeded after worktree handoff write failed")
	}
	firstSubject := env.state.LandingVerificationSubject
	if firstSubject == nil || !env.state.LandingVerifyDone {
		t.Fatalf("final verifier result lacks a durable subject: %+v", env.state)
	}
	productFile := filepath.Join(env.wt, "README.md")
	if err := os.WriteFile(productFile, []byte("# changed after final verification\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.coord.worktreeWrite = nil
	if err := env.coord.land(context.Background(), "retry landing"); err != nil {
		t.Fatalf("resume landing after product mutation: %v", err)
	}
	if verifyCalls != 2 {
		t.Fatalf("final verifier ran %d times, want rerun after subject changed", verifyCalls)
	}
	if env.state.LandingVerificationSubject == nil || env.state.LandingVerificationSubject.TreeSHA == firstSubject.TreeSHA {
		t.Fatalf("resumed final verification did not bind the changed tree: first=%+v current=%+v", firstSubject, env.state.LandingVerificationSubject)
	}
	if env.state.LandingCheckpointTreeSHA != env.state.LandingVerificationSubject.TreeSHA {
		t.Fatalf("landing checkpoint tree %q differs from final verifier tree %q", env.state.LandingCheckpointTreeSHA, env.state.LandingVerificationSubject.TreeSHA)
	}
}

func TestDeepLandingDoesNotReuseLegacyFinalVerificationWithoutSubject(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Phase = deep.PhaseLanding
	env.state.LandingVerifyDone = true
	env.state.LandingVerify = "passed (legacy result without subject)"
	if err := env.state.SaveDir(env.coord.stateDir); err != nil {
		t.Fatal(err)
	}
	verifyCalls := 0
	env.coord.finalVerify = func(context.Context, string) verificationResult {
		verifyCalls++
		return verificationResult{Outcome: verificationPassed}
	}
	if err := env.coord.land(context.Background(), "resume legacy landing"); err != nil {
		t.Fatalf("land: %v", err)
	}
	if verifyCalls != 1 || env.state.LandingVerificationSubject == nil || env.state.LandingCheckpointTreeSHA != env.state.LandingVerificationSubject.TreeSHA {
		t.Fatalf("legacy final verification was reused without provenance: calls=%d state=%+v", verifyCalls, env.state)
	}
}

func TestDeepLandingRejectsMutationDuringFinalVerification(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.coord.finalVerify = func(context.Context, string) verificationResult {
		if err := os.WriteFile(filepath.Join(env.wt, "README.md"), []byte("# changed while verifying\n"), 0o644); err != nil {
			t.Errorf("mutate product during verification: %v", err)
		}
		return verificationResult{Outcome: verificationPassed}
	}
	if err := env.coord.land(context.Background(), "test landing"); err == nil || !strings.Contains(err.Error(), "changed during final verification") {
		t.Fatalf("land error = %v, want exact-subject mutation failure", err)
	}
	if env.state.Phase != deep.PhaseLanding || env.state.LandingVerifyDone || env.state.LandingVerificationSubject != nil || env.state.LandingCommit != "" {
		t.Fatalf("mutation was persisted as a final verification/checkpoint: %+v", env.state)
	}
}

func TestDeepLandingStopsWhenFinalVerifierQuiescenceIsUnknown(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.coord.finalVerify = func(context.Context, string) verificationResult {
		return verificationResult{Outcome: verificationExecutionErr, Error: "remote channel lost", QuiescenceUnconfirmed: true}
	}
	if err := env.coord.land(context.Background(), "test landing"); err == nil || !strings.Contains(err.Error(), "final verifier process quiescence is unconfirmed") {
		t.Fatalf("land error = %v, want final verifier quiescence block", err)
	}
	if !env.state.ExecutionQuiescenceUnconfirmed || env.state.ExecutionQuiescenceTaskID != "mission-final-verifier" || env.state.LandingVerifyDone || env.state.LandingCommit != "" {
		t.Fatalf("unquiesced final verifier was treated as stable: %+v", env.state)
	}
}

func TestDeepLandingDoesNotRewriteGitVisibleHandoffInput(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	path := filepath.Join(env.wt, deepWorktreeHandoff)
	if err := os.WriteFile(path, []byte("product-owned handoff input\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := env.coord.git.commitAll(env.wt, "add product-owned handoff input"); err != nil {
		t.Fatal(err)
	}
	env.coord.finalVerify = func(context.Context, string) verificationResult {
		return verificationResult{Outcome: verificationPassed}
	}
	if err := env.coord.land(context.Background(), "test landing"); err != nil {
		t.Fatalf("land: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "product-owned handoff input\n" {
		t.Fatalf("landing rewrote a Git-visible handoff input: %q", data)
	}
	if env.state.LandingCheckpointTreeSHA != env.state.LandingVerificationSubject.TreeSHA {
		t.Fatalf("checkpoint tree %q differs from verified tree %q", env.state.LandingCheckpointTreeSHA, env.state.LandingVerificationSubject.TreeSHA)
	}
}

func TestDeepLandingReusesCheckpointAfterFinalStateSaveFailure(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.coord.persist = func(dir string, state *deep.DeepState) error {
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
	env.coord.persist = func(string, *deep.DeepState) error { return errors.New("disk full") }
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

func (g failingCommitGit) checkpointSubject(string, string, verificationSnapshot) (string, string, error) {
	return "", "", g.err
}

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

func TestDeepLoopRefusesVerificationWhenExecutorQuiescenceIsUnknown(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.fake.scriptErr = map[int]error{1: errExecutorQuiescenceUnconfirmed}
	verifyCalls := 0
	env.coord.verify = func(context.Context, string, string) verificationResult {
		verifyCalls++
		return verificationResult{Outcome: verificationPassed}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err == nil || !strings.Contains(err.Error(), "quiescence is unconfirmed") {
		t.Fatalf("runTask error = %v, want explicit quiescence failure", err)
	}
	if verifyCalls != 0 {
		t.Fatalf("task verifier ran %d times while executor writers may still be active", verifyCalls)
	}
	if !env.state.ExecutionQuiescenceUnconfirmed || env.state.ExecutionQuiescenceTaskID != "T-001" || env.state.Tasks[0].Status != deep.StatusNeedsHuman {
		t.Fatalf("unconfirmed executor state was not durably blocked: %+v", env.state)
	}
	if err := env.coord.run(context.Background()); err == nil || !strings.Contains(err.Error(), "executor writers may still be active") {
		t.Fatalf("resume error = %v, want global fail-closed block", err)
	}
	if env.fake.calls != 1 || verifyCalls != 0 {
		t.Fatalf("after resume: executor calls=%d verifier calls=%d, want 1/0", env.fake.calls, verifyCalls)
	}
}

func TestDeepLoopBlocksCheckpointWhenVerifierQuiescenceIsUnknown(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	verifyCalls := 0
	env.coord.verify = func(context.Context, string, string) verificationResult {
		verifyCalls++
		return verificationResult{Outcome: verificationExecutionErr, Error: "remote transport lost", QuiescenceUnconfirmed: true}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err == nil || !strings.Contains(err.Error(), "verifier process quiescence is unconfirmed") {
		t.Fatalf("runTask error = %v, want verifier quiescence block", err)
	}
	if verifyCalls != 1 || env.state.Tasks[0].Status != deep.StatusNeedsHuman || !env.state.ExecutionQuiescenceUnconfirmed || env.state.Tasks[0].CheckpointCommit != "" {
		t.Fatalf("unquiesced verifier produced unsafe task state: calls=%d state=%+v", verifyCalls, env.state)
	}
}

// An external stop that lands the session while a task's invocation is in
// flight must win: the in-flight task's saves may not resurrect the stopped
// session from stale in-memory state (the zombie-proof invariant behind
// `stint deep resume`).
func TestDeepLoopExternalStopBeatsInFlightTask(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	executorActive := true
	verifiedWhileExecutorActive := false
	finalVerifyCalls := 0
	finalVerify := func(context.Context, string) verificationResult {
		finalVerifyCalls++
		if executorActive {
			verifiedWhileExecutorActive = true
		}
		return verificationResult{Outcome: verificationPassed}
	}
	env.coord.finalVerify = finalVerify
	stopperState, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	stopper := &deepCoordinator{
		stateDir:    env.coord.stateDir,
		state:       &stopperState,
		taskTimeout: time.Minute,
		verify:      env.coord.verify,
		finalVerify: finalVerify,
		now:         func() time.Time { return env.clock.now },
		logf:        func(string, ...any) {},
		out:         io.Discard,
		git:         env.coord.git,
	}
	env.fake.after = func() {
		_ = stopper.land(context.Background(), "stopped by user")
		executorActive = false
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
	if verifiedWhileExecutorActive || finalVerifyCalls != 1 {
		t.Errorf("final verifier ran while executor active=%t and was called %d times; want one post-quiescence run", verifiedWhileExecutorActive, finalVerifyCalls)
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
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		if strings.Contains(command, "scoped") {
			return verificationResult{Command: command, Outcome: verificationPassed, Output: "task check ok"}
		}
		return verificationResult{Command: command, Outcome: verificationFailed}
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
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		if strings.Contains(command, "scoped") {
			return verificationResult{Command: command, Outcome: verificationPassed, Output: "task check ok"}
		}
		return verificationResult{Command: command, Outcome: verificationFailed}
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
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationPassed}
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
		if !strings.Contains(d, "command=`true`") || !strings.Contains(d, "outcome=passed") {
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
	env.coord.verify = func(ctx context.Context, command, _ string) verificationResult {
		<-ctx.Done() // emulate a hung command killed by the bound
		return verificationResult{Command: command, Outcome: verificationTimedOut, Error: ctx.Err().Error()}
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
		if in.Kind == deep.IncidentVerifyRun && (strings.Contains(in.Detail, "outcome=failed") || strings.Contains(in.Detail, "outcome=timed_out") || strings.Contains(in.Detail, "outcome=execution_error")) {
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
