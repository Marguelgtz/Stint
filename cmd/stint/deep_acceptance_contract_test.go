package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func TestAcceptanceContractPreflightFailsClosedUntilCoordinatorSupport(t *testing.T) {
	task := deep.Task{
		ID: "OBJ-1", Objective: "prove expected output", Source: "mission", RepositoryChange: deep.RepositoryChangeOptional,
		AcceptanceCheck: "test -e expected-output",
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
	if err := requireAcceptanceRuntimeSupport(mission); err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("versioned contract was not held behind the incomplete coordinator implementation: %v", err)
	}
	remoteCalled := false
	err = preflightRemoteVerifyTools(mission, func(context.Context, string) (string, error) {
		remoteCalled = true
		return "", nil
	})
	if err == nil || remoteCalled {
		t.Fatalf("remote preflight did not fail closed before tool lookup: err=%v remoteCalled=%t", err, remoteCalled)
	}
}
