package main

import (
	"fmt"
	"strings"
	"time"
)

type verificationOutcome string

const (
	verificationNotRun       verificationOutcome = "not_run"
	verificationPassed       verificationOutcome = "passed"
	verificationFailed       verificationOutcome = "failed"
	verificationTimedOut     verificationOutcome = "timed_out"
	verificationExecutionErr verificationOutcome = "execution_error"
	verificationInvalid      verificationOutcome = "invalid_command"
	verificationCanceled     verificationOutcome = "canceled"
)

// verificationResult preserves how a verifier ended instead of collapsing
// every nonzero, timeout, or launch failure into a boolean.
type verificationResult struct {
	Command     string
	Outcome     verificationOutcome
	ExitCode    int
	HasExitCode bool
	StartedAt   time.Time
	CompletedAt time.Time
	Output      string
	Error       string
}

func (r verificationResult) Passed() bool { return r.Outcome == verificationPassed }

func (r verificationResult) Summary() string {
	switch r.Outcome {
	case verificationPassed:
		return "repository verification passed"
	case verificationNotRun:
		return "not run"
	case verificationFailed:
		if r.HasExitCode {
			return fmt.Sprintf("repository verification failed (exit %d)", r.ExitCode)
		}
		return "repository verification failed"
	case verificationTimedOut:
		return "repository verification timed out"
	case verificationInvalid:
		return "repository verification command invalid: " + r.Error
	case verificationCanceled:
		return "repository verification canceled: " + r.Error
	case verificationExecutionErr:
		return "repository verification execution error: " + r.Error
	default:
		return "repository verification outcome unknown"
	}
}

func (r verificationResult) IncidentDetail() string {
	parts := []string{"command=`" + r.Command + "`", "outcome=" + string(r.Outcome)}
	if r.HasExitCode {
		parts = append(parts, fmt.Sprintf("exit=%d", r.ExitCode))
	}
	if r.Error != "" {
		parts = append(parts, "error="+r.Error)
	}
	if output := strings.TrimSpace(r.Output); output != "" {
		parts = append(parts, "output="+output)
	}
	return strings.Join(parts, " ")
}
