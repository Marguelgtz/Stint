package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

type noOpDeepExecutor struct{}

func (noOpDeepExecutor) run(context.Context, execInput) (execResult, error) {
	return completedResult(), nil
}

func newV2AcceptanceEnv(t *testing.T, taskVerify, missionVerify string, expectation deep.RepositoryChangeExpectation) *testEnv {
	return newV2AcceptanceEnvWithCheck(t, taskVerify, missionVerify, expectation, "test -e work-1.txt")
}

func newV2AcceptanceEnvWithCheck(t *testing.T, taskVerify, missionVerify string, expectation deep.RepositoryChangeExpectation, acceptanceCheck string) *testEnv {
	return newV2AcceptanceEnvWithReview(t, taskVerify, missionVerify, expectation, acceptanceCheck, false)
}

func newSemanticReviewEnv(t *testing.T, taskVerify, missionVerify string, expectation deep.RepositoryChangeExpectation, acceptanceCheck string) *testEnv {
	env := newV2AcceptanceEnvWithReview(t, taskVerify, missionVerify, expectation, acceptanceCheck, true)
	env.coord.verify = func(_ context.Context, command, workdir string) verificationResult {
		outcome, code, output := verificationPassed, 0, "semantic fixture check passed"
		if _, err := os.Stat(filepath.Join(workdir, "work-1.txt")); err != nil {
			outcome, code, output = verificationFailed, 1, err.Error()
		}
		return verificationResult{Command: command, Outcome: outcome, HasExitCode: true, ExitCode: code,
			StartedAt: env.clock.now, CompletedAt: env.clock.now, Output: output}
	}
	return env
}

func newV2AcceptanceEnvWithReview(t *testing.T, taskVerify, missionVerify string, expectation deep.RepositoryChangeExpectation, acceptanceCheck string, semanticReview bool) *testEnv {
	mission := deep.Mission{
		Name: "deterministic acceptance fixture", Objective: "prove a bounded outcome",
		Verify: missionVerify, AcceptanceContractVersion: deep.DeterministicAcceptanceContractVersion,
		Tasks: []deep.Task{{ID: "OBJ-1", Objective: "produce the requested outcome", Verify: taskVerify,
			RepositoryChange: expectation, AcceptanceCheck: acceptanceCheck, Status: deep.StatusQueued}},
	}
	if semanticReview {
		mission.SemanticReviewContractVersion = deep.SemanticReviewContractVersion
	}
	return newV2AcceptanceEnvForMission(t, mission)
}

func newV2AcceptanceEnvForMission(t *testing.T, mission deep.Mission) *testEnv {
	t.Helper()
	env := newTestEnv(t, nil, 3)
	identity, err := deep.AcceptanceContractIdentity(mission)
	if err != nil {
		t.Fatal(err)
	}
	mission.AcceptanceContractSHA256 = identity
	if mission.SemanticReviewContractVersion != 0 {
		mission.SemanticReviewContractSHA256, err = deep.SemanticReviewContractIdentity(mission)
		if err != nil {
			t.Fatal(err)
		}
	}
	now := env.clock.now
	state := deep.NewState(env.state.SessionID, mission, env.repo, env.wt,
		env.state.Deadline, env.state.LandBefore, 3, now)
	state.BaseCommit = env.state.BaseCommit
	stateDir := t.TempDir()
	if err := deep.BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatalf("begin acceptance fixture run: %v", err)
	}
	env.coord.stateDir = stateDir
	env.coord.state = &state
	env.state = &state
	return env
}

func TestV2AcceptanceAcceptsSuccessfulNoVerifierWorkUnit(t *testing.T) {
	env := newV2AcceptanceEnv(t, "", "", deep.RepositoryChangeRequired)
	acceptanceCalls := 0
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		acceptanceCalls++
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("run v2 task: %v", err)
	}
	task := env.state.Tasks[0]
	if acceptanceCalls != 1 || task.Status != deep.StatusAccepted || task.AcceptanceOutcome != deep.AcceptanceAccepted {
		t.Fatalf("acceptance calls/status/outcome = %d/%s/%s", acceptanceCalls, task.Status, task.AcceptanceOutcome)
	}
	if task.VerificationOutcome != deep.VerificationNotRun || task.VerificationRunID != "" || task.CheckpointCommit == "" ||
		task.CheckpointTreeSHA != task.AcceptanceCheckpointTreeSHA || task.AcceptanceSubject == nil ||
		*task.AcceptanceSubject != deep.TaskCheckpointSubject(deep.TaskCheckpoint{Commit: task.CheckpointCommit, TreeSHA: task.CheckpointTreeSHA}) {
		t.Fatalf("executor-result checkpoint and acceptance evidence were not kept distinct and bound: %+v", task)
	}
	checkpoint, _, found, err := deep.LoadTaskCheckpoint(env.coord.stateDir, env.state.SessionID, task.ID)
	if err != nil || !found || checkpoint.Basis != deep.TaskCheckpointBasisExecutor || checkpoint.VerificationRunID != "" {
		t.Fatalf("checkpoint basis=%+v found=%t err=%v", checkpoint, found, err)
	}
	if _, err := deep.LoadState(env.coord.stateDir, env.state.SessionID); err != nil {
		t.Fatalf("accepted no-verifier projection did not reload: %v", err)
	}
}

func TestV2PendingCheckpointCanBeAcceptedAfterAttemptCapWithoutRerunningExecutor(t *testing.T) {
	env := newV2AcceptanceEnv(t, "", "", deep.RepositoryChangeRequired)
	env.state.TaskAttemptCap = 1
	env.coord.taskTimeout = time.Minute
	env.fake.after = func() { env.clock.advance(61 * time.Minute) }
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("create checkpoint when acceptance window expires: %v", err)
	}
	pending := env.state.Tasks[0]
	if pending.Attempts != 1 || pending.Status != deep.StatusCheckpointed || pending.AcceptanceOutcome != deep.AcceptanceNotEvaluated {
		t.Fatalf("checkpoint without enough acceptance time = %+v", pending)
	}

	// A later resume receives a fresh acceptance window. It must finish the
	// already checkpointed objective even though no executor attempts remain.
	env.fake.after = nil
	env.state.LandBefore = env.clock.now.Add(10 * time.Minute)
	if err := deep.BeginResumeEpoch(env.coord.stateDir, env.state, deep.PhaseExecuting, env.clock.now); err != nil {
		t.Fatalf("resume acceptance window: %v", err)
	}
	if idx, ok := env.coord.selectTask(); !ok || idx != 0 {
		t.Fatalf("pending checkpoint at attempt cap was not selected: (%d, %t)", idx, ok)
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("evaluate pending checkpoint after resume: %v", err)
	}
	if env.fake.calls != 1 || env.state.Tasks[0].Status != deep.StatusAccepted || env.state.Tasks[0].AcceptanceOutcome != deep.AcceptanceAccepted {
		t.Fatalf("resume duplicated executor or failed acceptance: executor calls=%d task=%+v", env.fake.calls, env.state.Tasks[0])
	}
}

func TestV2RejectedPendingCheckpointAtAttemptCapDoesNotRerunExecutor(t *testing.T) {
	env := newV2AcceptanceEnv(t, "", "", deep.RepositoryChangeRequired)
	env.state.TaskAttemptCap = 1
	env.coord.taskTimeout = time.Minute
	env.fake.after = func() { env.clock.advance(61 * time.Minute) }
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationFailed, HasExitCode: true, ExitCode: 1}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("create checkpoint when acceptance window expires: %v", err)
	}
	if task := env.state.Tasks[0]; task.Status != deep.StatusCheckpointed || task.AcceptanceOutcome != deep.AcceptanceNotEvaluated {
		t.Fatalf("expected a pending checkpoint before resume, got %+v", task)
	}
	env.fake.after = nil
	env.state.LandBefore = env.clock.now.Add(10 * time.Minute)
	if err := deep.BeginResumeEpoch(env.coord.stateDir, env.state, deep.PhaseExecuting, env.clock.now); err != nil {
		t.Fatalf("resume acceptance window: %v", err)
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("evaluate pending checkpoint after resume: %v", err)
	}
	task := env.state.Tasks[0]
	if env.fake.calls != 1 || task.Attempts != 1 || task.Status != deep.StatusIncomplete || task.AcceptanceOutcome != deep.AcceptanceNotSatisfied {
		t.Fatalf("attempt cap was exceeded after failed acceptance check: executor calls=%d task=%+v", env.fake.calls, task)
	}
}

func TestV2CoordinatorCompletesMissionFromBoundAcceptance(t *testing.T) {
	env := newV2AcceptanceEnv(t, "", "", deep.RepositoryChangeRequired)
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run versioned deterministic mission: %v", err)
	}
	if env.fake.calls != 1 || env.state.Tasks[0].Status != deep.StatusAccepted ||
		env.state.MissionOutcome != deep.MissionOutcomeSucceeded || env.state.Phase != deep.PhaseLanded {
		t.Fatalf("versioned mission result = executor calls %d, phase %s, task %+v, outcome %s", env.fake.calls, env.state.Phase, env.state.Tasks[0], env.state.MissionOutcome)
	}
}

func TestV2FinalMissionFailurePreservesAcceptedObjectiveEvidence(t *testing.T) {
	env := newV2AcceptanceEnv(t, "", "required-final-check", deep.RepositoryChangeRequired)
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	env.coord.finalVerify = func(context.Context, string) verificationResult {
		return verificationResult{Command: "required-final-check", Outcome: verificationFailed, HasExitCode: true, ExitCode: 9, Output: "final assertion failed"}
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("land versioned mission after final verifier failure: %v", err)
	}
	task := env.state.Tasks[0]
	if task.Status != deep.StatusAccepted || task.AcceptanceOutcome != deep.AcceptanceAccepted || env.state.MissionOutcome != deep.MissionOutcomeFailed {
		t.Fatalf("failed final verifier changed task acceptance or reported mission success: task=%+v outcome=%s", task, env.state.MissionOutcome)
	}
}

func TestV2AcceptanceRequiresExecutorSuccessEvenWhenCheckWouldPass(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result execResult
		want   deep.ExecutorOutcome
	}{
		{name: "failure", result: execResult{exitCode: 1, completed: true, finishReason: "failed"}, want: deep.ExecutorOutcomeFailed},
		{name: "timeout", result: execResult{exitCode: -1, timedOut: true, finishReason: "timeout"}, want: deep.ExecutorOutcomeTimedOut},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newV2AcceptanceEnv(t, "generic-check", "", deep.RepositoryChangeOptional)
			env.fake.script = map[int]execResult{1: tc.result}
			var genericCalls, acceptanceCalls int
			env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
				if command == "generic-check" {
					genericCalls++
				} else {
					acceptanceCalls++
				}
				return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
			}
			if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
				t.Fatalf("run unsuccessful v2 executor: %v", err)
			}
			task := env.state.Tasks[0]
			if genericCalls != 1 || acceptanceCalls != 0 || task.Status == deep.StatusAccepted ||
				task.AcceptanceOutcome == deep.AcceptanceAccepted || task.CheckpointCommit != "" ||
				task.VerificationOutcome != deep.VerificationPassed {
				t.Fatalf("passing generic verification substituted for unsuccessful executor/objective proof: calls=%d/%d task=%+v", genericCalls, acceptanceCalls, task)
			}
			events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			var found deep.ExecutorOutcome
			for _, event := range events {
				if event.Type == deep.RunEventExecutorResult && event.ExecutorRun != nil {
					found = event.ExecutorRun.Outcome
				}
			}
			if found != tc.want {
				t.Fatalf("durable executor outcome=%q, want %q", found, tc.want)
			}
		})
	}
}

func TestV2AcceptanceCheckIsAdditionalToGenericVerification(t *testing.T) {
	env := newV2AcceptanceEnv(t, "test -f .stint-verified", "", deep.RepositoryChangeRequired)
	var calls []string
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		calls = append(calls, command)
		if command == "test -f .stint-verified" {
			return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
		}
		return verificationResult{Command: command, Outcome: verificationFailed, HasExitCode: true, ExitCode: 1, Output: "objective absent"}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("run v2 task: %v", err)
	}
	task := env.state.Tasks[0]
	if len(calls) != 2 || calls[0] != "test -f .stint-verified" || calls[1] != task.AcceptanceCheck {
		t.Fatalf("generic and objective checks were not both run in order: %v", calls)
	}
	if task.VerificationOutcome != deep.VerificationPassed || task.CheckpointCommit == "" || task.AcceptanceOutcome != deep.AcceptanceNotSatisfied ||
		task.Status != deep.StatusIncomplete || task.CheckpointTreeSHA != task.AcceptanceCheckpointTreeSHA {
		t.Fatalf("passing generic verification was treated as acceptance or its checkpoint was lost: %+v", task)
	}
	if _, err := deep.LoadState(env.coord.stateDir, env.state.SessionID); err != nil {
		t.Fatalf("not-satisfied acceptance projection did not reload: %v", err)
	}
}

func TestV2AcceptanceMutationAfterCheckpointRemainsUnresolved(t *testing.T) {
	env := newV2AcceptanceEnv(t, "", "", deep.RepositoryChangeOptional)
	env.coord.verify = func(_ context.Context, command, workdir string) verificationResult {
		if err := os.WriteFile(filepath.Join(workdir, "acceptance-mutated.txt"), []byte("mutation"), 0o644); err != nil {
			t.Fatalf("mutate during acceptance-check: %v", err)
		}
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("run v2 task with acceptance mutation: %v", err)
	}
	task := env.state.Tasks[0]
	if task.AcceptanceOutcome != deep.AcceptanceUnresolved || task.Status != deep.StatusNeedsHuman || task.CheckpointCommit == "" {
		t.Fatalf("mutated acceptance subject was claimed or checkpoint evidence was lost: %+v", task)
	}
	if task.AcceptanceSubject == nil || task.AcceptanceSubject.TreeSHA != task.CheckpointTreeSHA {
		t.Fatalf("unresolved acceptance no longer identifies its exact checkpoint: %+v", task)
	}
	if _, err := deep.LoadState(env.coord.stateDir, env.state.SessionID); err != nil {
		t.Fatalf("unresolved acceptance projection did not reload: %v", err)
	}
}

func TestV2AcceptanceRunnerCommandMismatchCannotAccept(t *testing.T) {
	env := newV2AcceptanceEnv(t, "", "", deep.RepositoryChangeOptional)
	env.coord.verify = func(context.Context, string, string) verificationResult {
		return verificationResult{Command: "test -e unrelated.txt", Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("run v2 task: %v", err)
	}
	task := env.state.Tasks[0]
	if task.AcceptanceOutcome != deep.AcceptanceUnresolved || task.Status != deep.StatusNeedsHuman ||
		task.AcceptanceCheckOutcome != deep.AcceptanceCheckExecutionErr {
		t.Fatalf("runner command mismatch was accepted: %+v", task)
	}
}

func TestV2NoChangeObjectiveCanBeAcceptedWithoutEmptyMarkerCommit(t *testing.T) {
	for _, expectation := range []struct {
		name       string
		change     deep.RepositoryChangeExpectation
		wantStatus deep.Status
		wantAccept deep.AcceptanceOutcome
	}{
		{name: "forbidden change accepted", change: deep.RepositoryChangeForbidden, wantStatus: deep.StatusAccepted, wantAccept: deep.AcceptanceAccepted},
		{name: "optional no-change accepted", change: deep.RepositoryChangeOptional, wantStatus: deep.StatusAccepted, wantAccept: deep.AcceptanceAccepted},
		{name: "required change rejected", change: deep.RepositoryChangeRequired, wantStatus: deep.StatusIncomplete, wantAccept: deep.AcceptanceNotSatisfied},
	} {
		t.Run(expectation.name, func(t *testing.T) {
			env := newV2AcceptanceEnvWithCheck(t, "", "", expectation.change, "test -f README.md")
			env.coord.executor = noOpDeepExecutor{}
			env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
				if command != "test -f README.md" {
					t.Fatalf("acceptance runner received %q, want the objective-specific check", command)
				}
				return verificationResult{Command: command, Outcome: verificationPassed, StartedAt: env.clock.now, CompletedAt: env.clock.now.Add(time.Second), HasExitCode: true, ExitCode: 0}
			}
			before, err := env.coord.git.headCommit(env.wt)
			if err != nil {
				t.Fatal(err)
			}
			if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
				t.Fatalf("run no-change v2 Work Unit: %v", err)
			}
			after, err := env.coord.git.headCommit(env.wt)
			if err != nil {
				t.Fatal(err)
			}
			task := env.state.Tasks[0]
			if before != after || task.Status != expectation.wantStatus || task.AcceptanceOutcome != expectation.wantAccept {
				t.Fatalf("no-change acceptance created a marker commit or violated expectation: HEAD %s -> %s task=%+v", before, after, task)
			}
			if task.CheckpointCommit != before || task.CheckpointTreeSHA == "" {
				t.Fatalf("no-op checkpoint did not reuse the existing commit: %+v", task)
			}
		})
	}
}

func TestV2ForbiddenRepositoryChangeCannotBeAcceptedByPassingCheck(t *testing.T) {
	env := newV2AcceptanceEnvWithCheck(t, "", "", deep.RepositoryChangeForbidden, "test -f README.md")
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		if command != "test -f README.md" {
			t.Fatalf("acceptance runner received %q, want the objective-specific check", command)
		}
		return verificationResult{Command: command, Outcome: verificationPassed, StartedAt: env.clock.now, CompletedAt: env.clock.now.Add(time.Second), HasExitCode: true, ExitCode: 0}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("run forbidden-change Work Unit: %v", err)
	}
	task := env.state.Tasks[0]
	if task.AcceptanceCheckOutcome != deep.AcceptanceCheckPassed || task.VerificationOutcome != deep.VerificationNotRun ||
		task.AcceptanceOutcome != deep.AcceptanceNotSatisfied || task.Status != deep.StatusIncomplete || task.CheckpointCommit == "" {
		t.Fatalf("passing Objective check overrode the forbidden repository change: %+v", task)
	}
}

func TestV2RepositoryChangeBaselineSurvivesRetriesAndCheckpoints(t *testing.T) {
	env := newV2AcceptanceEnv(t, "", "", deep.RepositoryChangeRequired)
	acceptanceCalls := 0
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		acceptanceCalls++
		if acceptanceCalls == 1 {
			return verificationResult{Command: command, Outcome: verificationFailed, HasExitCode: true, ExitCode: 1}
		}
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("first Work Unit attempt: %v", err)
	}
	if env.state.Tasks[0].Status != deep.StatusIncomplete || env.state.Tasks[0].AcceptanceOutcome != deep.AcceptanceNotSatisfied {
		t.Fatalf("first attempt unexpectedly satisfied the Objective: %+v", env.state.Tasks[0])
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now.Add(time.Minute)); err != nil {
		t.Fatalf("second Work Unit attempt: %v", err)
	}
	if env.state.Tasks[0].Status != deep.StatusAccepted || env.state.Tasks[0].Attempts != 2 {
		t.Fatalf("later checkpoint did not satisfy the Work Unit: %+v", env.state.Tasks[0])
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var acceptanceRuns []deep.AcceptanceRun
	for _, event := range events {
		if event.Type == deep.RunEventAcceptanceResult && event.AcceptanceRun != nil {
			acceptanceRuns = append(acceptanceRuns, *event.AcceptanceRun)
		}
	}
	if len(acceptanceRuns) != 2 || acceptanceRuns[0].RepositoryBaseline != acceptanceRuns[1].RepositoryBaseline ||
		acceptanceRuns[0].Checkpoint.TreeSHA == acceptanceRuns[1].Checkpoint.TreeSHA {
		t.Fatalf("retry did not retain one stable baseline and separate checkpoint subjects: %+v", acceptanceRuns)
	}
	if _, err := deep.LoadState(env.coord.stateDir, env.state.SessionID); err != nil {
		t.Fatalf("multiple checkpoint/acceptance generations did not replay: %v", err)
	}
}
