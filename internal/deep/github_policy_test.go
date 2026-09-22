package deep

import (
	"testing"
	"time"
)

func TestMissionParsesPersistedGitHubPolicy(t *testing.T) {
	mission, err := ParseMission(`# Publisher policy

## Objective
Keep the test mission valid.

## Tasks
- [ ] T-001: Verify the policy is stored.

## GitHub
- mode: engineering
- repository: Marguelgtz/Stint
- base: main
- allowed-authors: alice, bob
- approval: internal
`)
	if err != nil {
		t.Fatalf("ParseMission: %v", err)
	}
	if !mission.GitHubConfigured {
		t.Fatal("GitHub section was not recorded as explicitly configured")
	}
	want := GitHubPolicy{
		Mode: GitHubEngineering, Repository: "Marguelgtz/Stint", Base: "main",
		AllowedAuthors: []string{"alice", "bob"}, Approval: ApprovalInternal,
	}
	if !SameGitHubPolicy(mission.GitHub, want) {
		t.Fatalf("GitHub policy = %+v, want %+v", mission.GitHub, want)
	}
}

func TestGitHubPolicyRejectsIncompleteOrUnsafeValues(t *testing.T) {
	tests := []GitHubPolicy{
		{Mode: GitHubEngineering, Repository: "Marguelgtz/Stint", Approval: ApprovalInternal},
		{Mode: GitHubMaintenance, Repository: "Marguelgtz/Stint", Base: "main", Approval: ApprovalGitHub},
		{Mode: GitHubEngineering, Repository: "Marguelgtz/Stint", Base: "main", Approval: "unknown"},
		{Mode: GitHubEngineering, Repository: "owner/repo", Base: "main", AllowedAuthors: []string{"bad/name"}, Approval: ApprovalInternal},
		{Mode: GitHubNone, Repository: "owner/repo", Approval: ApprovalInternal},
	}
	for i, policy := range tests {
		if err := policy.Validate(); err == nil {
			t.Errorf("policy %d unexpectedly validated: %+v", i, policy)
		}
	}
	_, err := ParseMission(`# Duplicate policy

## Objective
Keep the mission valid.

## Tasks
- [ ] T-001: Check duplicate policy values.

## GitHub
- mode: engineering
- mode: none
- repository: owner/repository
- base: main
`)
	if err == nil {
		t.Fatal("duplicate GitHub mode was silently accepted")
	}
}

func TestSameGitHubPolicyComparesEveryFieldAndAuthorSet(t *testing.T) {
	a := GitHubPolicy{Mode: GitHubEngineering, Repository: "owner/repo", Base: "main", AllowedAuthors: []string{"alice", "bob"}, Approval: ApprovalInternal}
	b := GitHubPolicy{Mode: " engineering ", Repository: "owner/repo", Base: "main", AllowedAuthors: []string{"bob", "alice"}, Approval: "internal"}
	if !SameGitHubPolicy(a, b) {
		t.Fatal("author ordering or harmless whitespace changed policy identity")
	}
	b.Base = "release"
	if SameGitHubPolicy(a, b) {
		t.Fatal("base mismatch was not detected")
	}
	b = a
	b.Approval = ApprovalGitHub
	if SameGitHubPolicy(a, b) {
		t.Fatal("approval mismatch was not detected")
	}
}

func TestNewStateRoundTripsGitHubPolicy(t *testing.T) {
	mission, err := ParseMission(`# Policy persistence

## Objective
Keep the test mission valid.

## Tasks
- [ ] T-001: Verify policy persistence.

## GitHub
- mode: engineering
- repository: owner/repository
- base: release
- approval: internal
`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state := NewState("20260923-120000", mission, "/repo", "/repo/.stint-deep/session", now.Add(time.Hour), now.Add(50*time.Minute), 2, now)
	stateDir := t.TempDir()
	if err := state.SaveDir(stateDir); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !SameGitHubPolicy(loaded.GitHub, mission.GitHub) {
		t.Fatalf("persisted GitHub policy = %+v, want %+v", loaded.GitHub, mission.GitHub)
	}
}
