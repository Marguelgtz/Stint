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
		Phase: "executing", WorkerName: "hermes on GPU", Coordinator: "running (pid 42)",
		Now: now, LandBefore: now.Add(time.Hour), ActiveSince: &started,
		Tasks:   []Task{{ID: "PLAN-001", Status: "verified", Attempts: 1, Objective: "plan"}, {ID: "VANTA-001", Status: "active", Attempts: 1, Objective: "implement"}},
		Compute: Compute{Available: true, Status: "READY", Endpoint: "healthy", Runtime: "running", Utilization: "80%"},
		Worker:  Worker{Observed: true, NInferContext: 262144, NInferKV: 262144, NInferDefaultMaxTokens: 262144, MediumRequests: 2, Compression: "completed"},
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
	if !strings.Contains(out, "truncated") || !strings.Contains(out, "completion budget 262144") {
		t.Fatalf("worker render missing evidence:\n%s", out)
	}
}

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
