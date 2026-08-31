package main

import (
	"testing"
	"time"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestBuildSessionSnapshot(t *testing.T) {
	started := time.Date(2026, 8, 31, 6, 0, 0, 0, time.UTC)
	state := sessionstate.State{
		InstanceID: 1,
		HourlyUSD:  0.40,
		StartedAt:  started,
		Deadline:   started.Add(2 * time.Hour),
	}
	snapshot := buildSessionSnapshot(state, started.Add(45*time.Minute))
	if snapshot.Elapsed != 45*time.Minute {
		t.Fatalf("elapsed = %s, want 45m", snapshot.Elapsed)
	}
	if snapshot.Remaining != 75*time.Minute {
		t.Fatalf("remaining = %s, want 75m", snapshot.Remaining)
	}
	if snapshot.Expired {
		t.Fatal("snapshot unexpectedly expired")
	}
	if snapshot.EstimatedSpentUSD < 0.299 || snapshot.EstimatedSpentUSD > 0.301 {
		t.Fatalf("spent estimate = %.4f, want about 0.30", snapshot.EstimatedSpentUSD)
	}
	if snapshot.ScheduledExposure < 0.799 || snapshot.ScheduledExposure > 0.801 {
		t.Fatalf("scheduled exposure = %.4f, want about 0.80", snapshot.ScheduledExposure)
	}
}

func TestBuildSessionSnapshotExpired(t *testing.T) {
	started := time.Date(2026, 8, 31, 6, 0, 0, 0, time.UTC)
	state := sessionstate.State{InstanceID: 1, StartedAt: started, Deadline: started.Add(time.Hour)}
	snapshot := buildSessionSnapshot(state, started.Add(2*time.Hour))
	if !snapshot.Expired {
		t.Fatal("snapshot should be expired")
	}
	if snapshot.Remaining != 0 {
		t.Fatalf("remaining = %s, want 0", snapshot.Remaining)
	}
	if snapshot.Elapsed != time.Hour {
		t.Fatalf("elapsed = %s, want scheduled 1h", snapshot.Elapsed)
	}
}
