package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/deep"
)

func TestLoadDeepDashboardSnapshotDerivesActiveAttempt(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Now().UTC().Add(-time.Minute)
	state := deep.NewState("20260908-120000", deep.Mission{Name: "fixture", Objective: "ship", Tasks: []deep.Task{{ID: "T-001", Objective: "work", Status: deep.StatusActive}}}, "/repo", "/repo/.stint-deep/test", now.Add(time.Hour), now.Add(50*time.Minute), 2, now)
	if err := state.SaveDir(stateDir); err != nil {
		t.Fatal(err)
	}
	deep.AppendIncident(stateDir, state, deep.IncidentExecutorInvoke, "T-001", "attempt 1")
	snapshot, err := loadDeepDashboardSnapshot(stateDir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveSince == nil || len(snapshot.Events) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Coordinator != "not running" {
		t.Fatalf("coordinator = %q", snapshot.Coordinator)
	}
}

func TestParseDeepWorkerObservation(t *testing.T) {
	raw := `{"collectedAt":"2026-09-08T12:00:00Z","ninfer":{"running":true,"maxContext":262144,"kvCapacity":262144,"defaultMaxTokens":262144},"phaseRoutes":{"xhighRequests":2,"mediumRequests":8,"latestPhase":"medium","latestAt":"2026-09-08T11:59:58Z"},"compression":{"state":"completed","completed":1,"failed":0,"truncated":0,"lastAt":"2026-09-08T11:59:55Z"}}`
	worker, err := parseDeepWorkerObservation(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !worker.Observed || worker.Compression != "completed" || worker.MediumRequests != 8 || worker.NInferDefaultMaxTokens != 262144 {
		t.Fatalf("worker = %#v", worker)
	}
}

func TestParseDeepWorkerObservationRejectsStoppedNInfer(t *testing.T) {
	_, err := parseDeepWorkerObservation(`{"ninfer":{"running":false}}`)
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeepDashboardStopRequiresConfirmation(t *testing.T) {
	stateDir := t.TempDir()
	state := deep.NewState("20260908-120000", deep.Mission{Name: "fixture", Objective: "ship", Tasks: []deep.Task{{ID: "T-001", Objective: "work", Status: deep.StatusQueued}}}, "/repo", "/repo/.stint-deep/test", time.Now().Add(time.Hour), time.Now().Add(50*time.Minute), 2, time.Now())
	if err := state.SaveDir(stateDir); err != nil {
		t.Fatal(err)
	}
	controller := &deepDashboardController{paths: testDashboardPaths(stateDir)}
	controller.snapshot.State = state
	_, changed, land := controller.handleKey('s')
	if !changed || land || controller.model.Modal == nil {
		t.Fatalf("stop action changed=%t land=%t modal=%#v", changed, land, controller.model.Modal)
	}
	_, changed, land = controller.handleKey('S')
	if !changed || !land {
		t.Fatalf("confirmation changed=%t land=%t", changed, land)
	}
}

func testDashboardPaths(stateDir string) config.Paths {
	return config.Paths{StateDir: stateDir}
}

func TestDeepDashboardSnapshotMissingState(t *testing.T) {
	_, err := loadDeepDashboardSnapshot(t.TempDir(), "missing")
	if err == nil || !strings.Contains(err.Error(), "read deep state") {
		t.Fatalf("err = %v", err)
	}
}
