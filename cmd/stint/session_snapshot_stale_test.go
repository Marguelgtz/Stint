package main

import (
	"strings"
	"testing"
	"time"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestStaleStateWarning(t *testing.T) {
	now := time.Date(2026, 9, 4, 15, 40, 0, 0, time.UTC)
	withDeadline := func(remaining time.Duration, updatedAt time.Time) sessionstate.State {
		state := sessionstate.State{InstanceID: 1}
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
		{"fresh state, deadline close", withDeadline(10*time.Minute, now.Add(-30*time.Second)), true, ""},
		{"stale state, deadline close", withDeadline(10*time.Minute, now.Add(-40*time.Minute)), true, "refresh"},
		{"stale state, deadline far", withDeadline(2 * time.Hour, now.Add(-40 * time.Minute)), true, ""},
		{"stale state, tunnel down", withDeadline(10 * time.Minute, now.Add(-40 * time.Minute)), false, ""},
		{"stale state, deadline passed", withDeadline(-5 * time.Minute, now.Add(-40 * time.Minute)), true, "passed"},
		{"never written, deadline close", withDeadline(10*time.Minute, time.Time{}), true, "never"},
		{"no deadline", sessionstate.State{InstanceID: 1, UpdatedAt: time.Time{}}, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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

func TestHumanizeStateAge(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "less than a minute"},
		{90 * time.Second, "1 min"},
		{40 * time.Minute, "40 min"},
		{2*time.Hour + 5 * time.Minute, "2h05m"},
	}
	for _, tc := range cases {
		if got := humanizeStateAge(tc.in); got != tc.want {
			t.Fatalf("humanizeStateAge(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}