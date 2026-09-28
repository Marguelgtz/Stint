package deep

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	SemanticReviewContractVersion = 1
	// SemanticReviewMissionContractVersion adds a checkpoint-bound review of
	// the whole mission result. Version 1 remains the task-Objective review
	// contract for existing missions.
	SemanticReviewMissionContractVersion = 2
	SemanticReviewToolsetName            = "stint_review_no_tools_v1"
	SemanticReviewResultProtocol         = "framed-stint-review-result-v1"
)

type semanticReviewContractIdentity struct {
	Version                  int    `json:"version"`
	AcceptanceContractSHA256 string `json:"acceptanceContractSha256"`
	Scope                    string `json:"scope"`
	ToolPolicy               string `json:"toolPolicy"`
	ResultProtocol           string `json:"resultProtocol"`
}

// ValidateSemanticReviewContract requires an explicitly versioned Objective C
// deterministic acceptance contract. Legacy and v2 missions retain their
// existing semantics when this section is absent.
func ValidateSemanticReviewContract(reviewVersion, acceptanceVersion int, tasks []Task) error {
	switch reviewVersion {
	case 0:
		return nil
	case SemanticReviewContractVersion, SemanticReviewMissionContractVersion:
	default:
		return fmt.Errorf("unsupported semantic review contract version %d", reviewVersion)
	}
	if acceptanceVersion != DeterministicAcceptanceContractVersion {
		return errors.New("semantic review contract requires deterministic acceptance contract version 2")
	}
	if err := ValidateAcceptanceContract(acceptanceVersion, tasks); err != nil {
		return err
	}
	return nil
}

// HasSemanticReviewContract reports whether the mission explicitly opted in
// to checkpoint-bound Objective review. Both supported contract versions keep
// that behavior; version 2 additionally requires a final mission review.
func HasSemanticReviewContract(version int) bool {
	return version == SemanticReviewContractVersion || version == SemanticReviewMissionContractVersion
}

func SemanticReviewContractIdentity(mission Mission) (string, error) {
	if mission.SemanticReviewContractVersion == 0 {
		return "", nil
	}
	if err := ValidateSemanticReviewContract(mission.SemanticReviewContractVersion, mission.AcceptanceContractVersion, mission.Tasks); err != nil {
		return "", err
	}
	scope := "each-mission-objective-checkpoint"
	if mission.SemanticReviewContractVersion == SemanticReviewMissionContractVersion {
		scope = "each-mission-objective-checkpoint-and-final-mission-checkpoint"
	}
	identity := semanticReviewContractIdentity{
		Version: mission.SemanticReviewContractVersion, AcceptanceContractSHA256: mission.AcceptanceContractSHA256,
		Scope:          scope,
		ToolPolicy:     "hermes-safe-ignore-user-config-ignore-rules-toolset-" + SemanticReviewToolsetName,
		ResultProtocol: SemanticReviewResultProtocol,
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("encode semantic review contract identity: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func ValidateMissionSemanticReviewContract(mission Mission) error {
	want, err := SemanticReviewContractIdentity(mission)
	if err != nil {
		return err
	}
	if want != mission.SemanticReviewContractSHA256 {
		return errors.New("semantic review contract identity does not match the durable mission contract")
	}
	return nil
}
