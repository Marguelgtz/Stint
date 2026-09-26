package deep

import "strings"

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
	if phase == PhaseExecuting || phase == PhaseLanding {
		return MissionOutcomePending
	}
	return MissionOutcomeUnknown
}

// DetermineMissionOutcome applies the current deterministic completion
// contract. An explicit non-zero final mission verifier is a mission failure.
// Other missing or inconclusive evidence cannot establish success. Tasks
// count as complete only when their verified status is bound to a concrete
// verifier command, exact subject, and matching checkpoint tree; legacy
// verified states without that provenance remain unresolved.
func DetermineMissionOutcome(state DeepState) MissionOutcome {
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
