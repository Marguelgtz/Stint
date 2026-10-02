package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func TestJournaledFinalVerificationIsBoundAndReusedAfterLandingInterruption(t *testing.T) {
	env := newTestEnv(t, nil, 2)
	beginJournaledTestRun(t, env)
	calls := 0
	env.coord.finalVerify = func(_ context.Context, command string) verificationResult {
		calls++
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0, Output: "final checks passed"}
	}
	env.coord.worktreeWrite = func(string, []byte) error { return errors.New("injected handoff delivery interruption") }
	if err := env.coord.land(context.Background(), "final verification recovery fixture"); err == nil || !strings.Contains(err.Error(), "handoff") {
		t.Fatalf("first landing interruption=%v", err)
	}
	if calls != 1 || env.state.Phase != deep.PhaseLanding || !env.state.LandingVerifyDone || env.state.LandingVerificationRunID == "" ||
		env.state.LandingVerificationOutcome != deep.VerificationPassed {
		t.Fatalf("durable final verification state: calls=%d state=%+v", calls, env.state)
	}
	firstID := env.state.LandingVerificationRunID
	env.coord.worktreeWrite = nil
	if err := env.coord.land(context.Background(), "resumed final verification"); err != nil {
		t.Fatalf("resume interrupted landing: %v", err)
	}
	if calls != 1 || env.state.Phase != deep.PhaseLanded || env.state.LandingVerificationRunID != firstID ||
		env.state.LandingCheckpointTreeSHA != env.state.LandingVerificationSubject.TreeSHA {
		t.Fatalf("final verification was duplicated or unbound: calls=%d state=%+v", calls, env.state)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var starts, results int
	for _, event := range events {
		if event.Type == deep.RunEventVerificationStarted && event.VerificationRun != nil && event.VerificationRun.ID == firstID {
			starts++
			if event.VerificationRun.TimeoutSeconds != int(defaultMissionVerifyTime.Seconds()) {
				t.Errorf("final verification timeout = %d seconds, want %d", event.VerificationRun.TimeoutSeconds, int(defaultMissionVerifyTime.Seconds()))
			}
		}
		if event.Type == deep.RunEventVerificationResult && event.VerificationRun != nil && event.VerificationRun.ID == firstID {
			results++
		}
	}
	if starts != 1 || results != 1 {
		t.Fatalf("final verification invocation was not exactly-once reusable: starts=%d results=%d events=%+v", starts, results, events)
	}
}

func TestJournaledLandedEventTimestampFollowsFinalVerification(t *testing.T) {
	env := newTestEnv(t, nil, 2)
	beginJournaledTestRun(t, env)
	env.coord.finalVerify = func(_ context.Context, command string) verificationResult {
		env.clock.advance(10 * time.Second)
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0, Output: "final checks passed"}
	}

	if err := env.coord.land(context.Background(), "landed timestamp fixture"); err != nil {
		t.Fatalf("land run: %v", err)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var finalVerification *deep.VerificationRun
	var landed *deep.RunEvent
	for i := range events {
		event := &events[i]
		if event.Type == deep.RunEventVerificationResult && event.VerificationRun != nil && event.VerificationRun.Purpose == deep.VerificationPurposeMissionEnd {
			finalVerification = event.VerificationRun
		}
		if event.Type == deep.RunEventLanded {
			landed = event
		}
	}
	if finalVerification == nil || landed == nil {
		t.Fatalf("missing final verification or landing event: %+v", events)
	}
	if landed.OccurredAt.Before(finalVerification.EndedAt) {
		t.Fatalf("landed event time %s precedes completed final verification %s", landed.OccurredAt, finalVerification.EndedAt)
	}
	if env.state.LandedAt == nil || !env.state.LandedAt.Equal(landed.OccurredAt) {
		t.Fatalf("landing projection time=%v does not match event time %s", env.state.LandedAt, landed.OccurredAt)
	}
}

func TestJournaledFinalVerificationQuiescenceFailureIsDurableAndBlocksCheckpoint(t *testing.T) {
	env := newTestEnv(t, nil, 2)
	beginJournaledTestRun(t, env)
	env.coord.finalVerify = func(_ context.Context, command string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationExecutionErr, Error: "remote channel lost", QuiescenceUnconfirmed: true}
	}
	if err := env.coord.land(context.Background(), "unconfirmed final verifier"); err == nil || !strings.Contains(err.Error(), "quiescence is unconfirmed") {
		t.Fatalf("landing with uncertain verifier quiescence=%v", err)
	}
	if env.state.Phase != deep.PhaseLanding || env.state.LandingCommit != "" || !env.state.ExecutionQuiescenceUnconfirmed ||
		env.state.ExecutionQuiescenceTaskID != "mission-final-verifier" || env.state.LandingVerificationOutcome != deep.VerificationExecutionErr {
		t.Fatalf("final verifier uncertainty did not block landing: %+v", env.state)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil || len(events) != 4 || events[2].Type != deep.RunEventVerificationStarted || events[3].Type != deep.RunEventVerificationResult ||
		events[3].VerificationRun == nil || !events[3].VerificationRun.QuiescenceUnconfirmed {
		t.Fatalf("uncertain verifier was not durably recorded: events=%+v err=%v", events, err)
	}
}

func TestTaskReusesDurableVerificationResultAfterCoordinatorCrash(t *testing.T) {
	env := newTestEnv(t, nil, 2)
	beginJournaledTestRun(t, env)
	before, err := env.coord.git.verificationSubject(env.wt, env.coord.verificationBookkeepingPaths())
	if err != nil {
		t.Fatal(err)
	}
	executorID, err := deep.NewExecutorRunID()
	if err != nil {
		t.Fatal(err)
	}
	executorRun, err := deep.BeginExecutorRun(env.coord.stateDir, env.state, deep.ExecutorRun{
		ID: executorID, TaskID: "T-001", Attempt: 1, StartedAt: env.clock.now,
		ConfiguredTimeoutSeconds: 60, EffectiveTimeoutSeconds: 60, RemainingDeadlineSeconds: 3600,
		Runtime: deep.ExecutorRuntime{Worker: workerHermesOnBox, Provider: "custom", Model: "test-model"}, RepositoryBefore: &before.Subject,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.wt, "durable-result.txt"), []byte("executor output\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := env.coord.git.verificationSubject(env.wt, env.coord.verificationBookkeepingPaths())
	if err != nil {
		t.Fatal(err)
	}
	executorRun.Outcome = deep.ExecutorOutcomeSucceeded
	executorRun.EndedAt = env.clock.now.Add(time.Second)
	executorRun.Completed = true
	executorRun.ResultSummary = "executor completed"
	executorRun.RepositoryAfter = &after.Subject
	if err := deep.CompleteExecutorRun(env.coord.stateDir, env.state, executorRun, executorRun.EndedAt); err != nil {
		t.Fatal(err)
	}
	verificationID, err := deep.NewVerificationRunID()
	if err != nil {
		t.Fatal(err)
	}
	verificationRun, err := deep.BeginVerificationRun(env.coord.stateDir, env.state, deep.VerificationRun{
		ID: verificationID, Purpose: deep.VerificationPurposeTask, TaskID: "T-001", Attempt: 1,
		CommandSource: "mission", CommandSHA256: deep.VerificationCommandIdentity(env.state.Verify),
		Runtime:   deep.VerificationRuntime{Worker: workerHermesOnBox, Location: "compute", Shell: "sh", Protocol: "local-process-group-v1"},
		StartedAt: env.clock.now.Add(2 * time.Second), TimeoutSeconds: 180, RemainingDeadlineSeconds: 3500,
		Subject: after.Subject, BookkeepingBefore: landingVerificationBookkeeping(after.Bookkeeping),
	})
	if err != nil {
		t.Fatal(err)
	}
	verificationRun.Outcome = deep.VerificationPassed
	verificationRun.EndedAt = verificationRun.StartedAt.Add(time.Second)
	verificationRun.HasExitCode = true
	verificationRun.ExitCode = 0
	verifiedSubject := after.Subject
	verificationRun.SubjectAfter = &verifiedSubject
	verificationRun.BookkeepingAfter = landingVerificationBookkeeping(after.Bookkeeping)
	verificationRun.DurationMilliseconds = 1000
	if err := deep.CompleteVerificationRun(env.coord.stateDir, env.state, verificationRun); err != nil {
		t.Fatal(err)
	}
	if env.state.Tasks[0].ExecutorRunProcessed || env.state.Tasks[0].Status != deep.StatusActive {
		t.Fatalf("fixture did not stop between verifier result and task transition: %+v", env.state.Tasks[0])
	}
	if err := deep.BeginResumeEpoch(env.coord.stateDir, env.state, deep.PhaseExecuting, env.clock.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	verifyCalls := 0
	env.coord.verify = func(context.Context, string, string) verificationResult {
		verifyCalls++
		return verificationResult{Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now.Add(time.Minute)); err != nil {
		t.Fatalf("resume durable task verification: %v", err)
	}
	if env.fake.calls != 0 || verifyCalls != 0 || env.state.Tasks[0].Status != deep.StatusVerified || !env.state.Tasks[0].ExecutorRunProcessed {
		t.Fatalf("resume duplicated executor/verifier or lost task transition: executor=%d verifier=%d task=%+v", env.fake.calls, verifyCalls, env.state.Tasks[0])
	}
	loaded, ok, err := deep.LoadVerificationRun(env.coord.stateDir, env.state.SessionID, verificationID)
	if err != nil || !ok || loaded.Outcome != deep.VerificationPassed {
		t.Fatalf("recovered verification record=%+v found=%t err=%v", loaded, ok, err)
	}
}

func TestTaskVerificationMutationCannotBindDifferentCheckpointTree(t *testing.T) {
	env := newTestEnv(t, nil, 1)
	beginJournaledTestRun(t, env)
	env.coord.verify = func(_ context.Context, command, workdir string) verificationResult {
		if err := os.WriteFile(filepath.Join(workdir, "mutated-by-verifier.txt"), []byte("changed after verifier started\n"), 0o644); err != nil {
			t.Errorf("mutate verifier subject: %v", err)
		}
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	err := env.coord.runTask(context.Background(), 0, env.clock.now)
	if err == nil || !strings.Contains(err.Error(), "not bound to a stable exact subject") {
		t.Fatalf("task accepted verifier-mutated repository state: %v", err)
	}
	if env.state.Tasks[0].Status == deep.StatusVerified || env.state.Tasks[0].CheckpointCommit != "" {
		t.Fatalf("mutation between verification and checkpoint was accepted: %+v", env.state.Tasks[0])
	}
	if _, err := os.Stat(filepath.Join(env.wt, "mutated-by-verifier.txt")); err != nil {
		t.Fatalf("test mutation missing: %v", err)
	}
}

func TestExternalLandingDuringVerificationDefersTaskCheckpoint(t *testing.T) {
	env := newTestEnv(t, nil, 2)
	env.state.Tasks = env.state.Tasks[:1]
	beginJournaledTestRun(t, env)
	stopObserved := false
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		fresh, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
		if err != nil {
			t.Errorf("load state for concurrent landing: %v", err)
			return verificationResult{Outcome: verificationExecutionErr, Error: err.Error()}
		}
		stopper := *env.coord
		stopper.state = &fresh
		if err := stopper.land(context.Background(), "stop during task verification"); err == nil || !strings.Contains(err.Error(), "landing deferred until active task") {
			t.Errorf("concurrent landing result=%v", err)
		} else {
			stopObserved = true
		}
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("task result after concurrent landing: %v", err)
	}
	if !stopObserved || env.state.Phase != deep.PhaseLanding || env.state.Tasks[0].Status == deep.StatusVerified || env.state.Tasks[0].CheckpointCommit != "" {
		t.Fatalf("concurrent landing did not defer verification checkpoint: observed=%t state=%+v task=%+v", stopObserved, env.state, env.state.Tasks[0])
	}
	if env.state.ExecutionQuiescenceUnconfirmed {
		t.Fatalf("confirmed verifier result left stale quiescence block: %+v", env.state)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var sawVerificationResult, sawTaskCheckpoint bool
	for _, event := range events {
		sawVerificationResult = sawVerificationResult || event.Type == deep.RunEventVerificationResult
		if event.Type == deep.RunEventTaskCheckpointCreated {
			sawTaskCheckpoint = true
		}
	}
	if !sawVerificationResult || sawTaskCheckpoint {
		t.Fatalf("verification/landing events do not preserve deferred task checkpoint: %+v", events)
	}
}
