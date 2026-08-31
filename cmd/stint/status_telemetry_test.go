package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSnapshotJSONUsesExplicitTimeUnits(t *testing.T) {
	now := time.Date(2026, 8, 31, 8, 0, 0, 0, time.UTC)
	snapshot := sessionSnapshot{
		CollectedAt: now,
		Session:     sessionInfo{InstanceID: 42, Runtime: runtimeNInfer, ContextTokens: 172032},
		Time: sessionTimeSnapshot{
			StartedAt:         now.Add(-30 * time.Minute),
			Deadline:          now.Add(30 * time.Minute),
			Elapsed:           30 * time.Minute,
			Remaining:         30 * time.Minute,
			ScheduledDuration: time.Hour,
		},
		Health:      sessionHealth{Endpoint: endpointHealth{Refreshed: true, Healthy: true, Latency: 42 * time.Millisecond}},
		Performance: performanceSnapshot{Available: true, TTFT: 1500 * time.Millisecond, TotalLatency: 4 * time.Second, DecodeTokensSec: 123.4, Age: 90 * time.Second},
	}
	encoded, err := json.Marshal(snapshotJSON(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	timeBlock := payload["time"].(map[string]any)
	if got := timeBlock["elapsedSeconds"].(float64); got != 1800 {
		t.Fatalf("elapsedSeconds = %v, want 1800", got)
	}
	health := payload["health"].(map[string]any)
	endpoint := health["endpoint"].(map[string]any)
	if got := endpoint["latencyMilliseconds"].(float64); got != 42 {
		t.Fatalf("latencyMilliseconds = %v, want 42", got)
	}
	performance := payload["performance"].(map[string]any)
	if got := performance["ttftMilliseconds"].(float64); got != 1500 {
		t.Fatalf("ttftMilliseconds = %v, want 1500", got)
	}
	if got := performance["ageSeconds"].(float64); got != 90 {
		t.Fatalf("ageSeconds = %v, want 90", got)
	}
}

func TestStatusHelpIncludesTelemetryFlags(t *testing.T) {
	cmd, ok := findCommand("status")
	if !ok {
		t.Fatal("status command missing")
	}
	seen := map[string]bool{}
	for _, flag := range cmd.flags {
		seen[flag.name] = true
	}
	if !seen["--refresh"] || !seen["--json"] {
		t.Fatalf("status telemetry flags missing from help: %+v", cmd.flags)
	}
}
