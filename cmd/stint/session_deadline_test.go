package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/router"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func deadlineTestState(now time.Time) sessionstate.State {
	return sessionstate.State{
		InstanceID: 123,
		Profile:    "interactive",
		HourlyUSD:  0.40,
		Hours:      1,
		StartedAt:  now.Add(-10 * time.Minute),
		Deadline:   now.Add(50 * time.Minute),
		Status:     sessionstate.StatusReady,
	}
}

func TestParseSessionDuration(t *testing.T) {
	for input, want := range map[string]time.Duration{
		"15m":   15 * time.Minute,
		"1h":    time.Hour,
		"1h30m": 90 * time.Minute,
	} {
		got, err := parseSessionDuration(input)
		if err != nil {
			t.Fatalf("parse %q: %v", input, err)
		}
		if got != want {
			t.Fatalf("parse %q = %s, want %s", input, got, want)
		}
	}
	for _, input := range []string{"", "0", "-1m", "tomorrow"} {
		if _, err := parseSessionDuration(input); err == nil {
			t.Fatalf("parse %q unexpectedly succeeded", input)
		}
	}
}

func TestParseDeadlineMutationArgsAcceptsYesBeforeOrAfterDuration(t *testing.T) {
	for _, args := range [][]string{{"30m", "--yes"}, {"--yes", "30m"}} {
		delta, yes, err := parseDeadlineMutationArgs(deadlineExtend, args)
		if err != nil {
			t.Fatalf("parse %v: %v", args, err)
		}
		if delta != 30*time.Minute || !yes {
			t.Fatalf("parse %v = %s, yes=%v; want 30m, true", args, delta, yes)
		}
	}
}

func TestParseDeadlineMutationArgsRejectsUnknownOrExtraArguments(t *testing.T) {
	for _, args := range [][]string{{"30m", "--force"}, {"30m", "15m"}, {"--yes"}} {
		if _, _, err := parseDeadlineMutationArgs(deadlineExtend, args); err == nil {
			t.Fatalf("parse %v unexpectedly succeeded", args)
		}
	}
}

func TestBuildExtendPreview(t *testing.T) {
	now := time.Date(2026, 8, 31, 7, 10, 0, 0, time.UTC)
	state := deadlineTestState(now)
	preview, err := buildDeadlineMutationPreview(state, now, deadlineExtend, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := preview.NewDeadline, state.Deadline.Add(30*time.Minute); !got.Equal(want) {
		t.Fatalf("new deadline = %s, want %s", got, want)
	}
	if preview.ExposureDeltaUSD < 0.199 || preview.ExposureDeltaUSD > 0.201 {
		t.Fatalf("additional exposure = %.4f, want about 0.20", preview.ExposureDeltaUSD)
	}
	if preview.ProjectedUSD < 0.599 || preview.ProjectedUSD > 0.601 {
		t.Fatalf("projected = %.4f, want about 0.60", preview.ProjectedUSD)
	}
}

func TestBuildShortenPreview(t *testing.T) {
	now := time.Date(2026, 8, 31, 7, 10, 0, 0, time.UTC)
	state := deadlineTestState(now)
	preview, err := buildDeadlineMutationPreview(state, now, deadlineShorten, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ExposureDeltaUSD >= 0 {
		t.Fatalf("exposure delta = %.4f, want negative", preview.ExposureDeltaUSD)
	}
	if got, want := preview.NewDeadline, state.Deadline.Add(-15*time.Minute); !got.Equal(want) {
		t.Fatalf("new deadline = %s, want %s", got, want)
	}
}

func TestDeadlinePreviewRejectsInvalidHourlyRate(t *testing.T) {
	now := time.Date(2026, 8, 31, 7, 10, 0, 0, time.UTC)
	state := deadlineTestState(now)
	state.HourlyUSD = 0
	if _, err := buildDeadlineMutationPreview(state, now, deadlineExtend, 30*time.Minute); err == nil {
		t.Fatal("expected invalid hourly rate to fail")
	}
}

func TestExtendRespectsProfileCostCeiling(t *testing.T) {
	now := time.Date(2026, 8, 31, 7, 0, 0, 0, time.UTC)
	state := sessionstate.State{
		InstanceID: 123,
		Profile:    "interactive",
		HourlyUSD:  0.40,
		StartedAt:  now,
		Deadline:   now.Add(6 * time.Hour), // $2.40 scheduled
	}
	_, err := buildDeadlineMutationPreview(state, now, deadlineExtend, time.Hour)
	if err == nil {
		t.Fatal("expected extension beyond cost ceiling to fail")
	}
	if !strings.Contains(err.Error(), "$2.50") || !strings.Contains(err.Error(), "maximum additional duration") {
		t.Fatalf("error = %q, want ceiling and max-duration guidance", err)
	}
}

func TestMaxAdditionalDurationUsesProfileCeiling(t *testing.T) {
	now := time.Date(2026, 8, 31, 7, 0, 0, 0, time.UTC)
	state := sessionstate.State{
		Profile:   "interactive",
		HourlyUSD: 0.40,
		StartedAt: now,
		Deadline:  now.Add(6 * time.Hour),
	}
	profile, err := router.ResolveProfile("interactive")
	if err != nil {
		t.Fatal(err)
	}
	got := maxAdditionalDuration(state, profile)
	if got < 14*time.Minute+59*time.Second || got > 15*time.Minute+time.Second {
		t.Fatalf("max additional = %s, want about 15m", got)
	}
}

func TestDeadlineCommandsPersistUpdatedDeadline(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))

	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state := sessionstate.State{
		InstanceID:  123,
		Profile:     "interactive",
		GPUModel:    "RTX_4090",
		HourlyUSD:   0.40,
		Hours:       1,
		StartedAt:   now,
		Deadline:    now.Add(time.Hour),
		WatchdogPID: os.Getpid(), // test process stands in for a live watchdog
		Status:      sessionstate.StatusReady,
	}
	if err := sessionstate.Save(paths, state); err != nil {
		t.Fatal(err)
	}
	originalDeadline := state.Deadline

	if err := runExtend([]string{"30m", "--yes"}); err != nil {
		t.Fatalf("extend: %v", err)
	}
	extended, err := sessionstate.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if want := originalDeadline.Add(30 * time.Minute); !extended.Deadline.Equal(want) {
		t.Fatalf("extended deadline = %s, want %s", extended.Deadline, want)
	}
	if extended.Hours < 1.499 || extended.Hours > 1.501 {
		t.Fatalf("extended Hours = %.4f, want 1.5", extended.Hours)
	}
	if extended.WatchdogPID != os.Getpid() {
		t.Fatalf("watchdog pid changed from live test process: %d", extended.WatchdogPID)
	}

	if err := runShorten([]string{"--yes", "10m"}); err != nil {
		t.Fatalf("shorten: %v", err)
	}
	shortened, err := sessionstate.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if want := originalDeadline.Add(20 * time.Minute); !shortened.Deadline.Equal(want) {
		t.Fatalf("shortened deadline = %s, want %s", shortened.Deadline, want)
	}
	if shortened.Hours < 1.332 || shortened.Hours > 1.334 {
		t.Fatalf("shortened Hours = %.4f, want about 1.333", shortened.Hours)
	}
}

func TestFormatSessionDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0:                            "0s",
		42 * time.Second:             "42s",
		time.Hour + 2*time.Minute:    "1h2m",
		25*time.Hour + 3*time.Minute: "1d1h3m",
	}
	for input, want := range cases {
		if got := formatSessionDuration(input); got != want {
			t.Fatalf("format %s = %q, want %q", input, got, want)
		}
	}
}
