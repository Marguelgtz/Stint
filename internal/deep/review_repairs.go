package deep

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

func cloneTasksForEventProjection(tasks []Task) []Task {
	out := append([]Task(nil), tasks...)
	for i := range out {
		out[i].DependsOn = append([]string(nil), tasks[i].DependsOn...)
		out[i].Findings = append([]string(nil), tasks[i].Findings...)
		out[i].ReviewFindings = cloneReviewFindings(tasks[i].ReviewFindings)
		if tasks[i].VerificationSubject != nil {
			subject := *tasks[i].VerificationSubject
			out[i].VerificationSubject = &subject
		}
		if tasks[i].AcceptanceSubject != nil {
			subject := *tasks[i].AcceptanceSubject
			out[i].AcceptanceSubject = &subject
		}
		if tasks[i].VerifiedAt != nil {
			at := *tasks[i].VerifiedAt
			out[i].VerifiedAt = &at
		}
		if tasks[i].VerificationBookkeeping != nil {
			out[i].VerificationBookkeeping = make(map[string]string, len(tasks[i].VerificationBookkeeping))
			for key, value := range tasks[i].VerificationBookkeeping {
				out[i].VerificationBookkeeping[key] = value
			}
		}
		if tasks[i].RepairContext != nil {
			ctx := *tasks[i].RepairContext
			ctx.Finding.Locations = append([]string(nil), tasks[i].RepairContext.Finding.Locations...)
			out[i].RepairContext = &ctx
		}
	}
	return out
}

// ReviewRepairContext ties a generated repair Work Unit to the immutable
// finding and checkpoint that caused it. The canonical creation event stores
// only these bounded identities; its task contract is derived from the
// already-durable parent task.
type ReviewRepairContext struct {
	ParentTaskID            string        `json:"parentTaskId"`
	ReviewCycleID           string        `json:"reviewCycleId"`
	FindingID               string        `json:"findingId"`
	SourceCheckpointEventID string        `json:"sourceCheckpointEventId"`
	SourceCheckpointCommit  string        `json:"sourceCheckpointCommit"`
	SourceCheckpointTreeSHA string        `json:"sourceCheckpointTreeSha"`
	Finding                 ReviewFinding `json:"finding"`
}

// ReviewRepairWorkUnit is the compact journal fact that expands a finding
// into one deterministic follow-up Work Unit.
type ReviewRepairWorkUnit struct {
	TaskID                  string `json:"taskId"`
	ParentTaskID            string `json:"parentTaskId"`
	ReviewCycleID           string `json:"reviewCycleId"`
	FindingID               string `json:"findingId"`
	SourceCheckpointEventID string `json:"sourceCheckpointEventId"`
	SourceCheckpointCommit  string `json:"sourceCheckpointCommit"`
	SourceCheckpointTreeSHA string `json:"sourceCheckpointTreeSha"`
}

// ReviewFindingResolution records that an accepted repair Work Unit, with its
// own checkpoint-bound semantic review, resolved one earlier finding.
type ReviewFindingResolution struct {
	TaskID                  string `json:"taskId"`
	ReviewCycleID           string `json:"reviewCycleId"`
	FindingID               string `json:"findingId"`
	RepairTaskID            string `json:"repairTaskId"`
	ResolutionReviewCycleID string `json:"resolutionReviewCycleId"`
}

func reviewRepairDigest(cycleID, findingID string) string {
	sum := sha256.Sum256([]byte(cycleID + "\x00" + findingID))
	return hex.EncodeToString(sum[:])
}

func reviewRepairTaskID(cycleID, findingID string) string {
	return "STINT-REPAIR-" + reviewRepairDigest(cycleID, findingID)[:32]
}

func reviewRepairCreatedEventID(cycleID, findingID string) string {
	return "review-repair/" + cycleID + "/" + reviewRepairDigest(cycleID, findingID)[:32] + "/created"
}

func reviewFindingResolvedEventID(cycleID, findingID string) string {
	return "review-repair/" + cycleID + "/" + reviewRepairDigest(cycleID, findingID)[:32] + "/resolved"
}

func reviewRepairRecord(cycle ReviewCycle, finding ReviewFinding) ReviewRepairWorkUnit {
	return ReviewRepairWorkUnit{
		TaskID: reviewRepairTaskID(cycle.ID, finding.ID), ParentTaskID: cycle.TaskID,
		ReviewCycleID: cycle.ID, FindingID: finding.ID,
		SourceCheckpointEventID: cycle.CheckpointEventID, SourceCheckpointCommit: cycle.Checkpoint.Commit,
		SourceCheckpointTreeSHA: cycle.Checkpoint.TreeSHA,
	}
}

func validateReviewRepairRecord(record ReviewRepairWorkUnit) error {
	if record.TaskID == "" || len(record.TaskID) > 128 || strings.ContainsAny(record.TaskID, "\x00\r\n") ||
		record.ParentTaskID == "" || len(record.ParentTaskID) > 128 || strings.ContainsAny(record.ParentTaskID, "\x00\r\n") ||
		record.ReviewCycleID == "" || len(record.ReviewCycleID) != 32 || record.FindingID == "" || len(record.FindingID) > 64 ||
		record.SourceCheckpointEventID == "" || len(record.SourceCheckpointEventID) > maxRunEventIDBytes ||
		record.SourceCheckpointCommit == "" || len(record.SourceCheckpointCommit) > 128 ||
		record.SourceCheckpointTreeSHA == "" || len(record.SourceCheckpointTreeSHA) > 128 ||
		strings.ContainsAny(record.FindingID+record.SourceCheckpointEventID+record.SourceCheckpointCommit+record.SourceCheckpointTreeSHA, "\x00\r\n") {
		return errors.New("review repair Work Unit identity or source checkpoint is invalid")
	}
	if _, err := hex.DecodeString(record.ReviewCycleID); err != nil {
		return errors.New("review repair source cycle identity is not hexadecimal")
	}
	if record.TaskID != reviewRepairTaskID(record.ReviewCycleID, record.FindingID) {
		return errors.New("review repair Work Unit identity is not deterministic")
	}
	return nil
}

func validateReviewFindingResolution(record ReviewFindingResolution) error {
	if record.TaskID == "" || len(record.TaskID) > 128 || strings.ContainsAny(record.TaskID, "\x00\r\n") ||
		record.ReviewCycleID == "" || len(record.ReviewCycleID) != 32 || record.FindingID == "" || len(record.FindingID) > 64 ||
		record.RepairTaskID == "" || len(record.RepairTaskID) > 128 || strings.ContainsAny(record.RepairTaskID, "\x00\r\n") ||
		record.ResolutionReviewCycleID == "" || len(record.ResolutionReviewCycleID) != 32 {
		return errors.New("review finding resolution identity is invalid")
	}
	for _, id := range []string{record.ReviewCycleID, record.ResolutionReviewCycleID} {
		if _, err := hex.DecodeString(id); err != nil {
			return errors.New("review finding resolution cycle identity is not hexadecimal")
		}
	}
	if strings.ContainsAny(record.TaskID+record.FindingID+record.RepairTaskID, "\x00\r\n") {
		return errors.New("review finding resolution contains an invalid identity")
	}
	return nil
}

func makeReviewRepairTask(parent Task, record ReviewRepairWorkUnit, finding ReviewFinding) Task {
	context := &ReviewRepairContext{
		ParentTaskID: record.ParentTaskID, ReviewCycleID: record.ReviewCycleID,
		FindingID: record.FindingID, SourceCheckpointEventID: record.SourceCheckpointEventID,
		SourceCheckpointCommit: record.SourceCheckpointCommit, SourceCheckpointTreeSHA: record.SourceCheckpointTreeSHA,
		Finding: finding,
	}
	context.Finding.Disposition = ReviewFindingOpen
	context.Finding.RepairTaskID = ""
	locations := ""
	if len(finding.Locations) > 0 {
		locations = " Locations: " + strings.Join(finding.Locations, ", ") + "."
	}
	return Task{
		ID:         record.TaskID,
		Objective:  fmt.Sprintf("Resolve semantic review finding %s for Objective %s: %s Evidence: %s%s", finding.ID, parent.ID, finding.Summary, finding.Evidence, locations),
		Acceptance: fmt.Sprintf("The cited review finding is resolved in this repair checkpoint. Finding evidence: %s", finding.Evidence),
		Verify:     parent.Verify, RepositoryChange: RepositoryChangeRequired, AcceptanceCheck: parent.AcceptanceCheck,
		DependsOn: append([]string(nil), parent.DependsOn...), Reasoning: parent.Reasoning,
		Status: StatusQueued, AcceptanceOutcome: AcceptanceNotEvaluated, Source: "review_repair", RepairContext: context,
	}
}

func reviewCycleByID(events []RunEvent, id string) (ReviewCycle, bool) {
	var cycle ReviewCycle
	found := false
	for _, event := range events {
		if event.ReviewCycle != nil && event.ReviewCycle.ID == id &&
			(event.Type == RunEventReviewResult || event.Type == RunEventReviewRecovery) {
			cycle, found = *event.ReviewCycle, true
		}
	}
	return cycle, found
}

func reviewCycleResultForTask(events []RunEvent, taskID string) (ReviewCycle, bool) {
	var cycle ReviewCycle
	found := false
	for _, event := range events {
		if event.ReviewCycle != nil && event.ReviewCycle.TaskID == taskID &&
			(event.Type == RunEventReviewResult || event.Type == RunEventReviewRecovery) {
			cycle, found = *event.ReviewCycle, true
		}
	}
	return cycle, found
}

func reviewRepairCreated(events []RunEvent, cycleID, findingID string) (ReviewRepairWorkUnit, bool) {
	for _, event := range events {
		if event.Type == RunEventReviewRepairCreated && event.ReviewRepair != nil &&
			event.ReviewRepair.ReviewCycleID == cycleID && event.ReviewRepair.FindingID == findingID {
			return *event.ReviewRepair, true
		}
	}
	return ReviewRepairWorkUnit{}, false
}

func reviewFindingResolution(events []RunEvent, cycleID, findingID string) (ReviewFindingResolution, bool) {
	for _, event := range events {
		if event.Type == RunEventReviewFindingResolved && event.ReviewDisposition != nil &&
			event.ReviewDisposition.ReviewCycleID == cycleID && event.ReviewDisposition.FindingID == findingID {
			return *event.ReviewDisposition, true
		}
	}
	return ReviewFindingResolution{}, false
}

// RecordReviewRepairWorkUnit turns one canonical open finding into a durable
// Work Unit. The event is sufficient to reconstruct the task from its parent
// contract and the immutable ReviewCycle result.
func RecordReviewRepairWorkUnit(stateDir string, state *DeepState, cycleID, findingID string, at time.Time) error {
	if state == nil || state.RunEventSchemaVersion != RunEventSchemaVersion || state.Phase != PhaseExecuting ||
		state.ExecutionQuiescenceUnconfirmed || !HasSemanticReviewContract(state.SemanticReviewContractVersion) {
		return errors.New("review repair creation requires an executing, quiescent run with an enabled semantic review contract")
	}
	if at.IsZero() {
		return errors.New("review repair creation requires an event timestamp")
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		return err
	}
	if old, found := reviewRepairCreated(events, cycleID, findingID); found {
		if old.TaskID != reviewRepairTaskID(cycleID, findingID) {
			return errors.New("review repair identity already has conflicting journal facts")
		}
		fresh, err := LoadState(stateDir, state.SessionID)
		if err != nil {
			return fmt.Errorf("reload durable review repair projection: %w", err)
		}
		*state = fresh
		return nil
	}
	cycle, found := reviewCycleByID(events, cycleID)
	if !found || cycle.SchemaVersion != reviewCycleSchemaGated || cycle.Outcome != ReviewOutcomeFindings {
		return errors.New("review repair source must be a canonical finding result under an enabled semantic review contract")
	}
	if latest, ok := reviewCycleResultForTask(events, cycle.TaskID); !ok || latest.ID != cycle.ID {
		return errors.New("review repair source is not the latest semantic review for its Objective")
	}
	if !hasCurrentAcceptedCheckpoint(events, cycle) {
		return errors.New("review repair source is not bound to the current accepted Objective checkpoint")
	}
	var finding ReviewFinding
	findingFound := false
	for _, candidate := range cycle.Findings {
		if candidate.ID == findingID {
			finding, findingFound = candidate, true
			break
		}
	}
	if !findingFound || finding.Disposition != ReviewFindingOpen || finding.RepairTaskID != "" {
		return errors.New("review repair source finding is missing or is not open")
	}
	parent, ok := findTask(state, cycle.TaskID)
	if !ok || parent.ReviewCycleID != cycle.ID || parent.ReviewOutcome != ReviewOutcomeFindings ||
		parent.AcceptanceOutcome != AcceptanceAccepted || parent.AcceptanceCheckpointEventID != cycle.CheckpointEventID ||
		parent.AcceptanceCheckpointCommit != cycle.Checkpoint.Commit || parent.AcceptanceCheckpointTreeSHA != cycle.Checkpoint.TreeSHA {
		return errors.New("review repair source does not match the projected accepted Objective review")
	}
	record := reviewRepairRecord(cycle, finding)
	projected := *state
	projected.Tasks = cloneTasksForEventProjection(state.Tasks)
	if err := applyReviewRepairCreated(&projected, record); err != nil {
		return err
	}
	if err := ValidateAcceptanceContract(projected.AcceptanceContractVersion, projected.Tasks); err != nil {
		return fmt.Errorf("generated review repair violates the deterministic Objective contract: %w", err)
	}
	event := RunEvent{
		EventID: reviewRepairCreatedEventID(cycleID, findingID), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: at.UTC(), Actor: "deep-coordinator", Type: RunEventReviewRepairCreated,
		FromPhase: state.Phase, ToPhase: state.Phase, ReviewRepair: &record,
		TaskSummary: summarizeRunTasks(projected.Tasks),
	}
	return appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked)
}

// RecordReviewFindingResolved closes the finding only after its linked repair
// Work Unit has accepted evidence and a checkpoint-bound semantic review.
func RecordReviewFindingResolved(stateDir string, state *DeepState, record ReviewFindingResolution, at time.Time) error {
	if state == nil || state.RunEventSchemaVersion != RunEventSchemaVersion || state.Phase != PhaseExecuting ||
		state.ExecutionQuiescenceUnconfirmed || !HasSemanticReviewContract(state.SemanticReviewContractVersion) {
		return errors.New("finding resolution requires an executing, quiescent run with an enabled semantic review contract")
	}
	if at.IsZero() {
		return errors.New("finding resolution requires an event timestamp")
	}
	if err := validateReviewFindingResolution(record); err != nil {
		return err
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		return err
	}
	if old, found := reviewFindingResolution(events, record.ReviewCycleID, record.FindingID); found {
		if old != record {
			return errors.New("finding resolution identity already has different journal facts")
		}
		fresh, err := LoadState(stateDir, state.SessionID)
		if err != nil {
			return fmt.Errorf("reload durable finding resolution projection: %w", err)
		}
		*state = fresh
		return nil
	}
	projected := *state
	projected.Tasks = cloneTasksForEventProjection(state.Tasks)
	if err := applyReviewFindingResolved(&projected, record); err != nil {
		return err
	}
	event := RunEvent{
		EventID: reviewFindingResolvedEventID(record.ReviewCycleID, record.FindingID),
		RunID:   state.RunID, EpochID: state.ExecutionEpochID, OccurredAt: at.UTC(),
		Actor: "deep-coordinator", Type: RunEventReviewFindingResolved,
		FromPhase: state.Phase, ToPhase: state.Phase, ReviewDisposition: &record,
		TaskSummary: summarizeRunTasks(projected.Tasks),
	}
	return appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked)
}

func reviewFindingByID(findings []ReviewFinding, id string) (ReviewFinding, bool) {
	for _, finding := range findings {
		if finding.ID == id {
			return finding, true
		}
	}
	return ReviewFinding{}, false
}

func applyReviewRepairCreated(state *DeepState, record ReviewRepairWorkUnit) error {
	if state == nil || state.Phase != PhaseExecuting || state.ExecutionQuiescenceUnconfirmed ||
		!HasSemanticReviewContract(state.SemanticReviewContractVersion) {
		return errors.New("review repair creation is not allowed in the current run state")
	}
	if err := validateReviewRepairRecord(record); err != nil {
		return err
	}
	parent, ok := findTask(state, record.ParentTaskID)
	if !ok || !isAcceptanceContractTask(*parent) || parent.ReviewCycleID != record.ReviewCycleID ||
		parent.ReviewOutcome != ReviewOutcomeFindings || parent.ReviewCheckpointEventID != record.SourceCheckpointEventID ||
		parent.ReviewCheckpointCommit != record.SourceCheckpointCommit || parent.ReviewCheckpointTreeSHA != record.SourceCheckpointTreeSHA ||
		parent.AcceptanceOutcome != AcceptanceAccepted || parent.AcceptanceCheckpointEventID != record.SourceCheckpointEventID ||
		parent.AcceptanceCheckpointCommit != record.SourceCheckpointCommit || parent.AcceptanceCheckpointTreeSHA != record.SourceCheckpointTreeSHA {
		return errors.New("review repair creation does not match its accepted parent finding checkpoint")
	}
	openFinding, ok := reviewFindingByID(parent.ReviewFindings, record.FindingID)
	if !ok || openFinding.Disposition != ReviewFindingOpen || openFinding.RepairTaskID != "" {
		return errors.New("review repair creation does not match an open projected finding")
	}
	if _, exists := findTask(state, record.TaskID); exists {
		return errors.New("review repair Work Unit already exists")
	}
	child := makeReviewRepairTask(*parent, record, openFinding)
	if !isReviewRepairTask(child) || child.ID == parent.ID {
		return errors.New("derived review repair Work Unit is invalid")
	}
	state.Tasks = append(state.Tasks, child)
	parent, _ = findTask(state, record.ParentTaskID)
	for i := range parent.ReviewFindings {
		if parent.ReviewFindings[i].ID == record.FindingID {
			parent.ReviewFindings[i].Disposition = ReviewFindingRepairCreated
			parent.ReviewFindings[i].RepairTaskID = record.TaskID
			return nil
		}
	}
	return errors.New("review repair finding disappeared during projection")
}

func applyReviewFindingResolved(state *DeepState, record ReviewFindingResolution) error {
	if state == nil || state.Phase != PhaseExecuting || state.ExecutionQuiescenceUnconfirmed ||
		!HasSemanticReviewContract(state.SemanticReviewContractVersion) {
		return errors.New("finding resolution is not allowed in the current run state")
	}
	if err := validateReviewFindingResolution(record); err != nil {
		return err
	}
	parent, ok := findTask(state, record.TaskID)
	if !ok || parent.ReviewCycleID != record.ReviewCycleID || parent.ReviewOutcome != ReviewOutcomeFindings {
		return errors.New("finding resolution does not match its source ReviewCycle projection")
	}
	finding, ok := reviewFindingByID(parent.ReviewFindings, record.FindingID)
	if !ok || finding.Disposition != ReviewFindingRepairCreated || finding.RepairTaskID != record.RepairTaskID {
		return errors.New("finding resolution does not match its durable repair Work Unit")
	}
	child, ok := findTask(state, record.RepairTaskID)
	if !ok || !isReviewRepairTask(*child) || child.RepairContext.ParentTaskID != record.TaskID ||
		child.RepairContext.ReviewCycleID != record.ReviewCycleID || child.RepairContext.FindingID != record.FindingID ||
		child.ReviewCycleID != record.ResolutionReviewCycleID || !taskHasSemanticCompletion(*state, child.ID, make(map[string]bool)) {
		return errors.New("finding resolution lacks accepted repair evidence and its checkpoint-bound semantic re-review")
	}
	for i := range parent.ReviewFindings {
		if parent.ReviewFindings[i].ID == record.FindingID {
			parent.ReviewFindings[i].Disposition = ReviewFindingResolved
			return nil
		}
	}
	return errors.New("finding disappeared during resolution projection")
}

func taskHasSemanticCompletion(state DeepState, taskID string, visiting map[string]bool) bool {
	if visiting[taskID] {
		return false
	}
	task, ok := findTask(&state, taskID)
	if !ok || task.Status != StatusAccepted || task.AcceptanceOutcome != AcceptanceAccepted || !taskHasBoundAcceptance(*task) ||
		task.ReviewCheckpointEventID != task.AcceptanceCheckpointEventID || task.ReviewCheckpointCommit != task.AcceptanceCheckpointCommit ||
		task.ReviewCheckpointTreeSHA != task.AcceptanceCheckpointTreeSHA {
		return false
	}
	if taskHasBoundClearReview(*task) {
		return true
	}
	if task.ReviewOutcome != ReviewOutcomeFindings || len(task.ReviewFindings) == 0 {
		return false
	}
	visiting[taskID] = true
	defer delete(visiting, taskID)
	for _, finding := range task.ReviewFindings {
		if finding.Disposition != ReviewFindingResolved || finding.RepairTaskID == "" ||
			!taskHasSemanticCompletion(state, finding.RepairTaskID, visiting) {
			return false
		}
	}
	return true
}

// TaskHasSatisfiedReviewGate reports whether a task's accepted checkpoint is
// clear-reviewed, or all findings against that checkpoint have accepted,
// recursively reviewed repair Work Units.
func TaskHasSatisfiedReviewGate(taskID string, tasks []Task) bool {
	return taskHasSemanticCompletion(DeepState{Tasks: tasks}, taskID, make(map[string]bool))
}

func validateReviewRepairEventTransition(prior []RunEvent, event RunEvent) error {
	switch event.Type {
	case RunEventReviewRepairCreated:
		record := event.ReviewRepair
		if record == nil || event.EpochID != prior[len(prior)-1].EpochID || event.FromPhase != PhaseExecuting || event.ToPhase != PhaseExecuting {
			return errors.New("review repair creation must belong to an executing epoch")
		}
		if err := validateReviewRepairRecord(*record); err != nil {
			return err
		}
		if event.EventID != reviewRepairCreatedEventID(record.ReviewCycleID, record.FindingID) {
			return errors.New("review repair creation event identity is not deterministic")
		}
		cycle, found := reviewCycleByID(prior, record.ReviewCycleID)
		if !found || cycle.SchemaVersion != reviewCycleSchemaGated || cycle.Outcome != ReviewOutcomeFindings ||
			cycle.TaskID != record.ParentTaskID || cycle.CheckpointEventID != record.SourceCheckpointEventID ||
			cycle.Checkpoint.Commit != record.SourceCheckpointCommit || cycle.Checkpoint.TreeSHA != record.SourceCheckpointTreeSHA {
			return errors.New("review repair creation does not cite a canonical finding ReviewCycle")
		}
		if latest, ok := reviewCycleResultForTask(prior, cycle.TaskID); !ok || latest.ID != cycle.ID || !hasCurrentAcceptedCheckpoint(prior, cycle) {
			return errors.New("review repair creation cites a stale or unaccepted review checkpoint")
		}
		finding, ok := reviewFindingByID(cycle.Findings, record.FindingID)
		if !ok || finding.Disposition != ReviewFindingOpen || finding.RepairTaskID != "" {
			return errors.New("review repair creation cites a missing or already routed finding")
		}
		if _, exists := reviewRepairCreated(prior, record.ReviewCycleID, record.FindingID); exists {
			return errors.New("review finding already has a repair Work Unit")
		}
	case RunEventReviewFindingResolved:
		record := event.ReviewDisposition
		if record == nil || event.EpochID != prior[len(prior)-1].EpochID || event.FromPhase != PhaseExecuting || event.ToPhase != PhaseExecuting {
			return errors.New("finding resolution must belong to an executing epoch")
		}
		if err := validateReviewFindingResolution(*record); err != nil {
			return err
		}
		if event.EventID != reviewFindingResolvedEventID(record.ReviewCycleID, record.FindingID) {
			return errors.New("finding resolution event identity is not deterministic")
		}
		repair, found := reviewRepairCreated(prior, record.ReviewCycleID, record.FindingID)
		if !found || repair.TaskID != record.RepairTaskID || repair.ParentTaskID != record.TaskID {
			return errors.New("finding resolution has no matching canonical repair creation")
		}
		if _, found := reviewFindingResolution(prior, record.ReviewCycleID, record.FindingID); found {
			return errors.New("review finding already has a resolution event")
		}
		cycle, found := reviewCycleByID(prior, record.ResolutionReviewCycleID)
		if !found || cycle.TaskID != record.RepairTaskID ||
			(cycle.Outcome != ReviewOutcomeClear && cycle.Outcome != ReviewOutcomeFindings) {
			return errors.New("finding resolution cites no completed semantic review of its repair Work Unit")
		}
	}
	return nil
}

func validateReviewRepairProjection(state DeepState, events []RunEvent) error {
	created := make(map[string]ReviewRepairWorkUnit)
	for _, event := range events {
		if event.Type == RunEventReviewRepairCreated && event.ReviewRepair != nil {
			record := *event.ReviewRepair
			created[record.TaskID] = record
		}
	}
	for _, record := range created {
		actual, ok := findTask(&state, record.TaskID)
		if !ok || !isReviewRepairTask(*actual) {
			return fmt.Errorf("repair Work Unit %s is missing from the deep.json projection", record.TaskID)
		}
		parent, ok := findTask(&state, record.ParentTaskID)
		if !ok {
			return fmt.Errorf("repair Work Unit %s has no projected parent Objective", record.TaskID)
		}
		cycle, ok := reviewCycleByID(events, record.ReviewCycleID)
		if !ok {
			return fmt.Errorf("repair Work Unit %s has no canonical source review", record.TaskID)
		}
		finding, ok := reviewFindingByID(cycle.Findings, record.FindingID)
		if !ok {
			return fmt.Errorf("repair Work Unit %s has no canonical source finding", record.TaskID)
		}
		expected := makeReviewRepairTask(*parent, record, finding)
		if actual.Source != expected.Source || actual.ID != expected.ID || actual.Objective != expected.Objective ||
			actual.Acceptance != expected.Acceptance || actual.Verify != expected.Verify || actual.RepositoryChange != expected.RepositoryChange ||
			actual.AcceptanceCheck != expected.AcceptanceCheck || !reflect.DeepEqual(actual.DependsOn, expected.DependsOn) ||
			actual.Reasoning != expected.Reasoning || !reflect.DeepEqual(actual.RepairContext, expected.RepairContext) {
			return fmt.Errorf("repair Work Unit %s contract disagrees with its canonical source finding", record.TaskID)
		}
	}
	for _, task := range state.Tasks {
		if isReviewRepairTask(task) {
			if _, ok := created[task.ID]; !ok {
				return fmt.Errorf("task %s has repair provenance without a canonical creation event", task.ID)
			}
		} else if task.Source == "review_repair" || task.RepairContext != nil {
			return fmt.Errorf("task %s has invalid review repair source metadata", task.ID)
		}
	}
	return nil
}

func validateReviewRepairJournalBoundary(state DeepState) error {
	if state.RunEventSchemaVersion != 0 {
		return nil
	}
	for _, task := range state.Tasks {
		if task.Source == "review_repair" || task.RepairContext != nil {
			return fmt.Errorf("task %s has generated review-repair state without a canonical RunEvent journal", task.ID)
		}
	}
	return nil
}

func sameRepairContract(a, b Task) bool {
	return a.ID == b.ID && a.Source == b.Source && a.Objective == b.Objective && a.Acceptance == b.Acceptance &&
		a.Verify == b.Verify && a.RepositoryChange == b.RepositoryChange && a.AcceptanceCheck == b.AcceptanceCheck &&
		reflect.DeepEqual(a.DependsOn, b.DependsOn) && a.Reasoning == b.Reasoning && reflect.DeepEqual(a.RepairContext, b.RepairContext)
}
