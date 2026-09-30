package deep

import (
	"testing"
	"time"
)

func TestQualificationPublicationPlanSelectsCurrentAcceptedCheckpointsInJournalOrder(t *testing.T) {
	old := publicationFixtureCheckpoint("EXPORT-001", 1, "a", "tree-a")
	current := publicationFixtureCheckpoint("EXPORT-001", 2, "b", "tree-b")
	repair := publicationFixtureCheckpoint("REPAIR-001", 1, "c", "tree-c")
	dependent := publicationFixtureCheckpoint("EXPORT-002", 1, "d", "tree-d")
	state := DeepState{
		RunID: "run-1", RunEventSchemaVersion: RunEventSchemaVersion, RunEventWatermark: 8,
		AcceptanceContractVersion:     DeterministicAcceptanceContractVersion,
		SemanticReviewContractVersion: SemanticReviewContractVersion,
		Tasks: []Task{
			{ID: "EXPORT-002", Status: StatusCheckpointed, AcceptanceOutcome: AcceptanceNotEvaluated, AcceptanceCheckOutcome: AcceptanceCheckPassed},
			{ID: "REPAIR-001", Source: "review_repair", RepairContext: &ReviewRepairContext{ParentTaskID: "EXPORT-001"},
				Status: StatusAccepted, Attempts: 1, CheckpointCommit: repair.Commit, CheckpointTreeSHA: repair.TreeSHA,
				AcceptanceOutcome: AcceptanceAccepted, AcceptanceCheckOutcome: AcceptanceCheckPassed,
				AcceptanceRunID: "accept-repair", AcceptanceCheckpointEventID: "cp-repair", AcceptanceCheckpointCommit: repair.Commit,
				AcceptanceCheckpointTreeSHA: repair.TreeSHA},
			{ID: "EXPORT-001", Status: StatusAccepted, Attempts: 2, CheckpointCommit: current.Commit, CheckpointTreeSHA: current.TreeSHA,
				AcceptanceOutcome: AcceptanceAccepted, AcceptanceCheckOutcome: AcceptanceCheckPassed,
				AcceptanceRunID: "accept-current", AcceptanceCheckpointEventID: "cp-current", AcceptanceCheckpointCommit: current.Commit,
				AcceptanceCheckpointTreeSHA: current.TreeSHA, ReviewCycleID: "review-current", ReviewOutcome: ReviewOutcomeClear,
				ReviewCheckpointEventID: "cp-current"},
		},
	}
	events := []RunEvent{
		publicationCheckpointEvent(1, "cp-old", old),
		publicationCheckpointEvent(2, "cp-current", current),
		{Sequence: 3, EventID: "accept-current", Type: RunEventAcceptanceResult, AcceptanceRun: &AcceptanceRun{
			ID: "accept-current", TaskID: "EXPORT-001", Attempt: 2, Decision: AcceptanceAccepted,
			CheckOutcome: AcceptanceCheckPassed, CheckpointEventID: "cp-current", Checkpoint: current}},
		publicationCheckpointEvent(4, "cp-repair", repair),
		{Sequence: 5, EventID: "accept-repair", Type: RunEventAcceptanceResult, AcceptanceRun: &AcceptanceRun{
			ID: "accept-repair", TaskID: "REPAIR-001", Attempt: 1, Decision: AcceptanceAccepted,
			CheckOutcome: AcceptanceCheckPassed, CheckpointEventID: "cp-repair", Checkpoint: repair}},
		publicationCheckpointEvent(6, "cp-dependent", dependent),
		{Sequence: 7, EventID: "review-current", Type: RunEventReviewResult, ReviewCycle: &ReviewCycle{
			ID: "review-current", TaskID: "EXPORT-001", CheckpointEventID: "cp-current", Checkpoint: current,
			Outcome: ReviewOutcomeClear}},
		{Sequence: 8, EventID: "run.outcome", Type: RunEventMissionReviewResult},
	}
	plan, err := BuildQualificationPublicationPlan(state, events)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Checkpoints) != 2 {
		t.Fatalf("selected checkpoints = %+v, want current Objective and repair only", plan.Checkpoints)
	}
	if plan.Checkpoints[0].TaskID != "EXPORT-001" || plan.Checkpoints[0].Sequence != 2 ||
		plan.Checkpoints[0].AcceptanceOutcome != AcceptanceAccepted || plan.Checkpoints[0].SemanticReviewOutcome != ReviewOutcomeClear {
		t.Fatalf("current accepted Objective publication = %+v", plan.Checkpoints[0])
	}
	if plan.Checkpoints[1].TaskID != "REPAIR-001" || plan.Checkpoints[1].Sequence != 4 ||
		plan.Checkpoints[1].SemanticReviewOutcome != ReviewOutcome("not_run") {
		t.Fatalf("accepted repair publication = %+v", plan.Checkpoints[1])
	}
}

func publicationFixtureCheckpoint(taskID string, attempt int, commit, tree string) TaskCheckpoint {
	return TaskCheckpoint{TaskID: taskID, Attempt: attempt, ExecutorRunID: "executor-" + commit,
		VerificationSubject: VerificationSubject{HeadCommit: commit, TreeSHA: tree}, Commit: commit, TreeSHA: tree}
}

func publicationCheckpointEvent(sequence uint64, id string, checkpoint TaskCheckpoint) RunEvent {
	return RunEvent{Sequence: sequence, EventID: id, Type: RunEventTaskCheckpointCreated, TaskCheckpoint: &checkpoint,
		OccurredAt: time.Unix(int64(sequence), 0).UTC()}
}
