package deep

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestV2AcceptanceProjectionCannotInventAnOutcomeWithoutJournalEvidence(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	mission, err := ParseMission("# v2\n\n## Objective\nrecord acceptance\n\n## Acceptance Contract\nversion: 2\n\n## Tasks\n- [ ] T-1: prove output\n  - repository-change: optional\n  - acceptance-check: test -e output.txt\n")
	if err != nil {
		t.Fatal(err)
	}
	state := NewState("run-acceptance-projection", mission, "/repo", "/worktree", now.Add(time.Hour), now.Add(50*time.Minute), 2, now)
	stateDir := t.TempDir()
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	state.Tasks[0].AcceptanceOutcome = AcceptanceAccepted
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(DeepDir(stateDir, state.SessionID), "deep.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(stateDir, state.SessionID); err == nil || !strings.Contains(err.Error(), "no canonical acceptance event") {
		t.Fatalf("LoadState accepted an invented acceptance outcome: %v", err)
	}
}

func acceptanceRunFixture(t *testing.T) (string, DeepState, AcceptanceRun, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	mission := Mission{
		Name: "acceptance journal fixture", Objective: "record deterministic acceptance", Verify: "go test ./...",
		AcceptanceContractVersion: DeterministicAcceptanceContractVersion,
		Tasks: []Task{{ID: "T-1", Objective: "produce output", RepositoryChange: RepositoryChangeRequired,
			AcceptanceCheck: "test -e output.txt", Status: StatusQueued}},
	}
	var err error
	mission.AcceptanceContractSHA256, err = AcceptanceContractIdentity(mission)
	if err != nil {
		t.Fatal(err)
	}
	state := NewState("run-acceptance-journal", mission, "/repo", "/repo/.stint-deep/acceptance", now.Add(time.Hour), now.Add(50*time.Minute), 2, now)
	state.ComputeBinding = &ComputeBinding{Provider: "vast", InstanceID: 99, BoundAt: now}
	stateDir := t.TempDir()
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}

	executor := executorRunFixture(t, stateDir, &state, now.Add(time.Second))
	executor.RepositoryBefore = &VerificationSubject{HeadCommit: "baseline-head", TreeSHA: "baseline-tree"}
	executor, err = BeginExecutorRun(stateDir, &state, executor)
	if err != nil {
		t.Fatal(err)
	}
	executor.Outcome = ExecutorOutcomeSucceeded
	executor.EndedAt = now.Add(10 * time.Second)
	executor.ExitCode = 0
	executor.Completed = true
	executor.FinishReason = "completed"
	executor.ResultSummary = "executor completed"
	executor.RepositoryAfter = &VerificationSubject{HeadCommit: "subject-head", TreeSHA: "subject-tree"}
	if err := CompleteExecutorRun(stateDir, &state, executor, executor.EndedAt); err != nil {
		t.Fatal(err)
	}

	verificationID, err := NewVerificationRunID()
	if err != nil {
		t.Fatal(err)
	}
	subject := VerificationSubject{HeadCommit: "subject-head", TreeSHA: "subject-tree"}
	verification := VerificationRun{
		ID: verificationID, Purpose: VerificationPurposeTask, TaskID: "T-1", Attempt: 1,
		CommandSource: "mission", CommandSHA256: VerificationCommandIdentity(state.Verify),
		Runtime:   VerificationRuntime{Worker: "hermes-onbox", Location: "compute", Shell: "sh", Protocol: "local-process-group-v1"},
		StartedAt: now.Add(11 * time.Second), TimeoutSeconds: 120, Subject: subject,
	}
	verification, err = BeginVerificationRun(stateDir, &state, verification)
	if err != nil {
		t.Fatal(err)
	}
	verification.Outcome = VerificationPassed
	verification.EndedAt = now.Add(20 * time.Second)
	verification.HasExitCode = true
	verification.ExitCode = 0
	verification.SubjectAfter = &subject
	if err := CompleteVerificationRun(stateDir, &state, verification); err != nil {
		t.Fatal(err)
	}
	checkpoint := TaskCheckpoint{
		TaskID: "T-1", Attempt: 1, ExecutorRunID: executor.ID, VerificationRunID: verification.ID,
		VerificationSubject: subject, Commit: "checkpoint-commit", TreeSHA: subject.TreeSHA,
	}
	checkpointAt := now.Add(21 * time.Second)
	if err := RecordTaskCheckpoint(stateDir, &state, checkpoint, checkpointAt); err != nil {
		t.Fatal(err)
	}

	acceptanceID, err := NewAcceptanceRunID()
	if err != nil {
		t.Fatal(err)
	}
	run := AcceptanceRun{
		ID: acceptanceID, TaskID: "T-1", Attempt: 1,
		ContractSHA256:     mission.AcceptanceContractSHA256,
		CommandSHA256:      AcceptanceCommandIdentity(mission.Tasks[0].AcceptanceCheck),
		RepositoryChange:   RepositoryChangeRequired,
		RepositoryBaseline: VerificationSubject{HeadCommit: "baseline-head", TreeSHA: "baseline-tree"},
		SubjectBefore:      TaskCheckpointSubject(checkpoint),
		CheckpointEventID:  taskCheckpointEventID(verification.ID), Checkpoint: checkpoint,
		Runtime:   AcceptanceRuntime{Worker: "hermes-onbox", Location: "compute", Shell: "sh", Protocol: "local-process-group-v1"},
		StartedAt: now.Add(22 * time.Second), TimeoutSeconds: 120, RemainingDeadlineSeconds: 3500,
	}
	started, err := BeginAcceptanceRun(stateDir, &state, run)
	if err != nil {
		t.Fatalf("begin acceptance run: %v", err)
	}
	return stateDir, state, started, now
}

func successfulAcceptanceRun(run AcceptanceRun, now time.Time, subject VerificationSubject) AcceptanceRun {
	run.CheckOutcome = AcceptanceCheckPassed
	run.EndedAt = now.Add(32 * time.Second)
	run.HasExitCode = true
	run.ExitCode = 0
	run.SubjectAfter = &subject
	run.DurationMilliseconds = 10_000
	return run
}

func ptrVerificationSubject(subject VerificationSubject) *VerificationSubject { return &subject }

func TestAcceptanceRunRecordsObjectiveEvidenceSeparatelyFromVerification(t *testing.T) {
	stateDir, state, run, now := acceptanceRunFixture(t)
	if state.Tasks[0].Status != StatusVerified || state.Tasks[0].AcceptanceOutcome != AcceptanceNotEvaluated ||
		state.Tasks[0].AcceptanceCheckOutcome != AcceptanceCheckStarted || state.Tasks[0].VerificationOutcome != VerificationPassed {
		t.Fatalf("acceptance start conflated checkpoint verification and task acceptance: %+v", state.Tasks[0])
	}

	const outputMarker = "ACCEPTANCE-CHECK-OUTPUT-ONLY"
	ref, err := PersistAcceptanceOutput(stateDir, state.SessionID, run.ID, []byte("test result\n"+outputMarker))
	if err != nil || ref == "" {
		t.Fatalf("persist acceptance output: ref=%q err=%v", ref, err)
	}
	info, err := os.Stat(filepath.Join(DeepDir(stateDir, state.SessionID), filepath.FromSlash(ref)))
	if err != nil || info.Mode().Perm()&0o077 != 0 || info.Size() > 4096 {
		t.Fatalf("acceptance artifact mode/size=%v err=%v", info, err)
	}
	run = successfulAcceptanceRun(run, now, TaskCheckpointSubject(run.Checkpoint))
	run.ArtifactRefs = []string{ref}
	if err := CompleteAcceptanceRun(stateDir, &state, run); err != nil {
		t.Fatalf("complete acceptance run: %v", err)
	}
	if state.Tasks[0].AcceptanceOutcome != AcceptanceAccepted || state.Tasks[0].AcceptanceCheckOutcome != AcceptanceCheckPassed ||
		state.Tasks[0].Status != StatusAccepted || state.Tasks[0].CheckpointTreeSHA != run.Checkpoint.TreeSHA ||
		!strings.Contains(state.Tasks[0].AcceptanceOutput, outputMarker) {
		t.Fatalf("accepted projection lost distinction or evidence: %+v", state.Tasks[0])
	}
	loaded, found, err := LoadAcceptanceRun(stateDir, state.SessionID, run.ID)
	if err != nil || !found || loaded.Decision != AcceptanceAccepted || loaded.CheckOutcome != AcceptanceCheckPassed ||
		loaded.RepositoryBaseline.TreeSHA != "baseline-tree" || loaded.Checkpoint.TreeSHA != "subject-tree" {
		t.Fatalf("loaded acceptance run=%+v found=%t err=%v", loaded, found, err)
	}
	output, err := ReadAcceptanceOutput(stateDir, state.SessionID, loaded)
	if err != nil || !strings.Contains(output, outputMarker) {
		t.Fatalf("read acceptance artifact: output=%q err=%v", output, err)
	}
	eventData, err := os.ReadFile(filepath.Join(DeepDir(stateDir, state.SessionID), runEventFileName))
	if err != nil || strings.Contains(string(eventData), outputMarker) || strings.Contains(string(eventData), "test -e output.txt") {
		t.Fatalf("raw acceptance output or command leaked into journal: err=%v", err)
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 8 || events[6].Type != RunEventAcceptanceStarted || events[7].Type != RunEventAcceptanceResult ||
		events[6].Sequence != 7 || events[7].Sequence != 8 || events[6].EpochID != events[7].EpochID {
		t.Fatalf("acceptance event ordering=%+v err=%v", events, err)
	}
	if _, err := LoadUnmatchedAcceptanceRun(stateDir, state.SessionID); err != nil {
		t.Fatal(err)
	} else if unmatched, _ := LoadUnmatchedAcceptanceRun(stateDir, state.SessionID); unmatched != nil {
		t.Fatalf("completed acceptance remained unmatched: %+v", unmatched)
	}
}

func TestAcceptanceResultJournalReplaysAfterProjectionFailureExactlyOnce(t *testing.T) {
	stateDir, state, run, now := acceptanceRunFixture(t)
	run = successfulAcceptanceRun(run, now, TaskCheckpointSubject(run.Checkpoint))
	run.Decision = deriveAcceptanceOutcome(run)
	const outputMarker = "RECOVERED-ACCEPTANCE-OUTPUT"
	ref, err := PersistAcceptanceOutput(stateDir, state.SessionID, run.ID, []byte(outputMarker))
	if err != nil {
		t.Fatal(err)
	}
	run.ArtifactRefs = []string{ref}
	projected := state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if err := applyAcceptanceResult(&projected, run); err != nil {
		t.Fatal(err)
	}
	event := RunEvent{
		EventID: acceptanceEventID(run.ID, "result"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: run.EndedAt, Actor: "deep-coordinator", Type: RunEventAcceptanceResult,
		FromPhase: state.Phase, ToPhase: state.Phase, AcceptanceRun: &run,
		TaskSummary: summarizeRunTasks(projected.Tasks),
	}
	projectionErr := errors.New("injected acceptance projection failure")
	if err := appendAndProjectRunEvent(stateDir, &state, event, func(string, *DeepState) error { return projectionErr }); !errors.Is(err, projectionErr) {
		t.Fatalf("append result with failed projection: %v", err)
	}
	if state.Tasks[0].AcceptanceOutcome != AcceptanceNotEvaluated {
		t.Fatalf("failed projection mutated caller state: %+v", state.Tasks[0])
	}
	loaded, err := LoadState(stateDir, state.SessionID)
	if err != nil || loaded.RunEventWatermark != 8 || loaded.Tasks[0].AcceptanceOutcome != AcceptanceAccepted ||
		!strings.Contains(loaded.Tasks[0].AcceptanceOutput, outputMarker) {
		t.Fatalf("journal replay watermark=%d task=%+v err=%v", loaded.RunEventWatermark, loaded.Tasks[0], err)
	}
	if err := CompleteAcceptanceRun(stateDir, &loaded, run); err != nil {
		t.Fatalf("idempotent acceptance result reconciliation: %v", err)
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 8 || loaded.RunEventWatermark != 8 {
		t.Fatalf("idempotent result duplicated history: events=%d watermark=%d err=%v", len(events), loaded.RunEventWatermark, err)
	}

	drift := loaded
	drift.Tasks = append([]Task(nil), loaded.Tasks...)
	drift.Tasks[0].AcceptanceOutcome = AcceptanceUnresolved
	if err := drift.SaveDir(stateDir); err == nil {
		t.Fatal("deep.json accepted drift from canonical acceptance result")
	}
}

func TestUnmatchedAcceptanceStartRecoversAsUnresolvedAcrossEpoch(t *testing.T) {
	stateDir, state, started, now := acceptanceRunFixture(t)
	firstEpoch := state.ExecutionEpochID
	if err := BeginResumeEpoch(stateDir, &state, PhaseExecuting, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if state.ExecutionEpochID == firstEpoch || state.RunEventWatermark != 8 {
		t.Fatalf("resume did not advance epoch while preserving sequence: epoch=%q watermark=%d", state.ExecutionEpochID, state.RunEventWatermark)
	}
	recovered, err := RecoverUnmatchedAcceptanceRun(stateDir, &state, now.Add(61*time.Second))
	if err != nil || recovered == nil || recovered.ID != started.ID || recovered.Decision != AcceptanceUnresolved ||
		!recovered.QuiescenceUnconfirmed || recovered.CheckOutcome != AcceptanceCheckUnknown {
		t.Fatalf("recovered acceptance=%+v err=%v", recovered, err)
	}
	if state.RunEventWatermark != 9 || !state.ExecutionQuiescenceUnconfirmed || state.ExecutionQuiescenceTaskID != "T-1" ||
		state.Tasks[0].AcceptanceOutcome != AcceptanceUnresolved || state.Tasks[0].AcceptanceCheckOutcome != AcceptanceCheckUnknown {
		t.Fatalf("recovery failed to block unresolved acceptance: state=%+v task=%+v", state, state.Tasks[0])
	}
	if unmatched, err := LoadUnmatchedAcceptanceRun(stateDir, state.SessionID); err != nil || unmatched != nil {
		t.Fatalf("recovery event did not close unmatched invocation: run=%+v err=%v", unmatched, err)
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 9 || events[7].Type != RunEventEpochStarted || events[8].Type != RunEventAcceptanceRecovery ||
		events[6].EpochID == events[8].EpochID || events[8].Sequence != 9 {
		t.Fatalf("recovery history=%+v err=%v", events, err)
	}
}

func TestAcceptanceRunRejectsWrongBaselineAndContractCommand(t *testing.T) {
	stateDir, state, run, _ := acceptanceRunFixture(t)
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil || len(events) != 7 {
		t.Fatalf("read pre-invocation history: events=%d err=%v", len(events), err)
	}
	if err := validateAcceptanceEventTransition(events, RunEvent{Type: RunEventLandingStarted}); err == nil || !strings.Contains(err.Error(), "unmatched") {
		t.Fatalf("journal allowed another lifecycle transition while acceptance process may still be running: %v", err)
	}
	// A different baseline would make a retry's output appear to satisfy a
	// repository-change requirement even if it did not change the original tree.
	bad := run
	bad.ID, err = NewAcceptanceRunID()
	if err != nil {
		t.Fatal(err)
	}
	bad.StartEventID = acceptanceEventID(bad.ID, "started")
	bad.RepositoryBaseline.TreeSHA = "later-attempt-baseline"
	badEvent := RunEvent{Type: RunEventAcceptanceStarted, EpochID: state.ExecutionEpochID, AcceptanceRun: &bad}
	if err := validateAcceptanceEventTransition(events[:6], badEvent); err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Fatalf("acceptance start accepted a noncanonical baseline: %v", err)
	}
	bad = run
	bad.SubjectBefore.TreeSHA = "changed-before-check"
	if err := validateAcceptanceRun(bad, true); err == nil || !strings.Contains(err.Error(), "checkpoint identity") {
		t.Fatalf("acceptance start accepted a subject that differs from its checkpoint: %v", err)
	}
	bad = run
	bad.ID, err = NewAcceptanceRunID()
	if err != nil {
		t.Fatal(err)
	}
	bad.StartEventID = acceptanceEventID(bad.ID, "started")
	bad.CommandSHA256 = AcceptanceCommandIdentity("test -e another-output")
	if acceptanceCommandFactsMatch(state, bad) {
		t.Fatal("acceptance command provenance accepted a different contract command")
	}
}

func TestAcceptanceDecisionRequiresTypedEvidenceAndDeclaredRepositoryChange(t *testing.T) {
	baseline := VerificationSubject{HeadCommit: "before", TreeSHA: "tree-before"}
	changed := TaskCheckpoint{VerificationSubject: VerificationSubject{HeadCommit: "before", TreeSHA: "tree-after"}, Commit: "after", TreeSHA: "tree-after"}
	unchanged := TaskCheckpoint{VerificationSubject: baseline, Commit: "before", TreeSHA: "tree-before"}
	base := AcceptanceRun{
		CheckOutcome: AcceptanceCheckPassed, SubjectAfter: ptrVerificationSubject(TaskCheckpointSubject(changed)),
		RepositoryBaseline: baseline, SubjectBefore: TaskCheckpointSubject(changed), Checkpoint: changed,
	}
	tests := []struct {
		name   string
		mutate func(*AcceptanceRun)
		want   AcceptanceOutcome
	}{
		{name: "required change satisfied", mutate: func(run *AcceptanceRun) { run.RepositoryChange = RepositoryChangeRequired }, want: AcceptanceAccepted},
		{name: "required change absent", mutate: func(run *AcceptanceRun) {
			run.RepositoryChange = RepositoryChangeRequired
			run.Checkpoint = unchanged
			run.SubjectBefore = TaskCheckpointSubject(unchanged)
			run.SubjectAfter = ptrVerificationSubject(TaskCheckpointSubject(unchanged))
		}, want: AcceptanceNotSatisfied},
		{name: "optional change absent", mutate: func(run *AcceptanceRun) {
			run.RepositoryChange = RepositoryChangeOptional
			run.Checkpoint = unchanged
			run.SubjectBefore = TaskCheckpointSubject(unchanged)
			run.SubjectAfter = ptrVerificationSubject(TaskCheckpointSubject(unchanged))
		}, want: AcceptanceAccepted},
		{name: "forbidden change absent", mutate: func(run *AcceptanceRun) {
			run.RepositoryChange = RepositoryChangeForbidden
			run.Checkpoint = unchanged
			run.SubjectBefore = TaskCheckpointSubject(unchanged)
			run.SubjectAfter = ptrVerificationSubject(TaskCheckpointSubject(unchanged))
		}, want: AcceptanceAccepted},
		{name: "forbidden change present", mutate: func(run *AcceptanceRun) { run.RepositoryChange = RepositoryChangeForbidden }, want: AcceptanceNotSatisfied},
		{name: "objective check failed", mutate: func(run *AcceptanceRun) {
			run.RepositoryChange = RepositoryChangeRequired
			run.CheckOutcome = AcceptanceCheckFailed
		}, want: AcceptanceNotSatisfied},
		{name: "timeout is unresolved", mutate: func(run *AcceptanceRun) {
			run.RepositoryChange = RepositoryChangeRequired
			run.CheckOutcome = AcceptanceCheckTimedOut
			run.Error = "timed out"
		}, want: AcceptanceUnresolved},
		{name: "tree changed during check", mutate: func(run *AcceptanceRun) {
			run.RepositoryChange = RepositoryChangeRequired
			run.SubjectAfter = ptrVerificationSubject(baseline)
		}, want: AcceptanceUnresolved},
		{name: "check started against another subject", mutate: func(run *AcceptanceRun) {
			run.RepositoryChange = RepositoryChangeRequired
			run.SubjectBefore.TreeSHA = "tree-other"
		}, want: AcceptanceUnresolved},
		{name: "quiescence unknown", mutate: func(run *AcceptanceRun) {
			run.RepositoryChange = RepositoryChangeRequired
			run.QuiescenceUnconfirmed = true
		}, want: AcceptanceUnresolved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := base
			tt.mutate(&run)
			if got := deriveAcceptanceOutcome(run); got != tt.want {
				t.Fatalf("deriveAcceptanceOutcome = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTerminalTaskSemanticsAreContractScoped(t *testing.T) {
	workUnit := Task{ID: "OBJ-1", Status: StatusVerified, AcceptanceOutcome: AcceptanceNotEvaluated}
	if !taskTerminalInContract(workUnit, 0) {
		t.Fatal("legacy verified Work Unit ceased to be terminal")
	}
	if taskTerminalInContract(workUnit, DeterministicAcceptanceContractVersion) {
		t.Fatal("v2 verified checkpoint was treated as Objective acceptance")
	}
	workUnit.Status = StatusAccepted
	workUnit.AcceptanceOutcome = AcceptanceAccepted
	if !taskTerminalInContract(workUnit, DeterministicAcceptanceContractVersion) {
		t.Fatal("v2 accepted Work Unit is not terminal")
	}
	coordinatorTask := Task{ID: "STINT-PLAN-001", Source: "coordinator", Status: StatusVerified}
	if !taskTerminalInContract(coordinatorTask, DeterministicAcceptanceContractVersion) {
		t.Fatal("v2 contract changed terminal semantics for coordinator-owned rows")
	}
}
