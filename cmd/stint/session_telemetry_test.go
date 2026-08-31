package main

import (
	"context"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestParseNvidiaSMILine(t *testing.T) {
	now := time.Date(2026, 8, 31, 8, 0, 0, 0, time.UTC)
	gpu, err := parseNvidiaSMILine("87, 21914, 24564, 366.2, 450.0, 69", now)
	if err != nil {
		t.Fatal(err)
	}
	if !gpu.Available || gpu.UtilizationPercent == nil || *gpu.UtilizationPercent != 87 {
		t.Fatalf("unexpected GPU telemetry: %+v", gpu)
	}
	if gpu.MemoryUsedMiB == nil || *gpu.MemoryUsedMiB != 21914 || gpu.MemoryTotalMiB == nil || *gpu.MemoryTotalMiB != 24564 {
		t.Fatalf("unexpected GPU memory telemetry: %+v", gpu)
	}
	if gpu.PowerDrawW == nil || *gpu.PowerDrawW != 366.2 || gpu.TemperatureC == nil || *gpu.TemperatureC != 69 {
		t.Fatalf("unexpected GPU power/temperature telemetry: %+v", gpu)
	}
}

func TestParseNvidiaSMILineAllowsOptionalNA(t *testing.T) {
	gpu, err := parseNvidiaSMILine("12, 1000, 24564, N/A, [Not Supported], 50", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !gpu.Available || gpu.PowerDrawW != nil || gpu.PowerLimitW != nil {
		t.Fatalf("optional N/A fields were not preserved as unavailable: %+v", gpu)
	}
}

func TestParseNvidiaSMILineRejectsMalformedOutput(t *testing.T) {
	if _, err := parseNvidiaSMILine("87, 21914", time.Now().UTC()); err == nil {
		t.Fatal("expected malformed nvidia-smi output to fail")
	}
	if _, err := parseNvidiaSMILine("busy, 21914, 24564, 100, 450, 70", time.Now().UTC()); err == nil {
		t.Fatal("expected invalid numeric output to fail")
	}
}

func TestCollectSessionSnapshotLocalModeDoesNotRunRemoteProbes(t *testing.T) {
	now := time.Date(2026, 8, 31, 8, 0, 0, 0, time.UTC)
	state := sessionstate.State{InstanceID: 9, Deadline: now.Add(time.Hour), TunnelPID: 10, WatchdogPID: 11}
	endpointCalls := 0
	remoteCalls := 0
	inferenceCalls := 0
	deps := snapshotProbeDeps{
		processRunning: func(pid int) bool { return pid == 10 },
		endpoint: func(context.Context) endpointHealth {
			endpointCalls++
			return endpointHealth{}
		},
		remote: func(context.Context, config.Paths, sessionstate.State) remoteTelemetrySample {
			remoteCalls++
			return remoteTelemetrySample{}
		},
		inference: func(context.Context) inferenceTelemetry {
			inferenceCalls++
			return inferenceTelemetry{}
		},
		performance: func(config.Paths, sessionstate.State, time.Time) performanceSnapshot {
			return performanceSnapshot{Available: true, DecodeTokensSec: 123}
		},
	}
	snapshot := collectSessionSnapshot(context.Background(), config.Paths{}, state, now, false, deps)
	if endpointCalls != 0 || remoteCalls != 0 || inferenceCalls != 0 {
		t.Fatalf("local snapshot invoked remote probes: endpoint=%d remote=%d inference=%d", endpointCalls, remoteCalls, inferenceCalls)
	}
	if !snapshot.Health.Tunnel.Running || snapshot.Health.Watchdog.Running {
		t.Fatalf("unexpected local process health: %+v", snapshot.Health)
	}
	if !snapshot.Performance.Available || snapshot.Performance.DecodeTokensSec != 123 {
		t.Fatalf("cached performance not attached: %+v", snapshot.Performance)
	}
}

func TestCollectSessionSnapshotRefreshRunsIndependentProbesConcurrently(t *testing.T) {
	now := time.Now().UTC()
	state := sessionstate.State{InstanceID: 9, Deadline: now.Add(time.Hour)}
	started := make(chan string, 2)
	release := make(chan struct{})
	deps := snapshotProbeDeps{
		processRunning: func(int) bool { return false },
		endpoint: func(context.Context) endpointHealth {
			started <- "endpoint"
			<-release
			return endpointHealth{Refreshed: true, Healthy: true}
		},
		remote: func(context.Context, config.Paths, sessionstate.State) remoteTelemetrySample {
			started <- "remote"
			<-release
			return remoteTelemetrySample{Runtime: runtimeHealth{Refreshed: true, SSH: true, Running: true}, GPU: gpuTelemetry{Refreshed: true, Available: true}}
		},
		inference: func(context.Context) inferenceTelemetry {
			return inferenceTelemetry{Refreshed: true, Available: true, Agents: 2, ResidentDepth: 40000}
		},
		performance: func(config.Paths, sessionstate.State, time.Time) performanceSnapshot { return performanceSnapshot{} },
	}
	done := make(chan sessionSnapshot, 1)
	go func() {
		done <- collectSessionSnapshot(context.Background(), config.Paths{}, state, now, true, deps)
	}()
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case name := <-started:
			seen[name] = true
		case <-time.After(time.Second):
			t.Fatal("refresh probes did not start concurrently")
		}
	}
	if !seen["endpoint"] || !seen["remote"] {
		t.Fatalf("missing concurrent probe start: %v", seen)
	}
	close(release)
	snapshot := <-done
	if !snapshot.Health.Endpoint.Healthy || !snapshot.Health.Runtime.SSH || !snapshot.GPU.Available {
		t.Fatalf("refresh results not assembled: %+v", snapshot)
	}
	if !snapshot.Inference.Available || snapshot.Inference.Agents != 2 || snapshot.Inference.ResidentDepth != 40000 {
		t.Fatalf("inference results not assembled: %+v", snapshot.Inference)
	}
}

func TestCollectSessionSnapshotRefreshRecordsInferenceWithoutFailing(t *testing.T) {
	now := time.Now().UTC()
	state := sessionstate.State{InstanceID: 9, Deadline: now.Add(time.Hour)}
	deps := snapshotProbeDeps{
		processRunning: func(int) bool { return false },
		endpoint:       func(context.Context) endpointHealth { return endpointHealth{Refreshed: true, Healthy: true} },
		remote: func(context.Context, config.Paths, sessionstate.State) remoteTelemetrySample {
			return remoteTelemetrySample{Runtime: runtimeHealth{Refreshed: true, SSH: true}, GPU: gpuTelemetry{Refreshed: true}}
		},
		// The probe degrades (for example a disabled metrics endpoint) but the
		// snapshot must still be assembled with the failure recorded.
		inference: func(context.Context) inferenceTelemetry {
			return inferenceTelemetry{Refreshed: true, UnavailableReason: "runtime serves neither /metrics nor /slots (start llama.cpp with --metrics --slots)"}
		},
		performance: func(config.Paths, sessionstate.State, time.Time) performanceSnapshot { return performanceSnapshot{} },
	}
	snapshot := collectSessionSnapshot(context.Background(), config.Paths{}, state, now, true, deps)
	if !snapshot.Inference.Refreshed || snapshot.Inference.Available {
		t.Fatalf("inference probe state lost: %+v", snapshot.Inference)
	}
	if snapshot.Inference.UnavailableReason == "" {
		t.Fatal("inference unavailable reason not recorded")
	}
	if !snapshot.Health.Endpoint.Healthy || !snapshot.Health.Runtime.SSH {
		t.Fatalf("inference failure must not affect other domains: %+v", snapshot)
	}
}

func TestCollectSessionSnapshotKeepsPartialTelemetryFailures(t *testing.T) {
	now := time.Now().UTC()
	state := sessionstate.State{InstanceID: 9, Deadline: now.Add(time.Hour)}
	deps := snapshotProbeDeps{
		processRunning: func(int) bool { return false },
		endpoint: func(context.Context) endpointHealth {
			return endpointHealth{Refreshed: true, Healthy: true, ModelVisible: true}
		},
		remote: func(context.Context, config.Paths, sessionstate.State) remoteTelemetrySample {
			return remoteTelemetrySample{
				Runtime: runtimeHealth{Refreshed: true, Meta: sampleMeta{Error: "ssh timeout"}},
				GPU:     gpuTelemetry{Refreshed: true, Meta: sampleMeta{Error: "ssh timeout"}},
			}
		},
		inference:   func(context.Context) inferenceTelemetry { return inferenceTelemetry{Refreshed: true, Available: true} },
		performance: func(config.Paths, sessionstate.State, time.Time) performanceSnapshot { return performanceSnapshot{} },
	}
	snapshot := collectSessionSnapshot(context.Background(), config.Paths{}, state, now, true, deps)
	if !snapshot.Health.Endpoint.Healthy {
		t.Fatal("healthy endpoint was lost when SSH telemetry failed")
	}
	if snapshot.Health.Runtime.Meta.Error != "ssh timeout" || snapshot.GPU.Meta.Error != "ssh timeout" {
		t.Fatalf("remote errors were not retained: runtime=%q gpu=%q", snapshot.Health.Runtime.Meta.Error, snapshot.GPU.Meta.Error)
	}
}

func TestOptionalTelemetryFloat(t *testing.T) {
	if value, err := parseOptionalTelemetryFloat("N/A"); err != nil || value != nil {
		t.Fatalf("N/A = %v, %v; want nil, nil", value, err)
	}
	value, err := parseOptionalTelemetryFloat("42.5")
	if err != nil || value == nil || *value != 42.5 {
		t.Fatalf("42.5 = %v, %v", value, err)
	}
	if _, err := parseOptionalTelemetryFloat("forty"); err == nil {
		t.Fatal("expected invalid telemetry float error")
	}
}
