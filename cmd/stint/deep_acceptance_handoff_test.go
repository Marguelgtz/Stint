package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func TestVersionedHandoffDoesNotTreatVerifiedCheckpointAsAccepted(t *testing.T) {
	state := deep.DeepState{
		MissionName: "acceptance fixture", AcceptanceContractVersion: deep.DeterministicAcceptanceContractVersion,
		Objective: "deliver the API", Phase: deep.PhaseLanded, MissionOutcome: deep.MissionOutcomeIncomplete,
		Tasks: []deep.Task{{
			ID: "OBJ-1", Objective: "implement API", Status: deep.StatusVerified, Attempts: 1,
			AcceptanceOutcome: deep.AcceptanceUnresolved, AcceptanceCheckOutcome: deep.AcceptanceCheckTimedOut,
			AcceptanceCheck: "test -x ./bin/server", AcceptanceReason: "acceptance check timed out",
			CheckpointCommit: "checkpoint-sha", VerificationResult: "repository verification passed",
		}},
	}
	handoff := buildHandoff(state, "time budget exhausted", time.Now().UTC(), "", deep.RepoSummary{})
	for _, want := range []string{"Mission outcome | incomplete", "verified (acceptance unresolved)", "deterministic acceptance unresolved", "check timed_out", "repository verification passed", "checkpoint `checkpoint-sha`", "**OBJ-1** (verified): implement API"} {
		if !strings.Contains(handoff, want) {
			t.Fatalf("versioned handoff missing %q:\n%s", want, handoff)
		}
	}
	if strings.Contains(handoff, "All tasks reached a terminal state") {
		t.Fatalf("unaccepted Objective was represented as complete:\n%s", handoff)
	}
}

func TestVersionedHandoffReportsAcceptedEvidence(t *testing.T) {
	state := deep.DeepState{
		MissionName: "acceptance fixture", AcceptanceContractVersion: deep.DeterministicAcceptanceContractVersion,
		Objective: "deliver the API", Phase: deep.PhaseLanded, MissionOutcome: deep.MissionOutcomeSucceeded,
		Tasks: []deep.Task{{
			ID: "OBJ-1", Objective: "implement API", Status: deep.StatusAccepted,
			AcceptanceOutcome: deep.AcceptanceAccepted, AcceptanceCheckOutcome: deep.AcceptanceCheckPassed,
			AcceptanceCheck: "test -x ./bin/server", CheckpointCommit: "checkpoint-sha",
		}},
	}
	handoff := buildHandoff(state, "complete", time.Now().UTC(), "", deep.RepoSummary{})
	for _, want := range []string{"Mission outcome | succeeded", "accepted", "deterministic acceptance accepted (check passed)", "checkpoint `checkpoint-sha`"} {
		if !strings.Contains(handoff, want) {
			t.Fatalf("accepted handoff missing %q:\n%s", want, handoff)
		}
	}
}

func TestVersionedHandoffDoesNotMakeOptionalPlanRowRequired(t *testing.T) {
	state := deep.DeepState{
		MissionName: "acceptance fixture", AcceptanceContractVersion: deep.DeterministicAcceptanceContractVersion,
		Objective: "deliver the API", Phase: deep.PhaseLanded, MissionOutcome: deep.MissionOutcomeSucceeded,
		Tasks: []deep.Task{
			{
				ID: "OBJ-1", Objective: "implement API", Status: deep.StatusAccepted,
				AcceptanceOutcome: deep.AcceptanceAccepted, AcceptanceCheckOutcome: deep.AcceptanceCheckPassed,
				CheckpointCommit: "checkpoint-sha",
			},
			{ID: "STINT-PLAN-001", Objective: "write optional plan", Source: "coordinator", Status: deep.StatusBlocked},
		},
	}
	handoff := buildHandoff(state, "complete", time.Now().UTC(), "", deep.RepoSummary{})
	if !strings.Contains(handoff, "STINT-PLAN-001") {
		t.Fatalf("handoff hid the optional coordinator row's state:\n%s", handoff)
	}
	remaining := handoff[strings.Index(handoff, "## Remaining work & next action"):]
	if strings.Contains(remaining, "**STINT-PLAN-001**") || !strings.Contains(remaining, "All mission Objectives reached deterministic acceptance") {
		t.Fatalf("optional plan bootstrap was treated as required Objective work:\n%s", remaining)
	}
}
