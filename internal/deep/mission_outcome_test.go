package deep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDetermineMissionOutcomeRequiresBoundTaskAndFinalEvidence(t *testing.T) {
	task := Task{
		ID:                  "T-001",
		Status:              StatusVerified,
		VerificationCommand: "test -f result.txt",
		VerificationResult:  "repository verification passed",
		VerificationSubject: &VerificationSubject{HeadCommit: "task-head", TreeSHA: "task-tree"},
		CheckpointCommit:    "task-checkpoint",
		CheckpointTreeSHA:   "task-tree",
	}
	base := DeepState{
		Tasks:                    []Task{task},
		LandingCommit:            "landing-commit",
		LandingCheckpointTreeSHA: "landing-tree",
	}

	tests := []struct {
		name  string
		state DeepState
		want  MissionOutcome
	}{
		{name: "all task evidence is bound and no mission verifier is configured", state: base, want: MissionOutcomeSucceeded},
		{name: "configured final verifier passed on checkpoint tree", state: withFinalVerification(base, VerificationPassed, "landing-tree"), want: MissionOutcomeSucceeded},
		{name: "failed required final verifier overrides accepted tasks", state: withFinalVerification(base, VerificationFailed, "landing-tree"), want: MissionOutcomeFailed},
		{name: "failed final verifier without matching checkpoint provenance is unresolved", state: withFinalVerification(base, VerificationFailed, "other-tree"), want: MissionOutcomeUnresolved},
		{name: "task not accepted", state: func() DeepState {
			s := base
			s.Tasks = append([]Task(nil), s.Tasks...)
			s.Tasks[0].Status = StatusBlocked
			return s
		}(), want: MissionOutcomeIncomplete},
		{name: "legacy verified task lacks exact provenance", state: func() DeepState {
			s := base
			s.Tasks = append([]Task(nil), s.Tasks...)
			s.Tasks[0].VerificationSubject = nil
			return s
		}(), want: MissionOutcomeUnresolved},
		{name: "final verifier timed out", state: withFinalVerification(base, VerificationTimedOut, "landing-tree"), want: MissionOutcomeUnresolved},
		{name: "final verifier subject differs from checkpoint", state: withFinalVerification(base, VerificationPassed, "other-tree"), want: MissionOutcomeUnresolved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetermineMissionOutcome(tt.state); got != tt.want {
				t.Fatalf("DetermineMissionOutcome() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetermineMissionOutcomeV2RequiresBoundObjectiveAcceptance(t *testing.T) {
	base := acceptedMissionOutcomeFixture(t)
	tests := []struct {
		name   string
		mutate func(*DeepState)
		want   MissionOutcome
	}{
		{name: "all Objective outcomes accepted", want: MissionOutcomeSucceeded},
		{name: "verified checkpoint is not acceptance", mutate: func(s *DeepState) {
			s.Tasks[0].Status = StatusVerified
			s.Tasks[0].AcceptanceOutcome = AcceptanceNotEvaluated
			s.Tasks[0].AcceptanceCheckOutcome = ""
		}, want: MissionOutcomeIncomplete},
		{name: "accepted outcome without accepted task state is unresolved", mutate: func(s *DeepState) {
			s.Tasks[0].Status = StatusVerified
		}, want: MissionOutcomeUnresolved},
		{name: "insufficient deterministic evidence is unresolved", mutate: func(s *DeepState) {
			s.Tasks[0].AcceptanceOutcome = AcceptanceUnresolved
			s.Tasks[0].AcceptanceCheckOutcome = AcceptanceCheckTimedOut
		}, want: MissionOutcomeUnresolved},
		{name: "accepted outcome without matching checkpoint is unresolved", mutate: func(s *DeepState) {
			s.Tasks[0].AcceptanceCheckpointTreeSHA = "different-tree"
		}, want: MissionOutcomeUnresolved},
		{name: "required final verifier still gates mission success", mutate: func(s *DeepState) {
			s.Verify = "final-check"
		}, want: MissionOutcomeUnresolved},
		{name: "final verifier pass allows accepted mission", mutate: func(s *DeepState) {
			*s = withFinalVerification(*s, VerificationPassed, "landing-tree")
		}, want: MissionOutcomeSucceeded},
		{name: "required final verifier failure overrides accepted tasks", mutate: func(s *DeepState) {
			*s = withFinalVerification(*s, VerificationFailed, "landing-tree")
		}, want: MissionOutcomeFailed},
		{name: "failed final verifier on another tree is unresolved", mutate: func(s *DeepState) {
			*s = withFinalVerification(*s, VerificationFailed, "other-tree")
		}, want: MissionOutcomeUnresolved},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := base
			state.Tasks = append([]Task(nil), base.Tasks...)
			if tc.mutate != nil {
				tc.mutate(&state)
			}
			if got := DetermineMissionOutcome(state); got != tc.want {
				t.Fatalf("DetermineMissionOutcome() = %q, want %q", got, tc.want)
			}
		})
	}
}

func acceptedMissionOutcomeFixture(t *testing.T) DeepState {
	t.Helper()
	task := Task{
		ID: "OBJ-1", Objective: "produce the requested result", Status: StatusAccepted,
		RepositoryChange: RepositoryChangeOptional, AcceptanceCheck: "test -e result.txt",
		AcceptanceOutcome: AcceptanceAccepted, AcceptanceCheckOutcome: AcceptanceCheckPassed,
		AcceptanceRunID: strings.Repeat("a", 32), AcceptanceCheckpointEventID: "task-checkpoint/executor/" + strings.Repeat("b", 32) + "/created",
		ExecutorRunID: strings.Repeat("b", 32), Attempts: 1,
		AcceptanceCheckpointCommit: "task-checkpoint", AcceptanceCheckpointTreeSHA: "task-tree",
		CheckpointCommit: "task-checkpoint", CheckpointTreeSHA: "task-tree",
		VerificationSubject: &VerificationSubject{HeadCommit: "before-checkpoint", TreeSHA: "task-tree"},
		AcceptanceSubject:   &VerificationSubject{HeadCommit: "task-checkpoint", TreeSHA: "task-tree"},
	}
	mission := Mission{Objective: "complete the mission", Tasks: []Task{task}, AcceptanceContractVersion: DeterministicAcceptanceContractVersion}
	identity, err := AcceptanceContractIdentity(mission)
	if err != nil {
		t.Fatal(err)
	}
	mission.AcceptanceContractSHA256 = identity
	return DeepState{
		MissionName: mission.Name, Objective: mission.Objective, Tasks: mission.Tasks,
		AcceptanceContractVersion: mission.AcceptanceContractVersion, AcceptanceContractSHA256: mission.AcceptanceContractSHA256,
		LandingCommit: "landing-commit", LandingCheckpointTreeSHA: "landing-tree",
	}
}

func withFinalVerification(state DeepState, outcome VerificationOutcome, tree string) DeepState {
	state.Verify = "test -f result.txt"
	state.LandingVerifyDone = true
	state.LandingVerificationOutcome = outcome
	state.LandingVerificationSubject = &VerificationSubject{HeadCommit: "landing-head", TreeSHA: tree}
	state.LandingCheckpointTreeSHA = "landing-tree"
	if state.AcceptanceContractVersion != 0 {
		identity, err := AcceptanceContractIdentity(state.MissionDefinition())
		if err == nil {
			state.AcceptanceContractSHA256 = identity
		}
	}
	return state
}

func TestMissionOutcomeRoundTripAndLegacyStateRemainUnclaimed(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	mission := Mission{Name: "fixture", Objective: "ship", Tasks: []Task{{ID: "T-1", Objective: "work", Status: StatusQueued}}}
	state := NewState("20260926-120000", mission, "/repo", "/worktree", now.Add(time.Hour), now.Add(50*time.Minute), 2, now)
	if state.MissionOutcome != MissionOutcomePending {
		t.Fatalf("new session outcome = %q, want pending", state.MissionOutcome)
	}
	state.Phase = PhaseLanded
	state.MissionOutcome = MissionOutcomeFailed
	state.LandingVerificationOutcome = VerificationFailed
	if err := state.SaveDir(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(dir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MissionOutcome != MissionOutcomeFailed || loaded.LandingVerificationOutcome != VerificationFailed {
		t.Fatalf("typed mission outcome did not round-trip: %+v", loaded)
	}

	legacy := DeepState{SessionID: "legacy", Phase: PhaseLanded}
	if err := legacy.SaveDir(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(DeepDir(dir, "legacy"), "deep.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"missionOutcome"`) || strings.Contains(string(data), `"landingVerificationOutcome"`) {
		t.Fatalf("legacy state was given synthetic outcome fields: %s", data)
	}
	loadedLegacy, err := LoadState(dir, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if got := DisplayMissionOutcome(loadedLegacy.MissionOutcome, loadedLegacy.Phase); got != MissionOutcomeUnknown {
		t.Fatalf("legacy landed outcome display = %q, want unknown", got)
	}
}

func TestReopenAfterLandingPreservesOutcomeAndResetsCurrentOutcome(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	state := DeepState{
		Phase:                      PhaseLanded,
		MissionOutcome:             MissionOutcomeFailed,
		LandingVerificationOutcome: VerificationFailed,
		LandingReason:              "final verifier failed",
	}
	if !state.ReopenAfterLanding(now) {
		t.Fatal("ReopenAfterLanding() = false, want true")
	}
	if len(state.PreviousLandings) != 1 || state.PreviousLandings[0].MissionOutcome != MissionOutcomeFailed || state.PreviousLandings[0].VerificationOutcome != VerificationFailed {
		t.Fatalf("earlier landing lost its outcome: %+v", state.PreviousLandings)
	}
	if state.MissionOutcome != MissionOutcomePending || state.LandingVerificationOutcome != VerificationNotRun {
		t.Fatalf("reopened outcome state = %q/%q, want pending/not_run", state.MissionOutcome, state.LandingVerificationOutcome)
	}
}
