package main

import (
	"os"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const (
	staleSessionStateAge     = 2 * time.Minute
	staleSessionDeadlineNear = 15 * time.Minute
)

func sessionStateFreshness(paths config.Paths, state sessionstate.State, tunnelRunning bool, now time.Time) stateFreshnessSnapshot {
	updatedAt := state.UpdatedAt
	if updatedAt.IsZero() {
		if info, err := os.Stat(sessionstate.Path(paths)); err == nil {
			updatedAt = info.ModTime()
		}
	}

	age := time.Duration(0)
	if !updatedAt.IsZero() && now.After(updatedAt) {
		age = now.Sub(updatedAt)
	}
	result := stateFreshnessSnapshot{Age: age}
	nearDeadline := !state.Deadline.IsZero() && !state.Deadline.After(now.Add(staleSessionDeadlineNear))
	if age <= staleSessionStateAge || !nearDeadline {
		return result
	}

	stateClaimsLive := state.Status == sessionstate.StatusReady || state.Status == sessionstate.StatusRecoverable
	if !tunnelRunning && !stateClaimsLive {
		return result
	}
	result.DeadlineStale = true
	if tunnelRunning {
		result.Warning = "local session state has not been updated recently and its deadline is near; verify the current session state before relying on that deadline"
	} else {
		result.Warning = "local state still claims a live session, but its tunnel is down and the deadline is near; verify the provider state before relying on that deadline"
	}
	return result
}
