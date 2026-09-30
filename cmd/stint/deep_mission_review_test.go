package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func newMissionSemanticReviewEnv(t *testing.T, missionVerify string) *testEnv {
	t.Helper()
	mission := deep.Mission{
		Name: "whole mission review fixture", Objective: "deliver the complete result",
		Success:     []string{"the public behavior is available", "all declared constraints are respected"},
		Constraints: []string{"stay inside the worktree"}, Verify: missionVerify,
		AcceptanceContractVersion:     deep.DeterministicAcceptanceContractVersion,
		SemanticReviewContractVersion: deep.SemanticReviewMissionContractVersion,
		Tasks: []deep.Task{{ID: "OBJ-1", Objective: "produce the requested behavior", Acceptance: "expose the complete public behavior",
			RepositoryChange: deep.RepositoryChangeRequired, AcceptanceCheck: "test -e work-1.txt", Status: deep.StatusQueued}},
	}
	env := newV2AcceptanceEnvForMission(t, mission)
	env.coord.verify = func(_ context.Context, command, _ string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationPassed, HasExitCode: true, ExitCode: 0,
			StartedAt: env.clock.now, CompletedAt: env.clock.now, Output: "fixture check passed"}
	}
	return env
}

func clearReviewResult() execResult {
	return execResult{exitCode: 0, completed: true, finishReason: "completed", outputText: framedReviewOutput(`{"outcome":"clear","findings":[]}`)}
}

func TestMissionSemanticReviewGatesCompletionOnExactLandingCheckpoint(t *testing.T) {
	env := newMissionSemanticReviewEnv(t, "")
	env.fake.script = map[int]execResult{2: clearReviewResult(), 3: clearReviewResult()}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run version-2 semantic mission: %v", err)
	}
	if env.state.Phase != deep.PhaseLanded || env.state.MissionOutcome != deep.MissionOutcomeSucceeded ||
		env.state.MissionReviewOutcome != deep.ReviewOutcomeClear || env.state.MissionReviewCycleID == "" || env.fake.calls != 3 {
		t.Fatalf("mission review did not gate the terminal outcome: phase=%s outcome=%s review=%s calls=%d",
			env.state.Phase, env.state.MissionOutcome, env.state.MissionReviewOutcome, env.fake.calls)
	}
	cycle, found, err := deep.LoadMissionReviewCycle(env.coord.stateDir, env.state.SessionID, env.state.MissionReviewCycleID)
	if err != nil || !found || cycle.Outcome != deep.ReviewOutcomeClear || cycle.CheckpointCommit != env.state.LandingCommit ||
		cycle.CheckpointTreeSHA != env.state.LandingCheckpointTreeSHA || cycle.ReviewSubject.HeadCommit != env.state.LandingCommit ||
		cycle.ReviewSubject.TreeSHA != env.state.LandingCheckpointTreeSHA {
		t.Fatalf("mission review checkpoint binding = %+v found=%t err=%v", cycle, found, err)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var started, completed, landed int
	for _, event := range events {
		switch event.Type {
		case deep.RunEventMissionReviewStarted:
			started++
		case deep.RunEventMissionReviewResult:
			completed++
		case deep.RunEventLanded:
			landed++
			if event.MissionReviewCycleID != cycle.ID || event.CheckpointCommit != cycle.CheckpointCommit || event.CheckpointTree != cycle.CheckpointTreeSHA {
				t.Fatalf("landing event does not identify reviewed checkpoint: %+v", event)
			}
		}
	}
	if started != 1 || completed != 1 || landed != 1 {
		t.Fatalf("mission review/landing journal facts = started %d completed %d landed %d", started, completed, landed)
	}
	missionInput := env.fake.inputs[2]
	if !missionInput.semanticReviewer || missionInput.allowedCommands != nil || missionInput.executorRunID != "" ||
		!strings.Contains(missionInput.prompt, "missionSuccess") || !strings.Contains(missionInput.prompt, "checkpointTreeSha") ||
		!strings.Contains(missionInput.prompt, "gitDiff") || !strings.Contains(missionInput.prompt, "accepted") {
		t.Fatalf("mission reviewer did not receive bounded whole-mission evidence in a fresh no-tools context: %+v", missionInput)
	}
}

func TestMissionSemanticFindingsLeaveMissionUnresolvedWithoutChangingTaskAcceptance(t *testing.T) {
	env := newMissionSemanticReviewEnv(t, "")
	finding := framedReviewOutput(`{"outcome":"findings","findings":[{"id":"M-1","severity":"high","summary":"A required public behavior is absent.","evidence":"The complete checkpoint diff does not define the requested exported entrypoint.","locations":["api.go:10"]}]}`)
	env.fake.script = map[int]execResult{2: clearReviewResult(), 3: {exitCode: 0, completed: true, finishReason: "completed", outputText: finding}}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("land mission with a bounded mission finding: %v", err)
	}
	if env.state.Phase != deep.PhaseLanded || env.state.MissionOutcome != deep.MissionOutcomeUnresolved ||
		env.state.Tasks[0].AcceptanceOutcome != deep.AcceptanceAccepted || env.state.Tasks[0].Status != deep.StatusAccepted ||
		env.state.MissionReviewOutcome != deep.ReviewOutcomeFindings || len(env.state.MissionReviewFindings) != 1 ||
		env.state.MissionReviewFindings[0].Disposition != deep.ReviewFindingOpen {
		t.Fatalf("mission findings changed task acceptance or produced success: phase=%s outcome=%s task=%+v mission-review=%+v",
			env.state.Phase, env.state.MissionOutcome, env.state.Tasks[0], env.state.MissionReviewFindings)
	}
	if env.state.LandingHandoff == "" || !strings.Contains(env.state.LandingHandoff, "## Mission semantic review") ||
		!strings.Contains(env.state.LandingHandoff, "finding M-1") {
		t.Fatalf("handoff omitted mission-review status: %s", env.state.LandingHandoff)
	}
}

func TestMissionReviewForSameCheckpointIsReusableAcrossResumeEpoch(t *testing.T) {
	env := newMissionSemanticReviewEnv(t, "")
	env.fake.script = map[int]execResult{2: clearReviewResult(), 3: clearReviewResult()}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("initial mission run: %v", err)
	}
	cycleID := env.state.MissionReviewCycleID
	firstWatermark := env.state.RunEventWatermark
	if err := deep.BeginResumeEpoch(env.coord.stateDir, env.state, deep.PhaseLanded, env.clock.now.Add(time.Minute)); err != nil {
		t.Fatalf("resume landed mission: %v", err)
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("resume mission on unchanged checkpoint: %v", err)
	}
	if env.state.MissionOutcome != deep.MissionOutcomeSucceeded || env.state.MissionReviewCycleID != cycleID || env.fake.calls != 3 ||
		env.state.RunEventWatermark <= firstWatermark {
		t.Fatalf("resume did not preserve exact-checkpoint review evidence and continuous history: outcome=%s review=%s calls=%d watermark=%d",
			env.state.MissionOutcome, env.state.MissionReviewCycleID, env.fake.calls, env.state.RunEventWatermark)
	}
}

func TestFailedFinalVerifierSkipsMissionReviewAndCannotCompleteMission(t *testing.T) {
	env := newMissionSemanticReviewEnv(t, "required-final-check")
	env.coord.finalVerify = func(_ context.Context, command string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationFailed, HasExitCode: true, ExitCode: 5, Output: "final mission assertion failed"}
	}
	env.fake.script = map[int]execResult{2: clearReviewResult()}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("land after failed required mission verifier: %v", err)
	}
	if env.state.MissionOutcome != deep.MissionOutcomeFailed || env.state.MissionReviewCycleID != "" ||
		env.fake.calls != 2 || env.state.Tasks[0].AcceptanceOutcome != deep.AcceptanceAccepted {
		t.Fatalf("final verifier failure was obscured by mission review: outcome=%s review=%q calls=%d task=%+v",
			env.state.MissionOutcome, env.state.MissionReviewCycleID, env.fake.calls, env.state.Tasks[0])
	}
}

func TestTimedOutFinalVerifierSkipsMissionReviewAndLeavesOutcomeUnresolved(t *testing.T) {
	env := newMissionSemanticReviewEnv(t, "required-final-check")
	env.coord.finalVerify = func(_ context.Context, command string) verificationResult {
		return verificationResult{Command: command, Outcome: verificationTimedOut,
			Error: "context deadline exceeded", Output: "final mission verifier timed out"}
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("land after timed-out required mission verifier: %v", err)
	}
	if env.state.Phase != deep.PhaseLanded || env.state.MissionOutcome != deep.MissionOutcomeUnresolved ||
		env.state.MissionReviewCycleID != "" || env.state.LandingVerificationOutcome != deep.VerificationTimedOut || env.fake.calls != 2 {
		t.Fatalf("timed-out final verifier was obscured by mission review: phase=%s outcome=%s review=%q verifier=%s calls=%d",
			env.state.Phase, env.state.MissionOutcome, env.state.MissionReviewCycleID, env.state.LandingVerificationOutcome, env.fake.calls)
	}
}

func TestMutationDuringMissionReviewCannotCompleteLanding(t *testing.T) {
	env := newMissionSemanticReviewEnv(t, "")
	env.fake.script = map[int]execResult{2: clearReviewResult(), 3: clearReviewResult()}
	env.fake.before = func(in execInput) {
		if in.semanticReviewer && env.fake.calls == 3 {
			if err := os.WriteFile(filepath.Join(env.wt, "late-mission-review-write.txt"), []byte("changed"), 0o644); err != nil {
				t.Errorf("write concurrent product mutation: %v", err)
			}
		}
	}
	if err := env.coord.run(context.Background()); err == nil {
		t.Fatal("landing accepted a product-tree mutation during the mission review")
	}
	if env.state.Phase != deep.PhaseLanding || env.state.MissionOutcome != deep.MissionOutcomePending || env.state.LandingCommit != "" {
		t.Fatalf("stale mission review produced terminal completion: phase=%s outcome=%s commit=%q", env.state.Phase, env.state.MissionOutcome, env.state.LandingCommit)
	}
	if env.state.MissionReviewOutcome != deep.ReviewOutcomeClear || env.state.MissionReviewCheckpointTreeSHA == "" {
		t.Fatalf("the durable review should remain evidence only for its old exact checkpoint: %+v", env.state)
	}
}
