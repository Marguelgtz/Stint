package deep

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func verificationRunFixture(t *testing.T, stateDir string, state *DeepState, now time.Time) VerificationRun {
	t.Helper()
	state.Verify = "go test ./..."
	if err := state.SaveDir(stateDir); err != nil {
		t.Fatal(err)
	}
	state.Tasks[0].Status = StatusActive
	state.Tasks[0].Attempts = 1
	if err := state.SaveDir(stateDir); err != nil {
		t.Fatal(err)
	}
	id, err := NewVerificationRunID()
	if err != nil {
		t.Fatal(err)
	}
	subject := VerificationSubject{HeadCommit: "head-x", TreeSHA: "tree-x"}
	return VerificationRun{
		ID: id, Purpose: VerificationPurposeTask, TaskID: state.Tasks[0].ID, Attempt: 1,
		CommandSource: "mission", CommandSHA256: VerificationCommandIdentity(state.Verify),
		Runtime:   VerificationRuntime{Worker: "hermes-onbox", Location: "compute", Shell: "sh", Protocol: "local-process-group-v1"},
		StartedAt: now.Add(time.Second), TimeoutSeconds: 180, RemainingDeadlineSeconds: 1200,
		Subject: subject, BookkeepingBefore: map[string]string{"action_plan.md": "absent"},
	}
}

func completeVerificationFixture(run VerificationRun, now time.Time) VerificationRun {
	_ = now
	run.Outcome = VerificationPassed
	run.EndedAt = run.StartedAt.Add(10 * time.Second)
	run.HasExitCode = true
	run.ExitCode = 0
	after := run.Subject
	run.SubjectAfter = &after
	run.BookkeepingAfter = cloneStringMap(run.BookkeepingBefore)
	run.DurationMilliseconds = 9_000
	return run
}

func TestVerificationRunRecordsExactSubjectAndTypedResult(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	run := verificationRunFixture(t, stateDir, &state, now)
	var err error
	run, err = BeginVerificationRun(stateDir, &state, run)
	if err != nil {
		t.Fatalf("begin verification: %v", err)
	}
	if state.Tasks[0].VerificationRunID != run.ID || state.Tasks[0].VerificationOutcome != VerificationStarted || state.Tasks[0].Status != StatusActive {
		t.Fatalf("verification-start projection confused evidence with acceptance: %+v", state.Tasks[0])
	}
	run = completeVerificationFixture(run, now)
	const outputMarker = "SENSITIVE-VERIFIER-OUTPUT"
	verifierOutput := strings.Repeat("v", 5000) + "\n" + outputMarker
	ref, err := PersistVerificationOutput(stateDir, state.SessionID, run.ID, []byte(verifierOutput))
	if err != nil || ref == "" {
		t.Fatalf("persist bounded output artifact: ref=%q err=%v", ref, err)
	}
	info, err := os.Stat(filepath.Join(DeepDir(stateDir, state.SessionID), filepath.FromSlash(ref)))
	if err != nil || info.Size() != 4096 {
		t.Fatalf("verification artifact size=%v err=%v, want bounded 4096-byte tail", info, err)
	}
	run.ArtifactRefs = []string{ref}
	if err := CompleteVerificationRun(stateDir, &state, run); err != nil {
		t.Fatalf("complete verification: %v", err)
	}
	if state.RunEventWatermark != 3 || state.Tasks[0].VerificationRunID != run.ID || state.Tasks[0].VerificationOutcome != VerificationPassed ||
		state.Tasks[0].Status != StatusActive || state.Tasks[0].VerificationSubject == nil || *state.Tasks[0].VerificationSubject != run.Subject {
		t.Fatalf("verification result projection = %+v watermark=%d", state.Tasks[0], state.RunEventWatermark)
	}
	if !strings.Contains(state.Tasks[0].VerificationOutput, outputMarker) {
		t.Fatalf("bounded compatibility projection did not hydrate verifier artifact: %q", state.Tasks[0].VerificationOutput)
	}
	output, err := ReadVerificationOutput(stateDir, state.SessionID, run)
	if err != nil || !strings.Contains(output, outputMarker) {
		t.Fatalf("read bounded verifier artifact: output contains marker=%t err=%v", strings.Contains(output, outputMarker), err)
	}
	eventData, err := os.ReadFile(filepath.Join(DeepDir(stateDir, state.SessionID), runEventFileName))
	if err != nil || strings.Contains(string(eventData), outputMarker) {
		t.Fatalf("raw verifier output leaked into canonical journal: err=%v", err)
	}
	loaded, ok, err := LoadVerificationRun(stateDir, state.SessionID, run.ID)
	if err != nil || !ok || loaded.Outcome != VerificationPassed || loaded.SubjectAfter == nil || *loaded.SubjectAfter != run.Subject ||
		len(loaded.ArtifactRefs) != 1 || loaded.ArtifactRefs[0] != ref {
		t.Fatalf("loaded verification record=%+v found=%t err=%v", loaded, ok, err)
	}
	if _, err := BeginVerificationRun(stateDir, &state, run); err == nil {
		t.Fatal("verification invocation identity was allowed to start twice")
	}
	projectionDrift := state
	projectionDrift.Tasks = append([]Task(nil), state.Tasks...)
	projectionDrift.Tasks[0].VerificationOutcome = VerificationFailed
	if err := projectionDrift.SaveDir(stateDir); err == nil {
		t.Fatal("deep.json accepted verifier projection drift from its canonical result event")
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 3 || events[1].Type != RunEventVerificationStarted || events[2].Type != RunEventVerificationResult ||
		events[1].Sequence != 2 || events[2].Sequence != 3 || events[1].EpochID != events[2].EpochID {
		t.Fatalf("verification event order=%+v err=%v", events, err)
	}
}

func TestVerificationResultEventReplaysAfterProjectionFailure(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	started, err := BeginVerificationRun(stateDir, &state, verificationRunFixture(t, stateDir, &state, now))
	if err != nil {
		t.Fatal(err)
	}
	result := completeVerificationFixture(started, now)
	const outputMarker = "replayed verifier output"
	ref, err := PersistVerificationOutput(stateDir, state.SessionID, result.ID, []byte(outputMarker))
	if err != nil {
		t.Fatal(err)
	}
	result.ArtifactRefs = []string{ref}
	projected := state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if err := applyVerificationResult(&projected, result); err != nil {
		t.Fatal(err)
	}
	event := RunEvent{
		EventID: verificationEventID(result.ID, "result"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: result.EndedAt, Actor: "deep-coordinator", Type: RunEventVerificationResult,
		FromPhase: state.Phase, ToPhase: state.Phase, VerificationRun: &result,
		TaskSummary: summarizeRunTasks(projected.Tasks),
	}
	err = appendAndProjectRunEvent(stateDir, &state, event, func(string, *DeepState) error {
		return errors.New("injected verification projection failure")
	})
	if err == nil || !strings.Contains(err.Error(), "is durable but deep.json projection update failed") {
		t.Fatalf("verification projection failure=%v", err)
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil || loaded.RunEventWatermark != 3 || loaded.Tasks[0].VerificationOutcome != VerificationPassed || loaded.Tasks[0].VerificationRunID != result.ID ||
		loaded.Tasks[0].VerificationOutput != outputMarker {
		t.Fatalf("replayed verification result=%+v err=%v", loaded.Tasks[0], err)
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 3 {
		t.Fatalf("replay duplicated verification result: events=%d err=%v", len(events), err)
	}
}

func TestUnmatchedVerificationStartRecoversUnknownAcrossResumeEpoch(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	state.Verify = "go test ./..."
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	run := verificationRunFixture(t, stateDir, &state, now)
	run, err := BeginVerificationRun(stateDir, &state, run)
	if err != nil {
		t.Fatal(err)
	}
	startedEpoch := state.ExecutionEpochID
	if err := BeginResumeEpoch(stateDir, &state, PhaseExecuting, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	recovered, err := RecoverUnmatchedVerificationRun(stateDir, &state, now.Add(time.Minute+time.Second))
	if err != nil || recovered == nil || recovered.ID != run.ID || recovered.Outcome != VerificationUnknown || !recovered.EndedAt.IsZero() {
		t.Fatalf("unmatched verification recovery=%+v err=%v", recovered, err)
	}
	if state.ExecutionEpochID == startedEpoch || state.RunEventWatermark != 4 || !state.ExecutionQuiescenceUnconfirmed ||
		state.ExecutionQuiescenceTaskID != run.TaskID || state.Tasks[0].Status != StatusNeedsHuman ||
		state.Tasks[0].VerificationOutcome != VerificationUnknown || state.Tasks[0].Status == StatusVerified {
		t.Fatalf("unknown verifier recovery projection=%+v watermark=%d", state.Tasks[0], state.RunEventWatermark)
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil || loaded.RunEventWatermark != 4 || !loaded.ExecutionQuiescenceUnconfirmed || loaded.Tasks[0].VerificationOutcome != VerificationUnknown {
		t.Fatalf("replayed unknown verifier recovery=%+v err=%v", loaded, err)
	}
	if again, err := RecoverUnmatchedVerificationRun(stateDir, &loaded, now.Add(2*time.Minute)); err != nil || again != nil {
		t.Fatalf("verification recovery was duplicated: run=%+v err=%v", again, err)
	}
}

func TestMissionFinalVerificationRunProjectsSeparatelyFromLanding(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	state.Verify = "go test ./..."
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	if err := BeginLanding(stateDir, &state, "mission final check", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	id, err := NewVerificationRunID()
	if err != nil {
		t.Fatal(err)
	}
	subject := VerificationSubject{HeadCommit: "landing-head", TreeSHA: "landing-tree"}
	run := VerificationRun{
		ID: id, Purpose: VerificationPurposeMissionEnd, CommandSource: "mission",
		CommandSHA256: VerificationCommandIdentity(state.Verify),
		Runtime:       VerificationRuntime{Worker: "hermes", Location: "compute", Shell: "sh", Protocol: "remote-process-group-v1"},
		StartedAt:     now.Add(2 * time.Minute), TimeoutSeconds: 180, Subject: subject,
		BookkeepingBefore: map[string]string{"action_plan.md": "absent"},
	}
	run, err = BeginVerificationRun(stateDir, &state, run)
	if err != nil {
		t.Fatal(err)
	}
	if state.LandingVerificationRunID != run.ID || state.LandingVerifyDone || state.LandingVerificationOutcome != VerificationStarted {
		t.Fatalf("final verification start projection=%+v", state)
	}
	run = completeVerificationFixture(run, now)
	run.Purpose = VerificationPurposeMissionEnd
	run.TaskID = ""
	run.Attempt = 0
	run.CommandSource = "mission"
	if err := CompleteVerificationRun(stateDir, &state, run); err != nil {
		t.Fatal(err)
	}
	if !state.LandingVerifyDone || state.LandingVerificationOutcome != VerificationPassed || state.LandingVerificationRunID != run.ID ||
		state.LandingVerificationSubject == nil || *state.LandingVerificationSubject != subject || state.Phase != PhaseLanding {
		t.Fatalf("final verification result altered landing semantics: %+v", state)
	}
}

func TestLegacyRunDoesNotSynthesizeVerificationEvents(t *testing.T) {
	stateDir, state, _ := journalFixture(t)
	if unmatched, err := RecoverUnmatchedVerificationRun(stateDir, &state, time.Now()); err != nil || unmatched != nil {
		t.Fatalf("legacy recovery unexpectedly created verification history: run=%+v err=%v", unmatched, err)
	}
	if events, err := ReadRunEvents(stateDir, state.SessionID); err != nil || len(events) != 0 {
		t.Fatalf("legacy session acquired fabricated events: events=%+v err=%v", events, err)
	}
}
