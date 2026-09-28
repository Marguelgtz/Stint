package deep

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

const maxReviewFindings = 8

const (
	reviewCycleSchemaEvidenceOnly = 1
	reviewCycleSchemaGated        = 2
)

type ReviewOutcome string

const (
	ReviewOutcomeStarted        ReviewOutcome = "started"
	ReviewOutcomeClear          ReviewOutcome = "clear"
	ReviewOutcomeFindings       ReviewOutcome = "findings"
	ReviewOutcomeUnresolved     ReviewOutcome = "unresolved"
	ReviewOutcomeExecutionError ReviewOutcome = "execution_error"
	ReviewOutcomeTimedOut       ReviewOutcome = "timed_out"
	ReviewOutcomeCanceled       ReviewOutcome = "canceled"
	ReviewOutcomeUnknown        ReviewOutcome = "unknown"
)

type ReviewFindingSeverity string

const (
	ReviewSeverityCritical ReviewFindingSeverity = "critical"
	ReviewSeverityHigh     ReviewFindingSeverity = "high"
	ReviewSeverityMedium   ReviewFindingSeverity = "medium"
	ReviewSeverityLow      ReviewFindingSeverity = "low"
)

type ReviewFindingDisposition string

const (
	ReviewFindingOpen          ReviewFindingDisposition = "open"
	ReviewFindingRepairCreated ReviewFindingDisposition = "repair_created"
	ReviewFindingResolved      ReviewFindingDisposition = "resolved"
	ReviewFindingRejected      ReviewFindingDisposition = "rejected"
	ReviewFindingRiskAccepted  ReviewFindingDisposition = "risk_accepted"
)

// ReviewFinding is a bounded semantic claim against one immutable checkpoint.
// Evidence is a concise factual rationale, not hidden model reasoning.
type ReviewFinding struct {
	ID           string                   `json:"id"`
	Severity     ReviewFindingSeverity    `json:"severity"`
	Summary      string                   `json:"summary"`
	Evidence     string                   `json:"evidence"`
	Locations    []string                 `json:"locations,omitempty"`
	Disposition  ReviewFindingDisposition `json:"disposition"`
	RepairTaskID string                   `json:"repairTaskId,omitempty"`
}

// ReviewCycle is the canonical record of one fresh-context semantic review
// against an exact task checkpoint. It stores structured findings, not raw
// prompts, chain-of-thought, environment dumps, or unbounded model output.
type ReviewCycle struct {
	SchemaVersion                 int             `json:"schemaVersion"`
	ID                            string          `json:"id"`
	StartEventID                  string          `json:"startEventId"`
	TaskID                        string          `json:"taskId"`
	Attempt                       int             `json:"attempt"`
	PolicySHA256                  string          `json:"policySha256"`
	ContextSHA256                 string          `json:"contextSha256"`
	CheckpointEventID             string          `json:"checkpointEventId"`
	Checkpoint                    TaskCheckpoint  `json:"checkpoint"`
	Reviewer                      string          `json:"reviewer"`
	Provider                      string          `json:"provider,omitempty"`
	Model                         string          `json:"model,omitempty"`
	StartedInEpochID              string          `json:"startedInEpochId"`
	StartedAt                     time.Time       `json:"startedAt"`
	TimeoutMilliseconds           int64           `json:"timeoutMilliseconds"`
	RemainingDeadlineMilliseconds int64           `json:"remainingDeadlineMilliseconds,omitempty"`
	EndedAt                       time.Time       `json:"endedAt,omitempty"`
	DurationMilliseconds          int64           `json:"durationMilliseconds,omitempty"`
	Outcome                       ReviewOutcome   `json:"outcome"`
	Reason                        string          `json:"reason,omitempty"`
	QuiescenceUnconfirmed         bool            `json:"quiescenceUnconfirmed,omitempty"`
	Findings                      []ReviewFinding `json:"findings,omitempty"`
}

func NewReviewCycleID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate review-cycle identity: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func reviewCycleEventID(id, action string) string { return "review/" + id + "/" + action }

func validateReviewCycle(cycle ReviewCycle, starting bool) error {
	if (cycle.SchemaVersion != reviewCycleSchemaEvidenceOnly && cycle.SchemaVersion != reviewCycleSchemaGated) ||
		cycle.ID == "" || len(cycle.ID) != 32 || cycle.TaskID == "" ||
		len(cycle.TaskID) > 128 || strings.ContainsAny(cycle.TaskID, "\x00\r\n") || cycle.Attempt < 1 || cycle.Attempt > 1_000_000 {
		return errors.New("review cycle schema or Objective identity is invalid")
	}
	if _, err := hex.DecodeString(cycle.ID); err != nil {
		return errors.New("review cycle identity is not hexadecimal")
	}
	for name, value := range map[string]string{"policy": cycle.PolicySHA256, "context": cycle.ContextSHA256} {
		if len(value) != 64 {
			return fmt.Errorf("review cycle %s identity is invalid", name)
		}
		if _, err := hex.DecodeString(value); err != nil {
			return fmt.Errorf("review cycle %s identity is not hexadecimal", name)
		}
	}
	if cycle.StartEventID != reviewCycleEventID(cycle.ID, "started") || cycle.StartedInEpochID == "" ||
		len(cycle.StartedInEpochID) > 128 || strings.ContainsAny(cycle.StartedInEpochID, "\x00\r\n") || cycle.StartedAt.IsZero() ||
		cycle.CheckpointEventID == "" || cycle.CheckpointEventID != taskCheckpointRecordEventID(cycle.Checkpoint) ||
		cycle.Checkpoint.TaskID != cycle.TaskID || cycle.Checkpoint.Attempt != cycle.Attempt ||
		cycle.Checkpoint.Commit == "" || cycle.Checkpoint.TreeSHA == "" ||
		cycle.Checkpoint.VerificationSubject.TreeSHA != cycle.Checkpoint.TreeSHA {
		return errors.New("review cycle checkpoint or start provenance is invalid")
	}
	if err := validateTaskCheckpoint(cycle.Checkpoint); err != nil {
		return fmt.Errorf("review cycle checkpoint is invalid: %w", err)
	}
	maxDurationMilliseconds := int64((7 * 24 * time.Hour) / time.Millisecond)
	if cycle.TimeoutMilliseconds < 0 || cycle.TimeoutMilliseconds > maxDurationMilliseconds ||
		cycle.RemainingDeadlineMilliseconds < 0 || cycle.RemainingDeadlineMilliseconds > maxDurationMilliseconds {
		return errors.New("review-cycle time budget facts are invalid")
	}
	for name, value := range map[string]string{"reviewer": cycle.Reviewer, "provider": cycle.Provider, "model": cycle.Model} {
		if (name == "reviewer" && value == "") || len(value) > 128 || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("review cycle %s identity is invalid", name)
		}
	}
	if starting {
		if cycle.Outcome != ReviewOutcomeStarted || !cycle.EndedAt.IsZero() || cycle.DurationMilliseconds != 0 ||
			cycle.Reason != "" || cycle.QuiescenceUnconfirmed || len(cycle.Findings) != 0 {
			return errors.New("review-cycle start contains result-only facts")
		}
		return nil
	}
	if cycle.EndedAt.IsZero() || cycle.EndedAt.Before(cycle.StartedAt) || cycle.DurationMilliseconds < 0 ||
		cycle.DurationMilliseconds > int64((7*24*time.Hour)/time.Millisecond) || len(cycle.Reason) > maxRunEventTextBytes || strings.ContainsAny(cycle.Reason, "\x00\r\n") {
		return errors.New("review-cycle result timestamps or reason are invalid")
	}
	if len(cycle.Findings) > maxReviewFindings {
		return errors.New("review-cycle result exceeds its bounded finding limit")
	}
	switch cycle.Outcome {
	case ReviewOutcomeClear:
		if len(cycle.Findings) != 0 || cycle.Reason != "" {
			return errors.New("clear review cycle must have no findings or failure reason")
		}
	case ReviewOutcomeFindings:
		if len(cycle.Findings) == 0 || cycle.Reason != "" {
			return errors.New("review cycle with findings requires findings and no failure reason")
		}
	case ReviewOutcomeUnresolved, ReviewOutcomeExecutionError, ReviewOutcomeTimedOut, ReviewOutcomeCanceled, ReviewOutcomeUnknown:
		if strings.TrimSpace(cycle.Reason) == "" {
			return errors.New("inconclusive review cycle requires a reason")
		}
	default:
		return fmt.Errorf("invalid review-cycle outcome %q", cycle.Outcome)
	}
	if cycle.QuiescenceUnconfirmed && (cycle.SchemaVersion != reviewCycleSchemaGated || cycle.Outcome != ReviewOutcomeUnknown) {
		return errors.New("unconfirmed reviewer quiescence requires an unknown review outcome")
	}
	seen := make(map[string]bool, len(cycle.Findings))
	for _, finding := range cycle.Findings {
		if finding.ID == "" || len(finding.ID) > 64 || strings.ContainsAny(finding.ID, "\x00\r\n") || seen[finding.ID] {
			return errors.New("review finding identity is invalid or duplicated")
		}
		seen[finding.ID] = true
		switch finding.Severity {
		case ReviewSeverityCritical, ReviewSeverityHigh, ReviewSeverityMedium, ReviewSeverityLow:
		default:
			return fmt.Errorf("review finding %q has invalid severity", finding.ID)
		}
		if finding.Summary == "" || finding.Evidence == "" || len(finding.Summary) > 256 || len(finding.Evidence) > maxRunEventTextBytes ||
			strings.ContainsAny(finding.Summary+finding.Evidence, "\x00\r\n") || finding.Disposition != ReviewFindingOpen ||
			finding.RepairTaskID != "" || len(finding.Locations) > 4 {
			return fmt.Errorf("review finding %q exceeds its bounded evidence or initial disposition contract", finding.ID)
		}
		for _, location := range finding.Locations {
			if location == "" || len(location) > 128 || strings.ContainsAny(location, "\x00\r\n") {
				return fmt.Errorf("review finding %q has an invalid location reference", finding.ID)
			}
		}
	}
	data, err := json.Marshal(cycle)
	if err != nil {
		return fmt.Errorf("encode bounded review cycle: %w", err)
	}
	if len(data) > maxRunEventLineBytes-2048 {
		return errors.New("review-cycle record exceeds its bounded journal size")
	}
	return nil
}

// ValidateReviewCycleResult checks a completed review record before a caller
// attempts to append it to the run journal.
func ValidateReviewCycleResult(cycle ReviewCycle) error {
	return validateReviewCycle(cycle, false)
}

// BeginReviewCycle durably binds a fresh-context review to the latest
// checkpoint of its stable Objective / Work Unit before reviewer execution.
func BeginReviewCycle(stateDir string, state *DeepState, cycle ReviewCycle) (ReviewCycle, error) {
	if state == nil || state.RunID == "" || state.ExecutionEpochID == "" || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return ReviewCycle{}, errors.New("review-cycle start requires a journaled run epoch")
	}
	if state.Phase != PhaseExecuting || state.ExecutionQuiescenceUnconfirmed {
		return ReviewCycle{}, errors.New("review-cycle start requires an executing, quiescent run")
	}
	cycle.SchemaVersion = reviewCycleSchemaEvidenceOnly
	if state.SemanticReviewContractVersion == SemanticReviewContractVersion {
		cycle.SchemaVersion = reviewCycleSchemaGated
	}
	task, ok := findTask(state, cycle.TaskID)
	if !reviewPolicyFactsMatch(*state, cycle) || !ok || task.Attempts != cycle.Attempt || task.CheckpointCommit != cycle.Checkpoint.Commit ||
		task.CheckpointTreeSHA != cycle.Checkpoint.TreeSHA || task.ExecutorRunID != cycle.Checkpoint.ExecutorRunID ||
		task.VerificationRunID != cycle.Checkpoint.VerificationRunID || task.VerificationSubject == nil ||
		*task.VerificationSubject != cycle.Checkpoint.VerificationSubject {
		return ReviewCycle{}, errors.New("review-cycle start does not match the latest durable Objective checkpoint")
	}
	cycle.StartEventID = reviewCycleEventID(cycle.ID, "started")
	cycle.StartedInEpochID = state.ExecutionEpochID
	cycle.StartedAt = cycle.StartedAt.UTC()
	cycle.Outcome = ReviewOutcomeStarted
	if err := validateReviewCycle(cycle, true); err != nil {
		return ReviewCycle{}, err
	}
	prior, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		return ReviewCycle{}, err
	}
	if cycle.SchemaVersion == reviewCycleSchemaGated && !hasCurrentAcceptedCheckpoint(prior, cycle) {
		return ReviewCycle{}, errors.New("semantic review requires a canonical accepted decision for the current checkpoint")
	}
	event := RunEvent{
		EventID: cycle.StartEventID, RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: cycle.StartedAt, Actor: "deep-reviewer", Type: RunEventReviewStarted,
		FromPhase: state.Phase, ToPhase: state.Phase, ReviewCycle: &cycle,
		TaskSummary: summarizeRunTasks(state.Tasks),
	}
	if err := appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked); err != nil {
		return ReviewCycle{}, err
	}
	return cycle, nil
}

// CompleteReviewCycle persists structured review facts. Callers cannot apply a
// disposition directly; findings enter the durable history as open.
func CompleteReviewCycle(stateDir string, state *DeepState, cycle ReviewCycle) error {
	if state == nil || state.RunID == "" || state.ExecutionEpochID == "" || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return errors.New("review-cycle result requires a journaled run epoch")
	}
	if cycle.StartEventID != reviewCycleEventID(cycle.ID, "started") || cycle.EndedAt.IsZero() {
		return errors.New("review-cycle result requires its start identity and end timestamp")
	}
	if !reviewPolicyFactsMatch(*state, cycle) {
		return errors.New("review-cycle policy identity differs from the durable mission contract")
	}
	cycle.EndedAt = cycle.EndedAt.UTC()
	cycle.DurationMilliseconds = max(0, cycle.EndedAt.Sub(cycle.StartedAt).Milliseconds())
	if err := validateReviewCycle(cycle, false); err != nil {
		return err
	}
	if events, err := ReadRunEvents(stateDir, state.SessionID); err != nil {
		return err
	} else {
		for _, event := range events {
			if event.EventID != reviewCycleEventID(cycle.ID, "result") {
				continue
			}
			if event.Type != RunEventReviewResult || event.ReviewCycle == nil || !reflect.DeepEqual(*event.ReviewCycle, cycle) {
				return errors.New("review-cycle result identity already has different journal facts")
			}
			fresh, err := LoadState(stateDir, state.SessionID)
			if err != nil {
				return fmt.Errorf("reload durable review-cycle result projection: %w", err)
			}
			*state = fresh
			return nil
		}
	}
	projected := *state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if err := applyReviewResult(&projected, cycle); err != nil {
		return err
	}
	event := RunEvent{
		EventID: reviewCycleEventID(cycle.ID, "result"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: cycle.EndedAt, Actor: "deep-reviewer", Type: RunEventReviewResult,
		FromPhase: state.Phase, ToPhase: state.Phase, ReviewCycle: &cycle,
		TaskSummary: summarizeRunTasks(projected.Tasks),
	}
	return appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked)
}

func applyReviewStarted(state *DeepState, cycle ReviewCycle) error {
	task, ok := findTask(state, cycle.TaskID)
	if !ok || task.Attempts != cycle.Attempt || task.CheckpointCommit != cycle.Checkpoint.Commit || task.CheckpointTreeSHA != cycle.Checkpoint.TreeSHA {
		return errors.New("review-cycle start does not match the projected Objective checkpoint")
	}
	task.ReviewCycleID = cycle.ID
	task.ReviewOutcome = ReviewOutcomeStarted
	task.ReviewReason = ""
	task.ReviewCheckpointEventID = cycle.CheckpointEventID
	task.ReviewCheckpointCommit = cycle.Checkpoint.Commit
	task.ReviewCheckpointTreeSHA = cycle.Checkpoint.TreeSHA
	task.ReviewFindings = nil
	return nil
}

func applyReviewResult(state *DeepState, cycle ReviewCycle) error {
	task, ok := findTask(state, cycle.TaskID)
	if !ok || task.ReviewCycleID != cycle.ID || task.Attempts != cycle.Attempt ||
		task.ReviewCheckpointEventID != cycle.CheckpointEventID || task.ReviewCheckpointCommit != cycle.Checkpoint.Commit ||
		task.ReviewCheckpointTreeSHA != cycle.Checkpoint.TreeSHA {
		return errors.New("review-cycle result does not match its projected start")
	}
	task.ReviewOutcome = cycle.Outcome
	task.ReviewReason = cycle.Reason
	task.ReviewFindings = cloneReviewFindings(cycle.Findings)
	if cycle.QuiescenceUnconfirmed {
		state.ExecutionQuiescenceUnconfirmed = true
		state.ExecutionQuiescenceTaskID = task.ID
	}
	return nil
}

func cloneReviewFindings(in []ReviewFinding) []ReviewFinding {
	if len(in) == 0 {
		return nil
	}
	out := make([]ReviewFinding, len(in))
	copy(out, in)
	for i := range out {
		out[i].Locations = append([]string(nil), in[i].Locations...)
	}
	return out
}

func validateReviewEventTransition(prior []RunEvent, event RunEvent) error {
	if event.Type != RunEventReviewStarted && event.Type != RunEventReviewResult && event.Type != RunEventReviewRecovery {
		if hasUnmatchedReviewCycle(prior) {
			return errors.New("run history contains an unmatched semantic review cycle; recover it before another transition")
		}
		return nil
	}
	if event.ReviewCycle == nil {
		return errors.New("review event has no review-cycle record")
	}
	switch event.Type {
	case RunEventReviewStarted:
		if hasUnmatchedReviewCycle(prior) {
			return errors.New("cannot start a review cycle while another review invocation is unmatched")
		}
		for _, old := range prior {
			if old.ReviewCycle != nil && old.ReviewCycle.ID == event.ReviewCycle.ID {
				return errors.New("review-cycle identity already exists in run history")
			}
		}
		if !hasCheckpointEvent(prior, event.ReviewCycle.CheckpointEventID, *event.ReviewCycle) {
			return errors.New("review cycle references a checkpoint that is not in prior run history")
		}
		if event.ReviewCycle.SchemaVersion == reviewCycleSchemaGated && !hasCurrentAcceptedCheckpoint(prior, *event.ReviewCycle) {
			return errors.New("semantic review start has no prior accepted decision for the current checkpoint")
		}
	case RunEventReviewResult, RunEventReviewRecovery:
		start, ok := unmatchedReviewStart(prior, event.ReviewCycle.ID)
		if !ok || !sameReviewCycleStart(*start, *event.ReviewCycle) {
			return errors.New("review result does not match an unmatched durable review start")
		}
		if event.Type == RunEventReviewResult && start.SchemaVersion != event.ReviewCycle.SchemaVersion {
			return errors.New("review result schema differs from its durable start")
		}
		if event.Type == RunEventReviewRecovery && start.SchemaVersion != event.ReviewCycle.SchemaVersion &&
			!(start.SchemaVersion == reviewCycleSchemaEvidenceOnly && event.ReviewCycle.SchemaVersion == reviewCycleSchemaGated && event.ReviewCycle.QuiescenceUnconfirmed) {
			return errors.New("review recovery has an incompatible schema transition")
		}
		if event.OccurredAt.Before(start.StartedAt) {
			return errors.New("review result observation precedes its durable start")
		}
		if event.Type == RunEventReviewResult && event.EpochID != start.StartedInEpochID {
			return errors.New("review result cannot be reused across a resume epoch; recover the prior cycle as unresolved")
		}
		if event.Type == RunEventReviewRecovery && !event.ReviewCycle.QuiescenceUnconfirmed {
			return errors.New("review recovery must preserve unconfirmed reviewer quiescence")
		}
		for _, old := range prior {
			if old.Sequence <= reviewStartSequence(prior, start.ID) {
				continue
			}
			if old.Type == RunEventTaskCheckpointCreated && old.TaskCheckpoint != nil && old.TaskCheckpoint.TaskID == start.TaskID {
				return errors.New("review result follows a newer checkpoint for its Objective")
			}
			if old.Type == RunEventExecutorStarted && old.ExecutorRun != nil && old.ExecutorRun.TaskID == start.TaskID {
				return errors.New("review result follows a newer executor invocation for its Objective")
			}
		}
	}
	return nil
}

func hasCurrentAcceptedCheckpoint(events []RunEvent, cycle ReviewCycle) bool {
	latestCheckpointEventID := ""
	accepted := false
	for _, event := range events {
		switch {
		case event.Type == RunEventTaskCheckpointCreated && event.TaskCheckpoint != nil && event.TaskCheckpoint.TaskID == cycle.TaskID:
			latestCheckpointEventID = event.EventID
			accepted = false
		case event.Type == RunEventExecutorStarted && event.ExecutorRun != nil && event.ExecutorRun.TaskID == cycle.TaskID:
			accepted = false
		case event.Type == RunEventAcceptanceStarted && event.AcceptanceRun != nil && event.AcceptanceRun.TaskID == cycle.TaskID:
			accepted = false
		case (event.Type == RunEventAcceptanceResult || event.Type == RunEventAcceptanceRecovery) &&
			event.AcceptanceRun != nil && event.AcceptanceRun.TaskID == cycle.TaskID:
			run := event.AcceptanceRun
			accepted = event.Type == RunEventAcceptanceResult && run.Decision == AcceptanceAccepted &&
				run.CheckOutcome == AcceptanceCheckPassed && run.CheckpointEventID == latestCheckpointEventID &&
				reflect.DeepEqual(run.Checkpoint, cycle.Checkpoint)
		}
	}
	return latestCheckpointEventID == cycle.CheckpointEventID && accepted
}

func hasCheckpointEvent(events []RunEvent, eventID string, cycle ReviewCycle) bool {
	for _, event := range events {
		if event.EventID == eventID && event.Type == RunEventTaskCheckpointCreated && event.TaskCheckpoint != nil {
			return reflect.DeepEqual(*event.TaskCheckpoint, cycle.Checkpoint)
		}
	}
	return false
}

func sameReviewCycleStart(start, result ReviewCycle) bool {
	return start.ID == result.ID && start.StartEventID == result.StartEventID &&
		start.TaskID == result.TaskID && start.Attempt == result.Attempt && start.PolicySHA256 == result.PolicySHA256 &&
		start.ContextSHA256 == result.ContextSHA256 && start.CheckpointEventID == result.CheckpointEventID &&
		reflect.DeepEqual(start.Checkpoint, result.Checkpoint) && start.Reviewer == result.Reviewer &&
		start.Provider == result.Provider && start.Model == result.Model && start.StartedInEpochID == result.StartedInEpochID &&
		start.StartedAt.Equal(result.StartedAt)
}

func unmatchedReviewStart(events []RunEvent, id string) (*ReviewCycle, bool) {
	var start *ReviewCycle
	for _, event := range events {
		if event.ReviewCycle == nil || event.ReviewCycle.ID != id {
			continue
		}
		switch event.Type {
		case RunEventReviewStarted:
			copy := *event.ReviewCycle
			start = &copy
		case RunEventReviewResult, RunEventReviewRecovery:
			start = nil
		}
	}
	return start, start != nil
}

func hasUnmatchedReviewCycle(events []RunEvent) bool {
	open := make(map[string]struct{})
	for _, event := range events {
		if event.ReviewCycle == nil {
			continue
		}
		switch event.Type {
		case RunEventReviewStarted:
			open[event.ReviewCycle.ID] = struct{}{}
		case RunEventReviewResult, RunEventReviewRecovery:
			delete(open, event.ReviewCycle.ID)
		}
	}
	return len(open) > 0
}

func reviewStartSequence(events []RunEvent, id string) uint64 {
	for _, event := range events {
		if event.Type == RunEventReviewStarted && event.ReviewCycle != nil && event.ReviewCycle.ID == id {
			return event.Sequence
		}
	}
	return 0
}

func validateReviewProjection(state DeepState, events []RunEvent) error {
	latest := make(map[string]ReviewCycle)
	for _, event := range events {
		if event.ReviewCycle == nil {
			continue
		}
		if !reviewPolicyFactsMatch(state, *event.ReviewCycle) {
			return fmt.Errorf("review cycle %q refers to a different mission contract", event.ReviewCycle.ID)
		}
		switch event.Type {
		case RunEventReviewStarted, RunEventReviewResult, RunEventReviewRecovery:
			latest[event.ReviewCycle.TaskID] = *event.ReviewCycle
		}
	}
	for _, event := range events {
		cycleID, findingID := "", ""
		repairTaskID := ""
		disposition := ReviewFindingDisposition("")
		switch event.Type {
		case RunEventReviewRepairCreated:
			if event.ReviewRepair != nil {
				cycleID, findingID = event.ReviewRepair.ReviewCycleID, event.ReviewRepair.FindingID
				repairTaskID, disposition = event.ReviewRepair.TaskID, ReviewFindingRepairCreated
			}
		case RunEventReviewFindingResolved:
			if event.ReviewDisposition != nil {
				cycleID, findingID = event.ReviewDisposition.ReviewCycleID, event.ReviewDisposition.FindingID
				repairTaskID, disposition = event.ReviewDisposition.RepairTaskID, ReviewFindingResolved
			}
		}
		if cycleID == "" {
			continue
		}
		source, found := reviewCycleByID(events, cycleID)
		if !found {
			return fmt.Errorf("review disposition refers to missing cycle %s", cycleID)
		}
		current, found := latest[source.TaskID]
		if !found || current.ID != cycleID {
			continue // A later ReviewCycle supersedes this finding projection.
		}
		for i := range current.Findings {
			if current.Findings[i].ID == findingID {
				current.Findings[i].Disposition = disposition
				current.Findings[i].RepairTaskID = repairTaskID
			}
		}
		latest[source.TaskID] = current
	}
	for _, task := range state.Tasks {
		cycle, ok := latest[task.ID]
		if !ok {
			if task.ReviewCycleID != "" || task.ReviewOutcome != "" || task.ReviewReason != "" || task.ReviewCheckpointEventID != "" ||
				task.ReviewCheckpointCommit != "" || task.ReviewCheckpointTreeSHA != "" || len(task.ReviewFindings) != 0 {
				return fmt.Errorf("task %s has review projection without canonical review history", task.ID)
			}
			continue
		}
		if !reviewProjectionMatches(task, cycle) {
			return fmt.Errorf("task %s review projection disagrees with canonical review history", task.ID)
		}
	}
	return validateReviewRepairProjection(state, events)
}

func reviewProjectionMatches(task Task, cycle ReviewCycle) bool {
	return task.ReviewCycleID == cycle.ID && task.ReviewOutcome == cycle.Outcome && task.ReviewReason == cycle.Reason &&
		task.ReviewCheckpointEventID == cycle.CheckpointEventID && task.ReviewCheckpointCommit == cycle.Checkpoint.Commit &&
		task.ReviewCheckpointTreeSHA == cycle.Checkpoint.TreeSHA && reflect.DeepEqual(task.ReviewFindings, cycle.Findings)
}

func reviewPolicyFactsMatch(state DeepState, cycle ReviewCycle) bool {
	if state.SemanticReviewContractVersion == SemanticReviewContractVersion {
		return cycle.SchemaVersion == reviewCycleSchemaGated && cycle.PolicySHA256 == state.SemanticReviewContractSHA256
	}
	// D1 allowed evidence-only cycles under deterministic acceptance v2. Keep
	// those already-journaled facts readable after D2 adds a separate review
	// contract identity.
	return state.SemanticReviewContractVersion == 0 &&
		state.AcceptanceContractVersion == DeterministicAcceptanceContractVersion &&
		(cycle.SchemaVersion == reviewCycleSchemaEvidenceOnly || cycle.SchemaVersion == reviewCycleSchemaGated) &&
		cycle.PolicySHA256 == state.AcceptanceContractSHA256
}

func applyReviewRecovery(state *DeepState, cycle ReviewCycle, reason string) error {
	cycle.Outcome = ReviewOutcomeUnknown
	cycle.Reason = reason
	cycle.QuiescenceUnconfirmed = true
	return applyReviewResult(state, cycle)
}

// RecoverUnmatchedReviewCycle closes a start without a durable result as
// explicitly unknown. It never manufactures a clean review or finding list.
func RecoverUnmatchedReviewCycle(stateDir string, state *DeepState, at time.Time) (*ReviewCycle, error) {
	if state == nil || state.RunID == "" || state.ExecutionEpochID == "" || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return nil, errors.New("review-cycle recovery requires a journaled run epoch")
	}
	if state.Phase != PhaseExecuting {
		return nil, errors.New("review-cycle recovery requires an executing run")
	}
	cycle, err := LoadUnmatchedReviewCycle(stateDir, state.SessionID)
	if err != nil || cycle == nil {
		return cycle, err
	}
	at = at.UTC()
	if at.Before(cycle.StartedAt) {
		at = cycle.StartedAt
	}
	cycle.EndedAt = at
	cycle.DurationMilliseconds = max(0, at.Sub(cycle.StartedAt).Milliseconds())
	cycle.Outcome = ReviewOutcomeUnknown
	cycle.Reason = "coordinator resumed without a durable semantic review result"
	if cycle.SchemaVersion == reviewCycleSchemaEvidenceOnly {
		cycle.SchemaVersion = reviewCycleSchemaGated
	}
	cycle.QuiescenceUnconfirmed = true
	if err := validateReviewCycle(*cycle, false); err != nil {
		return nil, err
	}
	projected := *state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if err := applyReviewRecovery(&projected, *cycle, cycle.Reason); err != nil {
		return nil, err
	}
	event := RunEvent{
		EventID: reviewCycleEventID(cycle.ID, "recovery"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: at, Actor: "deep-coordinator", Type: RunEventReviewRecovery,
		FromPhase: state.Phase, ToPhase: state.Phase, Reason: cycle.Reason, ReviewCycle: cycle,
		TaskSummary: summarizeRunTasks(projected.Tasks),
	}
	if err := appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked); err != nil {
		return nil, err
	}
	return cycle, nil
}

func LoadReviewCycle(stateDir, sessionID, id string) (ReviewCycle, bool, error) {
	events, err := ReadRunEvents(stateDir, sessionID)
	if err != nil {
		return ReviewCycle{}, false, err
	}
	var cycle ReviewCycle
	found := false
	for _, event := range events {
		if event.ReviewCycle != nil && event.ReviewCycle.ID == id {
			cycle, found = *event.ReviewCycle, true
		}
	}
	return cycle, found, nil
}

func LoadUnmatchedReviewCycle(stateDir, sessionID string) (*ReviewCycle, error) {
	events, err := ReadRunEvents(stateDir, sessionID)
	if err != nil {
		return nil, err
	}
	open := make(map[string]ReviewCycle)
	for _, event := range events {
		if event.ReviewCycle == nil {
			continue
		}
		switch event.Type {
		case RunEventReviewStarted:
			open[event.ReviewCycle.ID] = *event.ReviewCycle
		case RunEventReviewResult, RunEventReviewRecovery:
			delete(open, event.ReviewCycle.ID)
		}
	}
	if len(open) == 0 {
		return nil, nil
	}
	if len(open) != 1 {
		return nil, fmt.Errorf("journal contains %d unresolved review cycles; refusing recovery", len(open))
	}
	for _, cycle := range open {
		return &cycle, nil
	}
	return nil, nil
}
