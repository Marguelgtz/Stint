package deep

import (
	"strings"
	"testing"
)

func TestSemanticReviewContractIsExplicitAndVersioned(t *testing.T) {
	base := "# semantic review\n\n## Objective\nprove the requested result\n\n## Acceptance Contract\nversion: 2\n\n## Semantic Review Contract\nversion: 1\n\n## Tasks\n- [ ] OBJ-1: add the result\n  - repository-change: required\n  - acceptance-check: test -e result.txt\n"
	mission, err := ParseMission(base)
	if err != nil {
		t.Fatalf("parse semantic review contract: %v", err)
	}
	if mission.SemanticReviewContractVersion != SemanticReviewContractVersion || mission.SemanticReviewContractSHA256 == "" {
		t.Fatalf("semantic review contract identity = %d/%q", mission.SemanticReviewContractVersion, mission.SemanticReviewContractSHA256)
	}

	_, err = ParseMission(strings.Replace(base, "## Acceptance Contract\nversion: 2\n\n", "", 1))
	if err == nil || !strings.Contains(err.Error(), "requires deterministic acceptance contract version 2") {
		t.Fatalf("semantic review contract without v2 acceptance = %v, want explicit contract error", err)
	}
	_, err = ParseMission(strings.Replace(base, "version: 1\n\n## Tasks", "version: 9\n\n## Tasks", 1))
	if err == nil || !strings.Contains(err.Error(), "unsupported semantic review contract version") {
		t.Fatalf("unsupported semantic review contract version = %v", err)
	}
}

func TestSemanticReviewContractIdentityDoesNotDependOnTaskProjection(t *testing.T) {
	mission, err := ParseMission("# identity\n\n## Objective\nship it\n\n## Acceptance Contract\nversion: 2\n\n## Semantic Review Contract\nversion: 1\n\n## Tasks\n- [ ] OBJ-1: implement it\n  - repository-change: optional\n  - acceptance-check: test -e output.txt\n")
	if err != nil {
		t.Fatal(err)
	}
	mission.Tasks[0].Status = StatusAccepted
	mission.Tasks[0].Attempts = 3
	mission.Tasks[0].ReviewOutcome = ReviewOutcomeFindings
	identity, err := SemanticReviewContractIdentity(mission)
	if err != nil {
		t.Fatal(err)
	}
	if identity != mission.SemanticReviewContractSHA256 {
		t.Fatalf("mutable task projection changed semantic review contract identity: %s != %s", identity, mission.SemanticReviewContractSHA256)
	}
}

func TestSaveDirCannotDowngradeJournaledSemanticReviewContract(t *testing.T) {
	stateDir, state, _, _ := acceptanceRunFixtureWithSemanticReview(t, true)
	state.SemanticReviewContractVersion = 0
	state.SemanticReviewContractSHA256 = ""
	if err := state.SaveDir(stateDir); err == nil || !strings.Contains(err.Error(), "lifecycle state must be changed through a RunEvent") {
		t.Fatalf("journaled semantic-review contract downgrade = %v, want rejection", err)
	}
}
