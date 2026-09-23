package deepdashboard

import (
	"strings"
	"testing"
	"time"
)

func testModel() Model {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	started := now.Add(-5 * time.Minute)
	return Model{
		Width: 100, Height: 40, NoColor: true, SessionID: "20260908-120000", Mission: "CP1",
		Phase: "executing", WorkerName: "hermes on GPU", Coordinator: "running (pid 42)", Latest: true, CanLand: true,
		Now: now, LandBefore: now.Add(time.Hour), ActiveSince: &started,
		Tasks:   []Task{{ID: "PLAN-001", Status: "verified", Attempts: 1, Objective: "plan"}, {ID: "VANTA-001", Status: "active", Attempts: 1, Objective: "implement"}},
		Compute: Compute{Available: true, Status: "READY", Endpoint: "healthy", Runtime: "running", Utilization: "80%"},
		Worker:  Worker{Observed: true, Scope: "Shared host logs; not attributed to this session", NInferContext: 262144, NInferKV: 262144, NInferDefaultMaxTokens: 262144, MediumRequests: 2, Compression: "completed"},
	}
}

func TestRunViewIncludesTruthfulProgress(t *testing.T) {
	out := Render(testModel())
	for _, want := range []string{"EXECUTING", "VANTA-001", "1 verified", "completed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
}

func TestWorkerViewShowsTruncation(t *testing.T) {
	m := testModel()
	m.View = WorkerView
	m.Worker.Compression = "truncated"
	out := Render(m)
	if !strings.Contains(out, "truncated") || !strings.Contains(out, "completion budget 262144") || !strings.Contains(out, "not attributed to this session") {
		t.Fatalf("worker render missing evidence:\n%s", out)
	}
}

func TestTasksViewShowsVerificationTimeAndCheckpointSHA(t *testing.T) {
	m := testModel()
	m.View = Tasks
	m.Tasks = []Task{{ID: "TASK-001", Status: "verified", Attempts: 1, Objective: "ship", Verify: "go test ./...", VerifiedAt: "2026-09-23T10:00:00Z", CheckpointCommit: "0123456789abcdef0123456789abcdef01234567"}}
	out := Render(m)
	for _, want := range []string{"TASK-001", "verified at 2026-09-23T10:00:00Z", "checkpoint SHA 0123456789abcdef0123456789abcdef01234567", "verify go test ./..."} {
		if !strings.Contains(out, want) {
			t.Fatalf("tasks view missing %q:\n%s", want, out)
		}
	}
}

func TestPhaseViewShowsHistoricalVerificationAndLandingEvidence(t *testing.T) {
	m := testModel()
	m.View = PhaseDetails
	m.Latest = false
	m.CanLand = false
	m.Branch = "stint/deep-20260923"
	m.BaseCommit = "base123"
	m.ComputeBinding = "vast instance 42"
	m.Verify = "go test ./..."
	m.LandingReason = "operator stop"
	m.LandingVerifyDone = true
	m.LandingVerify = "passed\nall tests passed"
	m.LandingCommit = "abcdef0123456789abcdef0123456789abcdef01"
	m.LandingHandoff = "full handoff content is present"
	m.LandedAt = ptrTime(time.Date(2026, 9, 23, 11, 0, 0, 0, time.UTC))
	m.Tasks = []Task{{ID: "TASK-001", Status: "verified", VerifiedAt: "2026-09-23T10:00:00Z", CheckpointCommit: "0123456789abcdef0123456789abcdef01234567"}}
	out := Render(m)
	for _, want := range []string{"PHASE DETAILS", "historical session · landing disabled", "Base commit      base123", "Compute binding  vast instance 42", "TASK-001 · verified · verified at 2026-09-23T10:00:00Z", "checkpoint SHA 0123456789abcdef0123456789abcdef01234567", "Final verify     passed", "Landing commit   abcdef0123456789abcdef0123456789abcdef01", "Handoff          written", "Landed at"} {
		if !strings.Contains(out, want) {
			t.Fatalf("phase view missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "full handoff content") {
		t.Fatalf("phase view should report handoff presence without rendering its contents:\n%s", out)
	}
}

func TestPhaseViewFitsViewport(t *testing.T) {
	m := testModel()
	m.View, m.Height = PhaseDetails, 14
	m.Tasks = make([]Task, 20)
	for i := range m.Tasks {
		m.Tasks[i] = Task{ID: "TASK", Status: "verified", CheckpointCommit: "0123456789abcdef"}
	}
	if got := len(strings.Split(Render(m), "\n")); got > m.Height {
		t.Fatalf("phase view rendered %d lines for %d-row viewport", got, m.Height)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }

func TestActivityViewBoundsToViewport(t *testing.T) {
	m := testModel()
	m.View, m.Height = Activity, 12
	for i := 0; i < 30; i++ {
		m.Events = append(m.Events, Event{Time: "12:00", Kind: "verify-run", Task: "T", Detail: "pass"})
	}
	if got := len(strings.Split(Render(m), "\n")); got > m.Height {
		t.Fatalf("rendered %d lines for %d-row viewport", got, m.Height)
	}
}
