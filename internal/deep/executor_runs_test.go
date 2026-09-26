package deep

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func executorRunFixture(t *testing.T, stateDir string, state *DeepState, now time.Time) ExecutorRun {
	t.Helper()
	id, err := NewExecutorRunID()
	if err != nil {
		t.Fatal(err)
	}
	subject := &VerificationSubject{HeadCommit: "head-before", TreeSHA: "tree-before"}
	return ExecutorRun{
		ID: id, TaskID: "T-1", Attempt: state.Tasks[0].Attempts + 1, StartedAt: now,
		ConfiguredTimeoutSeconds: 600, EffectiveTimeoutSeconds: 420, RemainingDeadlineSeconds: 1800,
		Runtime:          ExecutorRuntime{Worker: "hermes-onbox", Provider: "custom", Model: "model-test", Reasoning: "medium"},
		RepositoryBefore: subject,
	}
}

func TestExecutorRunStartAndResultAreCanonicalFacts(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	run := executorRunFixture(t, stateDir, &state, now.Add(time.Second))
	var err error
	run, err = BeginExecutorRun(stateDir, &state, run)
	if err != nil {
		t.Fatalf("begin executor run: %v", err)
	}
	if state.Tasks[0].Status != StatusActive || state.Tasks[0].Attempts != 1 || state.Tasks[0].ExecutorRunID != run.ID || state.RunEventWatermark != 2 {
		t.Fatalf("executor start projection = task %+v watermark %d", state.Tasks[0], state.RunEventWatermark)
	}
	run.Outcome = ExecutorOutcomeSucceeded
	run.EndedAt = now.Add(15 * time.Second)
	run.ExitCode = 0
	run.Completed = true
	run.FinishReason = "completed"
	run.DurationMilliseconds = 14_000
	run.ResultSummary = "exit=0 finish=completed in 14s"
	run.RepositoryAfter = &VerificationSubject{HeadCommit: "head-after", TreeSHA: "tree-after"}
	if err := CompleteExecutorRun(stateDir, &state, run); err != nil {
		t.Fatalf("complete executor run: %v", err)
	}
	if state.RunEventWatermark != 3 || state.Tasks[0].ExecutorRunID != run.ID || state.Tasks[0].LastResult != run.ResultSummary || state.Tasks[0].ExecutorRunProcessed {
		t.Fatalf("executor result projection = task %+v watermark %d", state.Tasks[0], state.RunEventWatermark)
	}
	drift := state
	drift.Tasks = append([]Task(nil), state.Tasks...)
	drift.Tasks[0].LastResult = "contradictory executor result"
	if err := drift.SaveDir(stateDir); err == nil || !strings.Contains(err.Error(), "contradicts its watermark event") {
		t.Fatalf("SaveDir accepted executor projection drift: %v", err)
	}
	loaded, ok, err := LoadExecutorRun(stateDir, state.SessionID, run.ID)
	if err != nil || !ok || loaded.Outcome != ExecutorOutcomeSucceeded || loaded.RepositoryAfter == nil || loaded.RepositoryAfter.TreeSHA != "tree-after" {
		t.Fatalf("load executor result = %+v found=%t err=%v", loaded, ok, err)
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 3 || events[1].Type != RunEventExecutorStarted || events[2].Type != RunEventExecutorResult {
		t.Fatalf("executor event history = %+v err=%v", events, err)
	}
	if events[1].Sequence != 2 || events[2].Sequence != 3 || events[1].EpochID != events[2].EpochID {
		t.Fatalf("executor events lost run ordering or epoch identity: %+v", events[1:])
	}
}

func TestExecutorRunResultProjectionFailureReplaysOnce(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	run := executorRunFixture(t, stateDir, &state, now.Add(time.Second))
	var err error
	run, err = BeginExecutorRun(stateDir, &state, run)
	if err != nil {
		t.Fatal(err)
	}
	run.Outcome = ExecutorOutcomeSucceeded
	run.EndedAt = now.Add(10 * time.Second)
	run.Completed = true
	run.ResultSummary = "executor completed"
	run.RepositoryAfter = &VerificationSubject{HeadCommit: "head-after", TreeSHA: "tree-after"}
	event := RunEvent{
		EventID: executorEventID(run.ID, "result"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: run.EndedAt, Actor: "deep-coordinator", Type: RunEventExecutorResult,
		FromPhase: state.Phase, ToPhase: state.Phase, ExecutorRun: &run,
		TaskSummary: summarizeRunTasks(state.Tasks),
	}
	err = appendAndProjectRunEvent(stateDir, &state, event, func(string, *DeepState) error {
		return errors.New("injected executor projection failure")
	})
	if err == nil || !strings.Contains(err.Error(), "is durable but deep.json projection update failed") {
		t.Fatalf("complete event projection failure = %v", err)
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil {
		t.Fatalf("replay executor result: %v", err)
	}
	if loaded.RunEventWatermark != 3 || loaded.Tasks[0].ExecutorRunID != run.ID || loaded.Tasks[0].LastResult != run.ResultSummary {
		t.Fatalf("replayed executor result = watermark %d task %+v", loaded.RunEventWatermark, loaded.Tasks[0])
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 3 {
		t.Fatalf("replay duplicated result event: count=%d err=%v", len(events), err)
	}
	if _, ok, err := LoadExecutorRun(stateDir, state.SessionID, run.ID); err != nil || !ok {
		t.Fatalf("recovered executor record found=%t err=%v", ok, err)
	}
}

func TestUnmatchedExecutorStartBecomesExplicitRecoveryBlock(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	run := executorRunFixture(t, stateDir, &state, now.Add(time.Second))
	run, err := BeginExecutorRun(stateDir, &state, run)
	if err != nil {
		t.Fatal(err)
	}
	firstEpoch := state.ExecutionEpochID
	if err := BeginResumeEpoch(stateDir, &state, PhaseExecuting, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	recovered, err := RecoverUnmatchedExecutorRun(stateDir, &state, now.Add(time.Minute+time.Second))
	if err != nil || recovered == nil || recovered.ID != run.ID || recovered.Outcome != ExecutorOutcomeUnknown {
		t.Fatalf("unmatched recovery = %+v err=%v", recovered, err)
	}
	if state.ExecutionEpochID == firstEpoch || state.RunEventWatermark != 4 || !state.ExecutionQuiescenceUnconfirmed ||
		state.ExecutionQuiescenceTaskID != "T-1" || state.Tasks[0].Status != StatusNeedsHuman {
		t.Fatalf("recovery projection = epoch %q watermark %d blocked=%t task=%q state=%+v", state.ExecutionEpochID, state.RunEventWatermark, state.ExecutionQuiescenceUnconfirmed, state.ExecutionQuiescenceTaskID, state.Tasks[0])
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 4 || events[1].Sequence != 2 || events[3].Sequence != 4 || events[3].Type != RunEventExecutorRecoveryRequired {
		t.Fatalf("recovery history = %+v err=%v", events, err)
	}
	if again, err := RecoverUnmatchedExecutorRun(stateDir, &state, now.Add(2*time.Minute)); err != nil || again != nil {
		t.Fatalf("recovery was not idempotent: run=%+v err=%v", again, err)
	}
}
