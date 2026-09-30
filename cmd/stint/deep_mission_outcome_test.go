package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func TestDeepLandingFailurePreservesAcceptedTaskAndRecordsMissionFailure(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	env.state.Verify = "final-check"
	env.state.Tasks[0].Verify = "task-check"
	if err := env.state.SaveDir(env.coord.stateDir); err != nil {
		t.Fatal(err)
	}
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0}
	}
	if err := env.coord.runTask(context.Background(), 0, env.clock.now); err != nil {
		t.Fatalf("accept task before mission final verifier: %v", err)
	}
	acceptedTask := env.state.Tasks[0]
	if acceptedTask.Status != deep.StatusVerified {
		t.Fatalf("task status before final verifier = %s, want verified", acceptedTask.Status)
	}
	env.coord.finalVerify = func(context.Context, string) verificationResult {
		return verificationResult{Outcome: verificationFailed, HasExitCode: true, ExitCode: 7, Output: "final assertion failed"}
	}
	var output bytes.Buffer
	env.coord.out = &output
	if err := env.coord.land(context.Background(), "finished work"); err != nil {
		t.Fatalf("land after failed mission verifier: %v", err)
	}
	if env.state.Phase != deep.PhaseLanded || env.state.MissionOutcome != deep.MissionOutcomeFailed || env.state.LandingVerificationOutcome != deep.VerificationFailed {
		t.Fatalf("landing phase/outcome/verifier = %s/%s/%s", env.state.Phase, env.state.MissionOutcome, env.state.LandingVerificationOutcome)
	}
	if env.state.Tasks[0].Status != deep.StatusVerified || env.state.Tasks[0].CheckpointTreeSHA != acceptedTask.CheckpointTreeSHA || env.state.Tasks[0].CheckpointCommit != acceptedTask.CheckpointCommit {
		t.Fatalf("final mission verifier changed previously accepted task evidence: before=%+v after=%+v", acceptedTask, env.state.Tasks[0])
	}
	if !strings.Contains(output.String(), "mission outcome: failed") {
		t.Fatalf("landing output omitted failed mission outcome: %q", output.String())
	}
	handoff, err := os.ReadFile(env.state.HandoffPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(handoff), "| Phase | landed (finished work) |") || !strings.Contains(string(handoff), "| Mission outcome | failed |") || !strings.Contains(string(handoff), "final assertion failed") {
		t.Fatalf("handoff did not distinguish operational landing from mission failure:\n%s", handoff)
	}
	fresh, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.MissionOutcome != deep.MissionOutcomeFailed || fresh.LandingVerificationOutcome != deep.VerificationFailed {
		t.Fatalf("mission failure outcome was not durable: %+v", fresh)
	}
}

func TestDeepLandingGreenFinalVerifierCannotCompleteUnacceptedTasks(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	env.state.Tasks[0].Status = deep.StatusBlocked
	env.state.Verify = "generic green check"
	if err := env.state.SaveDir(env.coord.stateDir); err != nil {
		t.Fatal(err)
	}
	env.coord.finalVerify = func(context.Context, string) verificationResult {
		return verificationResult{Outcome: verificationPassed}
	}
	if err := env.coord.land(context.Background(), "no safe work remains"); err != nil {
		t.Fatalf("land: %v", err)
	}
	if env.state.MissionOutcome != deep.MissionOutcomeIncomplete || env.state.Tasks[0].Status != deep.StatusBlocked {
		t.Fatalf("green final verifier overrode incomplete task: outcome=%s task=%s", env.state.MissionOutcome, env.state.Tasks[0].Status)
	}
}

func TestDeepLandingRerunsA2FinalResultWithoutTypedOutcome(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Phase = deep.PhaseLanding
	env.state.LandingVerifyDone = true
	env.state.LandingVerify = "passed (A2 result)"
	snapshot, err := env.coord.git.verificationSubject(env.wt, env.coord.verificationBookkeepingPaths())
	if err != nil {
		t.Fatal(err)
	}
	env.state.LandingVerificationSubject = &snapshot.Subject
	env.state.LandingVerificationBookkeeping = landingVerificationBookkeeping(snapshot.Bookkeeping)
	if err := env.state.SaveDir(env.coord.stateDir); err != nil {
		t.Fatal(err)
	}
	verifyCalls := 0
	env.coord.finalVerify = func(context.Context, string) verificationResult {
		verifyCalls++
		return verificationResult{Outcome: verificationPassed}
	}
	if err := env.coord.land(context.Background(), "resume A2 landing"); err != nil {
		t.Fatalf("land: %v", err)
	}
	if verifyCalls != 1 || env.state.LandingVerificationOutcome != deep.VerificationPassed {
		t.Fatalf("A2 summary was reused without typed outcome: calls=%d outcome=%q", verifyCalls, env.state.LandingVerificationOutcome)
	}
}

func TestDeepRunSessionReturnsFailureWhenMissionVerifierFails(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	env.state.Tasks[0].Verify = "test -f result.txt"
	env.state.Verify = "false"
	now := time.Now().UTC()
	env.state.StartedAt = now
	env.state.Deadline = now.Add(2 * time.Hour)
	env.state.LandBefore = now.Add(90 * time.Minute)
	if err := env.state.SaveDir(env.coord.stateDir); err != nil {
		t.Fatal(err)
	}
	hermesDir := t.TempDir()
	hermesPath := filepath.Join(hermesDir, "hermes")
	if err := os.WriteFile(hermesPath, []byte("#!/bin/sh\nprintf implementation > result.txt\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", hermesDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	err := deepRunSession(env.coord.stateDir, env.state, &deepRunConfig{
		worker:      workerHermesOnBox,
		taskTimeout: time.Minute,
		missionName: env.state.MissionName,
		taskCount:   1,
	}, env.coord.git, false)
	if err == nil || !strings.Contains(err.Error(), "mission failed its required final verification") {
		t.Fatalf("deepRunSession error = %v, want explicit failed mission result", err)
	}
	if env.state.Phase != deep.PhaseLanded || env.state.MissionOutcome != deep.MissionOutcomeFailed || env.state.Tasks[0].Status != deep.StatusVerified {
		t.Fatalf("failed mission terminal state = phase %s outcome %s task %s", env.state.Phase, env.state.MissionOutcome, env.state.Tasks[0].Status)
	}
}
