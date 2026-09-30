package deep

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// statusFixture builds a journaled Deep Work run in a fresh state directory and
// advances it to a landed state, so the status projection has a durable phase,
// a distinct mission outcome, a landing commit, and bounded task rows.
func statusFixture(t *testing.T) (string, DeepState, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 30, 17, 13, 48, 0, time.UTC)
	mission := Mission{
		Name:      "status provider fixture",
		Objective: "expose durable status only",
		// Secret-bearing and path-bearing prose lives here on purpose: the
		// provider must never leak mission prose into a status reply.
		Success: []string{
			"the probe confirms api_key=sk-test-secret-4f9a and token=ghp_0123456789abcdef are not in the reply",
			"state under /var/lib/stint-onbox/state/deep is never quoted in a reply",
		},
		Verify: "make test && make check",
		Tasks: []Task{
			{ID: "STINT-STATUS-001", Objective: "provider implementation", Status: StatusAccepted, Source: "mission", Attempts: 2,
				CheckpointCommit:  "0123456789abcdef0123456789abcdef01234567",
				CheckpointTreeSHA: "tree-0123456789abcdef", AcceptanceOutcome: AcceptanceAccepted},
			{ID: "T-2", Objective: "protocol tests", Status: StatusBlocked, Source: "mission", Attempts: 1,
				CheckpointCommit:  "fedcba9876543210fedcba9876543210fedcba98",
				CheckpointTreeSHA: "tree-fedcba9876543210", AcceptanceOutcome: AcceptanceNotEvaluated},
		},
	}
	state := NewState("status-run-20260930", mission, "/home/ops/stint/repo", "/home/ops/stint/repo/.stint-deep/status-run-20260930", now.Add(time.Hour), now.Add(50*time.Minute), 2, now)
	stateDir := t.TempDir()
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatalf("BeginNewRun: %v", err)
	}
	if err := BeginLanding(stateDir, &state, "status fixture landed", now.Add(time.Minute)); err != nil {
		t.Fatalf("BeginLanding: %v", err)
	}
	if err := CompleteLanding(stateDir, &state, "landed-commit-1", "landed-tree-1", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("CompleteLanding: %v", err)
	}
	return stateDir, state, now
}

func decodeReply(t *testing.T, data []byte) DeepStatusReply {
	t.Helper()
	var reply DeepStatusReply
	if err := unmarshal(data, &reply); err != nil {
		t.Fatalf("decode status reply: %v\n%s", err, data)
	}
	return reply
}

func TestBuildStatusReplyProjectsLandedRunWithSeparateOutcomes(t *testing.T) {
	stateDir, state, _ := statusFixture(t)
	data, err := BuildStatusReply(stateDir, state.SessionID)
	if err != nil {
		t.Fatalf("BuildStatusReply: %v", err)
	}
	reply := decodeReply(t, data)
	if reply.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d, want 1", reply.SchemaVersion)
	}
	if reply.SessionID != state.SessionID || reply.RunID != state.RunID || reply.ExecutionEpochID != state.ExecutionEpochID {
		t.Fatalf("identity = session %q run %q epoch %q", reply.SessionID, reply.RunID, reply.ExecutionEpochID)
	}
	if reply.RunEventWatermark != state.RunEventWatermark {
		t.Fatalf("runEventWatermark = %d, want %d", reply.RunEventWatermark, state.RunEventWatermark)
	}
	// Landed is an operational boundary; a fixture with blocked tasks is not
	// a succeeded mission. The reply must keep both facts distinct.
	if reply.Phase != string(PhaseLanded) {
		t.Fatalf("phase = %q, want landed", reply.Phase)
	}
	if reply.MissionOutcome != string(MissionOutcomeIncomplete) {
		t.Fatalf("missionOutcome = %q, want incomplete for a run with blocked tasks", reply.MissionOutcome)
	}
	if reply.LandingCommit != "landed-commit-1" || reply.LandingCheckpointTreeSha != "landed-tree-1" {
		t.Fatalf("landing = commit %q tree %q", reply.LandingCommit, reply.LandingCheckpointTreeSha)
	}
	if reply.Deadline == "" {
		t.Fatal("deadline omitted for a run that has one")
	}
	if len(reply.Tasks) != 2 {
		t.Fatalf("task rows = %d, want 2", len(reply.Tasks))
	}
	first := reply.Tasks[0]
	if first.ID != "STINT-STATUS-001" || first.Source != "mission" || first.Status != string(StatusAccepted) ||
		first.Attempts != 2 || first.CheckpointCommit != "0123456789abcdef0123456789abcdef01234567" ||
		first.CheckpointTreeSha != "tree-0123456789abcdef" || first.AcceptanceOutcome != string(AcceptanceAccepted) {
		t.Fatalf("first task row = %+v", first)
	}
	second := reply.Tasks[1]
	if second.ID != "T-2" || second.Status != string(StatusBlocked) || second.AcceptanceOutcome != string(AcceptanceNotEvaluated) ||
		second.ReviewOutcome != "" || second.CheckpointCommit != "fedcba9876543210fedcba9876543210fedcba98" ||
		second.CheckpointTreeSha != "tree-fedcba9876543210" {
		t.Fatalf("second task row leaked or lost fields: %+v", second)
	}
	for _, leaked := range []string{
		"sk-test-secret-4f9a", "ghp_0123456789abcdef", "api_key", "token=",
		"/var/lib/stint-onbox/state", "/home/ops/stint", "make test && make check",
		"status provider fixture", "expose durable status only", "never quote",
	} {
		if got := string(data); containsString(got, leaked) {
			t.Fatalf("reply leaked %q", leaked)
		}
	}
	// The reply must not carry any field outside the allow-list: every key in
	// the JSON is one of the projected fields.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatalf("parse reply keys: %v", err)
	}
	allowed := map[string]bool{
		"schemaVersion": true, "sessionId": true, "runId": true, "executionEpochId": true,
		"runEventWatermark": true, "phase": true, "missionOutcome": true, "deadline": true,
		"landingCommit": true, "landingCheckpointTreeSha": true, "missionReviewOutcome": true,
		"tasks": true,
	}
	for key := range keys {
		if !allowed[key] {
			t.Fatalf("reply carries non-allow-listed key %q", key)
		}
	}
	for _, row := range reply.Tasks {
		var rowKeys map[string]json.RawMessage
		rowData, _ := json.Marshal(row)
		if err := json.Unmarshal(rowData, &rowKeys); err != nil {
			t.Fatalf("parse task row keys: %v", err)
		}
		rowAllowed := map[string]bool{
			"id": true, "source": true, "status": true, "attempts": true,
			"checkpointCommit": true, "checkpointTreeSha": true,
			"acceptanceOutcome": true, "reviewOutcome": true,
		}
		for key := range rowKeys {
			if !rowAllowed[key] {
				t.Fatalf("task row %s carries non-allow-listed key %q", row.ID, key)
			}
		}
	}
}

func TestBuildStatusReplyMissingStateFailsClosed(t *testing.T) {
	stateDir := t.TempDir()
	if _, err := BuildStatusReply(stateDir, "never-started"); err == nil {
		t.Fatal("BuildStatusReply on missing state succeeded; want a failure")
	}
}

func TestBuildStatusReplyCorruptStateFailsClosed(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.MkdirAll(DeepDir(stateDir, "corrupt-run"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(DeepDir(stateDir, "corrupt-run"), "deep.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildStatusReply(stateDir, "corrupt-run"); err == nil {
		t.Fatal("BuildStatusReply on corrupt state succeeded; want a failure")
	}
}

// TestBuildStatusReplySurvivesRestartAndEpochRecovery proves the provider
// observes durable state: a fresh process that re-opens the same state
// directory sees the same observation after a restart, and the observation
// follows epoch recovery into a new epoch without losing run identity or
// landing facts. The provider itself must add no durable files, so a status
// read can never create competing persistence.
func TestBuildStatusReplySurvivesRestartAndEpochRecovery(t *testing.T) {
	stateDir, state, now := statusFixture(t)
	before, err := BuildStatusReply(stateDir, state.SessionID)
	if err != nil {
		t.Fatalf("BuildStatusReply before restart: %v", err)
	}
	beforeReply := decodeReply(t, before)
	firstEpoch := state.ExecutionEpochID
	if beforeReply.Phase != string(PhaseLanded) || beforeReply.MissionOutcome != string(MissionOutcomeIncomplete) ||
		beforeReply.LandingCommit != "landed-commit-1" {
		t.Fatalf("pre-restart projection = phase %q outcome %q commit %q", beforeReply.Phase, beforeReply.MissionOutcome, beforeReply.LandingCommit)
	}
	// Process restart: a new process opens the same state directory and
	// recovers the run through the existing state and journal replay path.
	restarted, err := LoadState(stateDir, state.SessionID)
	if err != nil {
		t.Fatalf("LoadState after restart: %v", err)
	}
	if restarted.Phase != PhaseLanded || restarted.RunEventWatermark != state.RunEventWatermark {
		t.Fatalf("restart lost durable state: phase %q watermark %d", restarted.Phase, restarted.RunEventWatermark)
	}
	restartedReplyData, err := BuildStatusReply(stateDir, state.SessionID)
	if err != nil {
		t.Fatalf("BuildStatusReply after restart: %v", err)
	}
	restartedReply := decodeReply(t, restartedReplyData)
	if restartedReply.Phase != beforeReply.Phase || restartedReply.MissionOutcome != beforeReply.MissionOutcome ||
		restartedReply.ExecutionEpochID != beforeReply.ExecutionEpochID || restartedReply.RunEventWatermark != beforeReply.RunEventWatermark {
		t.Fatalf("observation drifted across restart: before=%+v after=%+v", beforeReply, restartedReply)
	}
	// Epoch recovery: the coordinator starts a new execution epoch in the
	// same run, and the provider must expose it with the same run identity.
	if err := BeginResumeEpoch(stateDir, &restarted, PhaseLanded, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("BeginResumeEpoch: %v", err)
	}
	if restarted.RunID != state.RunID {
		t.Fatalf("resume changed run identity: %q -> %q", state.RunID, restarted.RunID)
	}
	if restarted.ExecutionEpochID == firstEpoch || restarted.ExecutionEpochID == "" {
		t.Fatalf("resume did not move to a new epoch: first=%q resumed=%q", firstEpoch, restarted.ExecutionEpochID)
	}
	after, err := BuildStatusReply(stateDir, state.SessionID)
	if err != nil {
		t.Fatalf("BuildStatusReply after restart and epoch recovery: %v", err)
	}
	afterReply := decodeReply(t, after)
	if afterReply.Phase != string(PhaseExecuting) || afterReply.MissionOutcome != string(MissionOutcomePending) ||
		afterReply.ExecutionEpochID != restarted.ExecutionEpochID || afterReply.RunID != restarted.RunID {
		t.Fatalf("recovered projection drifted: phase=%q outcome=%q epoch=%q", afterReply.Phase, afterReply.MissionOutcome, afterReply.ExecutionEpochID)
	}
	// A new epoch reopens the run: the prior landing moves out of the
	// current landing fields (it is retained in the durable state's
	// PreviousLandings), so the allow-listed projection must show the
	// reopened operating state, not the stale landing.
	if afterReply.LandingCommit != "" || afterReply.LandingCheckpointTreeSha != "" {
		t.Fatalf("reopened projection still carries stale landing facts: commit=%q tree=%q", afterReply.LandingCommit, afterReply.LandingCheckpointTreeSha)
	}
	// The provider reads only: its observations must not have created a
	// competing status store. The session directory holds exactly the
	// established Deep Work state files.
	entries, err := os.ReadDir(DeepDir(stateDir, state.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	want := map[string]bool{"deep.json": true, "run-events.jsonl": true, "run-events.lock": true}
	if len(names) != len(want) {
		t.Fatalf("status provider added or removed durable files: %v", names)
	}
	for _, name := range names {
		if !want[name] {
			t.Fatalf("unexpected durable file %q", name)
		}
	}
}

func TestBuildStatusReplyInvalidSelectionFailsClosed(t *testing.T) {
	stateDir := t.TempDir()
	for _, id := range []string{
		"", ".", "..", "a/b", "../escape", "a b", "a;b", "a|b",
		"$(reboot)", "a*b", "a?b", "a~b", "a#b", "a^b", "a`b",
		"status-run-20260930/x", "a/b/../y", "a.b", strings.Repeat("a", 129),
	} {
		if _, err := BuildStatusReply(stateDir, id); err == nil {
			t.Fatalf("BuildStatusReply accepted invalid selection %q", id)
		}
	}
	// A well-formed selection for a run that does not exist also fails:
	// validity of the identity is not validity of the state.
	if _, err := BuildStatusReply(stateDir, "does-not-exist"); err == nil {
		t.Fatal("BuildStatusReply on unknown well-formed selection succeeded")
	}
}

func TestValidateStatusSessionID(t *testing.T) {
	valid := []string{
		"20260930-171348",
		"status-run-20260930",
		"run_2026A",
	}
	for _, id := range valid {
		if err := ValidateStatusSessionID(id); err != nil {
			t.Fatalf("ValidateStatusSessionID(%q) = %v, want valid", id, err)
		}
	}
	invalid := []string{
		"", ".", "..", "a/b", "a b", "a.b", "a/b/../c", "a~b",
		strings.Repeat("a", 129),
	}
	for _, id := range invalid {
		if err := ValidateStatusSessionID(id); err == nil {
			t.Fatalf("ValidateStatusSessionID(%q) succeeded, want invalid", id)
		}
	}
}

func TestStatusReplyOmitsEmptyEvidenceFields(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Date(2026, 9, 30, 17, 13, 48, 0, time.UTC)
	mission := Mission{
		Name:      "bare fixture",
		Objective: "no optional facts",
		Tasks:     []Task{{ID: "T-1", Objective: "work", Status: StatusQueued}},
	}
	state := NewState("status-bare", mission, "/repo", "/repo/.stint-deep/status-bare", now.Add(time.Hour), now.Add(50*time.Minute), 2, now)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatalf("BeginNewRun: %v", err)
	}
	data, err := BuildStatusReply(stateDir, state.SessionID)
	if err != nil {
		t.Fatalf("BuildStatusReply: %v", err)
	}
	reply := decodeReply(t, data)
	if reply.Phase != string(PhaseExecuting) || reply.MissionOutcome != string(MissionOutcomePending) {
		t.Fatalf("phase=%q outcome=%q", reply.Phase, reply.MissionOutcome)
	}
	if reply.LandingCommit != "" || reply.LandingCheckpointTreeSha != "" || reply.MissionReviewOutcome != "" {
		t.Fatalf("empty evidence fields must be omitted: %+v", reply)
	}
	if reply.Tasks[0].CheckpointCommit != "" || reply.Tasks[0].CheckpointTreeSha != "" || reply.Tasks[0].AcceptanceOutcome != "" {
		t.Fatalf("task empty evidence fields must be omitted: %+v", reply.Tasks[0])
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse reply: %v", err)
	}
	for _, key := range []string{"landingCommit", "landingCheckpointTreeSha", "missionReviewOutcome"} {
		if _, present := raw[key]; present {
			t.Fatalf("key %q present despite empty value", key)
		}
	}
}

// TestBuildStatusReplyBoundsTaskRows proves the task row bound is enforced
// before the byte cap, so a run with an unbounded task list fails clearly
// instead of overflowing the reply.
func TestBuildStatusReplyBoundsTaskRows(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Date(2026, 9, 30, 17, 13, 48, 0, time.UTC)
	tasks := make([]Task, statusMaxTaskRows+1)
	for i := range tasks {
		tasks[i] = Task{ID: "T-" + itoa(i), Objective: "work", Status: StatusQueued}
	}
	mission := Mission{Name: "many tasks", Objective: "bound check", Tasks: tasks}
	state := NewState("status-many", mission, "/repo", "/repo/.stint-deep/status-many", now.Add(time.Hour), now.Add(50*time.Minute), 2, now)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatalf("BeginNewRun: %v", err)
	}
	_, err := BuildStatusReply(stateDir, state.SessionID)
	if err == nil {
		t.Fatalf("BuildStatusReply accepted %d task rows, want the bound to fail clearly", len(tasks))
	}
	if !containsString(err.Error(), "bounded to") {
		t.Fatalf("error %q does not name the bound", err)
	}
}

func itoa(i int) string {
	return fmt.Sprint(i)
}

func containsString(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
