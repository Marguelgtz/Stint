package deep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleMission = `# demo-mission

## Objective
Ship a small example.

## Success
- The example builds.
- Tests pass.

## Constraints
- Do not touch infra/.

## Verification
go test ./...

## Tasks
- [ ] T1: Add the example file
  - acceptance: example.go exists and compiles
- [ ] T2: Add a test for it
  - acceptance: go test reports ok
`

func TestParseMissionFull(t *testing.T) {
	m, err := ParseMission(sampleMission)
	if err != nil {
		t.Fatalf("ParseMission: %v", err)
	}
	if m.Name != "demo-mission" {
		t.Errorf("name = %q", m.Name)
	}
	if !strings.Contains(m.Objective, "Ship a small example") {
		t.Errorf("objective = %q", m.Objective)
	}
	if len(m.Success) != 2 || m.Success[0] != "The example builds." {
		t.Errorf("success = %v", m.Success)
	}
	if len(m.Constraints) != 1 || m.Constraints[0] != "Do not touch infra/." {
		t.Errorf("constraints = %v", m.Constraints)
	}
	if m.Verify != "go test ./..." {
		t.Errorf("verify = %q", m.Verify)
	}
	if len(m.Tasks) != 2 {
		t.Fatalf("tasks = %d", len(m.Tasks))
	}
	if m.Tasks[0].ID != "T1" || m.Tasks[0].Acceptance != "example.go exists and compiles" {
		t.Errorf("task T1 = %+v", m.Tasks[0])
	}
	if m.Tasks[1].Status != StatusQueued || m.Tasks[1].Source != "mission" {
		t.Errorf("task T2 = %+v", m.Tasks[1])
	}
}

func TestParseMissionRejectsMissingObjective(t *testing.T) {
	if _, err := ParseMission("# x\n\n## Tasks\n- [ ] T1: do it\n"); err == nil {
		t.Fatal("expected error for missing objective")
	}
}

func TestParseMissionRejectsNoTasks(t *testing.T) {
	if _, err := ParseMission("# x\n\n## Objective\no\n"); err == nil {
		t.Fatal("expected error for missing tasks")
	}
}

func TestParseMissionRejectsDuplicateIDs(t *testing.T) {
	_, err := ParseMission("# x\n\n## Objective\no\n\n## Tasks\n- [ ] T1: a\n- [ ] T1: b\n")
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate ID error, got %v", err)
	}
}

func TestParseMissionFencedVerification(t *testing.T) {
	m, err := ParseMission("# x\n\n## Objective\no\n\n## Verification\n```\nmake check\n```\n\n## Tasks\n- [ ] T1: a\n")
	if err != nil {
		t.Fatalf("ParseMission: %v", err)
	}
	if m.Verify != "make check" {
		t.Errorf("verify = %q", m.Verify)
	}
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 2, 15, 0, 0, 0, time.UTC)
	m, err := ParseMission(sampleMission)
	if err != nil {
		t.Fatal(err)
	}
	deadline := now.Add(2 * time.Hour)
	landBefore := deadline.Add(-10 * time.Minute)
	state := NewState(NewSessionID(now), m, "/repo", "/worktree", deadline, landBefore, 3, now)

	if err := state.SaveDir(dir); err != nil {
		t.Fatalf("SaveDir: %v", err)
	}

	// Latest pointer resolves to this session.
	loaded, err := LoadLatestState(dir)
	if err != nil {
		t.Fatalf("LoadLatestState: %v", err)
	}
	if loaded.SessionID != state.SessionID {
		t.Errorf("session = %q want %q", loaded.SessionID, state.SessionID)
	}
	if loaded.Branch != BranchName(state.SessionID) {
		t.Errorf("branch = %q", loaded.Branch)
	}
	if len(loaded.Tasks) != 2 || loaded.Tasks[0].Status != StatusQueued {
		t.Errorf("tasks = %+v", loaded.Tasks)
	}
	if loaded.Deadline != deadline || loaded.LandBefore != landBefore {
		t.Errorf("deadline/landBefore = %v / %v", loaded.Deadline, loaded.LandBefore)
	}

	// State file is owner-only.
	info, err := os.Stat(filepath.Join(DeepDir(dir, state.SessionID), "deep.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode())
	}

	// Mutate and re-save; reload reflects the change.
	loaded.Tasks[0].Status = StatusVerified
	if err := loaded.SaveDir(dir); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	again, err := LoadState(dir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Tasks[0].Status != StatusVerified {
		t.Errorf("status = %q", again.Tasks[0].Status)
	}
}

func TestStatusTerminal(t *testing.T) {
	for _, s := range []Status{StatusVerified, StatusBlocked, StatusNeedsHuman, StatusDropped} {
		if !s.Terminal() {
			t.Errorf("%s should be terminal", s)
		}
	}
	for _, s := range []Status{StatusQueued, StatusActive, StatusIncomplete} {
		if s.Terminal() {
			t.Errorf("%s should not be terminal", s)
		}
	}
}

func TestBuildTaskPromptReconstruction(t *testing.T) {
	m, err := ParseMission(sampleMission)
	if err != nil {
		t.Fatal(err)
	}
	task := m.Tasks[1]
	task.Attempts = 2
	task.LastResult = "attempt 1: verify failed (example.go missing)"
	repo := RepoSummary{
		Branch:     "stint/deep-20260902-150000",
		HeadCommit: "abc1234",
		RecentLog:  "abc1234 base commit",
		DiffStat:   " example.go | 5 +++++\n 1 file changed",
		Changed:    "M example.go",
	}
	prompt := BuildTaskPrompt(m, task, 3, repo)
	for _, want := range []string{
		"MISSION: demo-mission",
		"OBJECTIVE: Ship a small example.",
		"CURRENT TASK: T2 (attempt 3)",
		"ACCEPTANCE: go test reports ok",
		"PREVIOUS ATTEMPT RESULT (attempt 2):",
		"attempt 1: verify failed (example.go missing)",
		"branch: stint/deep-20260902-150000",
		"head: abc1234",
		"uncommitted changes:",
		"Never push, open pull requests",
		"CONSTRAINTS:",
		"Do not touch infra/.",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q\nprompt:\n%s", want, prompt)
		}
	}
}
func TestExecSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	m, err := ParseMission(sampleMission)
	if err != nil {
		t.Fatal(err)
	}
	state := NewState(NewSessionID(now), m, "/repo", "/worktree", now.Add(time.Hour), now.Add(50*time.Minute), 3, now)
	state.Exec = &ExecSettings{AutoApprove: false, Provider: "openai-compatible", Model: "qwen3.8-27b", ClineConfig: "/cfg", TaskTimeoutSec: 900}
	if err := state.SaveDir(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(dir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Exec == nil {
		t.Fatal("Exec settings were not persisted")
	}
	if loaded.Exec.AutoApprove || loaded.Exec.Model != "qwen3.8-27b" || loaded.Exec.TaskTimeoutSec != 900 {
		t.Errorf("exec = %+v", loaded.Exec)
	}

	// A session started before the field existed loads with Exec == nil.
	legacy := NewState(NewSessionID(now.Add(time.Minute)), m, "/repo", "/worktree", now.Add(2*time.Hour), now.Add(time.Hour+50*time.Minute), 3, now)
	if err := legacy.SaveDir(dir); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(dir, legacy.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Exec != nil {
		t.Errorf("legacy state Exec = %+v, want nil (resume falls back to defaults)", got.Exec)
	}
}

func TestCoordinatorPidLiveness(t *testing.T) {
	dir := t.TempDir()
	if alive, pid := CoordinatorAlive(dir, "s1"); alive || pid != 0 {
		t.Fatalf("no pid file: alive=%v pid=%d, want none", alive, pid)
	}
	if err := WriteCoordinatorPid(dir, "s1", os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if alive, pid := CoordinatorAlive(dir, "s1"); !alive || pid != os.Getpid() {
		t.Fatalf("alive=%v pid=%d, want the running coordinator detected", alive, pid)
	}
	if err := WriteCoordinatorPid(dir, "s1", 999999999); err != nil {
		t.Fatal(err)
	}
	if alive, _ := CoordinatorAlive(dir, "s1"); alive {
		t.Fatal("stale pid file reported as a live coordinator")
	}
	if err := ClearCoordinatorPid(dir, "s1"); err != nil {
		t.Fatal(err)
	}
	if err := ClearCoordinatorPid(dir, "s1"); err != nil {
		t.Errorf("ClearCoordinatorPid twice: %v, want nil (missing file is fine)", err)
	}
	info, err := os.Stat(CoordinatorPidFile(dir, "s1"))
	if err == nil {
		t.Errorf("pid file still present: %v", info)
	}
}
