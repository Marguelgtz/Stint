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
	SemanticReviewToolsetName     = "stint_review_no_tools_v1"
	SemanticReviewResultProtocol  = "framed-stint-review-result-v1"
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
	case SemanticReviewContractVersion:
	default:
		return fmt.Errorf("unsupported semantic review contract version %d", reviewVersion)
	}
	if acceptanceVersion != DeterministicAcceptanceContractVersion {
		return errors.New("semantic review contract version 1 requires deterministic acceptance contract version 2")
	}
	if err := ValidateAcceptanceContract(acceptanceVersion, tasks); err != nil {
		return err
	}
	return nil
}

func SemanticReviewContractIdentity(mission Mission) (string, error) {
	if mission.SemanticReviewContractVersion == 0 {
		return "", nil
	}
	if err := ValidateSemanticReviewContract(mission.SemanticReviewContractVersion, mission.AcceptanceContractVersion, mission.Tasks); err != nil {
		return "", err
	}
	identity := semanticReviewContractIdentity{
		Version: SemanticReviewContractVersion, AcceptanceContractSHA256: mission.AcceptanceContractSHA256,
		Scope:          "each-mission-objective-checkpoint",
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
