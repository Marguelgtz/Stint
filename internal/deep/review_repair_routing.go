package deep

import (
	"fmt"
	"time"
)

// ReconcileReviewRepairs resumes journal-backed graph expansion and finding
// resolution. Repeated calls are idempotent and never rewrite review history.
func ReconcileReviewRepairs(stateDir string, state *DeepState, at time.Time) error {
	if state == nil || !HasSemanticReviewContract(state.SemanticReviewContractVersion) ||
		state.AcceptanceContractVersion != DeterministicAcceptanceContractVersion {
		return nil
	}
	if state.RunEventSchemaVersion != RunEventSchemaVersion || state.Phase != PhaseExecuting || state.ExecutionQuiescenceUnconfirmed {
		return nil
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.Type != RunEventReviewResult || event.ReviewCycle == nil || event.ReviewCycle.SchemaVersion != reviewCycleSchemaGated ||
			event.ReviewCycle.Outcome != ReviewOutcomeFindings {
			continue
		}
		cycle := *event.ReviewCycle
		parent, ok := findTask(state, cycle.TaskID)
		if !ok || parent.ReviewCycleID != cycle.ID || parent.ReviewCheckpointEventID != cycle.CheckpointEventID ||
			parent.AcceptanceCheckpointEventID != cycle.CheckpointEventID || parent.AcceptanceOutcome != AcceptanceAccepted {
			continue
		}
		for _, finding := range cycle.Findings {
			projectedFinding, exists := reviewFindingByID(parent.ReviewFindings, finding.ID)
			if !exists || projectedFinding.Disposition != ReviewFindingOpen || finding.Disposition != ReviewFindingOpen || finding.RepairTaskID != "" {
				continue
			}
			if err := RecordReviewRepairWorkUnit(stateDir, state, cycle.ID, finding.ID, at); err != nil {
				return fmt.Errorf("create repair Work Unit for finding %s/%s: %w", cycle.ID, finding.ID, err)
			}
		}
	}
	for progress := true; progress; {
		progress = false
		for _, task := range append([]Task(nil), state.Tasks...) {
			if !isReviewRepairTask(task) {
				continue
			}
			ctx := task.RepairContext
			parent, ok := findTask(state, ctx.ParentTaskID)
			if !ok || parent.ReviewCycleID != ctx.ReviewCycleID {
				continue
			}
			finding, ok := reviewFindingByID(parent.ReviewFindings, ctx.FindingID)
			if !ok || finding.Disposition != ReviewFindingRepairCreated || finding.RepairTaskID != task.ID {
				continue
			}
			if !taskHasSemanticCompletion(*state, task.ID, make(map[string]bool)) {
				continue
			}
			if task.ReviewCycleID == "" {
				continue
			}
			record := ReviewFindingResolution{
				TaskID: ctx.ParentTaskID, ReviewCycleID: ctx.ReviewCycleID,
				FindingID: ctx.FindingID, RepairTaskID: task.ID,
				ResolutionReviewCycleID: task.ReviewCycleID,
			}
			if err := RecordReviewFindingResolved(stateDir, state, record, at); err != nil {
				return fmt.Errorf("resolve review finding %s/%s through repair Work Unit %s: %w", ctx.ReviewCycleID, ctx.FindingID, task.ID, err)
			}
			progress = true
		}
	}
	return nil
}
