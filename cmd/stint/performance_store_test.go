package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func telemetryTestPaths(t *testing.T) config.Paths {
	t.Helper()
	root := t.TempDir()
	return config.Paths{
		ConfigDir: root + "/config",
		StateDir:  root + "/state",
		SSHDir:    root + "/ssh",
	}
}

func telemetryPerfState() sessionstate.State {
	return sessionstate.State{
		InstanceID:    42,
		Runtime:       runtimeNInfer,
		ContextTokens: 172032,
		GPUModel:      "RTX_4090",
	}
}

func TestPerformanceSampleRoundTrip(t *testing.T) {
	paths := telemetryTestPaths(t)
	state := telemetryPerfState()
	sampledAt := time.Date(2026, 8, 31, 8, 10, 0, 0, time.UTC)
	sample := perfSample{
		TTFT:             1800 * time.Millisecond,
		Total:            4 * time.Second,
		PromptTokens:     100,
		CompletionTokens: 256,
		DecodeTokensSec:  137.2,
	}
	if err := savePerformanceSample(paths, state, sample, sampledAt); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(performanceSamplePath(paths))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("performance file mode = %o, want 600", info.Mode().Perm())
	}
	snapshot := loadPerformanceSnapshot(paths, state, sampledAt.Add(3*time.Minute))
	if !snapshot.Available {
		t.Fatalf("sample unexpectedly unavailable: %s", snapshot.UnavailableReason)
	}
	if snapshot.TTFT != 1800*time.Millisecond || snapshot.TotalLatency != 4*time.Second {
		t.Fatalf("latencies = %s / %s", snapshot.TTFT, snapshot.TotalLatency)
	}
	if snapshot.DecodeTokensSec != 137.2 || snapshot.Age != 3*time.Minute {
		t.Fatalf("decode/age = %.1f / %s", snapshot.DecodeTokensSec, snapshot.Age)
	}
}

func TestPerformanceSampleRejectsDifferentInstance(t *testing.T) {
	paths := telemetryTestPaths(t)
	state := telemetryPerfState()
	if err := savePerformanceSample(paths, state, perfSample{DecodeTokensSec: 100}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	state.InstanceID++
	snapshot := loadPerformanceSnapshot(paths, state, time.Now().UTC())
	if snapshot.Available || !strings.Contains(snapshot.UnavailableReason, "previous instance") {
		t.Fatalf("unexpected mismatch result: %+v", snapshot)
	}
}

func TestPerformanceSampleRejectsRuntimeAndContextMismatch(t *testing.T) {
	paths := telemetryTestPaths(t)
	state := telemetryPerfState()
	if err := savePerformanceSample(paths, state, perfSample{DecodeTokensSec: 100}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	runtimeMismatch := state
	runtimeMismatch.Runtime = runtimeLlamaCpp
	if snapshot := loadPerformanceSnapshot(paths, runtimeMismatch, time.Now().UTC()); snapshot.Available || !strings.Contains(snapshot.UnavailableReason, "different runtime") {
		t.Fatalf("runtime mismatch unexpectedly accepted: %+v", snapshot)
	}

	contextMismatch := state
	contextMismatch.ContextTokens = 126976
	if snapshot := loadPerformanceSnapshot(paths, contextMismatch, time.Now().UTC()); snapshot.Available || !strings.Contains(snapshot.UnavailableReason, "different context") {
		t.Fatalf("context mismatch unexpectedly accepted: %+v", snapshot)
	}
}

func TestPerformanceSampleMissingAndMalformedAreNonFatal(t *testing.T) {
	paths := telemetryTestPaths(t)
	state := telemetryPerfState()
	if snapshot := loadPerformanceSnapshot(paths, state, time.Now().UTC()); snapshot.Available || !strings.Contains(snapshot.UnavailableReason, "no benchmark") {
		t.Fatalf("missing cache result = %+v", snapshot)
	}
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.StateDir, performanceSampleFileName), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if snapshot := loadPerformanceSnapshot(paths, state, time.Now().UTC()); snapshot.Available || !strings.Contains(snapshot.UnavailableReason, "parse performance") {
		t.Fatalf("malformed cache result = %+v", snapshot)
	}
}
