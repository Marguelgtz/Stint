package deep

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecutorReceiptPersistsOnlyBoundedQuiescentFacts(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	run, err := NewExecutorRunID()
	if err != nil {
		t.Fatal(err)
	}
	receipt := ExecutorReceipt{
		SchemaVersion: ExecutorReceiptSchemaVersion, ExecutorRunID: run,
		EndedAtUnixNano: now.Add(5 * time.Second).UnixNano(), DurationMillis: 4_000,
		ExitCode: 0, Launched: true, Completed: true, ProcessQuiescent: true,
		RepositoryAfterHeadCommit: "head-after", RepositoryAfterTreeSHA: "tree-after",
		RepositoryAfterObservedAtUnixNano: now.Add(6 * time.Second).UnixNano(),
	}
	if err := PersistExecutorReceipt(stateDir, state.SessionID, receipt); err != nil {
		t.Fatalf("persist executor receipt: %v", err)
	}
	loaded, found, err := LoadExecutorReceipt(stateDir, state.SessionID, run)
	if err != nil || !found || loaded != receipt {
		t.Fatalf("loaded executor receipt=%+v found=%t err=%v", loaded, found, err)
	}
	path, err := ExecutorReceiptPath(stateDir, state.SessionID, run)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("executor receipt mode=%v err=%v, want 0600", info, err)
	}
	if _, err := ExecutorReceiptPath(stateDir, state.SessionID, "../../outside"); err == nil {
		t.Fatal("executor receipt accepted a path-like run identity")
	}
	if _, err := ExecutorReceiptPath(stateDir, "..", receipt.ExecutorRunID); err == nil {
		t.Fatal("executor receipt accepted a path-like session identity")
	}
	for name, mutate := range map[string]func(*ExecutorReceipt){
		"not quiescent":         func(r *ExecutorReceipt) { r.ProcessQuiescent = false },
		"success exit mismatch": func(r *ExecutorReceipt) { r.ExitCode = 1 },
		"timeout zero exit":     func(r *ExecutorReceipt) { r.TimedOut = true },
		"prelaunch success":     func(r *ExecutorReceipt) { r.Launched = false },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := receipt
			mutate(&invalid)
			if err := ValidateExecutorReceipt(invalid); err == nil {
				t.Fatalf("accepted contradictory receipt: %+v", invalid)
			}
		})
	}
}

func TestExecutorReceiptReconcilesUnknownInvocationInLaterEpoch(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	start, err := BeginExecutorRun(stateDir, &state, executorRunFixture(t, stateDir, &state, now.Add(time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	firstEpoch := state.ExecutionEpochID
	if err := BeginResumeEpoch(stateDir, &state, PhaseExecuting, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	unknown, err := RecoverUnmatchedExecutorRun(stateDir, &state, now.Add(time.Minute+time.Second))
	if err != nil || unknown == nil || unknown.Outcome != ExecutorOutcomeUnknown {
		t.Fatalf("record unknown invocation: run=%+v err=%v", unknown, err)
	}
	if err := BeginResumeEpoch(stateDir, &state, PhaseExecuting, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	receipt := ExecutorReceipt{
		SchemaVersion: ExecutorReceiptSchemaVersion, ExecutorRunID: start.ID,
		// The remote supervisor's wall clock may differ from the coordinator's.
		// Event sequence and the receipt duration establish causal order; wall
		// timestamps retain their own clock provenance.
		EndedAtUnixNano: now.Add(-time.Minute).UnixNano(), DurationMillis: 14_000,
		ExitCode: 0, Launched: true, Completed: true, ProcessQuiescent: true,
		RepositoryAfterHeadCommit: "recovered-head", RepositoryAfterTreeSHA: "recovered-tree",
		RepositoryAfterObservedAtUnixNano: now.Add(-time.Minute + 20*time.Second).UnixNano(),
	}
	if err := PersistExecutorReceipt(stateDir, state.SessionID, receipt); err != nil {
		t.Fatal(err)
	}
	observedAt := now.Add(2*time.Minute + time.Second)
	recoverySubject := &VerificationSubject{HeadCommit: "recovered-head", TreeSHA: "recovered-tree"}
	reconciled, err := ReconcileExecutorRunReceipt(stateDir, &state, receipt, recoverySubject, "", observedAt)
	if err != nil {
		t.Fatalf("reconcile durable completion receipt: %v", err)
	}
	if reconciled.Outcome != ExecutorOutcomeSucceeded || !reconciled.EndedAt.Equal(time.Unix(0, receipt.EndedAtUnixNano).UTC()) ||
		reconciled.EndTimeSource != ExecutorEndTimeSupervisor || reconciled.RepositoryAfter == nil ||
		reconciled.RepositoryAfter.HeadCommit != receipt.RepositoryAfterHeadCommit || reconciled.RepositoryAfter.TreeSHA != receipt.RepositoryAfterTreeSHA ||
		reconciled.RepositoryAfterObservedAt.UnixNano() != receipt.RepositoryAfterObservedAtUnixNano || reconciled.RepositoryAtRecovery == nil ||
		*reconciled.RepositoryAtRecovery != *recoverySubject || !reconciled.RepositoryAtRecoveryAt.Equal(observedAt) {
		t.Fatalf("reconciled executor facts = %+v", reconciled)
	}
	if state.ExecutionQuiescenceUnconfirmed || state.ExecutionQuiescenceTaskID != "" || state.Tasks[0].Status != StatusActive || state.Tasks[0].ExecutorRunProcessed {
		t.Fatalf("reconciliation did not safely restore task processing: %+v", state)
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 6 || events[1].Type != RunEventExecutorStarted || events[1].EpochID != firstEpoch ||
		events[3].Type != RunEventExecutorRecoveryRequired || events[5].Type != RunEventExecutorReconciled ||
		events[5].EpochID != state.ExecutionEpochID || events[5].OccurredAt != observedAt {
		t.Fatalf("receipt reconciliation history = %+v", events)
	}
	for i, event := range events {
		if event.Sequence != uint64(i+1) {
			t.Fatalf("event sequence %d = %d", i, event.Sequence)
		}
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil || loaded.RunEventWatermark != 6 || loaded.Tasks[0].Status != StatusActive || loaded.ExecutionQuiescenceUnconfirmed {
		t.Fatalf("replayed reconciliation projection=%+v err=%v", loaded, err)
	}
	if unmatched, err := LoadUnmatchedExecutorRun(stateDir, state.SessionID); err != nil || unmatched != nil {
		t.Fatalf("reconciled invocation remained unmatched: %+v err=%v", unmatched, err)
	}
	if recovered, err := RecoverUnmatchedExecutorRun(stateDir, &loaded, now.Add(3*time.Minute)); err != nil || recovered != nil {
		t.Fatalf("reconciliation was not idempotent: run=%+v err=%v", recovered, err)
	}
	if _, err := os.Stat(filepath.Join(DeepDir(stateDir, state.SessionID), "run-events.jsonl")); err != nil {
		t.Fatal(err)
	}
}

func TestExecutorReceiptRecoveryCaptureFailureIsDurablyBlocked(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	start, err := BeginExecutorRun(stateDir, &state, executorRunFixture(t, stateDir, &state, now.Add(time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if err := BeginResumeEpoch(stateDir, &state, PhaseExecuting, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverUnmatchedExecutorRun(stateDir, &state, now.Add(time.Minute+time.Second)); err != nil {
		t.Fatal(err)
	}
	receipt := ExecutorReceipt{
		SchemaVersion: ExecutorReceiptSchemaVersion, ExecutorRunID: start.ID,
		EndedAtUnixNano: now.Add(15 * time.Second).UnixNano(), DurationMillis: 14_000,
		ExitCode: 0, Launched: true, Completed: true, ProcessQuiescent: true,
		RepositoryAfterHeadCommit: "executor-head", RepositoryAfterTreeSHA: "executor-tree",
		RepositoryAfterObservedAtUnixNano: now.Add(16 * time.Second).UnixNano(),
	}
	observedAt := now.Add(2 * time.Minute)
	run, err := ReconcileExecutorRunReceipt(stateDir, &state, receipt, nil, "Git subject capture failed", observedAt)
	if err != nil {
		t.Fatalf("reconcile with a failed recovery-time subject capture: %v", err)
	}
	if run.RepositoryAtRecovery != nil || !run.RepositoryAtRecoveryAt.Equal(observedAt) || run.RepositoryAtRecoveryError != "Git subject capture failed" ||
		state.Tasks[0].Status != StatusNeedsHuman || state.Tasks[0].Blocker != executorReceiptRepositoryConflictBlocker || !state.Tasks[0].ExecutorRunProcessed {
		t.Fatalf("unidentified recovery repository state did not remain blocked: run=%+v state=%+v", run, state)
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil || loaded.RunEventWatermark != state.RunEventWatermark || loaded.Tasks[0].Status != StatusNeedsHuman ||
		loaded.Tasks[0].Blocker != executorReceiptRepositoryConflictBlocker {
		t.Fatalf("capture-failure reconciliation did not replay its fail-closed projection: state=%+v err=%v", loaded, err)
	}
}

func TestExecutorReconciliationEventReplaysAfterProjectionWriteFailure(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	start, err := BeginExecutorRun(stateDir, &state, executorRunFixture(t, stateDir, &state, now.Add(time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if err := BeginResumeEpoch(stateDir, &state, PhaseExecuting, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverUnmatchedExecutorRun(stateDir, &state, now.Add(time.Minute+time.Second)); err != nil {
		t.Fatal(err)
	}
	receipt := ExecutorReceipt{
		SchemaVersion: ExecutorReceiptSchemaVersion, ExecutorRunID: start.ID,
		EndedAtUnixNano: now.Add(15 * time.Second).UnixNano(), DurationMillis: 14_000,
		ExitCode: 0, Launched: true, Completed: true, ProcessQuiescent: true,
		RepositoryAfterHeadCommit: "recovered-head", RepositoryAfterTreeSHA: "recovered-tree",
		RepositoryAfterObservedAtUnixNano: now.Add(16 * time.Second).UnixNano(),
	}
	observedAt := now.Add(2 * time.Minute)
	recoverySubject := &VerificationSubject{HeadCommit: "recovered-head", TreeSHA: "recovered-tree"}
	run, err := executorRunFromReceipt(start, receipt, recoverySubject, "", observedAt)
	if err != nil {
		t.Fatal(err)
	}
	event := RunEvent{
		EventID: executorEventID(run.ID, "reconciled"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: observedAt, Actor: "deep-coordinator", Type: RunEventExecutorReconciled,
		FromPhase: state.Phase, ToPhase: state.Phase, Reason: "durable executor completion receipt reconciled", ExecutorRun: &run,
	}
	projected := state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if err := applyExecutorReconciled(&projected, run); err != nil {
		t.Fatal(err)
	}
	event.TaskSummary = summarizeRunTasks(projected.Tasks)
	err = appendAndProjectRunEvent(stateDir, &state, event, func(string, *DeepState) error {
		return errors.New("simulated deep.json write failure")
	})
	if err == nil || !strings.Contains(err.Error(), "simulated deep.json write failure") {
		t.Fatalf("reconciliation unexpectedly hid projection failure: %v", err)
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || events[len(events)-1].Type != RunEventExecutorReconciled {
		t.Fatalf("durable reconciliation event missing after projection failure: events=%+v err=%v", events, err)
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil {
		t.Fatalf("replay reconciliation after projection failure: %v", err)
	}
	if loaded.RunEventWatermark != uint64(len(events)) || loaded.ExecutionQuiescenceUnconfirmed || loaded.Tasks[0].Status != StatusActive {
		t.Fatalf("projection replay did not apply reconciliation exactly once: %+v", loaded)
	}
	if unmatched, err := LoadUnmatchedExecutorRun(stateDir, state.SessionID); err != nil || unmatched != nil {
		t.Fatalf("replayed executor reconciliation remained unmatched: %+v err=%v", unmatched, err)
	}
	if _, err := RecoverUnmatchedExecutorRun(stateDir, &loaded, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("recovery after replay: %v", err)
	}
	afterRecovery, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(afterRecovery) != len(events) {
		t.Fatalf("replayed reconciliation duplicated history: before=%d after=%d err=%v", len(events), len(afterRecovery), err)
	}
}
