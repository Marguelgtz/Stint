package deep

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
	"time"
)

func reviewCycleFixture(t *testing.T) (string, DeepState, ReviewCycle, time.Time) {
	t.Helper()
	stateDir, state, acceptance, now := acceptanceRunFixture(t)
	acceptance = successfulAcceptanceRun(acceptance, now, TaskCheckpointSubject(acceptance.Checkpoint))
	if err := CompleteAcceptanceRun(stateDir, &state, acceptance); err != nil {
		t.Fatalf("complete deterministic acceptance: %v", err)
	}
	hash := sha256.Sum256([]byte("review packet"))
	id, err := NewReviewCycleID()
	if err != nil {
		t.Fatal(err)
	}
	cycle := ReviewCycle{
		ID: id, TaskID: acceptance.TaskID, Attempt: acceptance.Attempt,
		PolicySHA256: acceptance.ContractSHA256, ContextSHA256: hex.EncodeToString(hash[:]),
		CheckpointEventID: acceptance.CheckpointEventID, Checkpoint: acceptance.Checkpoint,
		Reviewer: "hermes-readonly-v1", Provider: "fixture", Model: "review-model",
		StartedAt: now.Add(40 * time.Second),
	}
	return stateDir, state, cycle, now
}

func TestReviewCycleJournalStoresBoundedStructuredFindingWithoutChangingAcceptance(t *testing.T) {
	stateDir, state, cycle, now := reviewCycleFixture(t)
	started, err := BeginReviewCycle(stateDir, &state, cycle)
	if err != nil {
		t.Fatalf("begin review cycle: %v", err)
	}
	if state.Tasks[0].Status != StatusAccepted || state.Tasks[0].ReviewOutcome != ReviewOutcomeStarted {
		t.Fatalf("review start changed deterministic acceptance: %+v", state.Tasks[0])
	}
	cycle = started
	cycle.EndedAt = now.Add(55 * time.Second)
	cycle.DurationMilliseconds = cycle.EndedAt.Sub(cycle.StartedAt).Milliseconds()
	cycle.Findings = []ReviewFinding{{
		ID: "F-1", Severity: ReviewSeverityHigh,
		Summary:   "The requested behavior is not exposed by the public API.",
		Evidence:  "The checkpoint contains only an internal helper declaration.",
		Locations: []string{"internal/api.go:42"}, Disposition: ReviewFindingOpen,
	}}
	cycle.Outcome = ReviewOutcomeFindings
	if err := CompleteReviewCycle(stateDir, &state, cycle); err != nil {
		t.Fatalf("complete review cycle: %v", err)
	}
	if state.Tasks[0].Status != StatusAccepted || state.Tasks[0].AcceptanceOutcome != AcceptanceAccepted ||
		state.Tasks[0].ReviewOutcome != ReviewOutcomeFindings || len(state.Tasks[0].ReviewFindings) != 1 {
		t.Fatalf("review facts overwrote deterministic acceptance or were lost: %+v", state.Tasks[0])
	}
	loaded, found, err := LoadReviewCycle(stateDir, state.SessionID, cycle.ID)
	if err != nil || !found || !reflect.DeepEqual(loaded, cycle) {
		t.Fatalf("loaded review cycle = found %t, cycle %+v, err %v", found, loaded, err)
	}
	state, err = LoadState(stateDir, state.SessionID)
	if err != nil {
		t.Fatalf("load review projection: %v", err)
	}
	if state.RunEventWatermark == 0 || state.Tasks[0].ReviewCheckpointTreeSHA != cycle.Checkpoint.TreeSHA {
		t.Fatalf("review projection lost checkpoint provenance: %+v", state.Tasks[0])
	}
}

func TestReviewCycleResultProjectionFailureReplaysOnce(t *testing.T) {
	stateDir, state, cycle, now := reviewCycleFixture(t)
	started, err := BeginReviewCycle(stateDir, &state, cycle)
	if err != nil {
		t.Fatal(err)
	}
	cycle = started
	cycle.EndedAt = now.Add(50 * time.Second)
	cycle.DurationMilliseconds = cycle.EndedAt.Sub(cycle.StartedAt).Milliseconds()
	cycle.Outcome = ReviewOutcomeClear
	projected := state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if err := applyReviewResult(&projected, cycle); err != nil {
		t.Fatal(err)
	}
	event := RunEvent{
		EventID: reviewCycleEventID(cycle.ID, "result"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: cycle.EndedAt, Actor: "deep-reviewer", Type: RunEventReviewResult,
		FromPhase: state.Phase, ToPhase: state.Phase, ReviewCycle: &cycle,
		TaskSummary: summarizeRunTasks(projected.Tasks),
	}
	projectionErr := errors.New("injected review projection failure")
	if err := appendAndProjectRunEvent(stateDir, &state, event, func(string, *DeepState) error { return projectionErr }); !errors.Is(err, projectionErr) {
		t.Fatalf("append/project error = %v, want injected projection error", err)
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil {
		t.Fatalf("replay durable review result: %v", err)
	}
	if loaded.Tasks[0].ReviewOutcome != ReviewOutcomeClear || loaded.RunEventWatermark != state.RunEventWatermark+1 {
		t.Fatalf("review replay projection = %+v watermark %d; pre-replay watermark %d", loaded.Tasks[0], loaded.RunEventWatermark, state.RunEventWatermark)
	}
	if err := CompleteReviewCycle(stateDir, &loaded, cycle); err != nil {
		t.Fatalf("idempotently reconcile durable result: %v", err)
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	results := 0
	for _, event := range events {
		if event.Type == RunEventReviewResult && event.ReviewCycle != nil && event.ReviewCycle.ID == cycle.ID {
			results++
		}
	}
	if results != 1 {
		t.Fatalf("review result event count = %d, want exactly one", results)
	}
}

func TestReviewCycleMissingOutcomeCannotBecomeClear(t *testing.T) {
	stateDir, state, cycle, now := reviewCycleFixture(t)
	started, err := BeginReviewCycle(stateDir, &state, cycle)
	if err != nil {
		t.Fatal(err)
	}
	started.EndedAt = now.Add(50 * time.Second)
	if err := CompleteReviewCycle(stateDir, &state, started); err == nil {
		t.Fatal("review with no explicit result outcome was accepted as clear")
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Tasks[0].ReviewOutcome != ReviewOutcomeStarted {
		t.Fatalf("invalid result changed review projection: %+v", loaded.Tasks[0])
	}
}

func TestLegacyMissionCannotAcquireSemanticReviewFacts(t *testing.T) {
	now := time.Date(2026, 9, 27, 14, 0, 0, 0, time.UTC)
	mission := Mission{Name: "legacy", Objective: "inspect only", Tasks: []Task{{ID: "T-1", Objective: "inspect", Status: StatusQueued}}}
	state := NewState("run-legacy-review", mission, "/repo", "/repo/.stint-deep/legacy", now.Add(time.Hour), now.Add(50*time.Minute), 1, now)
	stateDir := t.TempDir()
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("legacy context"))
	cycle := ReviewCycle{
		ID: "00112233445566778899aabbccddeeff", TaskID: "T-1", Attempt: 1,
		PolicySHA256: hex.EncodeToString(hash[:]), ContextSHA256: hex.EncodeToString(hash[:]),
		Reviewer: "hermes-readonly-v1", StartedAt: now.Add(time.Second),
	}
	if _, err := BeginReviewCycle(stateDir, &state, cycle); err == nil {
		t.Fatal("legacy mission accepted new semantic review facts without an explicit contract")
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type == RunEventReviewStarted {
			t.Fatal("legacy review attempt appended a synthetic review event")
		}
	}
}

func TestReviewCycleResultCannotBeReusedAcrossResumeEpoch(t *testing.T) {
	stateDir, state, cycle, now := reviewCycleFixture(t)
	started, err := BeginReviewCycle(stateDir, &state, cycle)
	if err != nil {
		t.Fatal(err)
	}
	if err := BeginResumeEpoch(stateDir, &state, PhaseExecuting, now.Add(time.Minute)); err != nil {
		t.Fatalf("resume: %v", err)
	}
	started.EndedAt = now.Add(time.Minute + time.Second)
	started.DurationMilliseconds = started.EndedAt.Sub(started.StartedAt).Milliseconds()
	started.Outcome = ReviewOutcomeClear
	if err := CompleteReviewCycle(stateDir, &state, started); err == nil {
		t.Fatal("review result from an earlier epoch was reused after resume")
	}
	if _, err := RecoverUnmatchedReviewCycle(stateDir, &state, now.Add(time.Minute+2*time.Second)); err != nil {
		t.Fatalf("recover stale review result as unknown: %v", err)
	}
}

func TestResumeRecoversUnmatchedReviewAsUnknownWithoutChangingVerifiedEvidence(t *testing.T) {
	stateDir, state, cycle, now := reviewCycleFixture(t)
	started, err := BeginReviewCycle(stateDir, &state, cycle)
	if err != nil {
		t.Fatal(err)
	}
	if err := BeginResumeEpoch(stateDir, &state, PhaseExecuting, now.Add(time.Minute)); err != nil {
		t.Fatalf("resume after interrupted review: %v", err)
	}
	recovered, err := RecoverUnmatchedReviewCycle(stateDir, &state, now.Add(time.Minute+time.Second))
	if err != nil || recovered == nil {
		t.Fatalf("review recovery = %+v, err %v", recovered, err)
	}
	if recovered.ID != started.ID || recovered.Outcome != ReviewOutcomeUnknown || recovered.Reason == "" ||
		state.Tasks[0].ReviewOutcome != ReviewOutcomeUnknown || state.Tasks[0].Status != StatusAccepted ||
		state.Tasks[0].AcceptanceOutcome != AcceptanceAccepted || state.Tasks[0].CheckpointTreeSHA != cycle.Checkpoint.TreeSHA {
		t.Fatalf("recovery invented a disposition or lost prior evidence: cycle %+v task %+v", recovered, state.Tasks[0])
	}
	if again, err := RecoverUnmatchedReviewCycle(stateDir, &state, now.Add(2*time.Minute)); err != nil || again != nil {
		t.Fatalf("second recovery = %+v, err %v; want no unmatched cycle", again, err)
	}
}
