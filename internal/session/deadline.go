package session

import (
	"errors"
	"time"
)

// DeadlineChange describes a proposed mutation of a session deadline.
// Deadline is the source of truth; Remaining is always derived from it.
type DeadlineChange struct {
	PreviousDeadline time.Time
	NewDeadline      time.Time
	PreviousDuration time.Duration
	NewDuration      time.Duration
}

// Remaining returns the non-negative time until the recorded deadline.
func Remaining(state State, now time.Time) time.Duration {
	if state.Deadline.IsZero() || !now.Before(state.Deadline) {
		return 0
	}
	return state.Deadline.Sub(now)
}

// Elapsed returns the non-negative elapsed time since the paid session began.
func Elapsed(state State, now time.Time) time.Duration {
	if state.StartedAt.IsZero() || !now.After(state.StartedAt) {
		return 0
	}
	end := now
	if !state.Deadline.IsZero() && end.After(state.Deadline) {
		end = state.Deadline
	}
	if !end.After(state.StartedAt) {
		return 0
	}
	return end.Sub(state.StartedAt)
}

// ScheduledDuration returns the currently scheduled paid duration.
func ScheduledDuration(state State) time.Duration {
	if state.StartedAt.IsZero() || state.Deadline.IsZero() || !state.Deadline.After(state.StartedAt) {
		return 0
	}
	return state.Deadline.Sub(state.StartedAt)
}

// ExtendDeadline proposes extending from the current deadline, never from now.
func ExtendDeadline(state State, now time.Time, delta time.Duration) (DeadlineChange, error) {
	if delta <= 0 {
		return DeadlineChange{}, errors.New("extension duration must be greater than zero")
	}
	if state.Deadline.IsZero() {
		return DeadlineChange{}, errors.New("session has no deadline")
	}
	if !now.Before(state.Deadline) {
		return DeadlineChange{}, errors.New("session deadline has already passed")
	}
	return deadlineChange(state, state.Deadline.Add(delta)), nil
}

// ShortenDeadline proposes subtracting from the current deadline. Shortening
// may not make the session immediately expired; callers should use down for
// immediate destruction.
func ShortenDeadline(state State, now time.Time, delta time.Duration) (DeadlineChange, error) {
	if delta <= 0 {
		return DeadlineChange{}, errors.New("shortening duration must be greater than zero")
	}
	if state.Deadline.IsZero() {
		return DeadlineChange{}, errors.New("session has no deadline")
	}
	if !now.Before(state.Deadline) {
		return DeadlineChange{}, errors.New("session deadline has already passed")
	}
	candidate := state.Deadline.Add(-delta)
	if !candidate.After(now) {
		return DeadlineChange{}, errors.New("shortening would expire the session immediately; use stint down")
	}
	return deadlineChange(state, candidate), nil
}

// WithDeadline applies a validated deadline and keeps Hours aligned with the
// currently scheduled total duration. Historical callers may still read Hours,
// so it must not become stale after extend/shorten operations.
func WithDeadline(state State, deadline time.Time) State {
	state.Deadline = deadline.UTC()
	if duration := ScheduledDuration(state); duration > 0 {
		state.Hours = duration.Hours()
	}
	return state
}

func deadlineChange(state State, next time.Time) DeadlineChange {
	return DeadlineChange{
		PreviousDeadline: state.Deadline,
		NewDeadline:      next.UTC(),
		PreviousDuration: ScheduledDuration(state),
		NewDuration:      scheduledDurationFor(state.StartedAt, next),
	}
}

func scheduledDurationFor(startedAt, deadline time.Time) time.Duration {
	if startedAt.IsZero() || deadline.IsZero() || !deadline.After(startedAt) {
		return 0
	}
	return deadline.Sub(startedAt)
}
