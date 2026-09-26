package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func beginJournaledTestRun(t *testing.T, env *testEnv) {
	t.Helper()
	env.coord.stateDir = t.TempDir()
	if err := deep.BeginNewRun(env.coord.stateDir, env.state, env.clock.now); err != nil {
		t.Fatalf("begin journaled fixture: %v", err)
	}
}

func TestJournaledTaskPersistsExecutorStartBeforeLaunchAndResultBeforeVerification(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.coord.execCfg.provider = "custom:qwen-stint-{reasoning}"
	env.coord.execCfg.reasoning = "medium"
	beginJournaledTestRun(t, env)
	startObservedBeforeLaunch := false
	env.fake.before = func(execInput) {
		state, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
		if err != nil {
			t.Errorf("load pre-launch state: %v", err)
			return
		}
		events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
		if err != nil {
			t.Errorf("read pre-launch journal: %v", err)
			return
		}
		if state.Tasks[0].Status != deep.StatusActive || state.Tasks[0].Attempts != 1 || len(events) != 2 || events[1].Type != deep.RunEventExecutorStarted {
			t.Errorf("executor launch began before durable start: task=%+v events=%+v", state.Tasks[0], events)
			return
		}
		startObservedBeforeLaunch = true
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("run journaled task: %v", err)
	}
	if !startObservedBeforeLaunch {
		t.Fatal("executor start was not durable before invocation")
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil || len(events) != 3 || events[1].Type != deep.RunEventExecutorStarted || events[2].Type != deep.RunEventExecutorResult {
		t.Fatalf("executor journal = %+v err=%v", events, err)
	}
	started, completed := events[1].ExecutorRun, events[2].ExecutorRun
	if started == nil || completed == nil || started.ID != completed.ID || started.Attempt != 1 || started.RepositoryBefore == nil || completed.RepositoryAfter == nil {
		t.Fatalf("executor run provenance is incomplete: start=%+v result=%+v", started, completed)
	}
	if started.Runtime.Provider != "custom:qwen-stint-medium" || started.Runtime.Model != env.coord.execCfg.model ||
		started.Runtime.Reasoning != "medium" || completed.Runtime != started.Runtime ||
		len(env.fake.inputs) != 1 || env.fake.inputs[0].provider != started.Runtime.Provider || env.fake.inputs[0].reasoning != started.Runtime.Reasoning {
		t.Fatalf("executor runtime differs between durable facts and invocation: start=%+v result=%+v input=%+v", started.Runtime, completed.Runtime, env.fake.inputs)
	}
	if started.RepositoryBefore.TreeSHA == completed.RepositoryAfter.TreeSHA {
		t.Fatal("fixture executor did not produce a distinct repository result state")
	}
	if events[1].Sequence != 2 || events[2].Sequence != 3 || events[1].EpochID != events[2].EpochID {
		t.Fatalf("executor events lost sequence or epoch identity: %+v", events[1:])
	}
	if env.state.Tasks[0].Status != deep.StatusVerified || env.state.Tasks[0].ExecutorRunID != completed.ID || !env.state.Tasks[0].ExecutorRunProcessed {
		t.Fatalf("task result projection = %+v", env.state.Tasks[0])
	}
}

func TestEmptyConfiguredProviderIsPersistedAsHermesDefault(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.coord.execCfg.provider = ""
	env.coord.execCfg.reasoning = "medium"
	beginJournaledTestRun(t, env)
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("run task with Hermes default provider: %v", err)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil || len(events) != 3 || events[1].ExecutorRun == nil || events[2].ExecutorRun == nil {
		t.Fatalf("executor provider history = %+v err=%v", events, err)
	}
	if events[1].ExecutorRun.Runtime.Provider != "custom" || events[2].ExecutorRun.Runtime != events[1].ExecutorRun.Runtime ||
		len(env.fake.inputs) != 1 || env.fake.inputs[0].provider != "custom" {
		t.Fatalf("empty provider resolution differs across record, result, and invocation: start=%+v result=%+v input=%+v", events[1].ExecutorRun.Runtime, events[2].ExecutorRun.Runtime, env.fake.inputs)
	}
}

func TestJournaledTaskRetryKeepsBothExecutorInvocations(t *testing.T) {
	env := newTestEnv(t, map[int]execResult{1: failedResult()}, 3)
	env.state.Tasks = env.state.Tasks[:1]
	beginJournaledTestRun(t, env)
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationPassed}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("run first journaled attempt: %v", err)
	}
	if env.state.Tasks[0].Attempts != 1 || env.state.Tasks[0].Status != deep.StatusIncomplete || !env.state.Tasks[0].ExecutorRunProcessed {
		t.Fatalf("first attempt did not reach a retryable state: %+v", env.state.Tasks[0])
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now.Add(time.Minute)); err != nil {
		t.Fatalf("run second journaled attempt: %v", err)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var starts, results []deep.ExecutorRun
	for _, event := range events {
		if event.ExecutorRun == nil || event.ExecutorRun.TaskID != "T-001" {
			continue
		}
		switch event.Type {
		case deep.RunEventExecutorStarted:
			starts = append(starts, *event.ExecutorRun)
		case deep.RunEventExecutorResult:
			results = append(results, *event.ExecutorRun)
		}
	}
	if len(starts) != 2 || len(results) != 2 || starts[0].ID == starts[1].ID ||
		starts[0].Attempt != 1 || results[0].Attempt != 1 || starts[1].Attempt != 2 || results[1].Attempt != 2 ||
		results[0].Outcome != deep.ExecutorOutcomeFailed || results[1].Outcome != deep.ExecutorOutcomeSucceeded {
		t.Fatalf("retry overwrote or misattributed invocation history: starts=%+v results=%+v", starts, results)
	}
	if env.state.Tasks[0].Status != deep.StatusVerified || env.state.Tasks[0].Attempts != 2 || env.state.Tasks[0].ExecutorRunID != starts[1].ID {
		t.Fatalf("successful retry task projection = %+v", env.state.Tasks[0])
	}
}

func TestResumeReusesDurableExecutorResultInsteadOfRelaunching(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks[1].Status = deep.StatusVerified
	beginJournaledTestRun(t, env)
	if err := os.WriteFile(filepath.Join(env.wt, ".stint-verified"), []byte("verified"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := env.coord.git.verificationSubject(env.wt, env.coord.verificationBookkeepingPaths())
	if err != nil {
		t.Fatal(err)
	}
	id, err := deep.NewExecutorRunID()
	if err != nil {
		t.Fatal(err)
	}
	run, err := deep.BeginExecutorRun(env.coord.stateDir, env.state, deep.ExecutorRun{
		ID: id, TaskID: "T-001", Attempt: 1, StartedAt: env.clock.now,
		ConfiguredTimeoutSeconds: 60, EffectiveTimeoutSeconds: 60, RemainingDeadlineSeconds: 3600,
		Runtime:          deep.ExecutorRuntime{Worker: workerHermesOnBox, Provider: env.coord.execCfg.provider, Model: env.coord.execCfg.model},
		RepositoryBefore: &before.Subject,
	})
	if err != nil {
		t.Fatalf("persist simulated invocation start: %v", err)
	}
	run.Outcome = deep.ExecutorOutcomeSucceeded
	run.EndedAt = env.clock.now.Add(time.Second)
	run.ExitCode = 0
	run.Completed = true
	run.FinishReason = "completed"
	run.DurationMilliseconds = 1000
	run.ResultSummary = "exit=0 finish=completed in 1s"
	run.RepositoryAfter = &before.Subject
	if err := deep.CompleteExecutorRun(env.coord.stateDir, env.state, run); err != nil {
		t.Fatalf("persist simulated result: %v", err)
	}
	checkpointHead, checkpointTree, err := env.coord.git.checkpointSubject(env.wt, taskCheckpointMessage(env.state.Tasks[0]), before)
	if err != nil || strings.TrimSpace(checkpointHead) == before.Subject.HeadCommit || strings.TrimSpace(checkpointTree) != before.Subject.TreeSHA {
		t.Fatalf("simulate checkpoint side effect after result: head=%q tree=%q err=%v", checkpointHead, checkpointTree, err)
	}
	if err := deep.BeginResumeEpoch(env.coord.stateDir, env.state, deep.PhaseExecuting, env.clock.now.Add(time.Minute)); err != nil {
		t.Fatalf("begin resumed epoch: %v", err)
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("resume journaled task: %v", err)
	}
	if env.fake.calls != 0 {
		t.Fatalf("resume launched %d duplicate executor invocations, want 0", env.fake.calls)
	}
	if env.state.Tasks[0].Status != deep.StatusVerified || env.state.Tasks[0].Attempts != 1 || env.state.Tasks[0].ExecutorRunID != id || !env.state.Tasks[0].ExecutorRunProcessed {
		t.Fatalf("recovered task projection = %+v", env.state.Tasks[0])
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 4 || events[1].Sequence != 2 || events[2].Sequence != 3 || events[3].Sequence != 4 || events[3].Boundary != deep.RunEventBoundaryResume {
		t.Fatalf("resume did not preserve one run-wide event sequence: %+v", events)
	}
}

func TestResumeBlocksUnmatchedExecutorWithoutRetryOrVerification(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	beginJournaledTestRun(t, env)
	before, err := env.coord.git.verificationSubject(env.wt, env.coord.verificationBookkeepingPaths())
	if err != nil {
		t.Fatal(err)
	}
	id, err := deep.NewExecutorRunID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deep.BeginExecutorRun(env.coord.stateDir, env.state, deep.ExecutorRun{
		ID: id, TaskID: "T-001", Attempt: 1, StartedAt: env.clock.now,
		ConfiguredTimeoutSeconds: 60, EffectiveTimeoutSeconds: 60, RemainingDeadlineSeconds: 3600,
		RepositoryBefore: &before.Subject,
	}); err != nil {
		t.Fatalf("persist unmatched start: %v", err)
	}
	if err := deep.BeginResumeEpoch(env.coord.stateDir, env.state, deep.PhaseExecuting, env.clock.now.Add(time.Minute)); err != nil {
		t.Fatalf("begin resumed epoch: %v", err)
	}
	verifyCalls := 0
	env.coord.verify = func(context.Context, string, string) verificationResult {
		verifyCalls++
		return verificationResult{Outcome: verificationPassed}
	}
	err = env.coord.run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "process quiescence is unknown") {
		t.Fatalf("resume unmatched executor = %v, want explicit hard block", err)
	}
	if env.fake.calls != 0 || verifyCalls != 0 {
		t.Fatalf("unmatched invocation triggered retry or verification: executor calls=%d verify calls=%d", env.fake.calls, verifyCalls)
	}
	loaded, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.ExecutionQuiescenceUnconfirmed || loaded.ExecutionQuiescenceTaskID != "T-001" || loaded.Tasks[0].Status != deep.StatusNeedsHuman {
		t.Fatalf("unmatched invocation did not persist hard recovery block: %+v", loaded)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil || len(events) != 4 || events[3].Type != deep.RunEventExecutorRecoveryRequired || events[3].Sequence != 4 {
		t.Fatalf("unmatched recovery journal = %+v err=%v", events, err)
	}
}

func TestInterruptedLandingRecordsUnmatchedExecutorRecoveryBeforeHardBlock(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	beginJournaledTestRun(t, env)
	before, err := env.coord.git.verificationSubject(env.wt, env.coord.verificationBookkeepingPaths())
	if err != nil {
		t.Fatal(err)
	}
	id, err := deep.NewExecutorRunID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deep.BeginExecutorRun(env.coord.stateDir, env.state, deep.ExecutorRun{
		ID: id, TaskID: "T-001", Attempt: 1, StartedAt: env.clock.now,
		ConfiguredTimeoutSeconds: 60, EffectiveTimeoutSeconds: 60, RemainingDeadlineSeconds: 3600,
		RepositoryBefore: &before.Subject,
	}); err != nil {
		t.Fatalf("persist unmatched start: %v", err)
	}
	if err := deep.BeginLanding(env.coord.stateDir, env.state, "operator requested stop", env.clock.now.Add(time.Second)); err != nil {
		t.Fatalf("begin landing: %v", err)
	}
	env.state.ExecutionQuiescenceUnconfirmed = true
	env.state.ExecutionQuiescenceTaskID = "T-001"
	if err := env.state.SaveDir(env.coord.stateDir); err != nil {
		t.Fatalf("persist landing quiescence block: %v", err)
	}
	if err := deep.BeginResumeEpoch(env.coord.stateDir, env.state, deep.PhaseLanding, env.clock.now.Add(time.Minute)); err != nil {
		t.Fatalf("resume interrupted landing: %v", err)
	}
	if err := env.coord.run(context.Background()); err == nil || !strings.Contains(err.Error(), "process quiescence is unknown") {
		t.Fatalf("resume unmatched landing executor = %v, want explicit recovery block", err)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 || events[4].Type != deep.RunEventExecutorRecoveryRequired || events[4].Sequence != 5 {
		t.Fatalf("interrupted landing did not durably record unmatched executor before stopping: %+v", events)
	}
	fresh, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil || !fresh.ExecutionQuiescenceUnconfirmed || fresh.Tasks[0].Status != deep.StatusNeedsHuman {
		t.Fatalf("landing recovery projection = task %+v blocked=%t err=%v", fresh.Tasks[0], fresh.ExecutionQuiescenceUnconfirmed, err)
	}
}

func TestJournaledExecutorTimeoutIsRecordedAsTimeout(t *testing.T) {
	env := newTestEnv(t, map[int]execResult{1: completedResult()}, 2)
	beginJournaledTestRun(t, env)
	env.fake.scriptErr = map[int]error{1: context.DeadlineExceeded}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("run timed-out journaled task: %v", err)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[2].Type != deep.RunEventExecutorResult || events[2].ExecutorRun == nil {
		t.Fatalf("timeout result event missing: %+v", events)
	}
	if events[2].ExecutorRun.Outcome != deep.ExecutorOutcomeTimedOut ||
		events[2].ExecutorRun.Error != context.DeadlineExceeded.Error() || env.state.Tasks[0].Status == deep.StatusVerified {
		t.Fatalf("executor timeout was misrepresented: record=%+v task=%+v", events[2].ExecutorRun, env.state.Tasks[0])
	}
}
