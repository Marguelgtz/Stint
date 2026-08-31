package spark

import "testing"

func TestOnboardingEvidenceIsStable(t *testing.T) {
	plan := CreateOnboardingPlan("")
	expected := []string{"go-vet", "spark-profile", "unit-tests"}
	if len(plan.ExpectedEvidence) != len(expected) {
		t.Fatalf("unexpected evidence: %#v", plan.ExpectedEvidence)
	}
	for i := range expected {
		if plan.ExpectedEvidence[i] != expected[i] {
			t.Fatalf("expected %q at %d, got %q", expected[i], i, plan.ExpectedEvidence[i])
		}
	}
}
