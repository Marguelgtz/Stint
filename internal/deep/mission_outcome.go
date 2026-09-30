package deep

import (
	"encoding/hex"
	"strings"
)

// MissionOutcome is the outcome of deterministic mission-level completion
// checks. It is independent of PhaseLanded, which only records that Stint
// reached a recoverable stopping and handoff boundary.
type MissionOutcome string

const (
	MissionOutcomePending    MissionOutcome = "pending"
	MissionOutcomeSucceeded  MissionOutcome = "succeeded"
	MissionOutcomeFailed     MissionOutcome = "failed"
	MissionOutcomeIncomplete MissionOutcome = "incomplete"
	MissionOutcomeUnresolved MissionOutcome = "unresolved"
	MissionOutcomeUnknown    MissionOutcome = "unknown"
)

// DisplayMissionOutcome returns a safe label for persisted values. Empty
// outcomes in legacy terminal states remain unknown; active/landing legacy
// states can still be described as pending without changing their durable
// representation.
func DisplayMissionOutcome(outcome MissionOutcome, phase Phase) MissionOutcome {
	switch outcome {
	case MissionOutcomePending, MissionOutcomeSucceeded, MissionOutcomeFailed,
		MissionOutcomeIncomplete, MissionOutcomeUnresolved, MissionOutcomeUnknown:
		return outcome
	}
	if phase == PhaseInitializing || phase == PhaseExecuting || phase == PhaseLanding {
		return MissionOutcomePending
	}
	return MissionOutcomeUnknown
}

// DetermineMissionOutcome applies the deterministic completion semantics
// declared by the mission contract version. Legacy contracts preserve the
// verified-is-terminal rule; version 2 requires bound Objective acceptance.
func DetermineMissionOutcome(state DeepState) MissionOutcome {
	if state.AcceptanceContractVersion == DeterministicAcceptanceContractVersion {
		return determineAcceptedMissionOutcome(state)
	}
	if state.AcceptanceContractVersion != 0 {
		return MissionOutcomeUnresolved
	}
	return determineLegacyMissionOutcome(state)
}

// MissionObjectivesSemanticallyComplete reports whether every Objective in a
// deterministic-acceptance mission has accepted evidence and, when the
// semantic-review contract is enabled, a satisfied checkpoint-bound review
// gate. A whole-mission review is only useful after this boundary.
func MissionObjectivesSemanticallyComplete(state DeepState) bool {
	if state.AcceptanceContractVersion != DeterministicAcceptanceContractVersion {
		return false
	}
	seen := false
	for _, task := range state.Tasks {
		if !task.IsAcceptanceContractTask() {
			continue
		}
		seen = true
		if task.Status != StatusAccepted || task.AcceptanceOutcome != AcceptanceAccepted || !taskHasBoundAcceptance(task) {
			return false
		}
		if HasSemanticReviewContract(state.SemanticReviewContractVersion) && !TaskHasSatisfiedReviewGate(task.ID, state.Tasks) {
			return false
		}
	}
	return seen
}

// MissionReviewRequiredAtLanding reports whether the semantic mission-review
// gate applies to this landing checkpoint. A configured final verifier must
// have passed against the same Git tree; a failed, timed-out, canceled, or
// otherwise inconclusive verifier keeps mission outcome unresolved without
// preventing the coordinator from reaching a recoverable landing boundary.
func MissionReviewRequiredAtLanding(state DeepState, checkpointCommit, checkpointTree string) bool {
	if state.SemanticReviewContractVersion != SemanticReviewMissionContractVersion ||
		!MissionObjectivesSemanticallyComplete(state) || checkpointCommit == "" || checkpointTree == "" {
		return false
	}
	if strings.TrimSpace(state.Verify) == "" {
		return state.LandingVerifyDone && state.LandingVerificationOutcome == VerificationNotRun &&
			state.LandingVerificationRunID == "" && state.LandingVerificationSubject == nil
	}
	subject := state.LandingVerificationSubject
	return state.LandingVerifyDone && state.LandingVerificationOutcome == VerificationPassed &&
		state.LandingVerificationRunID != "" && subject != nil && subject.HeadCommit != "" &&
		subject.TreeSHA == checkpointTree
}

func determineLegacyMissionOutcome(state DeepState) MissionOutcome {
	if strings.TrimSpace(state.Verify) != "" && state.LandingVerifyDone &&
		state.LandingVerificationOutcome == VerificationFailed && finalVerificationMatchesCheckpoint(state) {
		return MissionOutcomeFailed
	}
	if len(state.Tasks) == 0 {
		return MissionOutcomeIncomplete
	}
	for _, task := range state.Tasks {
		if task.Status != StatusVerified {
			return MissionOutcomeIncomplete
		}
		if !taskHasBoundVerification(task) {
			return MissionOutcomeUnresolved
		}
	}
	if strings.TrimSpace(state.Verify) != "" {
		if !state.LandingVerifyDone || state.LandingVerificationOutcome != VerificationPassed ||
			!finalVerificationMatchesCheckpoint(state) {
			return MissionOutcomeUnresolved
		}
	}
	if state.LandingCommit == "" || state.LandingCheckpointTreeSHA == "" {
		return MissionOutcomeUnresolved
	}
	return MissionOutcomeSucceeded
}

func determineAcceptedMissionOutcome(state DeepState) MissionOutcome {
	if err := ValidateMissionAcceptanceContract(state.MissionDefinition()); err != nil {
		return MissionOutcomeUnresolved
	}
	if err := ValidateMissionSemanticReviewContract(state.MissionDefinition()); err != nil {
		return MissionOutcomeUnresolved
	}
	if strings.TrimSpace(state.Verify) != "" && state.LandingVerifyDone &&
		state.LandingVerificationOutcome == VerificationFailed {
		if finalVerificationMatchesCheckpoint(state) {
			return MissionOutcomeFailed
		}
		return MissionOutcomeUnresolved
	}
	if len(state.Tasks) == 0 {
		return MissionOutcomeIncomplete
	}
	for _, task := range state.Tasks {
		if task.IsAcceptanceContractTask() {
			switch task.AcceptanceOutcome {
			case AcceptanceAccepted:
				if !taskHasBoundAcceptance(task) {
					return MissionOutcomeUnresolved
				}
				if HasSemanticReviewContract(state.SemanticReviewContractVersion) && !TaskHasSatisfiedReviewGate(task.ID, state.Tasks) {
					return MissionOutcomeUnresolved
				}
			case AcceptanceUnresolved:
				return MissionOutcomeUnresolved
			case AcceptanceNotSatisfied, AcceptanceNotEvaluated, "":
				return MissionOutcomeIncomplete
			default:
				return MissionOutcomeUnresolved
			}
			continue
		}
		// Coordinator-owned rows such as the optional action-plan bootstrap are
		// operational helpers, not mission-authored Objectives. Their state
		// cannot gate version 2 mission acceptance.
	}
	if strings.TrimSpace(state.Verify) != "" {
		if !state.LandingVerifyDone || state.LandingVerificationOutcome != VerificationPassed ||
			!finalVerificationMatchesCheckpoint(state) {
			return MissionOutcomeUnresolved
		}
	}
	if state.LandingCommit == "" || state.LandingCheckpointTreeSHA == "" {
		return MissionOutcomeUnresolved
	}
	if state.SemanticReviewContractVersion == SemanticReviewMissionContractVersion &&
		(!missionReviewProjectionMatchesCheckpoint(state, state.MissionReviewCycleID, state.LandingCommit, state.LandingCheckpointTreeSHA) ||
			state.MissionReviewOutcome != ReviewOutcomeClear) {
		return MissionOutcomeUnresolved
	}
	return MissionOutcomeSucceeded
}

func taskHasBoundClearReview(task Task) bool {
	if task.ReviewOutcome != ReviewOutcomeClear || task.ReviewReason != "" || len(task.ReviewFindings) != 0 ||
		len(task.ReviewCycleID) != 32 || task.ReviewCheckpointEventID == "" || task.ReviewCheckpointCommit == "" ||
		task.ReviewCheckpointTreeSHA == "" || task.ReviewCheckpointCommit != task.AcceptanceCheckpointCommit ||
		task.ReviewCheckpointTreeSHA != task.AcceptanceCheckpointTreeSHA ||
		task.ReviewCheckpointEventID != task.AcceptanceCheckpointEventID {
		return false
	}
	if _, err := hex.DecodeString(task.ReviewCycleID); err != nil {
		return false
	}
	return true
}

func finalVerificationMatchesCheckpoint(state DeepState) bool {
	subject := state.LandingVerificationSubject
	return subject != nil && subject.HeadCommit != "" && subject.TreeSHA != "" &&
		state.LandingCheckpointTreeSHA != "" && subject.TreeSHA == state.LandingCheckpointTreeSHA
}

func taskHasBoundVerification(task Task) bool {
	if task.VerificationCommand == "" || task.VerificationSubject == nil || task.VerificationSubject.HeadCommit == "" ||
		task.VerificationSubject.TreeSHA == "" || task.CheckpointCommit == "" ||
		task.CheckpointTreeSHA == "" {
		return false
	}
	return task.VerificationSubject.TreeSHA == task.CheckpointTreeSHA
}

func taskHasBoundAcceptance(task Task) bool {
	if task.Status != StatusAccepted || task.AcceptanceCheckOutcome != AcceptanceCheckPassed ||
		len(task.AcceptanceRunID) != 32 || task.AcceptanceCheckpointEventID == "" ||
		task.CheckpointCommit == "" || task.CheckpointTreeSHA == "" ||
		task.AcceptanceCheckpointCommit != task.CheckpointCommit ||
		task.AcceptanceCheckpointTreeSHA != task.CheckpointTreeSHA || task.AcceptanceSubject == nil ||
		task.VerificationSubject == nil || task.VerificationSubject.TreeSHA != task.CheckpointTreeSHA {
		return false
	}
	if _, err := hex.DecodeString(task.AcceptanceRunID); err != nil {
		return false
	}
	checkpoint := TaskCheckpoint{
		TaskID: task.ID, Attempt: task.Attempts, ExecutorRunID: task.ExecutorRunID,
		VerificationRunID: task.VerificationRunID, VerificationSubject: *task.VerificationSubject,
		Commit: task.CheckpointCommit, TreeSHA: task.CheckpointTreeSHA,
	}
	if task.AcceptanceCheckpointEventID != taskCheckpointRecordEventID(checkpoint) {
		return false
	}
	want := VerificationSubject{HeadCommit: task.CheckpointCommit, TreeSHA: task.CheckpointTreeSHA}
	return *task.AcceptanceSubject == want
}
