package deep

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
	"time"
)

func missionReviewFixture(t *testing.T) (string, DeepState, MissionReviewCycle, time.Time) {
	t.Helper()
	stateDir, state, acceptance, now := acceptanceRunFixtureWithReviewVersion(t, SemanticReviewMissionContractVersion, "go test ./...")
	acceptance = successfulAcceptanceRun(acceptance, now, TaskCheckpointSubject(acceptance.Checkpoint))
	if err := CompleteAcceptanceRun(stateDir, &state, acceptance); err != nil {
		t.Fatalf("complete deterministic acceptance: %v", err)
	}
	hash := sha256.Sum256([]byte("Objective review packet"))
	reviewID, err := NewReviewCycleID()
	if err != nil {
		t.Fatal(err)
	}
	objectiveReview := ReviewCycle{
		ID: reviewID, TaskID: acceptance.TaskID, Attempt: acceptance.Attempt,
		PolicySHA256: state.SemanticReviewContractSHA256, ContextSHA256: hex.EncodeToString(hash[:]),
		CheckpointEventID: acceptance.CheckpointEventID, Checkpoint: acceptance.Checkpoint,
		Reviewer: "hermes-readonly-v1", Provider: "fixture", Model: "review-model",
		StartedAt: now.Add(40 * time.Second),
	}
	objectiveReview, err = BeginReviewCycle(stateDir, &state, objectiveReview)
	if err != nil {
		t.Fatalf("begin Objective review: %v", err)
	}
	objectiveReview.EndedAt = now.Add(55 * time.Second)
	objectiveReview.DurationMilliseconds = objectiveReview.EndedAt.Sub(objectiveReview.StartedAt).Milliseconds()
	objectiveReview.Outcome = ReviewOutcomeClear
	if err := CompleteReviewCycle(stateDir, &state, objectiveReview); err != nil {
		t.Fatalf("complete Objective review: %v", err)
	}
	if err := BeginLanding(stateDir, &state, "test landing", now.Add(56*time.Second)); err != nil {
		t.Fatal(err)
	}
	verificationID, err := NewVerificationRunID()
	if err != nil {
		t.Fatal(err)
	}
	verifiedSubject := VerificationSubject{HeadCommit: "landing-head", TreeSHA: "landing-tree"}
	verification := VerificationRun{
		ID: verificationID, Purpose: VerificationPurposeMissionEnd, CommandSource: "mission",
		CommandSHA256: VerificationCommandIdentity(state.Verify),
		Runtime:       VerificationRuntime{Worker: "hermes-onbox", Location: "compute", Shell: "sh", Protocol: "local-process-group-v1"},
		StartedAt:     now.Add(57 * time.Second), TimeoutSeconds: 180, Subject: verifiedSubject,
	}
	verification, err = BeginVerificationRun(stateDir, &state, verification)
	if err != nil {
		t.Fatalf("begin final mission verification: %v", err)
	}
	verification.Outcome = VerificationPassed
	verification.EndedAt = now.Add(67 * time.Second)
	verification.HasExitCode = true
	verification.ExitCode = 0
	verification.SubjectAfter = &verifiedSubject
	verification.DurationMilliseconds = 10_000
	if err := CompleteVerificationRun(stateDir, &state, verification); err != nil {
		t.Fatalf("complete final mission verification: %v", err)
	}
	now = now.Add(68 * time.Second)
	hash = sha256.Sum256([]byte("mission packet"))
	id, err := NewReviewCycleID()
	if err != nil {
		t.Fatal(err)
	}
	cycle := MissionReviewCycle{
		ID: id, PolicySHA256: state.SemanticReviewContractSHA256,
		ContextSHA256: hex.EncodeToString(hash[:]), CheckpointCommit: "landing-commit", CheckpointTreeSHA: "landing-tree",
		ReviewSubject:            VerificationSubject{HeadCommit: "landing-commit", TreeSHA: "landing-tree"},
		FinalVerificationSubject: &verifiedSubject, VerificationRunID: verification.ID,
		Reviewer: "hermes-safe-no-tools-v1", Provider: "fixture", Model: "review-model",
		StartedAt: now.Add(2 * time.Second), TimeoutMilliseconds: 30_000, RemainingDeadlineMilliseconds: 600_000,
	}
	return stateDir, state, cycle, now
}

func completedMissionReview(cycle MissionReviewCycle, at time.Time, outcome ReviewOutcome) MissionReviewCycle {
	cycle.EndedAt = at.UTC()
	cycle.DurationMilliseconds = max(0, cycle.EndedAt.Sub(cycle.StartedAt).Milliseconds())
	cycle.Outcome = outcome
	if outcome == ReviewOutcomeUnresolved || outcome == ReviewOutcomeExecutionError || outcome == ReviewOutcomeTimedOut ||
		outcome == ReviewOutcomeCanceled || outcome == ReviewOutcomeUnknown {
		cycle.Reason = "review did not establish a clear mission result"
	}
	if outcome == ReviewOutcomeFindings {
		cycle.Findings = []ReviewFinding{{
			ID: "F-1", Severity: ReviewSeverityHigh, Summary: "The overall result is incomplete.",
			Evidence: "A declared mission success criterion is absent from the checkpoint.", Disposition: ReviewFindingOpen,
		}}
	}
	return cycle
}

func TestMissionReviewCycleIsJournaledAndBoundToLandingCheckpoint(t *testing.T) {
	stateDir, state, cycle, now := missionReviewFixture(t)
	started, err := BeginMissionReviewCycle(stateDir, &state, cycle)
	if err != nil {
		t.Fatalf("begin mission review: %v", err)
	}
	if state.MissionReviewOutcome != ReviewOutcomeStarted || state.MissionReviewCheckpointCommit != "landing-commit" {
		t.Fatalf("mission review start projection = %+v", state)
	}
	completed := completedMissionReview(started, now.Add(15*time.Second), ReviewOutcomeClear)
	if err := CompleteMissionReviewCycle(stateDir, &state, completed); err != nil {
		t.Fatalf("complete mission review: %v", err)
	}
	loaded, found, err := LoadMissionReviewCycle(stateDir, state.SessionID, cycle.ID)
	if err != nil || !found || !reflect.DeepEqual(loaded, completed) {
		t.Fatalf("loaded mission cycle = found %t, cycle %+v, err %v", found, loaded, err)
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 2 || events[len(events)-2].Type != RunEventMissionReviewStarted ||
		events[len(events)-1].Type != RunEventMissionReviewResult || events[len(events)-1].Sequence != uint64(len(events)) ||
		events[len(events)-1].MissionReview == nil || events[len(events)-1].MissionReview.CheckpointTreeSHA != "landing-tree" {
		t.Fatalf("mission review events = %+v", events)
	}
	fresh, err := LoadState(stateDir, state.SessionID)
	if err != nil || fresh.MissionReviewCycleID != cycle.ID || fresh.MissionReviewOutcome != ReviewOutcomeClear ||
		fresh.MissionReviewSubject == nil || fresh.MissionReviewSubject.HeadCommit != "landing-commit" {
		t.Fatalf("mission-review projection after reload = %+v, err %v", fresh, err)
	}
	forged := fresh
	forged.MissionReviewOutcome = ReviewOutcomeFindings
	if err := forged.SaveDir(stateDir); err == nil {
		t.Fatal("deep.json allowed a non-journaled mission-review outcome")
	}
}

func TestMissionReviewCannotStartBeforeObjectiveAcceptanceAndReview(t *testing.T) {
	stateDir, state, cycle, _ := missionReviewFixture(t)
	watermark := state.RunEventWatermark
	state.Tasks = cloneTasksForEventProjection(state.Tasks)
	state.Tasks[0].Status = StatusVerified
	state.Tasks[0].AcceptanceOutcome = AcceptanceUnresolved
	if _, err := BeginMissionReviewCycle(stateDir, &state, cycle); err == nil {
		t.Fatal("mission review started before all Objective acceptance and review gates were satisfied")
	}
	if state.MissionReviewCycleID != "" || state.RunEventWatermark != watermark {
		t.Fatalf("rejected mission-review start changed durable state: id=%q watermark=%d, want %d",
			state.MissionReviewCycleID, state.RunEventWatermark, watermark)
	}
}

func TestMissionReviewMustReferenceTheExactFinalVerifierRunAndSubject(t *testing.T) {
	stateDir, state, cycle, _ := missionReviewFixture(t)
	tests := []struct {
		name   string
		mutate func(*MissionReviewCycle)
	}{
		{name: "different run", mutate: func(cycle *MissionReviewCycle) { cycle.VerificationRunID = "ffffffffffffffffffffffffffffffff" }},
		{name: "different subject", mutate: func(cycle *MissionReviewCycle) {
			subject := *cycle.FinalVerificationSubject
			subject.HeadCommit = "another-head"
			cycle.FinalVerificationSubject = &subject
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			candidate := cycle
			if cycle.FinalVerificationSubject != nil {
				subject := *cycle.FinalVerificationSubject
				candidate.FinalVerificationSubject = &subject
			}
			tc.mutate(&candidate)
			if _, err := BeginMissionReviewCycle(stateDir, &state, candidate); err == nil {
				t.Fatal("mission review started without the exact passed final-verifier provenance")
			}
		})
	}
}

func TestMissionReviewProjectionReplaysAfterJournalAppendBeforeProjection(t *testing.T) {
	stateDir, state, cycle, now := missionReviewFixture(t)
	started, err := BeginMissionReviewCycle(stateDir, &state, cycle)
	if err != nil {
		t.Fatal(err)
	}
	completed := completedMissionReview(started, now.Add(20*time.Second), ReviewOutcomeClear)
	watermarkBeforeResult := state.RunEventWatermark
	event := RunEvent{
		EventID: missionReviewEventID(completed.ID, "result"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: completed.EndedAt, Actor: "deep-mission-reviewer", Type: RunEventMissionReviewResult,
		FromPhase: PhaseLanding, ToPhase: PhaseLanding, MissionReview: &completed,
		TaskSummary: summarizeRunTasks(state.Tasks),
	}
	writeFailure := errors.New("simulated projection storage failure")
	err = appendAndProjectRunEvent(stateDir, &state, event, func(string, *DeepState) error { return writeFailure })
	if !errors.Is(err, writeFailure) {
		t.Fatalf("append with projection failure = %v", err)
	}
	fresh, err := LoadState(stateDir, state.SessionID)
	if err != nil || fresh.RunEventWatermark != watermarkBeforeResult+1 || fresh.MissionReviewOutcome != ReviewOutcomeClear || fresh.MissionReviewCycleID != cycle.ID {
		t.Fatalf("journal replay did not recover mission review exactly once: watermark=%d outcome=%s id=%q err=%v",
			fresh.RunEventWatermark, fresh.MissionReviewOutcome, fresh.MissionReviewCycleID, err)
	}
	if err := CompleteMissionReviewCycle(stateDir, &fresh, completed); err != nil {
		t.Fatalf("idempotent repeated result append: %v", err)
	}
	again, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || uint64(len(again)) != watermarkBeforeResult+1 {
		t.Fatalf("repeated mission result duplicated the event: len=%d err=%v", len(again), err)
	}
}

func TestUnmatchedMissionReviewRecoveryPreservesHardQuiescenceBlock(t *testing.T) {
	stateDir, state, cycle, now := missionReviewFixture(t)
	if _, err := BeginMissionReviewCycle(stateDir, &state, cycle); err != nil {
		t.Fatal(err)
	}
	if err := BeginResumeEpoch(stateDir, &state, PhaseLanding, now.Add(time.Minute)); err != nil {
		t.Fatalf("begin resume epoch: %v", err)
	}
	recovered, err := RecoverUnmatchedMissionReviewCycle(stateDir, &state, now.Add(time.Minute+time.Second))
	if err != nil || recovered == nil || recovered.Outcome != ReviewOutcomeUnknown || !recovered.QuiescenceUnconfirmed {
		t.Fatalf("unmatched mission review recovery = %+v, err %v", recovered, err)
	}
	if !state.ExecutionQuiescenceUnconfirmed || state.ExecutionQuiescenceTaskID != "mission-semantic-review" ||
		state.MissionReviewOutcome != ReviewOutcomeUnknown {
		t.Fatalf("recovery did not fail closed: %+v", state)
	}
	if _, err := LoadState(stateDir, state.SessionID); err != nil {
		t.Fatalf("recovered mission review projection does not replay: %v", err)
	}
}

func TestMissionReviewCheckpointMismatchCannotCompleteLandingSuccessfully(t *testing.T) {
	stateDir, state, cycle, now := missionReviewFixture(t)
	started, err := BeginMissionReviewCycle(stateDir, &state, cycle)
	if err != nil {
		t.Fatal(err)
	}
	completed := completedMissionReview(started, now.Add(15*time.Second), ReviewOutcomeClear)
	if err := CompleteMissionReviewCycle(stateDir, &state, completed); err != nil {
		t.Fatal(err)
	}
	if got := missionReviewProjectionMatchesCheckpoint(state, cycle.ID, "other-commit", "other-tree"); got {
		t.Fatal("mission review was accepted for a different landing checkpoint")
	}
	if got := missionReviewProjectionMatchesCheckpoint(state, cycle.ID, cycle.CheckpointCommit, "other-tree"); got {
		t.Fatal("mission review was accepted for a different checkpoint tree")
	}
}
