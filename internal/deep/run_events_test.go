package deep

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func journalFixture(t *testing.T) (string, DeepState, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 26, 14, 0, 0, 0, time.UTC)
	mission := Mission{Name: "journal fixture", Objective: "record lifecycle", Tasks: []Task{{ID: "T-1", Objective: "work", Status: StatusQueued}}}
	state := NewState("run-20260926", mission, "/repo", "/repo/.stint-deep/run-20260926", now.Add(time.Hour), now.Add(50*time.Minute), 2, now)
	state.ComputeBinding = &ComputeBinding{Provider: "vast", InstanceID: 1234, BoundAt: now}
	return t.TempDir(), state, now
}

func TestRunJournalSequencesLifecycleAcrossResumeEpochs(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatalf("BeginNewRun: %v", err)
	}
	runID, firstEpoch := state.RunID, state.ExecutionEpochID
	if runID != state.SessionID || state.RunEventWatermark != 1 || state.Phase != PhaseExecuting {
		t.Fatalf("new run projection = run %q epoch %q seq %d phase %q", runID, firstEpoch, state.RunEventWatermark, state.Phase)
	}
	if err := BeginLanding(stateDir, &state, "finished useful work", now.Add(time.Minute)); err != nil {
		t.Fatalf("BeginLanding: %v", err)
	}
	if err := CompleteLanding(stateDir, &state, "checkpoint-a", "tree-a", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("CompleteLanding: %v", err)
	}
	if state.Phase != PhaseLanded || state.RunEventWatermark != 3 || state.MissionOutcome != MissionOutcomeIncomplete {
		t.Fatalf("landed projection = phase %q seq %d outcome %q", state.Phase, state.RunEventWatermark, state.MissionOutcome)
	}
	if err := BeginResumeEpoch(stateDir, &state, PhaseLanded, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("BeginResumeEpoch: %v", err)
	}
	if state.RunID != runID || state.ExecutionEpochID == firstEpoch || state.RunEventWatermark != 4 || state.Phase != PhaseExecuting {
		t.Fatalf("resumed projection = run %q epoch %q seq %d phase %q", state.RunID, state.ExecutionEpochID, state.RunEventWatermark, state.Phase)
	}
	if len(state.PreviousLandings) != 1 || state.PreviousLandings[0].MissionOutcome != MissionOutcomeIncomplete {
		t.Fatalf("resume lost previous landing: %+v", state.PreviousLandings)
	}

	events, exists, err := readRunEventsLocked(DeepDir(stateDir, state.SessionID), state.SessionID)
	if err != nil || !exists {
		t.Fatalf("read journal: exists=%t err=%v", exists, err)
	}
	if len(events) != 4 {
		t.Fatalf("event count = %d, want 4: %+v", len(events), events)
	}
	for i, event := range events {
		if event.Sequence != uint64(i+1) || event.RunID != runID {
			t.Fatalf("event %d identity/sequence = %s/%d", i, event.RunID, event.Sequence)
		}
	}
	if events[0].EpochID != firstEpoch || events[3].EpochID == firstEpoch || events[3].Boundary != RunEventBoundaryResume {
		t.Fatalf("resume event did not create a new epoch in the same run: first=%+v resumed=%+v", events[0], events[3])
	}
	if events[0].ComputeProvider != "vast" || events[0].ComputeInstance != 1234 || events[3].ComputeProvider != "vast" || events[3].ComputeInstance != 1234 {
		t.Fatalf("epoch events did not preserve bounded compute identity: start=%+v resume=%+v", events[0], events[3])
	}
	if events[0].TaskSummary == nil || events[0].TaskSummary.Total != 1 || events[0].TaskSummary.Queued != 1 ||
		events[2].TaskSummary == nil || events[2].TaskSummary.Queued != 1 {
		t.Fatalf("lifecycle events did not preserve bounded task state: start=%+v landing=%+v", events[0], events[2])
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil || loaded.RunEventWatermark != 4 || loaded.ExecutionEpochID != state.ExecutionEpochID {
		t.Fatalf("LoadState after resume = watermark %d epoch %q err %v", loaded.RunEventWatermark, loaded.ExecutionEpochID, err)
	}
}

func TestRunJournalComparesEarlierVerificationSubjectByValue(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	state.Verify = "final-check"
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	if err := BeginLanding(stateDir, &state, "first landing", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	state.LandingVerifyDone = true
	state.LandingVerificationOutcome = VerificationPassed
	state.LandingVerificationSubject = &VerificationSubject{HeadCommit: "verified-head", TreeSHA: "verified-tree"}
	if err := state.SaveDir(stateDir); err != nil {
		t.Fatal(err)
	}
	if err := CompleteLanding(stateDir, &state, "verified-checkpoint", "verified-tree", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := BeginResumeEpoch(stateDir, &state, PhaseLanded, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// JSON round-tripping gives the previous landing's VerificationSubject a
	// different pointer. Durable comparisons must compare its value so later
	// projection writes and lifecycle transitions remain possible.
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.PreviousLandings) != 1 || loaded.PreviousLandings[0].VerificationSubject == nil {
		t.Fatalf("resume did not preserve verification provenance: %+v", loaded.PreviousLandings)
	}
	if err := loaded.SaveDir(stateDir); err != nil {
		t.Fatalf("save state after resuming a landing with subject provenance: %v", err)
	}
	if err := BeginLanding(stateDir, &loaded, "second landing", now.Add(4*time.Minute)); err != nil {
		t.Fatalf("begin second landing: %v", err)
	}
}

func TestBeginNewRunRecoversPreparedBoundaryAndRefusesLegacyOverwrite(t *testing.T) {
	t.Run("resume startup after prepared projection", func(t *testing.T) {
		stateDir, state, now := journalFixture(t)
		epoch, err := NewExecutionEpochID()
		if err != nil {
			t.Fatal(err)
		}
		state.RunID = state.SessionID
		state.ExecutionEpochID = epoch
		state.RunEventSchemaVersion = RunEventSchemaVersion
		state.Phase = PhaseInitializing
		if err := state.SaveDir(stateDir); err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadState(stateDir, state.SessionID)
		if err != nil || loaded.Phase != PhaseInitializing || loaded.RunEventWatermark != 0 {
			t.Fatalf("prepared startup state = phase %q watermark %d err %v", loaded.Phase, loaded.RunEventWatermark, err)
		}
		if err := BeginNewRun(stateDir, &loaded, now); err != nil {
			t.Fatalf("complete prepared startup: %v", err)
		}
		if loaded.Phase != PhaseExecuting || loaded.RunEventWatermark != 1 || loaded.ExecutionEpochID != epoch {
			t.Fatalf("started state = phase %q watermark %d epoch %q", loaded.Phase, loaded.RunEventWatermark, loaded.ExecutionEpochID)
		}
	})

	t.Run("do not replace an existing legacy run", func(t *testing.T) {
		stateDir, state, now := journalFixture(t)
		if err := state.SaveDir(stateDir); err != nil {
			t.Fatal(err)
		}
		if err := BeginNewRun(stateDir, &state, now); err == nil || !strings.Contains(err.Error(), "session state already exists") {
			t.Fatalf("BeginNewRun over existing legacy state = %v, want refusal", err)
		}
		loaded, err := LoadState(stateDir, state.SessionID)
		if err != nil || loaded.RunID != "" || loaded.RunEventWatermark != 0 || loaded.Phase != PhaseExecuting {
			t.Fatalf("legacy state changed during refused new-run start: %+v err=%v", loaded, err)
		}
	})
}

func TestRunJournalLegacyResumeStartsAtExplicitBoundary(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	state.Phase = PhaseLanded
	state.LandingCommit = "legacy-checkpoint"
	state.LandingCheckpointTreeSHA = "legacy-tree"
	state.MissionOutcome = MissionOutcomeUnknown
	if err := state.SaveDir(stateDir); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil || loaded.RunID != "" || loaded.RunEventWatermark != 0 {
		t.Fatalf("legacy state was assigned synthetic history: %+v err=%v", loaded, err)
	}
	if err := BeginResumeEpoch(stateDir, &loaded, PhaseLanded, now.Add(time.Minute)); err != nil {
		t.Fatalf("begin first journal-aware epoch: %v", err)
	}
	events, _, err := readRunEventsLocked(DeepDir(stateDir, state.SessionID), state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Sequence != 1 || events[0].Boundary != RunEventBoundaryLegacyResume {
		t.Fatalf("legacy journal boundary = %+v, want exactly one seq-1 legacy resume event", events)
	}
	if loaded.RunID != loaded.SessionID || loaded.RunEventWatermark != 1 || loaded.Phase != PhaseExecuting || len(loaded.PreviousLandings) != 1 {
		t.Fatalf("legacy resume projection = %+v", loaded)
	}
}

func TestRunJournalLegacyActiveResumeAddsCurrentOutcomeWithoutHistory(t *testing.T) {
	for _, phase := range []Phase{PhaseExecuting, PhaseLanding} {
		t.Run(string(phase), func(t *testing.T) {
			stateDir, state, now := journalFixture(t)
			state.Phase = phase
			state.MissionOutcome = ""
			if phase == PhaseLanding {
				state.LandingReason = "legacy interrupted landing"
			}
			if err := state.SaveDir(stateDir); err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadState(stateDir, state.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			if err := BeginResumeEpoch(stateDir, &loaded, phase, now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			recovered, err := LoadState(stateDir, state.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			if recovered.MissionOutcome != MissionOutcomePending || recovered.RunEventWatermark != 1 {
				t.Fatalf("legacy resume projection = outcome %q phase %q watermark %d", recovered.MissionOutcome, recovered.Phase, recovered.RunEventWatermark)
			}
			events, _, err := readRunEventsLocked(DeepDir(stateDir, state.SessionID), state.SessionID)
			if err != nil || len(events) != 1 || events[0].Boundary != RunEventBoundaryLegacyResume {
				t.Fatalf("legacy resume synthesized history: events=%+v err=%v", events, err)
			}
		})
	}
}

func TestRunJournalReplaysEventWhenProjectionPersistenceFails(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	event := RunEvent{
		EventID:     landingEventID(state.RunID, state.ExecutionEpochID, "started"),
		RunID:       state.RunID,
		EpochID:     state.ExecutionEpochID,
		OccurredAt:  now.Add(time.Minute),
		Actor:       "deep-coordinator",
		Type:        RunEventLandingStarted,
		FromPhase:   PhaseExecuting,
		ToPhase:     PhaseLanding,
		Reason:      "crash-boundary fixture",
		TaskSummary: summarizeRunTasks(state.Tasks),
	}
	err := appendAndProjectRunEvent(stateDir, &state, event, func(string, DeepState) error {
		return errors.New("injected projection persistence failure")
	})
	if err == nil || !strings.Contains(err.Error(), "is durable but deep.json projection update failed") {
		t.Fatalf("event/projection failure = %v, want explicit durable-event recovery error", err)
	}
	if state.Phase != PhaseExecuting || state.RunEventWatermark != 1 {
		t.Fatalf("failed transition mutated caller projection: phase=%q watermark=%d", state.Phase, state.RunEventWatermark)
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil {
		t.Fatalf("replay after projection failure: %v", err)
	}
	if loaded.Phase != PhaseLanding || loaded.RunEventWatermark != 2 || loaded.LandingReason != event.Reason {
		t.Fatalf("recovered projection = phase %q watermark %d reason %q", loaded.Phase, loaded.RunEventWatermark, loaded.LandingReason)
	}
	if _, err := LoadState(stateDir, state.SessionID); err != nil {
		t.Fatalf("second recovery: %v", err)
	}
	events, _, err := readRunEventsLocked(DeepDir(stateDir, state.SessionID), state.SessionID)
	if err != nil || len(events) != 2 {
		t.Fatalf("replay duplicated event: count=%d err=%v", len(events), err)
	}
}

func TestRunJournalReplaysResumeAfterProjectionFailureWithContext(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	state.Exec = &ExecSettings{Worker: "hermes-onbox", Provider: "provider-v2", Model: "model-v2", TaskTimeoutSec: 420}
	state.ComputeBinding = &ComputeBinding{Provider: "vast", InstanceID: 4321, BoundAt: now}
	state.Deadline = now.Add(2 * time.Hour)
	state.LandBefore = now.Add(90 * time.Minute)
	newEpoch, err := NewExecutionEpochID()
	if err != nil {
		t.Fatal(err)
	}
	event := RunEvent{
		EventID:         epochStartedEventID(state.RunID, newEpoch),
		RunID:           state.RunID,
		EpochID:         newEpoch,
		OccurredAt:      now.Add(time.Minute),
		Actor:           "deep-coordinator",
		Type:            RunEventEpochStarted,
		Boundary:        RunEventBoundaryResume,
		FromPhase:       PhaseExecuting,
		ToPhase:         PhaseExecuting,
		Deadline:        state.Deadline,
		LandBefore:      state.LandBefore,
		ComputeProvider: "vast",
		ComputeInstance: 4321,
		TaskSummary:     summarizeRunTasks(state.Tasks),
	}

	projectionWrites := 0
	preparedWatermark := state.RunEventWatermark
	err = appendAndProjectRunEvent(stateDir, &state, event, func(dir string, projection DeepState) error {
		projectionWrites++
		if projection.RunEventWatermark == preparedWatermark {
			return writeProjectionLocked(dir, projection)
		}
		return errors.New("injected post-event projection failure")
	})
	if err == nil || !strings.Contains(err.Error(), "is durable but deep.json projection update failed") {
		t.Fatalf("resume projection failure = %v, want a durable-event recovery error", err)
	}
	if projectionWrites != 2 {
		t.Fatalf("projection writes around resume event = %d, want prepared context and replayable transition", projectionWrites)
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RunEventWatermark != 2 || loaded.ExecutionEpochID != newEpoch || loaded.Phase != PhaseExecuting {
		t.Fatalf("resumed projection = phase %q epoch %q watermark %d", loaded.Phase, loaded.ExecutionEpochID, loaded.RunEventWatermark)
	}
	if loaded.Exec == nil || loaded.Exec.Model != "model-v2" || loaded.ComputeBinding == nil || loaded.ComputeBinding.InstanceID != 4321 {
		t.Fatalf("resume context was lost while replaying its epoch event: exec=%+v binding=%+v", loaded.Exec, loaded.ComputeBinding)
	}
	if !loaded.Deadline.Equal(state.Deadline) || !loaded.LandBefore.Equal(state.LandBefore) {
		t.Fatalf("resume deadline context was not restored: deadline=%s landBefore=%s", loaded.Deadline, loaded.LandBefore)
	}
}

func TestRunJournalRejectsProjectionAheadOfDurableHistory(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	state.RunEventWatermark++
	data, err := marshalIndent(state)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(DeepDir(stateDir, state.SessionID), "deep.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(stateDir, state.SessionID); err == nil || !strings.Contains(err.Error(), "ahead of durable journal") {
		t.Fatalf("LoadState with future watermark = %v, want fail closed", err)
	}
}

func TestRunJournalSerializesConcurrentLifecycleWriters(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	first, err := LoadState(stateDir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for _, candidate := range []*DeepState{&first, &second} {
		wait.Add(1)
		go func(candidate *DeepState) {
			defer wait.Done()
			<-start
			errs <- BeginLanding(stateDir, candidate, "concurrent stop", now.Add(time.Minute))
		}(candidate)
	}
	close(start)
	wait.Wait()
	close(errs)
	succeeded, rejected := 0, 0
	for err := range errs {
		if err == nil {
			succeeded++
		} else if strings.Contains(err.Error(), "stale Deep Work projection") {
			rejected++
		} else {
			t.Fatalf("unexpected concurrent transition error: %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("concurrent writers: %d succeeded, %d rejected; want exactly one of each", succeeded, rejected)
	}
	events, _, err := readRunEventsLocked(DeepDir(stateDir, state.SessionID), state.SessionID)
	if err != nil || len(events) != 2 || events[1].Type != RunEventLandingStarted {
		t.Fatalf("concurrent journal result = %d events err=%v", len(events), err)
	}
}

func TestRunJournalQuarantinesOnlyIncompleteTail(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(DeepDir(stateDir, state.SessionID), runEventFileName)
	if f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600); err != nil {
		t.Fatal(err)
	} else {
		if _, err := f.WriteString(`{"schemaVersion":1,"eventId":"partial`); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil || loaded.RunEventWatermark != 1 {
		t.Fatalf("recover interrupted final append: watermark=%d err=%v", loaded.RunEventWatermark, err)
	}
	quarantined, err := os.ReadFile(filepath.Join(DeepDir(stateDir, state.SessionID), runEventTailName))
	if err != nil || string(quarantined) != `{"schemaVersion":1,"eventId":"partial` {
		t.Fatalf("quarantined tail = %q err=%v", quarantined, err)
	}
	events, _, err := readRunEventsLocked(DeepDir(stateDir, state.SessionID), state.SessionID)
	if err != nil || len(events) != 1 {
		t.Fatalf("valid prefix changed after tail recovery: events=%d err=%v", len(events), err)
	}
}

func TestRunJournalFailsClosedOnMalformedCompleteEvent(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(DeepDir(stateDir, state.SessionID), runEventFileName)
	if f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600); err != nil {
		t.Fatal(err)
	} else {
		if _, err := f.WriteString("{malformed complete event}\n"); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadState(stateDir, state.SessionID); err == nil || !strings.Contains(err.Error(), "decode complete run event") {
		t.Fatalf("malformed complete event was skipped: %v", err)
	}
}

func TestRunJournalPreventsIndependentLifecycleProjectionMutation(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	state.Phase = PhaseLanding
	if err := state.SaveDir(stateDir); err == nil || !strings.Contains(err.Error(), "must be changed through a RunEvent") {
		t.Fatalf("direct journal-backed lifecycle mutation = %v, want rejected", err)
	}
}
