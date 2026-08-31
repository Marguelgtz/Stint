package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
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

func TestSnapshotJSONInferenceContract(t *testing.T) {
	snapshot := refreshedInferenceSnapshot()
	encoded, err := json.Marshal(snapshotJSON(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	inference := payload["inference"].(map[string]any)
	if inference["refreshed"] != true || inference["available"] != true {
		t.Fatalf("inference refresh/availability = %v / %v", inference["refreshed"], inference["available"])
	}
	if inference["agents"] != float64(2) || inference["residentDepth"] != float64(45000) {
		t.Fatalf("inference agents/depth = %v / %v", inference["agents"], inference["residentDepth"])
	}
	if inference["decodeTokensSec"] != float64(63.25) {
		t.Fatalf("inference.decodeTokensSec = %v, want 63.25", inference["decodeTokensSec"])
	}
	if inference["cacheReuseRatio"] != float64(0.87) {
		t.Fatalf("inference.cacheReuseRatio = %v, want 0.87", inference["cacheReuseRatio"])
	}
	performance := payload["performance"].(map[string]any)
	// The cached benchmark domain keeps its own decodeTokensSec (zero when no
	// sample is cached); the live probe's rate must only appear under
	// "inference", never blended into this block.
	if got := performance["decodeTokensSec"].(float64); got == float64(63.25) {
		t.Fatal("live decode rate leaked into the cached performance benchmark domain")
	}
}

func TestSnapshotJSONInferenceUnavailableContract(t *testing.T) {
	snapshot := refreshedInferenceSnapshot()
	snapshot.Inference = inferenceTelemetry{Refreshed: true, UnavailableReason: "inference endpoint unreachable through the tunnel", Meta: sampleMeta{Error: "connection refused"}}
	encoded, err := json.Marshal(snapshotJSON(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	inference := payload["inference"].(map[string]any)
	if inference["available"] != false {
		t.Fatalf("inference.available = %v, want false", inference["available"])
	}
	if inference["unavailableReason"] != "inference endpoint unreachable through the tunnel" {
		t.Fatalf("inference.unavailableReason = %v", inference["unavailableReason"])
	}
	if inference["decodeTokensSec"] != nil {
		t.Fatalf("rates must be null when the probe is unavailable: %v", inference["decodeTokensSec"])
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

// captureStdout runs fn with stdout redirected and returns what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	fn()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = original
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func refreshedInferenceSnapshot() sessionSnapshot {
	decode, prefill, reuse, spec := 63.25, 1204.5, 0.87, 0.71
	return sessionSnapshot{
		Session: sessionInfo{InstanceID: 42, Status: "READY", Runtime: runtimeNInfer, Model: "qwen3.8-27b", GPUModel: "RTX 4090", ContextTokens: 172032},
		Inference: inferenceTelemetry{
			Refreshed: true, Available: true, Processing: 1, Deferred: 2, Agents: 2, ResidentDepth: 45000,
			DecodeTokensSec: &decode, PrefillTokensSec: &prefill, CacheReuseRatio: &reuse, SpecAcceptRatio: &spec,
			Lanes: []inferenceLane{
				{ID: 0, NCTX: 172032, Processing: true, NPrompt: 45000},
				{ID: 1, NCTX: 172032, Retained: true, NPrompt: 12000},
			},
		},
	}
}

func TestStatusHumanOutputShowsInferenceLiveSection(t *testing.T) {
	snapshot := refreshedInferenceSnapshot()
	out := captureStdout(t, func() { printSessionSnapshotHuman(snapshot, true) })
	for _, want := range []string{"INFERENCE LIVE", "Agents             2 active", "Live prompt depth  45000 tokens · 2 queued", "Decode             63.2 tok/s", "Prefill            1204.5 tok/s", "Cache reuse        87% of prompt", "Speculative        71% accepted", "Lanes              0: 45000 tok · 1: 12000 tok (resident)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
	}
}

func TestStatusHumanOutputInferenceUnavailableShowsReason(t *testing.T) {
	snapshot := refreshedInferenceSnapshot()
	snapshot.Inference = inferenceTelemetry{Refreshed: true, UnavailableReason: "runtime serves neither /metrics nor /slots (start llama.cpp with --metrics --slots)"}
	out := captureStdout(t, func() { printSessionSnapshotHuman(snapshot, true) })
	if !strings.Contains(out, "INFERENCE LIVE") || !strings.Contains(out, "unavailable") {
		t.Fatalf("status output missing unavailable inference line:\n%s", out)
	}
	if !strings.Contains(out, "--metrics --slots") {
		t.Fatalf("unavailable reason not surfaced:\n%s", out)
	}
}

func TestStatusHumanOutputInferenceNotRefreshedWithoutFlag(t *testing.T) {
	snapshot := refreshedInferenceSnapshot()
	snapshot.Inference = inferenceTelemetry{}
	out := captureStdout(t, func() { printSessionSnapshotHuman(snapshot, false) })
	if !strings.Contains(out, "Live inference     not refreshed (use --refresh)") {
		t.Fatalf("local status must announce the unrefreshed inference domain:\n%s", out)
	}
}
