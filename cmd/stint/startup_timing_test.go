package main

import (
	"testing"
	"time"
)

func TestStartupDurationMeasuresStartToServing(t *testing.T) {
	started := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	serving := started.Add(3*time.Minute + 12*time.Second + 400*time.Millisecond)

	if got, want := startupDuration(started, serving), 3*time.Minute+12*time.Second+400*time.Millisecond; got != want {
		t.Fatalf("startupDuration = %s, want %s", got, want)
	}
	if got, want := formatStartupDuration(started, serving), "3m12s"; got != want {
		t.Fatalf("formatStartupDuration = %q, want %q", got, want)
	}
}

func TestStartupDurationRejectsInvalidBoundaries(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name    string
		started time.Time
		serving time.Time
	}{
		{name: "missing start", serving: now},
		{name: "missing serving", started: now},
		{name: "serving before start", started: now, serving: now.Add(-time.Second)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := startupDuration(tc.started, tc.serving); got != 0 {
				t.Fatalf("startupDuration = %s, want 0", got)
			}
		})
	}
}
