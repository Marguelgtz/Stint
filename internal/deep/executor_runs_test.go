package deep

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	beforeRecovery := state
	beforeRecovery.Tasks = append([]Task(nil), state.Tasks...)
	recoveryObservedAt := now.Add(time.Minute + time.Second)
	recovered, err := RecoverUnmatchedExecutorRun(stateDir, &state, recoveryObservedAt)
	if err != nil || recovered == nil || recovered.ID != run.ID || recovered.Outcome != ExecutorOutcomeUnknown {
		t.Fatalf("unmatched recovery = %+v err=%v", recovered, err)
	}
	if !recovered.EndedAt.IsZero() || recovered.DurationMilliseconds != 0 || recovered.ExitCode != 0 || recovered.Completed ||
		recovered.FinishReason != "" || recovered.Error != "" || recovered.ResultSummary != "" || recovered.RepositoryAfter != nil {
		t.Fatalf("recovery fabricated executor terminal facts: %+v", recovered)
	}
	if state.ExecutionEpochID == firstEpoch || state.RunEventWatermark != 4 || !state.ExecutionQuiescenceUnconfirmed ||
		state.ExecutionQuiescenceTaskID != "T-1" || state.Tasks[0].Status != StatusNeedsHuman {
		t.Fatalf("recovery projection = epoch %q watermark %d blocked=%t task=%q state=%+v", state.ExecutionEpochID, state.RunEventWatermark, state.ExecutionQuiescenceUnconfirmed, state.ExecutionQuiescenceTaskID, state.Tasks[0])
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 4 || events[1].Sequence != 2 || events[3].Sequence != 4 || events[3].Type != RunEventExecutorRecoveryRequired {
		t.Fatalf("recovery history = %+v err=%v", events, err)
	}
	recoveryEvent := events[3]
	if !recoveryEvent.OccurredAt.Equal(recoveryObservedAt) || recoveryEvent.ExecutorRun == nil || !recoveryEvent.ExecutorRun.EndedAt.IsZero() {
		t.Fatalf("recovery event did not preserve observation time separately from executor end: %+v", recoveryEvent)
	}
	recoveryJSON, err := json.Marshal(recoveryEvent.ExecutorRun)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"exitCode"`, `"completed"`, `"durationMilliseconds"`} {
		if strings.Contains(string(recoveryJSON), forbidden) {
			t.Fatalf("recovery record serialized unobserved terminal field %s: %s", forbidden, recoveryJSON)
		}
	}
	// Simulate a crash after the recovery event append and before its projection
	// write: restore the prior watermark and ensure replay reconstructs the same
	// unknown outcome without inventing an end time.
	projection, err := marshalIndent(beforeRecovery)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(DeepDir(stateDir, state.SessionID), "deep.json"), projection, 0o600); err != nil {
		t.Fatal(err)
	}
	replayed, err := LoadState(stateDir, state.SessionID)
	if err != nil || replayed.RunEventWatermark != 4 || replayed.Tasks[0].Status != StatusNeedsHuman || !replayed.ExecutionQuiescenceUnconfirmed {
		t.Fatalf("replay recovery projection = %+v err=%v", replayed, err)
	}
	replayedRun, ok, err := LoadExecutorRun(stateDir, state.SessionID, run.ID)
	if err != nil || !ok || replayedRun.Outcome != ExecutorOutcomeUnknown || !replayedRun.EndedAt.IsZero() ||
		replayedRun.DurationMilliseconds != 0 || replayedRun.Completed || replayedRun.Error != "" {
		t.Fatalf("replayed recovery record invented terminal facts: run=%+v found=%t err=%v", replayedRun, ok, err)
	}
	if again, err := RecoverUnmatchedExecutorRun(stateDir, &state, now.Add(2*time.Minute)); err != nil || again != nil {
		t.Fatalf("recovery was not idempotent: run=%+v err=%v", again, err)
	}
}

func TestCompleteExecutorRunRequiresObservedEndTime(t *testing.T) {
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
	run.ExitCode = 0
	run.Completed = true
	if err := CompleteExecutorRun(stateDir, &state, run); err == nil || !strings.Contains(err.Error(), "end timestamp") {
		t.Fatalf("executor result without observed end time = %v, want rejection", err)
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 2 || events[1].Type != RunEventExecutorStarted {
		t.Fatalf("missing-time result changed canonical history: events=%+v err=%v", events, err)
	}
}

func TestExecutorAttemptsKeepIndependentJournalHistory(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}

	first := executorRunFixture(t, stateDir, &state, now.Add(time.Second))
	first, err := BeginExecutorRun(stateDir, &state, first)
	if err != nil {
		t.Fatal(err)
	}
	first.Outcome = ExecutorOutcomeFailed
	first.EndedAt = now.Add(10 * time.Second)
	first.ExitCode = 1
	first.Error = "first attempt failed"
	first.ResultSummary = "exit=1"
	first.DurationMilliseconds = 9_000
	if err := CompleteExecutorRun(stateDir, &state, first); err != nil {
		t.Fatalf("complete first executor attempt: %v", err)
	}
	state.Tasks[0].ExecutorRunProcessed = true
	if err := state.SaveDir(stateDir); err != nil {
		t.Fatalf("persist first-attempt processing boundary: %v", err)
	}

	second := executorRunFixture(t, stateDir, &state, now.Add(20*time.Second))
	second.Attempt = 2
	second, err = BeginExecutorRun(stateDir, &state, second)
	if err != nil {
		t.Fatalf("begin second executor attempt: %v", err)
	}
	second.Outcome = ExecutorOutcomeSucceeded
	second.EndedAt = now.Add(30 * time.Second)
	second.ExitCode = 0
	second.Completed = true
	second.FinishReason = "completed"
	second.ResultSummary = "exit=0"
	second.DurationMilliseconds = 10_000
	if err := CompleteExecutorRun(stateDir, &state, second); err != nil {
		t.Fatalf("complete second executor attempt: %v", err)
	}

	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 || events[1].Type != RunEventExecutorStarted || events[2].Type != RunEventExecutorResult ||
		events[3].Type != RunEventExecutorStarted || events[4].Type != RunEventExecutorResult {
		t.Fatalf("attempt history was overwritten or incomplete: %+v", events)
	}
	firstRun, firstFound, err := LoadExecutorRun(stateDir, state.SessionID, first.ID)
	if err != nil || !firstFound || firstRun.Attempt != 1 || firstRun.Outcome != ExecutorOutcomeFailed {
		t.Fatalf("first attempt history = %+v found=%t err=%v", firstRun, firstFound, err)
	}
	secondRun, secondFound, err := LoadExecutorRun(stateDir, state.SessionID, second.ID)
	if err != nil || !secondFound || secondRun.Attempt != 2 || secondRun.Outcome != ExecutorOutcomeSucceeded || first.ID == second.ID {
		t.Fatalf("second attempt history = %+v found=%t err=%v; first=%+v", secondRun, secondFound, err, firstRun)
	}
}

func TestLegacyAttemptsRemainReadableWithoutSyntheticExecutorHistory(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	state.Tasks[0].Attempts = 2
	state.Tasks[0].LastResult = "legacy attempt summary"
	if err := state.SaveDir(stateDir); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil || loaded.Tasks[0].Attempts != 2 || loaded.Tasks[0].LastResult != "legacy attempt summary" {
		t.Fatalf("legacy attempt projection = %+v err=%v", loaded.Tasks[0], err)
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 0 {
		t.Fatalf("legacy attempts acquired synthetic events: %+v err=%v", events, err)
	}
	if err := BeginResumeEpoch(stateDir, &loaded, PhaseExecuting, now.Add(time.Minute)); err != nil {
		t.Fatalf("begin first journal-aware epoch: %v", err)
	}
	run := executorRunFixture(t, stateDir, &loaded, now.Add(2*time.Minute))
	run.Attempt = 3
	if _, err := BeginExecutorRun(stateDir, &loaded, run); err != nil {
		t.Fatalf("begin first typed post-legacy executor run: %v", err)
	}
	events, err = ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 2 || events[0].Boundary != RunEventBoundaryLegacyResume ||
		events[1].Type != RunEventExecutorStarted || events[1].ExecutorRun == nil || events[1].ExecutorRun.Attempt != 3 {
		t.Fatalf("legacy transition fabricated prior executor history: %+v err=%v", events, err)
	}
}

func TestExecutorFactsRejectOversizedAndStructurallyInvalidFields(t *testing.T) {
	t.Run("record excludes prompts environment and logs", func(t *testing.T) {
		stateDir, state, now := journalFixture(t)
		if err := BeginNewRun(stateDir, &state, now); err != nil {
			t.Fatal(err)
		}
		run := executorRunFixture(t, stateDir, &state, now.Add(time.Second))
		encoded, err := json.Marshal(run)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{`"prompt"`, `"environment"`, `"credentials"`, `"logs"`, `"output"`} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("executor record contains unbounded or sensitive field %s: %s", forbidden, encoded)
			}
		}
	})
	t.Run("oversized runtime identity", func(t *testing.T) {
		stateDir, state, now := journalFixture(t)
		if err := BeginNewRun(stateDir, &state, now); err != nil {
			t.Fatal(err)
		}
		run := executorRunFixture(t, stateDir, &state, now.Add(time.Second))
		run.Runtime.Provider = strings.Repeat("p", 129)
		if _, err := BeginExecutorRun(stateDir, &state, run); err == nil || !strings.Contains(err.Error(), "runtime identity") {
			t.Fatalf("oversized runtime identity = %v, want rejection", err)
		}
	})
	t.Run("incomplete compute identity", func(t *testing.T) {
		stateDir, state, now := journalFixture(t)
		if err := BeginNewRun(stateDir, &state, now); err != nil {
			t.Fatal(err)
		}
		run := executorRunFixture(t, stateDir, &state, now.Add(time.Second))
		run.ComputeProvider = "vast"
		if _, err := BeginExecutorRun(stateDir, &state, run); err == nil || !strings.Contains(err.Error(), "compute identity") {
			t.Fatalf("incomplete compute identity = %v, want rejection", err)
		}
	})
	t.Run("oversized artifact reference", func(t *testing.T) {
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
		run.Outcome = ExecutorOutcomeFailed
		run.EndedAt = now.Add(2 * time.Second)
		run.ArtifactRefs = []string{strings.Repeat("a", 257)}
		if err := CompleteExecutorRun(stateDir, &state, run); err == nil || !strings.Contains(err.Error(), "artifact reference") {
			t.Fatalf("oversized artifact reference = %v, want rejection", err)
		}
	})
	t.Run("oversized result summary", func(t *testing.T) {
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
		run.Outcome = ExecutorOutcomeFailed
		run.EndedAt = now.Add(2 * time.Second)
		run.ResultSummary = strings.Repeat("x", maxExecutorRunSummaryBytes+1)
		if err := CompleteExecutorRun(stateDir, &state, run); err == nil || !strings.Contains(err.Error(), "result facts exceed limits") {
			t.Fatalf("oversized result summary = %v, want rejection", err)
		}
	})
}
