package main

import (
	"os"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestSessionStateFreshnessWarningConditions(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	paths := config.Paths{StateDir: t.TempDir()}
	tests := []struct {
		name          string
		state         sessionstate.State
		tunnelRunning bool
		wantWarning   bool
	}{
		{
			name:          "stale state with live tunnel",
			state:         sessionstate.State{UpdatedAt: now.Add(-3 * time.Minute), Deadline: now.Add(10 * time.Minute), Status: sessionstate.StatusReady},
			tunnelRunning: true, wantWarning: true,
		},
		{
			name:        "stale ready state with tunnel down",
			state:       sessionstate.State{UpdatedAt: now.Add(-3 * time.Minute), Deadline: now.Add(10 * time.Minute), Status: sessionstate.StatusReady},
			wantWarning: true,
		},
		{
			name:  "stale booting state with tunnel down",
			state: sessionstate.State{UpdatedAt: now.Add(-3 * time.Minute), Deadline: now.Add(10 * time.Minute), Status: sessionstate.StatusBooting},
		},
		{
			name:          "fresh state near deadline",
			state:         sessionstate.State{UpdatedAt: now.Add(-time.Minute), Deadline: now.Add(10 * time.Minute), Status: sessionstate.StatusReady},
			tunnelRunning: true,
		},
		{
			name:          "stale state with distant deadline",
			state:         sessionstate.State{UpdatedAt: now.Add(-3 * time.Minute), Deadline: now.Add(time.Hour), Status: sessionstate.StatusReady},
			tunnelRunning: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionStateFreshness(paths, tc.state, tc.tunnelRunning, now)
			if (got.Warning != "") != tc.wantWarning || got.DeadlineStale != tc.wantWarning {
				t.Fatalf("freshness = %+v, want warning=%v", got, tc.wantWarning)
			}
		})
	}
}

func TestSessionStateFreshnessUsesFileTimeWhenUpdatedAtMissing(t *testing.T) {
	now := time.Now().UTC()
	paths := config.Paths{StateDir: t.TempDir()}
	path := sessionstate.Path(paths)
	if err := os.WriteFile(path, []byte(`{"instanceId":42}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-4 * time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	state := sessionstate.State{Deadline: now.Add(5 * time.Minute), Status: sessionstate.StatusReady}
	got := sessionStateFreshness(paths, state, true, now)
	if got.Age < 3*time.Minute || !got.DeadlineStale {
		t.Fatalf("file modification time fallback not applied: %+v", got)
	}
}

func TestSessionStateFreshnessClampsFutureTimestamp(t *testing.T) {
	now := time.Now().UTC()
	got := sessionStateFreshness(config.Paths{StateDir: t.TempDir()}, sessionstate.State{UpdatedAt: now.Add(time.Hour)}, true, now)
	if got.Age != 0 || got.DeadlineStale {
		t.Fatalf("future state timestamp should have a finite zero age: %+v", got)
	}
}
