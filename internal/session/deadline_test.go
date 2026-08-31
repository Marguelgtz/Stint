package session

import (
	"strings"
	"testing"
	"time"
)

func TestRemainingClampsAtZero(t *testing.T) {
	now := time.Date(2026, 8, 31, 7, 0, 0, 0, time.UTC)
	state := State{Deadline: now.Add(30 * time.Minute)}
	if got := Remaining(state, now); got != 30*time.Minute {
		t.Fatalf("remaining = %s, want 30m", got)
	}
	if got := Remaining(state, state.Deadline); got != 0 {
		t.Fatalf("remaining at deadline = %s, want 0", got)
	}
	if got := Remaining(state, state.Deadline.Add(time.Second)); got != 0 {
		t.Fatalf("remaining after deadline = %s, want 0", got)
	}
}

func TestElapsedClampsToScheduledWindow(t *testing.T) {
	started := time.Date(2026, 8, 31, 6, 0, 0, 0, time.UTC)
	state := State{StartedAt: started, Deadline: started.Add(time.Hour)}
	if got := Elapsed(state, started.Add(15*time.Minute)); got != 15*time.Minute {
		t.Fatalf("elapsed = %s, want 15m", got)
	}
	if got := Elapsed(state, started.Add(2*time.Hour)); got != time.Hour {
		t.Fatalf("elapsed after deadline = %s, want 1h", got)
	}
}

func TestExtendDeadlineUsesCurrentDeadline(t *testing.T) {
	now := time.Date(2026, 8, 31, 7, 10, 0, 0, time.UTC)
	started := now.Add(-10 * time.Minute)
	state := State{StartedAt: started, Deadline: now.Add(50 * time.Minute), Hours: 1}
	change, err := ExtendDeadline(state, now, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	want := now.Add(80 * time.Minute)
	if !change.NewDeadline.Equal(want) {
		t.Fatalf("new deadline = %s, want %s", change.NewDeadline, want)
	}
	if change.NewDuration != 90*time.Minute {
		t.Fatalf("new duration = %s, want 90m", change.NewDuration)
	}
}

func TestShortenDeadlineUsesCurrentDeadline(t *testing.T) {
	now := time.Date(2026, 8, 31, 7, 10, 0, 0, time.UTC)
	started := now.Add(-10 * time.Minute)
	state := State{StartedAt: started, Deadline: now.Add(50 * time.Minute), Hours: 1}
	change, err := ShortenDeadline(state, now, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	want := now.Add(35 * time.Minute)
	if !change.NewDeadline.Equal(want) {
		t.Fatalf("new deadline = %s, want %s", change.NewDeadline, want)
	}
}

func TestDeadlineMutationsRejectInvalidDurations(t *testing.T) {
	now := time.Now().UTC()
	state := State{StartedAt: now, Deadline: now.Add(time.Hour)}
	if _, err := ExtendDeadline(state, now, 0); err == nil {
		t.Fatal("expected zero extension to fail")
	}
	if _, err := ExtendDeadline(state, now, -time.Minute); err == nil {
		t.Fatal("expected negative extension to fail")
	}
	if _, err := ShortenDeadline(state, now, 0); err == nil {
		t.Fatal("expected zero shortening to fail")
	}
}

func TestShortenDeadlineRejectsImmediateExpiry(t *testing.T) {
	now := time.Date(2026, 8, 31, 7, 0, 0, 0, time.UTC)
	state := State{StartedAt: now.Add(-time.Hour), Deadline: now.Add(10 * time.Minute)}
	_, err := ShortenDeadline(state, now, 10*time.Minute)
	if err == nil {
		t.Fatal("expected shortening to now to fail")
	}
	if !strings.Contains(err.Error(), "stint down") {
		t.Fatalf("error = %q, want stint down guidance", err)
	}
}

func TestMutationsRejectExpiredSession(t *testing.T) {
	now := time.Date(2026, 8, 31, 7, 0, 0, 0, time.UTC)
	state := State{StartedAt: now.Add(-time.Hour), Deadline: now}
	if _, err := ExtendDeadline(state, now, time.Minute); err == nil {
		t.Fatal("expected extend on expired session to fail")
	}
	if _, err := ShortenDeadline(state, now, time.Minute); err == nil {
		t.Fatal("expected shorten on expired session to fail")
	}
}

func TestWithDeadlineKeepsHoursAligned(t *testing.T) {
	started := time.Date(2026, 8, 31, 6, 0, 0, 0, time.UTC)
	state := State{StartedAt: started, Deadline: started.Add(time.Hour), Hours: 1}
	updated := WithDeadline(state, started.Add(90*time.Minute))
	if updated.Hours != 1.5 {
		t.Fatalf("hours = %v, want 1.5", updated.Hours)
	}
	if updated.Deadline.Location() != time.UTC {
		t.Fatalf("deadline location = %v, want UTC", updated.Deadline.Location())
	}
}
