package deep

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func completedFindingReview(t *testing.T) (string, DeepState, ReviewCycle, time.Time) {
	t.Helper()
	stateDir, state, cycle, now := reviewCycleFixture(t)
	started, err := BeginReviewCycle(stateDir, &state, cycle)
	if err != nil {
		t.Fatalf("begin finding review: %v", err)
	}
	started.Outcome = ReviewOutcomeFindings
	started.Findings = []ReviewFinding{{
		ID: "F-1", Severity: ReviewSeverityHigh,
		Summary: "The requested behavior is absent.", Evidence: "The checkpoint exposes only an internal helper.",
		Locations: []string{"internal/api.go:42"}, Disposition: ReviewFindingOpen,
	}}
	started.EndedAt = now.Add(time.Minute)
	started.DurationMilliseconds = started.EndedAt.Sub(started.StartedAt).Milliseconds()
	if err := CompleteReviewCycle(stateDir, &state, started); err != nil {
		t.Fatalf("complete finding review: %v", err)
	}
	return stateDir, state, started, now
}

func TestReviewFindingCreatesOneJournaledRepairWorkUnitWithoutChangingMissionContract(t *testing.T) {
	stateDir, state, cycle, now := completedFindingReview(t)
	wantContract := state.AcceptanceContractSHA256
	if err := RecordReviewRepairWorkUnit(stateDir, &state, cycle.ID, "F-1", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("create repair Work Unit: %v", err)
	}
	if len(state.Tasks) != 2 {
		t.Fatalf("task count = %d, want parent plus one repair: %+v", len(state.Tasks), state.Tasks)
	}
	parent, repair := state.Tasks[0], state.Tasks[1]
	if repair.ID != reviewRepairTaskID(cycle.ID, "F-1") || !isReviewRepairTask(repair) ||
		repair.Status != StatusQueued || repair.RepositoryChange != RepositoryChangeRequired || repair.AcceptanceCheck != parent.AcceptanceCheck ||
		repair.RepairContext == nil || repair.RepairContext.ParentTaskID != parent.ID || repair.RepairContext.ReviewCycleID != cycle.ID ||
		repair.RepairContext.Finding.ID != "F-1" || parent.ReviewFindings[0].Disposition != ReviewFindingRepairCreated ||
		parent.ReviewFindings[0].RepairTaskID != repair.ID {
		t.Fatalf("repair Work Unit or source finding projection is invalid: parent=%+v repair=%+v", parent, repair)
	}
	identity, err := AcceptanceContractIdentity(state.MissionDefinition())
	if err != nil || identity != wantContract {
		t.Fatalf("dynamic repair changed authored mission contract identity: identity=%s want=%s err=%v", identity, wantContract, err)
	}
	if err := RecordReviewRepairWorkUnit(stateDir, &state, cycle.ID, "F-1", now.Add(3*time.Minute)); err != nil {
		t.Fatalf("repeat repair creation: %v", err)
	}
	if len(state.Tasks) != 2 || state.RunEventWatermark == 0 {
		t.Fatalf("repair reconciliation was not idempotent: tasks=%d watermark=%d", len(state.Tasks), state.RunEventWatermark)
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil || len(loaded.Tasks) != 2 || loaded.Tasks[0].ReviewFindings[0].Disposition != ReviewFindingRepairCreated {
		t.Fatalf("repair history did not reload: tasks=%+v err=%v", loaded.Tasks, err)
	}
	if err := ValidateMissionAcceptanceContract(loaded.MissionDefinition()); err != nil {
		t.Fatalf("dynamic repair broke acceptance contract compatibility: %v", err)
	}
}

func TestReviewRepairEventWrittenBeforeProjectionReplaysExactlyOnce(t *testing.T) {
	stateDir, state, cycle, now := completedFindingReview(t)
	finding := cycle.Findings[0]
	record := reviewRepairRecord(cycle, finding)
	projected := state
	projected.Tasks = cloneTasksForEventProjection(state.Tasks)
	if err := applyReviewRepairCreated(&projected, record); err != nil {
		t.Fatalf("prepare repair projection: %v", err)
	}
	event := RunEvent{
		EventID: reviewRepairCreatedEventID(cycle.ID, finding.ID), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: now.Add(2 * time.Minute), Actor: "deep-coordinator", Type: RunEventReviewRepairCreated,
		FromPhase: PhaseExecuting, ToPhase: PhaseExecuting, ReviewRepair: &record,
		TaskSummary: summarizeRunTasks(projected.Tasks),
	}
	writeFailure := errors.New("simulated deep.json projection failure")
	if err := appendAndProjectRunEvent(stateDir, &state, event, func(string, *DeepState) error { return writeFailure }); !errors.Is(err, writeFailure) {
		t.Fatalf("projection failure = %v, want injected failure", err)
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil {
		t.Fatalf("replay durable repair event: %v", err)
	}
	if len(loaded.Tasks) != 2 || loaded.RunEventWatermark != state.RunEventWatermark+1 ||
		loaded.Tasks[0].ReviewFindings[0].Disposition != ReviewFindingRepairCreated ||
		loaded.Tasks[0].ReviewFindings[0].RepairTaskID != record.TaskID {
		t.Fatalf("repair event replay duplicated or lost its transition: watermark=%d tasks=%+v", loaded.RunEventWatermark, loaded.Tasks)
	}
	if err := RecordReviewRepairWorkUnit(stateDir, &loaded, cycle.ID, finding.ID, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("repeat repair creation after replay: %v", err)
	}
	if len(loaded.Tasks) != 2 {
		t.Fatalf("repair was duplicated after replay: %+v", loaded.Tasks)
	}
}

func TestReviewFindingCannotResolveWithoutAcceptedReReview(t *testing.T) {
	stateDir, state, cycle, now := completedFindingReview(t)
	if err := RecordReviewRepairWorkUnit(stateDir, &state, cycle.ID, "F-1", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	repair := state.Tasks[1]
	record := ReviewFindingResolution{
		TaskID: state.Tasks[0].ID, ReviewCycleID: cycle.ID, FindingID: "F-1", RepairTaskID: repair.ID,
		ResolutionReviewCycleID: strings.Repeat("a", 32),
	}
	if err := RecordReviewFindingResolved(stateDir, &state, record, now.Add(3*time.Minute)); err == nil {
		t.Fatal("finding resolved without accepted repair evidence and a semantic re-review")
	}
	if found, err := reviewFindingResolutionMustRead(stateDir, state.SessionID, cycle.ID, "F-1"); err != nil || found {
		t.Fatalf("invalid resolution entered the journal: found=%t err=%v", found, err)
	}
}

func reviewFindingResolutionMustRead(stateDir, sessionID, cycleID, findingID string) (bool, error) {
	events, err := ReadRunEvents(stateDir, sessionID)
	if err != nil {
		return false, err
	}
	_, found := reviewFindingResolution(events, cycleID, findingID)
	return found, nil
}

func TestReviewRepairProjectionRejectsUnjournaledContractEdits(t *testing.T) {
	stateDir, state, cycle, now := completedFindingReview(t)
	if err := RecordReviewRepairWorkUnit(stateDir, &state, cycle.ID, "F-1", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	forged := state
	forged.Tasks = cloneTasksForEventProjection(state.Tasks)
	forged.Tasks[1].Objective = "silently replace the generated repair contract"
	if err := forged.SaveDir(stateDir); err == nil {
		t.Fatal("unjournaled repair Work Unit contract edit was persisted")
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil || !reflect.DeepEqual(loaded.Tasks[1].RepairContext, state.Tasks[1].RepairContext) || loaded.Tasks[1].ID != reviewRepairTaskID(cycle.ID, "F-1") {
		t.Fatalf("failed contract edit affected canonical repair: task=%+v err=%v", loaded.Tasks[1], err)
	}
}
