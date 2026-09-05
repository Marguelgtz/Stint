package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

// scratchStintProfile points config/state resolution at a temp dir so tests
// never touch the operator's real session state.
func scratchStintProfile(t *testing.T) config.Paths {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths: %v", err)
	}
	return paths
}

func TestStaleStateWarningSymmetricTunnelDown(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	with := func(status string, remaining time.Duration, updatedAt time.Time) sessionstate.State {
		state := sessionstate.State{InstanceID: 1, Status: status}
		state.Deadline = now.Add(remaining)
		state.UpdatedAt = updatedAt
		return state
	}

	cases := []struct {
		name          string
		state         sessionstate.State
		tunnelRunning bool
		wantContains  string // empty means the warning must stay silent
	}{
		{"tunnel down, claims READY, deadline close, stale state", with(sessionstate.StatusReady, 10*time.Minute, now.Add(-40*time.Minute)), false, "tunnel is not running"},
		{"tunnel down, claims RECOVERABLE, deadline close", with(sessionstate.StatusRecoverable, -5*time.Minute, now.Add(-40*time.Minute)), false, "RECOVERABLE"},
		{"tunnel down, claims READY, deadline far, stale state", with(sessionstate.StatusReady, 2*time.Hour, now.Add(-40*time.Minute)), false, ""},
		{"tunnel down, claims READY, deadline close, fresh state", with(sessionstate.StatusReady, 10*time.Minute, now.Add(-30*time.Second)), false, ""},
		{"tunnel down, claims BOOTING (in-progress start), deadline close", with(sessionstate.StatusBooting, 10*time.Minute, now.Add(-40*time.Minute)), false, ""},
		{"tunnel down, no deadline", with(sessionstate.StatusReady, 0, now.Add(-40*time.Minute)), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A BOOTING state has no deadline; the no-deadline case sets one
			// explicitly via zero duration above.
			if tc.name == "tunnel down, no deadline" {
				tc.state.Deadline = time.Time{}
			}
			got := staleStateWarning(tc.state, tc.tunnelRunning, now)
			if tc.wantContains == "" {
				if got != "" {
					t.Fatalf("staleStateWarning = %q, want none", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantContains) {
				t.Fatalf("staleStateWarning = %q, want containing %q", got, tc.wantContains)
			}
		})
	}
}

func TestStateAgeSeconds(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		state sessionstate.State
		want  float64
	}{
		{"unwritten state", sessionstate.State{UpdatedAt: now.Add(-40 * time.Minute)}, 2400},
		{"never updated, session started 1h ago", sessionstate.State{StartedAt: now.Add(-time.Hour)}, 3600},
		{"never updated, no start time", sessionstate.State{}, 0},
		{"updatedAt in the future", sessionstate.State{UpdatedAt: now.Add(time.Minute)}, 0},
		{"fresh write", sessionstate.State{UpdatedAt: now.Add(-30 * time.Second)}, 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stateAgeSeconds(tc.state, now); got != tc.want {
				t.Fatalf("stateAgeSeconds = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCollectSessionSnapshotPopulatesStaleness(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	with := func(status string, remaining time.Duration, updatedAt time.Time) sessionstate.State {
		state := sessionstate.State{InstanceID: 1, Status: status, TunnelPID: 10}
		state.Deadline = now.Add(remaining)
		state.UpdatedAt = updatedAt
		return state
	}
	deps := func(tunnelRunning bool) snapshotProbeDeps {
		return snapshotProbeDeps{
			processRunning: func(pid int) bool { return tunnelRunning && pid > 0 },
			endpoint:       func(context.Context) endpointHealth { return endpointHealth{Refreshed: true} },
			remote: func(context.Context, config.Paths, sessionstate.State) remoteTelemetrySample {
				return remoteTelemetrySample{}
			},
			inference:   func(context.Context) inferenceTelemetry { return inferenceTelemetry{} },
			performance: func(config.Paths, sessionstate.State, time.Time) performanceSnapshot { return performanceSnapshot{} },
		}
	}

	cases := []struct {
		name              string
		state             sessionstate.State
		tunnelRunning     bool
		wantDeadlineStale bool
	}{
		{"tunnel down, READY, close deadline, stale", with(sessionstate.StatusReady, 10*time.Minute, now.Add(-40*time.Minute)), false, true},
		{"tunnel down, READY, far deadline, stale", with(sessionstate.StatusReady, 2*time.Hour, now.Add(-40*time.Minute)), false, false},
		{"tunnel down, READY, close deadline, fresh", with(sessionstate.StatusReady, 10*time.Minute, now.Add(-30*time.Second)), false, false},
		{"tunnel up, READY, close deadline, stale", with(sessionstate.StatusReady, 10*time.Minute, now.Add(-40*time.Minute)), true, true},
		{"tunnel up, READY, far deadline, stale", with(sessionstate.StatusReady, 2*time.Hour, now.Add(-40*time.Minute)), true, false},
		{"tunnel down, BOOTING, close deadline, stale", with(sessionstate.StatusBooting, 10*time.Minute, now.Add(-40*time.Minute)), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := collectSessionSnapshot(context.Background(), scratchStintProfile(t), tc.state, now, false, deps(tc.tunnelRunning))
			if snapshot.Staleness.DeadlineStale != tc.wantDeadlineStale {
				t.Fatalf("DeadlineStale = %v, want %v (tunnelRunning=%v, status=%s)", snapshot.Staleness.DeadlineStale, tc.wantDeadlineStale, tc.tunnelRunning, tc.state.Status)
			}
			if !tc.state.UpdatedAt.IsZero() {
				want := float64(now.Sub(tc.state.UpdatedAt).Seconds())
				if snapshot.Staleness.StateAgeSeconds < want-0.01 || snapshot.Staleness.StateAgeSeconds > want+0.01 {
					t.Fatalf("StateAgeSeconds = %v, want about %v", snapshot.Staleness.StateAgeSeconds, want)
				}
			}
		})
	}
}

func TestStatusJSONExposesStalenessDomain(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	state := sessionstate.State{InstanceID: 1, Status: sessionstate.StatusReady, TunnelPID: 10, Deadline: now.Add(10 * time.Minute), UpdatedAt: now.Add(-40 * time.Minute)}
	deps := snapshotProbeDeps{
		processRunning: func(pid int) bool { return false },
		endpoint:       func(context.Context) endpointHealth { return endpointHealth{Refreshed: true} },
		remote: func(context.Context, config.Paths, sessionstate.State) remoteTelemetrySample {
			return remoteTelemetrySample{}
		},
		inference:   func(context.Context) inferenceTelemetry { return inferenceTelemetry{} },
		performance: func(config.Paths, sessionstate.State, time.Time) performanceSnapshot { return performanceSnapshot{} },
	}
	snapshot := collectSessionSnapshot(context.Background(), scratchStintProfile(t), state, now, false, deps)
	encoded, err := json.Marshal(snapshotJSON(snapshot))
	if err != nil {
		t.Fatalf("marshal snapshot JSON: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	staleness, ok := decoded["staleness"].(map[string]any)
	if !ok {
		t.Fatalf("staleness domain missing from JSON: %s", encoded)
	}
	if staleness["deadlineStale"] != true {
		t.Fatalf("deadlineStale = %v, want true", staleness["deadlineStale"])
	}
	if age, ok := staleness["stateAgeSeconds"].(float64); !ok || age < 2399 || age > 2401 {
		t.Fatalf("stateAgeSeconds = %v, want about 2400", staleness["stateAgeSeconds"])
	}
}

// ageStateFile rewrites the session state file with the given mutation,
// bypassing sessionstate.Save (which would re-stamp updatedAt to now).
func ageStateFile(t *testing.T, paths config.Paths, mutate func(*sessionstate.State)) {
	t.Helper()
	data, err := os.ReadFile(sessionstate.Path(paths))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var state sessionstate.State
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("parse state file: %v", err)
	}
	mutate(&state)
	encoded, err := json.MarshalIndent(&state, "", "  ")
	if err != nil {
		t.Fatalf("encode state file: %v", err)
	}
	if err := os.WriteFile(sessionstate.Path(paths), encoded, 0o600); err != nil {
		t.Fatalf("write state file: %v", err)
	}
}

func TestRunStatusDeadSessionRenderPaths(t *testing.T) {
	t.Run("no recorded session", func(t *testing.T) {
		paths := scratchStintProfile(t)
		if err := paths.Ensure(); err != nil {
			t.Fatalf("Ensure: %v", err)
		}
		out := captureStdout(t, func() {
			if err := runStatus(); err != nil {
				t.Fatalf("runStatus: %v", err)
			}
		})
		if !strings.Contains(out, "Active compute     none") {
			t.Fatalf("missing no-session line in output:\n%s", out)
		}
	})

	t.Run("recoverable session shows resume next action", func(t *testing.T) {
		paths := scratchStintProfile(t)
		state := sessionstate.State{
			InstanceID: 1, Status: sessionstate.StatusRecoverable, Checkpoint: sessionstate.CheckpointRuntimeReady,
			Deadline: time.Now().UTC().Add(30 * time.Minute),
		}
		state.UpdatedAt = time.Now().UTC()
		if err := sessionstate.Save(paths, state); err != nil {
			t.Fatalf("Save: %v", err)
		}
		out := captureStdout(t, func() {
			if err := runStatus(); err != nil {
				t.Fatalf("runStatus: %v", err)
			}
		})
		if !strings.Contains(out, "Next action        stint resume") {
			t.Fatalf("missing resume next-action line in output:\n%s", out)
		}
	})

	t.Run("stale state claims READY with tunnel down prints freshness hint", func(t *testing.T) {
		paths := scratchStintProfile(t)
		now := time.Now().UTC()
		state := sessionstate.State{
			InstanceID: 1, Status: sessionstate.StatusReady, TunnelPID: 999999,
			Deadline: now.Add(10 * time.Minute),
		}
		if err := sessionstate.Save(paths, state); err != nil {
			t.Fatalf("Save: %v", err)
		}
		// Save() stamps a fresh updatedAt; age it directly on the file so
		// the state looks 40 minutes unwritten.
		ageStateFile(t, paths, func(s *sessionstate.State) { s.UpdatedAt = now.Add(-40 * time.Minute) })
		out := captureStdout(t, func() {
			if err := runStatus(); err != nil {
				t.Fatalf("runStatus: %v", err)
			}
		})
		if !strings.Contains(out, "State freshness") {
			t.Fatalf("missing State freshness hint in output:\n%s", out)
		}
		if !strings.Contains(out, "stint status --refresh") {
			t.Fatalf("missing --refresh remedy in output:\n%s", out)
		}
	})

	t.Run("healthy long session with tunnel up prints no freshness hint", func(t *testing.T) {
		paths := scratchStintProfile(t)
		now := time.Now().UTC()
		state := sessionstate.State{
			InstanceID: 1, Status: sessionstate.StatusReady, TunnelPID: os.Getpid(),
			Deadline: now.Add(2 * time.Hour),
		}
		state.UpdatedAt = now.Add(-30 * time.Second)
		if err := sessionstate.Save(paths, state); err != nil {
			t.Fatalf("Save: %v", err)
		}
		out := captureStdout(t, func() {
			if err := runStatus(); err != nil {
				t.Fatalf("runStatus: %v", err)
			}
		})
		if strings.Contains(out, "State freshness") {
			t.Fatalf("false-positive freshness hint on a healthy session:\n%s", out)
		}
	})
}
