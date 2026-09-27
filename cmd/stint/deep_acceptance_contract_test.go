package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func TestAcceptanceContractPreflightValidatesAndChecksRuntimeTools(t *testing.T) {
	task := deep.Task{
		ID: "OBJ-1", Objective: "prove expected output", Source: "mission", RepositoryChange: deep.RepositoryChangeOptional,
		AcceptanceCheck: "git status --short",
	}
	mission := deep.Mission{
		Objective:                 "prove the objective",
		AcceptanceContractVersion: deep.DeterministicAcceptanceContractVersion,
		Tasks:                     []deep.Task{task},
	}
	identity, err := deep.AcceptanceContractIdentity(mission)
	if err != nil {
		t.Fatal(err)
	}
	mission.AcceptanceContractSHA256 = identity
	if err := validateMissionVerifyCommands(mission); err != nil {
		t.Fatalf("valid versioned contract rejected: %v", err)
	}
	if err := requireAcceptanceRuntimeSupport(mission); err != nil {
		t.Fatalf("valid versioned contract is not supported by the coordinator: %v", err)
	}
	var remoteCommands []string
	err = preflightRemoteVerifyTools(mission, func(_ context.Context, command string) (string, error) {
		remoteCommands = append(remoteCommands, command)
		return "", nil
	})
	if err != nil {
		t.Fatalf("remote preflight rejected a supported contract: %v", err)
	}
	if len(remoteCommands) != 1 || !strings.Contains(remoteCommands[0], "'git'") {
		t.Fatalf("remote preflight did not include the acceptance-check command: %v", remoteCommands)
	}
	if err := preflightLocalVerifyTools(mission); err != nil {
		t.Fatalf("local preflight rejected a valid acceptance-check tool: %v", err)
	}

	mission.Tasks[0].AcceptanceCheck = "git status --short --untracked-files=all"
	if err := validateMissionVerifyCommands(mission); err == nil {
		t.Fatal("stale persisted acceptance contract passed pure validation")
	}
	remoteCalls := 0
	err = preflightRemoteVerifyTools(mission, func(context.Context, string) (string, error) {
		remoteCalls++
		return "", nil
	})
	if err == nil || remoteCalls != 0 {
		t.Fatalf("invalid acceptance-check data reached remote tool lookup: err=%v remoteCalls=%d", err, remoteCalls)
	}
}
