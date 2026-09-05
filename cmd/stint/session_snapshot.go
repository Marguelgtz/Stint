package main

import (
	"fmt"
	"math"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

type sampleMeta struct {
	SampledAt time.Time `json:"sampledAt,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type sessionInfo struct {
	InstanceID    int64  `json:"instanceId"`
	Status        string `json:"status"`
	GPUModel      string `json:"gpuModel,omitempty"`
	Runtime       string `json:"runtime,omitempty"`
	Model         string `json:"model,omitempty"`
	ContextTokens int    `json:"contextTokens,omitempty"`
	Clients       int    `json:"clients,omitempty"`
	Profile       string `json:"profile,omitempty"`
	Checkpoint    string `json:"checkpoint,omitempty"`
	LastError     string `json:"lastError,omitempty"`
}

type sessionTimeSnapshot struct {
	StartedAt         time.Time     `json:"startedAt,omitempty"`
	Deadline          time.Time     `json:"deadline,omitempty"`
	Elapsed           time.Duration `json:"-"`
	Remaining         time.Duration `json:"-"`
	ScheduledDuration time.Duration `json:"-"`
	Expired           bool          `json:"expired"`
}

type sessionCostSnapshot struct {
	HourlyUSD         float64 `json:"hourlyUsd"`
	EstimatedSpentUSD float64 `json:"estimatedSpentUsd"`
	ScheduledUSD      float64 `json:"scheduledUsd"`
}

// stateStaleness exposes how much the locally recorded session state may lag
// reality, so scripts can react (the JSON mirror of the `stint status`
// State-freshness warning). `stateAgeSeconds` is how long session.json has
// gone unwritten; `deadlineStale` mirrors staleStateWarning's firing
// conditions (tunnel running + deadline close/passed + stale state, or tunnel
// down while the state claims READY/RECOVERABLE with a close deadline).
type stateStaleness struct {
	StateAgeSeconds float64 `json:"stateAgeSeconds"`
	DeadlineStale   bool    `json:"deadlineStale"`
}

type processHealth struct {
	PID     int  `json:"pid,omitempty"`
	Running bool `json:"running"`
}

type endpointHealth struct {
	Refreshed    bool          `json:"refreshed"`
	Healthy      bool          `json:"healthy"`
	StatusCode   int           `json:"statusCode,omitempty"`
	Latency      time.Duration `json:"-"`
	ModelVisible bool          `json:"modelVisible"`
	Meta         sampleMeta    `json:"meta"`
}

type runtimeHealth struct {
	Refreshed bool       `json:"refreshed"`
	SSH       bool       `json:"ssh"`
	Running   bool       `json:"running"`
	Meta      sampleMeta `json:"meta"`
}

type sessionHealth struct {
	Tunnel   processHealth  `json:"tunnel"`
	Watchdog processHealth  `json:"watchdog"`
	Endpoint endpointHealth `json:"endpoint"`
	Runtime  runtimeHealth  `json:"runtime"`
}

type gpuTelemetry struct {
	Refreshed          bool       `json:"refreshed"`
	Available          bool       `json:"available"`
	UtilizationPercent *float64   `json:"utilizationPercent,omitempty"`
	MemoryUsedMiB      *float64   `json:"memoryUsedMiB,omitempty"`
	MemoryTotalMiB     *float64   `json:"memoryTotalMiB,omitempty"`
	PowerDrawW         *float64   `json:"powerDrawW,omitempty"`
	PowerLimitW        *float64   `json:"powerLimitW,omitempty"`
	TemperatureC       *float64   `json:"temperatureC,omitempty"`
	Meta               sampleMeta `json:"meta"`
}

type performanceSnapshot struct {
	Available         bool          `json:"available"`
	TTFT              time.Duration `json:"-"`
	TotalLatency      time.Duration `json:"-"`
	PromptTokens      int           `json:"promptTokens,omitempty"`
	CompletionTokens  int           `json:"completionTokens,omitempty"`
	DecodeTokensSec   float64       `json:"decodeTokensSec,omitempty"`
	SampledAt         time.Time     `json:"sampledAt,omitempty"`
	Age               time.Duration `json:"-"`
	UnavailableReason string        `json:"unavailableReason,omitempty"`
}

type sessionSnapshot struct {
	CollectedAt time.Time           `json:"collectedAt"`
	Session     sessionInfo         `json:"session"`
	Time        sessionTimeSnapshot `json:"time"`
	Cost        sessionCostSnapshot `json:"cost"`
	Staleness   stateStaleness      `json:"staleness"`
	Health      sessionHealth       `json:"health"`
	GPU         gpuTelemetry        `json:"gpu"`
	Inference   inferenceTelemetry  `json:"inference"`
	Performance performanceSnapshot `json:"performance"`
}

func buildSessionSnapshot(state sessionstate.State, now time.Time) sessionSnapshot {
	remaining := sessionstate.Remaining(state, now)
	expired := !state.Deadline.IsZero() && !now.Before(state.Deadline)
	elapsed := sessionstate.Elapsed(state, now)
	scheduled := sessionstate.ScheduledDuration(state)
	return sessionSnapshot{
		CollectedAt: now.UTC(),
		Session: sessionInfo{
			InstanceID:    state.InstanceID,
			Status:        state.Status,
			GPUModel:      state.GPUModel,
			Runtime:       runtimeForState(state),
			Model:         interactiveModelAlias,
			ContextTokens: contextForState(state),
			Clients:       clientsForState(state),
			Profile:       state.Profile,
			Checkpoint:    state.Checkpoint,
			LastError:     state.LastError,
		},
		Time: sessionTimeSnapshot{
			StartedAt:         state.StartedAt,
			Deadline:          state.Deadline,
			Elapsed:           elapsed,
			Remaining:         remaining,
			ScheduledDuration: scheduled,
			Expired:           expired,
		},
		Cost: sessionCostSnapshot{
			HourlyUSD:         state.HourlyUSD,
			EstimatedSpentUSD: scheduledCostUSD(state.HourlyUSD, elapsed),
			ScheduledUSD:      scheduledCostUSD(state.HourlyUSD, scheduled),
		},
		Staleness: stateStaleness{StateAgeSeconds: stateAgeSeconds(state, now)},
		Health: sessionHealth{
			Tunnel:   processHealth{PID: state.TunnelPID},
			Watchdog: processHealth{PID: state.WatchdogPID},
		},
	}
}

// printActiveSessionStatus preserves the existing arg-less status call site
// while routing it through the new local-only snapshot collector. The richer
// flag path (`status --refresh`, `status --json`) lives in status_telemetry.go.
func printActiveSessionStatus(state sessionstate.State) {
	paths, err := config.DefaultPaths()
	if err != nil {
		fmt.Printf("Telemetry          unavailable (%v)\n", err)
		printSessionSnapshotHuman(buildSessionSnapshot(state, time.Now().UTC()), false)
		return
	}
	now := time.Now().UTC()
	snapshot := collectSessionSnapshot(contextBackground(), paths, state, now, false, defaultSnapshotProbeDeps())
	printSessionSnapshotHuman(snapshot, false)
	if warning := staleStateWarning(state, snapshot.Health.Tunnel.Running, now); warning != "" {
		fmt.Println()
		fmt.Println(warning)
	}
}

// stateFreshnessWriteMax bounds how long local session state may go unwritten
// while the tunnel is still running before the local deadline view is
// considered possibly stale.
const stateFreshnessWriteMax = 2 * time.Minute

// stateFreshnessDangerRemaining is the locally computed remaining window in
// which a stale local deadline is dangerous to act on. The 2026-09-03 live
// investigation observed a pre-extend deadline displayed with ~10 minutes
// left while the true deadline was ~2 hours out; warning once the local
// deadline is within this window catches that class of mistake without
// nagging on healthy long sessions.
const stateFreshnessDangerRemaining = 15 * time.Minute

// stateAgeSeconds reports how long the locally recorded session state has
// gone unwritten. When the state file predates the updatedAt field (or was
// never rewritten), the age is measured from the session start instead, so a
// never-updated file still reports a finite, meaningful age (and JSON
// marshaling never hits +Inf).
func stateAgeSeconds(state sessionstate.State, now time.Time) float64 {
	anchor := state.UpdatedAt
	if anchor.IsZero() {
		anchor = state.StartedAt
	}
	if anchor.IsZero() {
		return 0
	}
	age := now.Sub(anchor)
	if age < 0 {
		age = 0
	}
	return math.Round(age.Seconds()*100) / 100
}

// staleStateWarning reports a hint when the local-only view of a session may
// lag the remote session, in either direction:
//
//   - tunnel running, local deadline close/passed, state unwritten:
//     e.g. a `stint extend` executed elsewhere has not been resynced locally
//     (the 2026-09-03 21:43 shape: pre-extend deadline shown with ~10 minutes
//     left while the true deadline was ~2 h out).
//   - tunnel NOT running while the state still claims a live session (READY
//     or RECOVERABLE) with a deadline at or near: the local state may be
//     stale (a session destroyed or replaced elsewhere) and the deadline
//     shown is untrustworthy. This is the symmetric case the first warning
//     did not cover.
//
// It stays silent while the state was written recently (a dashboard or
// refresh loop keeps it current) or the session has no deadline. The
// suggested remedy is always `stint status --refresh`, which reconciles the
// local view from the remote session without mutating it.
func staleStateWarning(state sessionstate.State, tunnelRunning bool, now time.Time) string {
	if state.Deadline.IsZero() {
		return ""
	}
	if !state.UpdatedAt.IsZero() && now.Sub(state.UpdatedAt) <= stateFreshnessWriteMax {
		return ""
	}
	age := "never written"
	if !state.UpdatedAt.IsZero() {
		age = humanizeStateAge(now.Sub(state.UpdatedAt)) + " old"
	}
	if !tunnelRunning {
		if state.Status != sessionstate.StatusReady && state.Status != sessionstate.StatusRecoverable {
			return ""
		}
		if remaining := sessionstate.Remaining(state, now); remaining > stateFreshnessDangerRemaining {
			return ""
		}
		return fmt.Sprintf("State freshness    local state is %s and claims %s while the tunnel is not running — the deadline shown may be stale; run `stint status --refresh` to resync from the remote session", age, state.Status)
	}
	if remaining := sessionstate.Remaining(state, now); remaining > stateFreshnessDangerRemaining {
		return ""
	}
	if !now.Before(state.Deadline) {
		return fmt.Sprintf("State freshness    local deadline has already passed while the tunnel is still running and local state is %s — run `stint status --refresh` to resync from the remote session", age)
	}
	return fmt.Sprintf("State freshness    local state is %s — the auto-destroy deadline shown may be stale — run `stint status --refresh` to resync from the remote session", age)
}

func humanizeStateAge(d time.Duration) string {
	if d < time.Minute {
		return "less than a minute"
	}
	if d < time.Hour {
		return fmt.Sprintf("%d min", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}
