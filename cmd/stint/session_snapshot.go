package main

import (
	"fmt"
	"strings"
	"time"

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
	Refreshed          bool     `json:"refreshed"`
	Available          bool     `json:"available"`
	UtilizationPercent *float64 `json:"utilizationPercent,omitempty"`
	MemoryUsedMiB      *float64 `json:"memoryUsedMiB,omitempty"`
	MemoryTotalMiB     *float64 `json:"memoryTotalMiB,omitempty"`
	PowerDrawW         *float64 `json:"powerDrawW,omitempty"`
	PowerLimitW        *float64 `json:"powerLimitW,omitempty"`
	TemperatureC       *float64 `json:"temperatureC,omitempty"`
	Meta               sampleMeta `json:"meta"`
}

type performanceSnapshot struct {
	Available          bool          `json:"available"`
	TTFT               time.Duration `json:"-"`
	TotalLatency       time.Duration `json:"-"`
	PromptTokens       int           `json:"promptTokens,omitempty"`
	CompletionTokens   int           `json:"completionTokens,omitempty"`
	DecodeTokensSec    float64       `json:"decodeTokensSec,omitempty"`
	SampledAt          time.Time     `json:"sampledAt,omitempty"`
	Age                time.Duration `json:"-"`
	UnavailableReason  string        `json:"unavailableReason,omitempty"`
}

type sessionSnapshot struct {
	CollectedAt time.Time           `json:"collectedAt"`
	Session     sessionInfo         `json:"session"`
	Time        sessionTimeSnapshot `json:"time"`
	Cost        sessionCostSnapshot `json:"cost"`
	Health      sessionHealth       `json:"health"`
	GPU         gpuTelemetry        `json:"gpu"`
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
		Health: sessionHealth{
			Tunnel:   processHealth{PID: state.TunnelPID},
			Watchdog: processHealth{PID: state.WatchdogPID},
		},
	}
}

func printActiveSessionStatus(snapshot sessionSnapshot) {
	fmt.Printf("Active compute     instance %d (%s)\n", snapshot.Session.InstanceID, snapshot.Session.Status)
	fmt.Printf("GPU                %s\n", snapshot.Session.GPUModel)
	if snapshot.Session.Runtime != "" {
		fmt.Printf("Runtime            %s\n", snapshot.Session.Runtime)
	}
	if snapshot.Session.ContextTokens > 0 {
		fmt.Printf("Context            %d tokens\n", snapshot.Session.ContextTokens)
	}
	fmt.Printf("Rate               $%.3f/hr\n", snapshot.Cost.HourlyUSD)
	if !snapshot.Time.StartedAt.IsZero() {
		fmt.Printf("Started            %s\n", snapshot.Time.StartedAt.Local().Format(time.RFC1123))
		fmt.Printf("Elapsed            %s\n", formatSessionDuration(snapshot.Time.Elapsed))
	}
	if snapshot.Time.Expired {
		fmt.Println("Remaining          expired")
	} else if !snapshot.Time.Deadline.IsZero() {
		fmt.Printf("Remaining          %s\n", formatSessionDuration(snapshot.Time.Remaining))
	}
	if snapshot.Cost.EstimatedSpentUSD > 0 {
		fmt.Printf("Spent estimate     $%.2f\n", snapshot.Cost.EstimatedSpentUSD)
	}
	if snapshot.Cost.ScheduledUSD > 0 {
		fmt.Printf("Session exposure   $%.2f scheduled\n", snapshot.Cost.ScheduledUSD)
	}
	if snapshot.Session.Checkpoint != "" {
		fmt.Printf("Checkpoint         %s\n", snapshot.Session.Checkpoint)
	}
	if snapshot.Session.LastError != "" {
		lastError := strings.ReplaceAll(snapshot.Session.LastError, "\n", " ")
		if len(lastError) > 140 {
			lastError = lastError[:137] + "..."
		}
		fmt.Printf("Last error         %s\n", lastError)
	}
	if !snapshot.Time.Deadline.IsZero() {
		fmt.Printf("Auto-destroy       %s\n", snapshot.Time.Deadline.Local().Format(time.RFC1123))
	}
}
