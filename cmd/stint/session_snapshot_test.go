package main

import (
	"testing"
	"time"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestBuildSessionSnapshot(t *testing.T) {
	started := time.Date(2026, 8, 31, 6, 0, 0, 0, time.UTC)
	state := sessionstate.State{
		InstanceID:    1,
		Profile:       "interactive",
		GPUModel:      "RTX_4090",
		Runtime:       runtimeNInfer,
		ContextTokens: 172032,
		HourlyUSD:     0.40,
		StartedAt:     started,
		Deadline:      started.Add(2 * time.Hour),
		Status:        sessionstate.StatusReady,
		TunnelPID:     111,
		WatchdogPID:   222,
	}
	snapshot := buildSessionSnapshot(state, started.Add(45*time.Minute))
	if snapshot.Session.InstanceID != 1 || snapshot.Session.Runtime != runtimeNInfer || snapshot.Session.ContextTokens != 172032 {
		t.Fatalf("unexpected session identity: %+v", snapshot.Session)
	}
	if snapshot.Time.Elapsed != 45*time.Minute {
		t.Fatalf("elapsed = %s, want 45m", snapshot.Time.Elapsed)
	}
	if snapshot.Time.Remaining != 75*time.Minute {
		t.Fatalf("remaining = %s, want 75m", snapshot.Time.Remaining)
	}
	if snapshot.Time.Expired {
		t.Fatal("snapshot unexpectedly expired")
	}
	if snapshot.Cost.EstimatedSpentUSD < 0.299 || snapshot.Cost.EstimatedSpentUSD > 0.301 {
		t.Fatalf("spent estimate = %.4f, want about 0.30", snapshot.Cost.EstimatedSpentUSD)
	}
	if snapshot.Cost.ScheduledUSD < 0.799 || snapshot.Cost.ScheduledUSD > 0.801 {
		t.Fatalf("scheduled exposure = %.4f, want about 0.80", snapshot.Cost.ScheduledUSD)
	}
	if snapshot.Health.Tunnel.PID != 111 || snapshot.Health.Watchdog.PID != 222 {
		t.Fatalf("process identifiers not copied into health domain: %+v", snapshot.Health)
	}
}

func TestBuildSessionSnapshotExpired(t *testing.T) {
	started := time.Date(2026, 8, 31, 6, 0, 0, 0, time.UTC)
	state := sessionstate.State{InstanceID: 1, HourlyUSD: 0.40, StartedAt: started, Deadline: started.Add(time.Hour)}
	snapshot := buildSessionSnapshot(state, started.Add(2*time.Hour))
	if !snapshot.Time.Expired {
		t.Fatal("snapshot should be expired")
	}
	if snapshot.Time.Remaining != 0 {
		t.Fatalf("remaining = %s, want 0", snapshot.Time.Remaining)
	}
	if snapshot.Time.Elapsed != 2*time.Hour {
		t.Fatalf("elapsed = %s, want actual wall time 2h", snapshot.Time.Elapsed)
	}
	if snapshot.Cost.EstimatedSpentUSD < 0.799 || snapshot.Cost.EstimatedSpentUSD > 0.801 {
		t.Fatalf("spent estimate = %.4f, want about 0.80", snapshot.Cost.EstimatedSpentUSD)
	}
	if snapshot.Cost.ScheduledUSD < 0.399 || snapshot.Cost.ScheduledUSD > 0.401 {
		t.Fatalf("scheduled exposure = %.4f, want about 0.40", snapshot.Cost.ScheduledUSD)
	}
}

func TestBuildSessionSnapshotDoesNotExposeRawState(t *testing.T) {
	now := time.Now().UTC()
	state := sessionstate.State{InstanceID: 77, Runtime: runtimeNInfer, ContextTokens: 172032, Deadline: now.Add(time.Hour)}
	snapshot := buildSessionSnapshot(state, now)
	state.InstanceID = 88
	state.ContextTokens = 1
	if snapshot.Session.InstanceID != 77 || snapshot.Session.ContextTokens != 172032 {
		t.Fatal("snapshot identity changed after source state mutation")
	}
}
