package deep

// VerificationSubject identifies the exact Git-visible product tree exercised
// by a verifier and the HEAD from which that worktree state was observed.
// Git-ignored inputs and external runtime state are outside this identity.
type VerificationSubject struct {
	HeadCommit string `json:"headCommit"`
	TreeSHA    string `json:"treeSha"`
}

// VerificationOutcome is the bounded result category recorded for a
// verification command. Detailed command output remains in the existing
// verification summary; this value is persisted separately when a mission
// outcome depends on the result.
type VerificationOutcome string

const (
	VerificationNotRun       VerificationOutcome = "not_run"
	VerificationPassed       VerificationOutcome = "passed"
	VerificationFailed       VerificationOutcome = "failed"
	VerificationTimedOut     VerificationOutcome = "timed_out"
	VerificationExecutionErr VerificationOutcome = "execution_error"
	VerificationInvalid      VerificationOutcome = "invalid_command"
	VerificationCanceled     VerificationOutcome = "canceled"
)

// Recorded reports whether this is a concrete verifier result that can be
// reused when recovering an interrupted landing.
func (o VerificationOutcome) Recorded() bool {
	switch o {
	case VerificationPassed, VerificationFailed, VerificationTimedOut,
		VerificationExecutionErr, VerificationInvalid, VerificationCanceled:
		return true
	default:
		return false
	}
}
