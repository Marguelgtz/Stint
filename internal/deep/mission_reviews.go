package deep

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

const maxMissionReviewFindings = 8
const missionReviewCycleSchemaVersion = 1

// MissionReviewCycle is a bounded semantic review of the complete mission
// result. It is bound to the exact repository subject and Git checkpoint that
// the landing transition will record. The journal stores this record directly;
// deep.json retains only its latest compatibility projection.
type MissionReviewCycle struct {
	SchemaVersion                 int                  `json:"schemaVersion"`
	ID                            string               `json:"id"`
	StartEventID                  string               `json:"startEventId"`
	PolicySHA256                  string               `json:"policySha256"`
	ContextSHA256                 string               `json:"contextSha256"`
	CheckpointCommit              string               `json:"checkpointCommit"`
	CheckpointTreeSHA             string               `json:"checkpointTreeSha"`
	ReviewSubject                 VerificationSubject  `json:"reviewSubject"`
	FinalVerificationSubject      *VerificationSubject `json:"finalVerificationSubject,omitempty"`
	VerificationRunID             string               `json:"verificationRunId,omitempty"`
	Reviewer                      string               `json:"reviewer"`
	Provider                      string               `json:"provider,omitempty"`
	Model                         string               `json:"model,omitempty"`
	StartedInEpochID              string               `json:"startedInEpochId"`
	StartedAt                     time.Time            `json:"startedAt"`
	TimeoutMilliseconds           int64                `json:"timeoutMilliseconds"`
	RemainingDeadlineMilliseconds int64                `json:"remainingDeadlineMilliseconds,omitempty"`
	EndedAt                       time.Time            `json:"endedAt,omitempty"`
	DurationMilliseconds          int64                `json:"durationMilliseconds,omitempty"`
	Outcome                       ReviewOutcome        `json:"outcome"`
	Reason                        string               `json:"reason,omitempty"`
	QuiescenceUnconfirmed         bool                 `json:"quiescenceUnconfirmed,omitempty"`
	Findings                      []ReviewFinding      `json:"findings,omitempty"`
}

func missionReviewEventID(id, action string) string { return "mission-review/" + id + "/" + action }

func validateMissionReviewCycle(cycle MissionReviewCycle, starting bool) error {
	if cycle.SchemaVersion != missionReviewCycleSchemaVersion || len(cycle.ID) != 32 || cycle.CheckpointCommit == "" ||
		cycle.CheckpointTreeSHA == "" || cycle.ReviewSubject.HeadCommit == "" || cycle.ReviewSubject.TreeSHA == "" ||
		cycle.ReviewSubject.HeadCommit != cycle.CheckpointCommit || cycle.ReviewSubject.TreeSHA != cycle.CheckpointTreeSHA || cycle.Reviewer == "" ||
		cycle.StartEventID != missionReviewEventID(cycle.ID, "started") || cycle.StartedInEpochID == "" ||
		cycle.StartedAt.IsZero() {
		return errors.New("mission review checkpoint or start provenance is invalid")
	}
	if _, err := hex.DecodeString(cycle.ID); err != nil {
		return errors.New("mission review identity is not hexadecimal")
	}
	for name, value := range map[string]string{"policy": cycle.PolicySHA256, "context": cycle.ContextSHA256} {
		if len(value) != 64 {
			return fmt.Errorf("mission review %s identity is invalid", name)
		}
		if _, err := hex.DecodeString(value); err != nil {
			return fmt.Errorf("mission review %s identity is not hexadecimal", name)
		}
	}
	if len(cycle.CheckpointCommit) > 128 || len(cycle.CheckpointTreeSHA) > 128 || len(cycle.VerificationRunID) > 128 ||
		len(cycle.StartedInEpochID) > 128 || strings.ContainsAny(cycle.CheckpointCommit+cycle.CheckpointTreeSHA+cycle.VerificationRunID+cycle.StartedInEpochID, "\x00\r\n") {
		return errors.New("mission review repository or run identity is invalid")
	}
	if len(cycle.ReviewSubject.HeadCommit) > 128 || len(cycle.ReviewSubject.TreeSHA) > 128 ||
		strings.ContainsAny(cycle.ReviewSubject.HeadCommit+cycle.ReviewSubject.TreeSHA, "\x00\r\n") {
		return errors.New("mission review subject is invalid")
	}
	if cycle.FinalVerificationSubject != nil && (cycle.FinalVerificationSubject.HeadCommit == "" || cycle.FinalVerificationSubject.TreeSHA == "" ||
		cycle.FinalVerificationSubject.TreeSHA != cycle.CheckpointTreeSHA || len(cycle.FinalVerificationSubject.HeadCommit) > 128 ||
		len(cycle.FinalVerificationSubject.TreeSHA) > 128 || strings.ContainsAny(cycle.FinalVerificationSubject.HeadCommit+cycle.FinalVerificationSubject.TreeSHA, "\x00\r\n")) {
		return errors.New("mission final-verification subject is invalid or differs from the reviewed tree")
	}
	if (cycle.VerificationRunID == "") != (cycle.FinalVerificationSubject == nil) {
		return errors.New("mission review final-verification run and subject must be recorded together")
	}
	maxDuration := int64((7 * 24 * time.Hour) / time.Millisecond)
	if cycle.TimeoutMilliseconds < 0 || cycle.TimeoutMilliseconds > maxDuration ||
		cycle.RemainingDeadlineMilliseconds < 0 || cycle.RemainingDeadlineMilliseconds > maxDuration {
		return errors.New("mission-review time budget facts are invalid")
	}
	for name, value := range map[string]string{"reviewer": cycle.Reviewer, "provider": cycle.Provider, "model": cycle.Model} {
		if (name == "reviewer" && value == "") || len(value) > 128 || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("mission review %s identity is invalid", name)
		}
	}
	if starting {
		if cycle.Outcome != ReviewOutcomeStarted || !cycle.EndedAt.IsZero() || cycle.DurationMilliseconds != 0 ||
			cycle.Reason != "" || cycle.QuiescenceUnconfirmed || len(cycle.Findings) != 0 {
			return errors.New("mission-review start contains result-only facts")
		}
		return nil
	}
	if cycle.EndedAt.IsZero() || cycle.EndedAt.Before(cycle.StartedAt) || cycle.DurationMilliseconds < 0 ||
		cycle.DurationMilliseconds > maxDuration || len(cycle.Reason) > maxRunEventTextBytes || strings.ContainsAny(cycle.Reason, "\x00\r\n") {
		return errors.New("mission-review result timestamp or reason is invalid")
	}
	if len(cycle.Findings) > maxMissionReviewFindings {
		return errors.New("mission-review result exceeds its bounded finding limit")
	}
	switch cycle.Outcome {
	case ReviewOutcomeClear:
		if len(cycle.Findings) != 0 || cycle.Reason != "" {
			return errors.New("clear mission review must have no findings or failure reason")
		}
	case ReviewOutcomeFindings:
		if len(cycle.Findings) == 0 || cycle.Reason != "" {
			return errors.New("mission review with findings requires findings and no failure reason")
		}
	case ReviewOutcomeUnresolved, ReviewOutcomeExecutionError, ReviewOutcomeTimedOut, ReviewOutcomeCanceled, ReviewOutcomeUnknown:
		if strings.TrimSpace(cycle.Reason) == "" {
			return errors.New("inconclusive mission review requires a reason")
		}
	default:
		return fmt.Errorf("invalid mission-review outcome %q", cycle.Outcome)
	}
	if cycle.QuiescenceUnconfirmed && cycle.Outcome != ReviewOutcomeUnknown {
		return errors.New("unconfirmed mission-review quiescence requires an unknown outcome")
	}
	seen := make(map[string]bool, len(cycle.Findings))
	for _, finding := range cycle.Findings {
		if finding.ID == "" || len(finding.ID) > 64 || strings.ContainsAny(finding.ID, "\x00\r\n") || seen[finding.ID] ||
			finding.Summary == "" || finding.Evidence == "" || len(finding.Summary) > 256 || len(finding.Evidence) > maxRunEventTextBytes ||
			strings.ContainsAny(finding.Summary+finding.Evidence, "\x00\r\n") || finding.Disposition != ReviewFindingOpen ||
			finding.RepairTaskID != "" || len(finding.Locations) > 4 {
			return errors.New("mission review finding is invalid or exceeds its bounded evidence contract")
		}
		seen[finding.ID] = true
		switch finding.Severity {
		case ReviewSeverityCritical, ReviewSeverityHigh, ReviewSeverityMedium, ReviewSeverityLow:
		default:
			return fmt.Errorf("mission-review finding %q has invalid severity", finding.ID)
		}
		for _, location := range finding.Locations {
			if location == "" || len(location) > 128 || strings.ContainsAny(location, "\x00\r\n") {
				return fmt.Errorf("mission-review finding %q has an invalid location", finding.ID)
			}
		}
	}
	data, err := json.Marshal(cycle)
	if err != nil {
		return fmt.Errorf("encode bounded mission-review cycle: %w", err)
	}
	if len(data) > maxRunEventLineBytes-2048 {
		return errors.New("mission-review record exceeds its bounded journal size")
	}
	return nil
}

// ValidateMissionReviewCycleResult validates a completed record before it is
// appended to the canonical run journal.
func ValidateMissionReviewCycleResult(cycle MissionReviewCycle) error {
	return validateMissionReviewCycle(cycle, false)
}

// BeginMissionReviewCycle durably records the reviewer invocation before it
// starts. It accepts only version-2 mission contracts and an exact landing
// checkpoint whose required final verifier has passed.
func BeginMissionReviewCycle(stateDir string, state *DeepState, cycle MissionReviewCycle) (MissionReviewCycle, error) {
	if state == nil || state.RunEventSchemaVersion != RunEventSchemaVersion || state.ExecutionEpochID == "" ||
		state.Phase != PhaseLanding || state.ExecutionQuiescenceUnconfirmed ||
		state.SemanticReviewContractVersion != SemanticReviewMissionContractVersion {
		return MissionReviewCycle{}, errors.New("mission review requires a quiescent landing under semantic review contract version 2")
	}
	if err := ValidateMissionAcceptanceContract(state.MissionDefinition()); err != nil {
		return MissionReviewCycle{}, err
	}
	if !MissionReviewRequiredAtLanding(*state, cycle.CheckpointCommit, cycle.CheckpointTreeSHA) {
		return MissionReviewCycle{}, errors.New("mission review requires accepted Objective evidence and a passed or unconfigured final verifier for its checkpoint")
	}
	if strings.TrimSpace(state.Verify) != "" {
		if !state.LandingVerifyDone || state.LandingVerificationOutcome != VerificationPassed ||
			state.LandingVerificationSubject == nil || cycle.FinalVerificationSubject == nil || *state.LandingVerificationSubject != *cycle.FinalVerificationSubject ||
			state.LandingVerificationRunID == "" || state.LandingVerificationRunID != cycle.VerificationRunID {
			return MissionReviewCycle{}, errors.New("mission review requires successful final verification of the same repository subject")
		}
	} else if !state.LandingVerifyDone || state.LandingVerificationOutcome != VerificationNotRun || cycle.VerificationRunID != "" || cycle.FinalVerificationSubject != nil {
		return MissionReviewCycle{}, errors.New("mission review has inconsistent no-verifier evidence")
	}
	cycle.SchemaVersion = missionReviewCycleSchemaVersion
	cycle.PolicySHA256 = state.SemanticReviewContractSHA256
	cycle.StartEventID = missionReviewEventID(cycle.ID, "started")
	cycle.StartedInEpochID = state.ExecutionEpochID
	cycle.StartedAt = cycle.StartedAt.UTC()
	cycle.Outcome = ReviewOutcomeStarted
	if err := validateMissionReviewCycle(cycle, true); err != nil {
		return MissionReviewCycle{}, err
	}
	event := RunEvent{
		EventID: cycle.StartEventID, RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: cycle.StartedAt, Actor: "deep-mission-reviewer", Type: RunEventMissionReviewStarted,
		FromPhase: PhaseLanding, ToPhase: PhaseLanding, MissionReview: &cycle,
		TaskSummary: summarizeRunTasks(state.Tasks),
	}
	if err := appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked); err != nil {
		return MissionReviewCycle{}, err
	}
	return cycle, nil
}

// CompleteMissionReviewCycle persists the bounded semantic result. Repeating
// the same result is idempotent across a projection-write failure.
func CompleteMissionReviewCycle(stateDir string, state *DeepState, cycle MissionReviewCycle) error {
	if state == nil || state.RunEventSchemaVersion != RunEventSchemaVersion || state.ExecutionEpochID == "" ||
		state.Phase != PhaseLanding || state.SemanticReviewContractVersion != SemanticReviewMissionContractVersion ||
		cycle.PolicySHA256 != state.SemanticReviewContractSHA256 || cycle.EndedAt.IsZero() {
		return errors.New("mission-review result requires its active version-2 landing contract")
	}
	cycle.EndedAt = cycle.EndedAt.UTC()
	cycle.DurationMilliseconds = max(0, cycle.EndedAt.Sub(cycle.StartedAt).Milliseconds())
	if err := validateMissionReviewCycle(cycle, false); err != nil {
		return err
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		return err
	}
	for _, old := range events {
		if old.EventID != missionReviewEventID(cycle.ID, "result") {
			continue
		}
		if old.Type != RunEventMissionReviewResult || old.MissionReview == nil || !reflect.DeepEqual(*old.MissionReview, cycle) {
			return errors.New("mission-review result identity already has different journal facts")
		}
		fresh, err := LoadState(stateDir, state.SessionID)
		if err != nil {
			return fmt.Errorf("reload durable mission-review result projection: %w", err)
		}
		*state = fresh
		return nil
	}
	projected := *state
	projected.MissionReviewFindings = cloneReviewFindings(state.MissionReviewFindings)
	if err := applyMissionReviewResult(&projected, cycle); err != nil {
		return err
	}
	event := RunEvent{
		EventID: missionReviewEventID(cycle.ID, "result"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: cycle.EndedAt, Actor: "deep-mission-reviewer", Type: RunEventMissionReviewResult,
		FromPhase: PhaseLanding, ToPhase: PhaseLanding, MissionReview: &cycle,
		TaskSummary: summarizeRunTasks(state.Tasks),
	}
	return appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked)
}

// LoadMissionReviewCycle reads one canonical mission-review fact from the
// append-only journal. deep.json is only a projection of that history.
func LoadMissionReviewCycle(stateDir, sessionID, id string) (MissionReviewCycle, bool, error) {
	events, err := ReadRunEvents(stateDir, sessionID)
	if err != nil {
		return MissionReviewCycle{}, false, err
	}
	var cycle MissionReviewCycle
	found := false
	for _, event := range events {
		if event.MissionReview == nil || event.MissionReview.ID != id {
			continue
		}
		if event.Type == RunEventMissionReviewStarted || event.Type == RunEventMissionReviewResult || event.Type == RunEventMissionReviewRecovery {
			cycle, found = *event.MissionReview, true
		}
	}
	return cycle, found, nil
}

// LatestMissionReviewForCheckpoint returns the latest completed review whose
// policy and exact checkpoint identity match the requested landing.
func LatestMissionReviewForCheckpoint(stateDir, sessionID, policySHA256, commit, treeSHA string) (MissionReviewCycle, bool, error) {
	events, err := ReadRunEvents(stateDir, sessionID)
	if err != nil {
		return MissionReviewCycle{}, false, err
	}
	var latest MissionReviewCycle
	found := false
	for _, event := range events {
		cycle := event.MissionReview
		if cycle == nil || cycle.PolicySHA256 != policySHA256 || cycle.CheckpointCommit != commit || cycle.CheckpointTreeSHA != treeSHA {
			continue
		}
		if event.Type == RunEventMissionReviewResult || event.Type == RunEventMissionReviewRecovery {
			latest, found = *cycle, true
		}
	}
	return latest, found, nil
}

// RecoverUnmatchedMissionReviewCycle closes an invocation without a result as
// unknown and fail-closed. A restart cannot assume that the reviewer process
// or one of its descendants stopped writing until its process boundary is
// reconciled.
func RecoverUnmatchedMissionReviewCycle(stateDir string, state *DeepState, at time.Time) (*MissionReviewCycle, error) {
	if state == nil || state.RunEventSchemaVersion != RunEventSchemaVersion || state.SemanticReviewContractVersion != SemanticReviewMissionContractVersion {
		return nil, nil
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		return nil, err
	}
	start, ok := unmatchedMissionReviewStart(events)
	if !ok {
		return nil, nil
	}
	cycle := *start
	cycle.Outcome = ReviewOutcomeUnknown
	cycle.Reason = "mission reviewer invocation has no durable result; process quiescence is unknown"
	cycle.QuiescenceUnconfirmed = true
	cycle.EndedAt = at.UTC()
	cycle.DurationMilliseconds = max(0, cycle.EndedAt.Sub(cycle.StartedAt).Milliseconds())
	if cycle.EndedAt.Before(cycle.StartedAt) {
		cycle.EndedAt = cycle.StartedAt
		cycle.DurationMilliseconds = 0
	}
	event := RunEvent{
		EventID: missionReviewEventID(cycle.ID, "recovery"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: cycle.EndedAt, Actor: "deep-recovery", Type: RunEventMissionReviewRecovery,
		FromPhase: state.Phase, ToPhase: state.Phase, Reason: cycle.Reason, MissionReview: &cycle,
		TaskSummary: summarizeRunTasks(state.Tasks),
	}
	if err := appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked); err != nil {
		return nil, err
	}
	return &cycle, nil
}

func applyMissionReviewStarted(state *DeepState, cycle MissionReviewCycle) error {
	if state == nil || state.Phase != PhaseLanding || state.SemanticReviewContractVersion != SemanticReviewMissionContractVersion ||
		cycle.PolicySHA256 != state.SemanticReviewContractSHA256 ||
		!MissionReviewRequiredAtLanding(*state, cycle.CheckpointCommit, cycle.CheckpointTreeSHA) {
		return errors.New("mission-review start does not match the active landing contract")
	}
	if strings.TrimSpace(state.Verify) != "" {
		if state.LandingVerificationSubject == nil || cycle.VerificationRunID != state.LandingVerificationRunID ||
			cycle.FinalVerificationSubject == nil || *cycle.FinalVerificationSubject != *state.LandingVerificationSubject {
			return errors.New("mission-review start differs from the successful final-verification evidence")
		}
	} else if cycle.VerificationRunID != "" || cycle.FinalVerificationSubject != nil {
		return errors.New("mission-review start contains final-verification evidence for an unconfigured verifier")
	}
	state.MissionReviewCycleID = cycle.ID
	state.MissionReviewOutcome = ReviewOutcomeStarted
	state.MissionReviewReason = ""
	state.MissionReviewCheckpointCommit = cycle.CheckpointCommit
	state.MissionReviewCheckpointTreeSHA = cycle.CheckpointTreeSHA
	subject := cycle.ReviewSubject
	state.MissionReviewSubject = &subject
	state.MissionReviewFindings = nil
	return nil
}

func applyMissionReviewResult(state *DeepState, cycle MissionReviewCycle) error {
	if state == nil || state.Phase != PhaseLanding || state.MissionReviewCycleID != cycle.ID ||
		state.MissionReviewCheckpointCommit != cycle.CheckpointCommit || state.MissionReviewCheckpointTreeSHA != cycle.CheckpointTreeSHA ||
		state.MissionReviewSubject == nil || *state.MissionReviewSubject != cycle.ReviewSubject {
		return errors.New("mission-review result does not match its projected start")
	}
	state.MissionReviewOutcome = cycle.Outcome
	state.MissionReviewReason = cycle.Reason
	state.MissionReviewFindings = cloneReviewFindings(cycle.Findings)
	if cycle.QuiescenceUnconfirmed {
		state.ExecutionQuiescenceUnconfirmed = true
		state.ExecutionQuiescenceTaskID = "mission-semantic-review"
	}
	return nil
}

func missionReviewProjectionMatchesCycle(state DeepState, cycle MissionReviewCycle) bool {
	return state.MissionReviewCycleID == cycle.ID && state.MissionReviewOutcome == cycle.Outcome &&
		state.MissionReviewReason == cycle.Reason && state.MissionReviewCheckpointCommit == cycle.CheckpointCommit &&
		state.MissionReviewCheckpointTreeSHA == cycle.CheckpointTreeSHA && state.MissionReviewSubject != nil &&
		*state.MissionReviewSubject == cycle.ReviewSubject && reflect.DeepEqual(state.MissionReviewFindings, cycle.Findings)
}

func missionReviewProjectionMatchesCheckpoint(state DeepState, cycleID, commit, tree string) bool {
	return cycleID != "" && state.MissionReviewCycleID == cycleID && state.MissionReviewOutcome != "" &&
		state.MissionReviewOutcome != ReviewOutcomeStarted && state.MissionReviewCheckpointCommit == commit &&
		state.MissionReviewCheckpointTreeSHA == tree && state.MissionReviewSubject != nil &&
		state.MissionReviewSubject.HeadCommit == commit && state.MissionReviewSubject.TreeSHA == tree
}

func missionReviewPolicyFactsMatch(state DeepState, cycle MissionReviewCycle) bool {
	return state.SemanticReviewContractVersion == SemanticReviewMissionContractVersion &&
		state.SemanticReviewContractSHA256 != "" && cycle.PolicySHA256 == state.SemanticReviewContractSHA256
}

func unmatchedMissionReviewStart(events []RunEvent) (*MissionReviewCycle, bool) {
	var start *MissionReviewCycle
	for _, event := range events {
		if event.MissionReview == nil {
			continue
		}
		switch event.Type {
		case RunEventMissionReviewStarted:
			copy := *event.MissionReview
			start = &copy
		case RunEventMissionReviewResult, RunEventMissionReviewRecovery:
			start = nil
		}
	}
	return start, start != nil
}

func validateMissionReviewEventTransition(prior []RunEvent, event RunEvent) error {
	missionReviewEvent := event.Type == RunEventMissionReviewStarted || event.Type == RunEventMissionReviewResult ||
		event.Type == RunEventMissionReviewRecovery
	if !missionReviewEvent {
		if _, unmatched := unmatchedMissionReviewStart(prior); unmatched {
			return errors.New("run history contains an unmatched mission-review cycle; recover it before another transition")
		}
		return nil
	}
	if event.MissionReview == nil {
		return errors.New("mission-review event has no cycle record")
	}
	cycle := event.MissionReview
	switch event.Type {
	case RunEventMissionReviewStarted:
		if _, unmatched := unmatchedMissionReviewStart(prior); unmatched {
			return errors.New("cannot start a mission review while another mission-review invocation is unmatched")
		}
		if hasUnmatchedReviewCycle(prior) {
			return errors.New("cannot start a mission review while an Objective review invocation is unmatched")
		}
		for _, old := range prior {
			if old.MissionReview != nil && old.MissionReview.ID == cycle.ID {
				return errors.New("mission-review identity already exists in run history")
			}
		}
		if _, ok := activeLandingReason(prior); !ok {
			return errors.New("mission-review start has no active landing boundary")
		}
		if cycle.VerificationRunID != "" {
			verified := false
			for _, old := range prior {
				run := old.VerificationRun
				if old.Type == RunEventVerificationResult && run != nil && run.ID == cycle.VerificationRunID &&
					run.Purpose == VerificationPurposeMissionEnd && run.Outcome == VerificationPassed &&
					cycle.FinalVerificationSubject != nil && run.Subject == *cycle.FinalVerificationSubject {
					verified = true
				}
			}
			if !verified {
				return errors.New("mission review references no successful final verification of its repository tree")
			}
		}
	case RunEventMissionReviewResult, RunEventMissionReviewRecovery:
		start, ok := unmatchedMissionReviewStart(prior)
		if !ok || !sameMissionReviewStart(*start, *cycle) {
			return errors.New("mission-review result does not match an unmatched durable start")
		}
		if event.Type == RunEventMissionReviewResult && (start.StartedInEpochID != event.EpochID || event.EpochID != prior[len(prior)-1].EpochID) {
			return errors.New("mission-review result cannot complete across a resume epoch; recover the prior cycle")
		}
		if event.Type == RunEventMissionReviewRecovery && !cycle.QuiescenceUnconfirmed {
			return errors.New("mission-review recovery must preserve unconfirmed reviewer quiescence")
		}
		if event.OccurredAt.Before(start.StartedAt) {
			return errors.New("mission-review result observation precedes its durable start")
		}
	}
	return nil
}

func sameMissionReviewStart(start, result MissionReviewCycle) bool {
	return start.ID == result.ID && start.StartEventID == result.StartEventID && start.PolicySHA256 == result.PolicySHA256 &&
		start.ContextSHA256 == result.ContextSHA256 && start.CheckpointCommit == result.CheckpointCommit &&
		start.CheckpointTreeSHA == result.CheckpointTreeSHA && start.ReviewSubject == result.ReviewSubject &&
		start.VerificationRunID == result.VerificationRunID && sameVerificationSubject(start.FinalVerificationSubject, result.FinalVerificationSubject) &&
		start.Reviewer == result.Reviewer && start.Provider == result.Provider &&
		start.Model == result.Model && start.StartedInEpochID == result.StartedInEpochID && start.StartedAt.Equal(result.StartedAt)
}

func missionReviewResultForCheckpoint(events []RunEvent, cycleID, commit, tree string) (MissionReviewCycle, bool) {
	var result MissionReviewCycle
	found := false
	for _, event := range events {
		if event.MissionReview == nil || event.MissionReview.ID != cycleID ||
			event.MissionReview.CheckpointCommit != commit || event.MissionReview.CheckpointTreeSHA != tree {
			continue
		}
		if event.Type == RunEventMissionReviewResult || event.Type == RunEventMissionReviewRecovery {
			result, found = *event.MissionReview, true
		}
	}
	return result, found
}

func latestMissionReviewCycle(events []RunEvent) (MissionReviewCycle, bool) {
	var cycle MissionReviewCycle
	found := false
	for _, event := range events {
		if event.MissionReview == nil {
			continue
		}
		if event.Type == RunEventMissionReviewStarted || event.Type == RunEventMissionReviewResult || event.Type == RunEventMissionReviewRecovery {
			cycle, found = *event.MissionReview, true
		}
	}
	return cycle, found
}

func isMissionReviewCycleID(id string) bool {
	return len(id) == 32 && func() bool { _, err := hex.DecodeString(id); return err == nil }()
}
