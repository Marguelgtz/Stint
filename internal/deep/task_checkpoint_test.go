package deep

import (
	"errors"
	"testing"
	"time"
)

func taskCheckpointFixture(t *testing.T) (string, DeepState, VerificationRun, TaskCheckpoint, time.Time) {
	t.Helper()
	stateDir, state, now := journalFixture(t)
	state.Verify = "go test ./..."
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	executor := executorRunFixture(t, stateDir, &state, now.Add(time.Second))
	var err error
	executor, err = BeginExecutorRun(stateDir, &state, executor)
	if err != nil {
		t.Fatal(err)
	}
	subject := VerificationSubject{HeadCommit: "head-after", TreeSHA: "tree-after"}
	executor.Outcome = ExecutorOutcomeSucceeded
	executor.EndedAt = now.Add(10 * time.Second)
	executor.ExitCode = 0
	executor.Completed = true
	executor.ResultSummary = "executor completed"
	executor.RepositoryAfter = &subject
	if err := CompleteExecutorRun(stateDir, &state, executor); err != nil {
		t.Fatal(err)
	}
	verificationID, err := NewVerificationRunID()
	if err != nil {
		t.Fatal(err)
	}
	verification := VerificationRun{
		ID: verificationID, Purpose: VerificationPurposeTask, TaskID: "T-1", Attempt: 1,
		CommandSource: "mission", CommandSHA256: VerificationCommandIdentity(state.Verify),
		Runtime:   VerificationRuntime{Worker: "hermes-onbox", Location: "compute", Shell: "sh", Protocol: "local-process-group-v1"},
		StartedAt: now.Add(11 * time.Second), TimeoutSeconds: 120, RemainingDeadlineSeconds: 2400,
		Subject: subject,
	}
	verification, err = BeginVerificationRun(stateDir, &state, verification)
	if err != nil {
		t.Fatal(err)
	}
	verification.Outcome = VerificationPassed
	verification.EndedAt = now.Add(20 * time.Second)
	verification.HasExitCode = true
	verification.ExitCode = 0
	verification.SubjectAfter = &subject
	verification.DurationMilliseconds = 9_000
	if err := CompleteVerificationRun(stateDir, &state, verification); err != nil {
		t.Fatal(err)
	}
	checkpoint := TaskCheckpoint{
		TaskID: "T-1", Attempt: 1, ExecutorRunID: executor.ID, VerificationRunID: verification.ID,
		VerificationSubject: subject, Commit: "checkpoint-commit", TreeSHA: subject.TreeSHA,
	}
	return stateDir, state, verification, checkpoint, now.Add(21 * time.Second)
}

func TestTaskCheckpointRequiresPassedExactVerificationAndProjectsCanonicalFact(t *testing.T) {
	stateDir, state, _, checkpoint, at := taskCheckpointFixture(t)
	wrongTree := checkpoint
	wrongTree.TreeSHA = "another-tree"
	if err := RecordTaskCheckpoint(stateDir, &state, wrongTree, at); err == nil {
		t.Fatal("task checkpoint accepted a tree different from its verified subject")
	}
	if state.RunEventWatermark != 5 || state.Tasks[0].Status != StatusActive {
		t.Fatalf("invalid checkpoint changed projection: watermark=%d task=%+v", state.RunEventWatermark, state.Tasks[0])
	}
	if err := RecordTaskCheckpoint(stateDir, &state, checkpoint, at); err != nil {
		t.Fatalf("record task checkpoint: %v", err)
	}
	if state.RunEventWatermark != 6 || state.Tasks[0].Status != StatusVerified ||
		state.Tasks[0].CheckpointCommit != checkpoint.Commit || state.Tasks[0].CheckpointTreeSHA != checkpoint.TreeSHA ||
		state.Tasks[0].VerifiedAt == nil || !state.Tasks[0].VerifiedAt.Equal(at) || !state.Tasks[0].ExecutorRunProcessed {
		t.Fatalf("checkpoint projection=%+v watermark=%d", state.Tasks[0], state.RunEventWatermark)
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 6 || events[5].Type != RunEventTaskCheckpointCreated ||
		events[5].TaskCheckpoint == nil || *events[5].TaskCheckpoint != checkpoint || events[5].Sequence != 6 {
		t.Fatalf("checkpoint event=%+v err=%v", events, err)
	}
	if err := RecordTaskCheckpoint(stateDir, &state, checkpoint, at.Add(time.Minute)); err != nil {
		t.Fatalf("idempotent checkpoint record: %v", err)
	}
	events, err = ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 6 || state.RunEventWatermark != 6 {
		t.Fatalf("repeated checkpoint duplicated history: watermark=%d events=%d err=%v", state.RunEventWatermark, len(events), err)
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil || loaded.Tasks[0].CheckpointCommit != checkpoint.Commit || loaded.Tasks[0].Status != StatusVerified {
		t.Fatalf("loaded checkpoint projection=%+v err=%v", loaded.Tasks[0], err)
	}

	tampered := loaded
	tampered.Tasks = append([]Task(nil), loaded.Tasks...)
	tampered.Tasks[0].CheckpointCommit = "different-commit"
	if err := tampered.SaveDir(stateDir); err == nil {
		t.Fatal("deep.json save rewrote canonical task checkpoint facts")
	}
}

func TestTaskCheckpointEventReplayAfterProjectionFailureIsExactlyOnce(t *testing.T) {
	stateDir, state, _, checkpoint, at := taskCheckpointFixture(t)
	projected := state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if err := applyTaskCheckpoint(&projected, checkpoint, at); err != nil {
		t.Fatal(err)
	}
	event := RunEvent{
		EventID: taskCheckpointEventID(checkpoint.VerificationRunID), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: at, Actor: "deep-coordinator", Type: RunEventTaskCheckpointCreated,
		FromPhase: state.Phase, ToPhase: state.Phase, TaskCheckpoint: &checkpoint,
		TaskSummary: summarizeRunTasks(projected.Tasks),
	}
	projectionErr := errors.New("injected projection write failure")
	if err := appendAndProjectRunEvent(stateDir, &state, event, func(string, *DeepState) error { return projectionErr }); !errors.Is(err, projectionErr) {
		t.Fatalf("append checkpoint with failed projection: %v", err)
	}
	if state.Tasks[0].Status != StatusActive {
		t.Fatalf("failed projection mutated caller state: %+v", state.Tasks[0])
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil || loaded.RunEventWatermark != 6 || loaded.Tasks[0].Status != StatusVerified || loaded.Tasks[0].CheckpointCommit != checkpoint.Commit {
		t.Fatalf("checkpoint replay=%+v task=%+v err=%v", loaded.RunEventWatermark, loaded.Tasks[0], err)
	}
	if err := RecordTaskCheckpoint(stateDir, &loaded, checkpoint, at); err != nil {
		t.Fatalf("reconcile already-journaled checkpoint: %v", err)
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 6 || loaded.RunEventWatermark != 6 {
		t.Fatalf("projection replay duplicated checkpoint: events=%d watermark=%d err=%v", len(events), loaded.RunEventWatermark, err)
	}
}
