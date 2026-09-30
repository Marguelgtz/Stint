package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
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
	if err != nil || len(events) != 6 || events[1].Type != deep.RunEventExecutorStarted || events[2].Type != deep.RunEventExecutorResult ||
		events[3].Type != deep.RunEventVerificationStarted || events[4].Type != deep.RunEventVerificationResult ||
		events[5].Type != deep.RunEventTaskCheckpointCreated || events[5].TaskCheckpoint == nil {
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
	verificationStart, verificationResult := events[3].VerificationRun, events[4].VerificationRun
	if verificationStart == nil || verificationResult == nil || verificationStart.ID != verificationResult.ID ||
		verificationStart.Outcome != deep.VerificationStarted || verificationResult.Outcome != deep.VerificationPassed ||
		verificationResult.SubjectAfter == nil || *verificationResult.SubjectAfter != verificationResult.Subject ||
		verificationResult.CommandSHA256 != deep.VerificationCommandIdentity(env.state.Verify) {
		t.Fatalf("task verification facts are not subject-bound: start=%+v result=%+v", verificationStart, verificationResult)
	}
	checkpoint := events[5].TaskCheckpoint
	if checkpoint.TaskID != env.state.Tasks[0].ID || checkpoint.Attempt != 1 || checkpoint.ExecutorRunID != completed.ID ||
		checkpoint.VerificationRunID != verificationResult.ID || checkpoint.VerificationSubject != verificationResult.Subject ||
		checkpoint.TreeSHA != verificationResult.Subject.TreeSHA || checkpoint.Commit != env.state.Tasks[0].CheckpointCommit {
		t.Fatalf("task checkpoint is not bound to its exact executor and verifier facts: %+v", checkpoint)
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
	if err != nil || len(events) != 6 || events[1].ExecutorRun == nil || events[2].ExecutorRun == nil {
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
	if err := deep.CompleteExecutorRun(env.coord.stateDir, env.state, run, run.EndedAt); err != nil {
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
	resumedHead, err := env.coord.git.repoHead(env.wt)
	if err != nil || strings.TrimSpace(resumedHead) != strings.TrimSpace(checkpointHead) {
		t.Fatalf("resume duplicated task checkpoint commit: before=%s after=%s err=%v", checkpointHead, resumedHead, err)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 4 || events[1].Sequence != 2 || events[2].Sequence != 3 || events[3].Sequence != 4 || events[3].Boundary != deep.RunEventBoundaryResume {
		t.Fatalf("resume did not preserve one run-wide event sequence: %+v", events)
	}
	var checkpointEvents int
	for _, event := range events {
		if event.Type != deep.RunEventTaskCheckpointCreated {
			continue
		}
		checkpointEvents++
		if event.TaskCheckpoint == nil || event.TaskCheckpoint.ExecutorRunID != id ||
			event.TaskCheckpoint.Commit != strings.TrimSpace(checkpointHead) || event.TaskCheckpoint.TreeSHA != strings.TrimSpace(checkpointTree) {
			t.Fatalf("recovered task checkpoint is not bound to the existing commit: %+v", event)
		}
	}
	if checkpointEvents != 1 {
		t.Fatalf("task checkpoint side effect was not reconciled into exactly one event: count=%d events=%+v", checkpointEvents, events)
	}
}

func TestResumeReconcilesLocalExecutorReceiptAfterCoordinatorCrash(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	now := time.Now().UTC().Truncate(time.Millisecond)
	env.clock.now = now
	env.state.StartedAt = now
	env.state.Deadline = now.Add(15 * time.Minute)
	env.state.LandBefore = now.Add(12 * time.Minute)
	hermesDir := t.TempDir()
	hermes := filepath.Join(hermesDir, "hermes")
	calls := filepath.Join(hermesDir, "calls")
	script := "#!/bin/sh\nprintf 'called\\n' >> " + shellQuote(calls) + "\n" +
		"printf 'implementation\\n' > recovered.txt\nprintf 'verified\\n' > .stint-verified\nprintf 'recovered worker output\\n'\n"
	if err := os.WriteFile(hermes, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	local := newLocalHermesExecutor(hermes)
	env.coord.executor = local
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	beginJournaledTestRun(t, env)
	before, err := env.coord.git.verificationSubject(env.wt, env.coord.verificationBookkeepingPaths())
	if err != nil {
		t.Fatal(err)
	}
	runID, err := deep.NewExecutorRunID()
	if err != nil {
		t.Fatal(err)
	}
	startedAt := env.clock.now
	started, err := deep.BeginExecutorRun(env.coord.stateDir, env.state, deep.ExecutorRun{
		ID: runID, TaskID: "T-001", Attempt: 1, StartedAt: startedAt,
		ConfiguredTimeoutSeconds: 60, EffectiveTimeoutSeconds: 60, RemainingDeadlineSeconds: 900,
		Runtime: env.coord.executorRuntimeFor(env.state.Tasks[0]), RepositoryBefore: &before.Subject,
	})
	if err != nil {
		t.Fatalf("persist executor start: %v", err)
	}
	input := env.coord.execInputFor(env.state.Tasks[0], time.Minute)
	input.stateDir, input.sessionID, input.executorRunID = env.coord.stateDir, env.state.SessionID, started.ID
	result, err := local.run(context.Background(), input)
	if err != nil || !result.completed {
		t.Fatalf("finish supervised local Hermes invocation: result=%+v err=%v", result, err)
	}
	if !result.endedAt.After(startedAt) {
		t.Fatalf("supervisor did not preserve the process end time: started=%s result=%s", startedAt, result.endedAt)
	}
	if _, found, err := deep.LoadExecutorRun(env.coord.stateDir, env.state.SessionID, runID); err != nil || !found {
		t.Fatalf("load durable start before simulated coordinator crash: found=%t err=%v", found, err)
	}
	if events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID); err != nil || len(events) != 2 || events[1].Type != deep.RunEventExecutorStarted {
		t.Fatalf("simulated crash did not leave only the start event: events=%+v err=%v", events, err)
	}
	if _, found, err := deep.LoadExecutorReceipt(env.coord.stateDir, env.state.SessionID, runID); err != nil || !found {
		t.Fatalf("completion receipt missing at crash boundary: found=%t err=%v", found, err)
	}
	callCount, err := os.ReadFile(calls)
	if err != nil || strings.Count(string(callCount), "called") != 1 {
		t.Fatalf("executor invocation count before resume=%q err=%v", callCount, err)
	}
	env.clock.now = time.Now().UTC().Add(time.Second)
	if err := deep.BeginResumeEpoch(env.coord.stateDir, env.state, deep.PhaseExecuting, env.clock.now); err != nil {
		t.Fatalf("begin resumed epoch: %v", err)
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("resume after executor completed but before result event: %v", err)
	}
	callCount, err = os.ReadFile(calls)
	if err != nil || strings.Count(string(callCount), "called") != 1 {
		t.Fatalf("resume duplicated the executor side effect: invocation log=%q err=%v", callCount, err)
	}
	if env.state.Tasks[0].Status != deep.StatusVerified || env.state.Tasks[0].Attempts != 1 {
		t.Fatalf("reconciled task did not continue through verification: %+v", env.state.Tasks[0])
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var reconciled, results int
	for _, event := range events {
		if event.Type == deep.RunEventExecutorReconciled && event.ExecutorRun != nil && event.ExecutorRun.ID == runID {
			reconciled++
			if event.ExecutorRun.EndedAt.UnixNano() == env.clock.now.UnixNano() || event.ExecutorRun.RepositoryAfterObservedAt.Before(event.ExecutorRun.EndedAt) {
				t.Fatalf("reconciliation confused executor completion time with recovery observation: %+v", event.ExecutorRun)
			}
		}
		if event.Type == deep.RunEventExecutorResult && event.ExecutorRun != nil && event.ExecutorRun.ID == runID {
			results++
		}
	}
	if reconciled != 1 || results != 0 {
		t.Fatalf("executor crash recovery was not represented exactly once: reconciled=%d ordinary results=%d", reconciled, results)
	}
}

func TestResumeReconcilesRemoteReceiptAfterTransportLoss(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	now := time.Now().UTC().Truncate(time.Millisecond)
	env.clock.now = now
	env.state.StartedAt = now
	env.state.Deadline = now.Add(15 * time.Minute)
	env.state.LandBefore = now.Add(12 * time.Minute)
	remoteDir := t.TempDir()
	hermes := filepath.Join(remoteDir, "hermes")
	calls := filepath.Join(remoteDir, "calls")
	script := "#!/bin/sh\nprintf 'called\\n' >> " + shellQuote(calls) + "\n" +
		"printf 'implementation\\n' > recovered.txt\nprintf 'verified\\n' > .stint-verified\nprintf 'remote worker output\\n'\n"
	if err := os.WriteFile(hermes, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", remoteDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(remoteDir, "state-home"))
	transportLost := false
	remote := func(ctx context.Context, command string) (string, error) {
		out, err := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
		if strings.Contains(command, "hermes chat") && !transportLost {
			transportLost = true
			if err != nil {
				return string(out), err
			}
			return string(out), errors.New("SSH response was lost after the supervisor wrote its receipt")
		}
		return string(out), err
	}
	env.coord.executor = newHermesExecutor(remote)
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	beginJournaledTestRun(t, env)
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err == nil || !strings.Contains(err.Error(), "quiescence is unconfirmed") {
		t.Fatalf("runTask after lost remote result response=%v, want fail-closed transport state", err)
	}
	if !transportLost {
		t.Fatal("remote executor invocation was not exercised")
	}
	callCount, err := os.ReadFile(calls)
	if err != nil || strings.Count(string(callCount), "called") != 1 {
		t.Fatalf("remote invocation count before resume=%q err=%v", callCount, err)
	}
	env.clock.now = time.Now().UTC().Add(time.Second)
	if err := deep.BeginResumeEpoch(env.coord.stateDir, env.state, deep.PhaseExecuting, env.clock.now); err != nil {
		t.Fatalf("begin resumed epoch: %v", err)
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("resume from remote completion receipt: %v", err)
	}
	callCount, err = os.ReadFile(calls)
	if err != nil || strings.Count(string(callCount), "called") != 1 {
		t.Fatalf("remote receipt recovery duplicated executor side effect: invocation log=%q err=%v", callCount, err)
	}
	if env.state.Tasks[0].Status != deep.StatusVerified || env.state.Tasks[0].Attempts != 1 || env.state.ExecutionQuiescenceUnconfirmed {
		t.Fatalf("remote receipt recovery did not restore safe task processing: state=%+v task=%+v", env.state, env.state.Tasks[0])
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var reconciled, uncertain int
	for _, event := range events {
		if event.ExecutorRun == nil {
			continue
		}
		if event.Type == deep.RunEventExecutorReconciled {
			reconciled++
		}
		if event.Type == deep.RunEventExecutorResult && event.ExecutorRun.Outcome == deep.ExecutorOutcomeQuiescenceUnconfirmed {
			uncertain++
			if !event.ExecutorRun.EndedAt.IsZero() || event.OccurredAt.Before(event.ExecutorRun.StartedAt) {
				t.Fatalf("transport uncertainty fabricated an executor end time: %+v", event)
			}
		}
	}
	if reconciled != 1 || uncertain != 1 {
		t.Fatalf("transport uncertainty and reconciliation facts were not both preserved: reconciled=%d uncertain=%d", reconciled, uncertain)
	}
}

func TestResumeRejectsProductTreeChangedAfterExecutorReceipt(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	now := time.Now().UTC().Truncate(time.Millisecond)
	env.clock.now = now
	env.state.StartedAt = now
	env.state.Deadline = now.Add(15 * time.Minute)
	env.state.LandBefore = now.Add(12 * time.Minute)
	hermesDir := t.TempDir()
	hermes := filepath.Join(hermesDir, "hermes")
	calls := filepath.Join(hermesDir, "calls")
	script := "#!/bin/sh\nprintf 'called\\n' >> " + shellQuote(calls) + "\nprintf 'executor result\\n' > recovered.txt\nprintf 'verified\\n' > .stint-verified\n"
	if err := os.WriteFile(hermes, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	env.coord.executor = newLocalHermesExecutor(hermes)
	verifyCalls := 0
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		verifyCalls++
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	beginJournaledTestRun(t, env)
	before, err := env.coord.git.verificationSubject(env.wt, env.coord.verificationBookkeepingPaths())
	if err != nil {
		t.Fatal(err)
	}
	runID, err := deep.NewExecutorRunID()
	if err != nil {
		t.Fatal(err)
	}
	started, err := deep.BeginExecutorRun(env.coord.stateDir, env.state, deep.ExecutorRun{
		ID: runID, TaskID: "T-001", Attempt: 1, StartedAt: now,
		ConfiguredTimeoutSeconds: 60, EffectiveTimeoutSeconds: 60, RemainingDeadlineSeconds: 900,
		Runtime: env.coord.executorRuntimeFor(env.state.Tasks[0]), RepositoryBefore: &before.Subject,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := env.coord.execInputFor(env.state.Tasks[0], time.Minute)
	input.stateDir, input.sessionID, input.executorRunID = env.coord.stateDir, env.state.SessionID, started.ID
	if result, err := env.coord.executor.run(context.Background(), input); err != nil || !result.completed {
		t.Fatalf("supervised executor run=%+v err=%v", result, err)
	}
	receipt, found, err := deep.LoadExecutorReceipt(env.coord.stateDir, env.state.SessionID, runID)
	if err != nil || !found || receipt.RepositoryAfterTreeSHA == "" {
		t.Fatalf("executor receipt=%+v found=%t err=%v", receipt, found, err)
	}
	if err := os.WriteFile(filepath.Join(env.wt, "recovered.txt"), []byte("external edit after executor completion\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.clock.now = time.Now().UTC().Add(time.Second)
	if err := deep.BeginResumeEpoch(env.coord.stateDir, env.state, deep.PhaseExecuting, env.clock.now); err != nil {
		t.Fatal(err)
	}
	err = env.coord.run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "repository state changed or is unidentified") {
		t.Fatalf("resume accepted a tree changed after supervisor completion: %v", err)
	}
	if env.state.Tasks[0].Status != deep.StatusNeedsHuman || env.state.Tasks[0].Blocker != "executor result repository state changed or is unidentified; verification and further work are stopped" ||
		env.state.Tasks[0].Attempts != 1 || verifyCalls != 0 {
		t.Fatalf("tree drift caused verification, retry, or acceptance: task=%+v verifier calls=%d", env.state.Tasks[0], verifyCalls)
	}
	loaded, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil || loaded.Tasks[0].Status != deep.StatusNeedsHuman || loaded.RunEventWatermark != env.state.RunEventWatermark {
		t.Fatalf("tree-drift reconciliation did not replay as an explicit human block: state=%+v err=%v", loaded, err)
	}
	callCount, err := os.ReadFile(calls)
	if err != nil || strings.Count(string(callCount), "called") != 1 {
		t.Fatalf("tree drift caused a duplicate executor invocation: log=%q err=%v", callCount, err)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	reconciled := 0
	for _, event := range events {
		if event.Type == deep.RunEventExecutorReconciled && event.ExecutorRun != nil && event.ExecutorRun.ID == runID {
			reconciled++
			if event.ExecutorRun.RepositoryAfter == nil || event.ExecutorRun.RepositoryAfter.TreeSHA != receipt.RepositoryAfterTreeSHA {
				t.Fatalf("reconciliation lost the supervisor's exact result tree: %+v", event)
			}
			if event.ExecutorRun.RepositoryAtRecovery == nil || event.ExecutorRun.RepositoryAtRecovery.TreeSHA == receipt.RepositoryAfterTreeSHA ||
				event.ExecutorRun.RepositoryAtRecoveryAt.IsZero() {
				t.Fatalf("reconciliation failed to preserve the changed recovery-time tree: %+v", event.ExecutorRun)
			}
		}
	}
	if reconciled != 1 {
		t.Fatalf("expected one reconciled executor event, got %d", reconciled)
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
	if len(events) != 5 || events[2].Type != deep.RunEventExecutorResult || events[2].ExecutorRun == nil {
		t.Fatalf("timeout result event missing: %+v", events)
	}
	if events[2].ExecutorRun.Outcome != deep.ExecutorOutcomeTimedOut ||
		events[2].ExecutorRun.Error != context.DeadlineExceeded.Error() || env.state.Tasks[0].Status == deep.StatusVerified {
		t.Fatalf("executor timeout was misrepresented: record=%+v task=%+v", events[2].ExecutorRun, env.state.Tasks[0])
	}
}
