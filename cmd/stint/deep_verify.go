package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

type verificationOutcome = deep.VerificationOutcome

const (
	verificationNotRun       = deep.VerificationNotRun
	verificationPassed       = deep.VerificationPassed
	verificationFailed       = deep.VerificationFailed
	verificationTimedOut     = deep.VerificationTimedOut
	verificationExecutionErr = deep.VerificationExecutionErr
	verificationInvalid      = deep.VerificationInvalid
	verificationCanceled     = deep.VerificationCanceled
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
	// QuiescenceUnconfirmed is set when a remote transport/protocol failure
	// prevents Stint from knowing whether verifier descendants can still write.
	QuiescenceUnconfirmed bool
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

// validateMissionVerifyCommands is pure: it inspects persisted executable
// command data without probing tools, contacting compute, or starting a shell.
func validateMissionVerifyCommands(mission deep.Mission) error {
	if err := deep.ValidateMissionAcceptanceContract(mission); err != nil {
		return fmt.Errorf("mission acceptance contract is invalid: %w", err)
	}
	if strings.TrimSpace(mission.Verify) != "" {
		if err := deep.ValidateVerifyCommand(mission.Verify); err != nil {
			return fmt.Errorf("mission verification command is invalid: %w", err)
		}
	}
	for _, task := range mission.Tasks {
		if strings.TrimSpace(task.Verify) != "" {
			if err := deep.ValidateVerifyCommand(task.Verify); err != nil {
				return fmt.Errorf("task %s verification command is invalid: %w", task.ID, err)
			}
		}
		if mission.AcceptanceContractVersion != 0 && strings.TrimSpace(task.AcceptanceCheck) != "" {
			if err := deep.ValidateVerifyCommand(task.AcceptanceCheck); err != nil {
				return fmt.Errorf("task %s acceptance-check command is invalid: %w", task.ID, err)
			}
		}
	}
	return nil
}

// Objective C is being introduced as a stacked change. Until the coordinator
// has the journaled acceptance lifecycle and contract-aware scheduling,
// command entrypoints fail closed instead of running a versioned mission with
// legacy verified-is-terminal semantics.
func requireAcceptanceRuntimeSupport(mission deep.Mission) error {
	if mission.AcceptanceContractVersion != 0 {
		return fmt.Errorf("mission acceptance contract version %d is not executable by this Stint build", mission.AcceptanceContractVersion)
	}
	return nil
}

func missionFromState(state deep.DeepState) deep.Mission {
	return state.MissionDefinition()
}
