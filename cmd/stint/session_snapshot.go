package main

import (
	"fmt"
	"strings"
	"time"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

type sessionSnapshot struct {
	State              sessionstate.State
	Elapsed            time.Duration
	Remaining          time.Duration
	Expired            bool
	ScheduledDuration  time.Duration
	EstimatedSpentUSD  float64
	ScheduledExposure  float64
}

func buildSessionSnapshot(state sessionstate.State, now time.Time) sessionSnapshot {
	remaining := sessionstate.Remaining(state, now)
	expired := !state.Deadline.IsZero() && !now.Before(state.Deadline)
	elapsed := sessionstate.Elapsed(state, now)
	scheduled := sessionstate.ScheduledDuration(state)
	return sessionSnapshot{
		State:             state,
		Elapsed:           elapsed,
		Remaining:         remaining,
		Expired:           expired,
		ScheduledDuration: scheduled,
		EstimatedSpentUSD: scheduledCostUSD(state.HourlyUSD, elapsed),
		ScheduledExposure: scheduledCostUSD(state.HourlyUSD, scheduled),
	}
}

func printActiveSessionStatus(state sessionstate.State) {
	snapshot := buildSessionSnapshot(state, time.Now().UTC())
	fmt.Printf("Active compute     instance %d (%s)\n", state.InstanceID, state.Status)
	fmt.Printf("GPU                %s\n", state.GPUModel)
	if state.Runtime != "" {
		fmt.Printf("Runtime            %s\n", state.Runtime)
	}
	if state.ContextTokens > 0 {
		fmt.Printf("Context            %d tokens\n", state.ContextTokens)
	}
	fmt.Printf("Rate               $%.3f/hr\n", state.HourlyUSD)
	if !state.StartedAt.IsZero() {
		fmt.Printf("Started            %s\n", state.StartedAt.Local().Format(time.RFC1123))
		fmt.Printf("Elapsed            %s\n", formatSessionDuration(snapshot.Elapsed))
	}
	if snapshot.Expired {
		fmt.Println("Remaining          expired")
	} else if !state.Deadline.IsZero() {
		fmt.Printf("Remaining          %s\n", formatSessionDuration(snapshot.Remaining))
	}
	if snapshot.EstimatedSpentUSD > 0 {
		fmt.Printf("Spent estimate     $%.2f\n", snapshot.EstimatedSpentUSD)
	}
	if snapshot.ScheduledExposure > 0 {
		fmt.Printf("Session exposure   $%.2f scheduled\n", snapshot.ScheduledExposure)
	}
	if state.Checkpoint != "" {
		fmt.Printf("Checkpoint         %s\n", state.Checkpoint)
	}
	if state.LastError != "" {
		lastError := strings.ReplaceAll(state.LastError, "\n", " ")
		if len(lastError) > 140 {
			lastError = lastError[:137] + "..."
		}
		fmt.Printf("Last error         %s\n", lastError)
	}
	if !state.Deadline.IsZero() {
		fmt.Printf("Auto-destroy       %s\n", state.Deadline.Local().Format(time.RFC1123))
	}
	switch state.Status {
	case sessionstate.StatusRecoverable:
		fmt.Println("Next action        stint resume")
	case sessionstate.StatusReady:
		fmt.Println("Next action        use Cline; stint down when finished")
	default:
		fmt.Println("Next action        wait for stint start")
	}
}
