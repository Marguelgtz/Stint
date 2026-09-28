package deep

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const DeterministicAcceptanceContractVersion = 2

type acceptanceContractTaskIdentity struct {
	TaskID                string                      `json:"taskId"`
	Objective             string                      `json:"objective"`
	DependsOn             []string                    `json:"dependsOn,omitempty"`
	RepositoryChange      RepositoryChangeExpectation `json:"repositoryChange"`
	VerificationSHA256    string                      `json:"verificationSha256,omitempty"`
	AcceptanceCheckSHA256 string                      `json:"acceptanceCheckSha256"`
}

type acceptanceContractIdentity struct {
	Version                   int                              `json:"version"`
	Objective                 string                           `json:"objective"`
	Success                   []string                         `json:"success,omitempty"`
	Constraints               []string                         `json:"constraints,omitempty"`
	MissionVerificationSHA256 string                           `json:"missionVerificationSha256,omitempty"`
	Tasks                     []acceptanceContractTaskIdentity `json:"tasks"`
}

// ValidateAcceptanceContract checks only the semantics explicitly selected by
// the mission contract version. Version zero remains legacy, even if a legacy
// mission happens to contain similarly named task fields.
func ValidateAcceptanceContract(version int, tasks []Task) error {
	switch version {
	case 0:
		return nil
	case DeterministicAcceptanceContractVersion:
	default:
		return fmt.Errorf("unsupported mission acceptance contract version %d", version)
	}
	for _, task := range tasks {
		if !isAcceptanceContractTask(task) {
			continue
		}
		if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.Objective) == "" {
			return fmt.Errorf("task %s: acceptance contract requires a stable Work Unit ID and objective", task.ID)
		}
		switch task.RepositoryChange {
		case RepositoryChangeRequired, RepositoryChangeOptional, RepositoryChangeForbidden:
		default:
			return fmt.Errorf("task %s: acceptance contract requires repository-change: required, optional, or forbidden", task.ID)
		}
		if strings.TrimSpace(task.AcceptanceCheck) == "" {
			return fmt.Errorf("task %s: acceptance contract requires an objective-specific acceptance-check command", task.ID)
		}
		if err := ValidateVerifyCommand(task.AcceptanceCheck); err != nil {
			return fmt.Errorf("task %s acceptance-check command: %w", task.ID, err)
		}
	}
	return nil
}

// AcceptanceContractIdentity returns a stable digest of the declared
// deterministic acceptance contract. It includes mission-authored Work Unit
// objectives, success and constraint declarations, dependencies, configured
// generic verifiers, repository-change expectations, and one-way identities
// of acceptance commands. Journal-generated repair Work Units are validated
// against their parent but do not mutate the authored mission identity. Raw
// command text is not copied into event records.
func AcceptanceContractIdentity(mission Mission) (string, error) {
	if mission.AcceptanceContractVersion == 0 {
		return "", nil
	}
	if strings.TrimSpace(mission.Objective) == "" {
		return "", errors.New("acceptance contract identity requires the mission objective")
	}
	if err := ValidateAcceptanceContract(mission.AcceptanceContractVersion, mission.Tasks); err != nil {
		return "", err
	}
	identity := acceptanceContractIdentity{
		Version: mission.AcceptanceContractVersion, Objective: mission.Objective,
		Success: append([]string(nil), mission.Success...), Constraints: append([]string(nil), mission.Constraints...),
	}
	if mission.Verify != "" {
		identity.MissionVerificationSHA256 = VerificationCommandIdentity(mission.Verify)
	}
	for _, task := range mission.Tasks {
		if !isMissionAuthoredAcceptanceTask(task) {
			continue
		}
		taskVerificationSHA256 := ""
		if task.Verify != "" {
			taskVerificationSHA256 = VerificationCommandIdentity(task.Verify)
		}
		identity.Tasks = append(identity.Tasks, acceptanceContractTaskIdentity{
			TaskID: task.ID, Objective: task.Objective, DependsOn: append([]string(nil), task.DependsOn...),
			RepositoryChange: task.RepositoryChange, VerificationSHA256: taskVerificationSHA256,
			AcceptanceCheckSHA256: VerificationCommandIdentity(task.AcceptanceCheck),
		})
	}
	if len(identity.Tasks) == 0 {
		return "", errors.New("acceptance contract has no mission Work Units")
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("encode acceptance contract identity: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// ValidateAcceptanceContractProjection protects a persisted mission contract
// from accidental edits and keeps pre-contract archives on the legacy path.
func ValidateMissionAcceptanceContract(mission Mission) error {
	want, err := AcceptanceContractIdentity(mission)
	if err != nil {
		return err
	}
	if want != mission.AcceptanceContractSHA256 {
		return errors.New("mission acceptance contract identity does not match its Work Unit declarations")
	}
	return nil
}

func isAcceptanceContractTask(task Task) bool {
	return isMissionAuthoredAcceptanceTask(task) || isReviewRepairTask(task)
}

func isMissionAuthoredAcceptanceTask(task Task) bool {
	return task.Source != "coordinator" && task.Source != "review_repair" && !isReservedTaskID(task.ID)
}

func isReviewRepairTask(task Task) bool {
	return task.Source == "review_repair" && task.RepairContext != nil
}
