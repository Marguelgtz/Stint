package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const (
	endpointTelemetryTimeout = 2 * time.Second
	remoteTelemetryTimeout   = 3 * time.Second
)

type remoteTelemetrySample struct {
	Runtime runtimeHealth
	GPU     gpuTelemetry
}

type snapshotProbeDeps struct {
	processRunning func(int) bool
	endpoint       func(context.Context) endpointHealth
	remote         func(context.Context, config.Paths, sessionstate.State) remoteTelemetrySample
	performance    func(config.Paths, sessionstate.State, time.Time) performanceSnapshot
}

func defaultSnapshotProbeDeps() snapshotProbeDeps {
	return snapshotProbeDeps{
		processRunning: localProcessRunning,
		endpoint:       probeEndpointHealth,
		remote:         probeRemoteTelemetry,
		performance:    loadPerformanceSnapshot,
	}
}

// collectSessionSnapshot assembles lifecycle state with best-effort observations.
// refresh=false is intentionally local-only: it never opens SSH and never sends
// an inference request. refresh=true adds endpoint and one read-only SSH sample.
func collectSessionSnapshot(ctx context.Context, paths config.Paths, state sessionstate.State, now time.Time, refresh bool, deps snapshotProbeDeps) sessionSnapshot {
	snapshot := buildSessionSnapshot(state, now)
	snapshot.Health.Tunnel.Running = deps.processRunning(state.TunnelPID)
	snapshot.Health.Watchdog.Running = deps.processRunning(state.WatchdogPID)
	snapshot.Performance = deps.performance(paths, state, now)
	if !refresh {
		return snapshot
	}

	endpointCh := make(chan endpointHealth, 1)
	remoteCh := make(chan remoteTelemetrySample, 1)

	go func() {
		endpointCtx, cancel := context.WithTimeout(ctx, endpointTelemetryTimeout)
		defer cancel()
		endpointCh <- deps.endpoint(endpointCtx)
	}()
	go func() {
		remoteCtx, cancel := context.WithTimeout(ctx, remoteTelemetryTimeout)
		defer cancel()
		remoteCh <- deps.remote(remoteCtx, paths, state)
	}()

	for received := 0; received < 2; received++ {
		select {
		case endpoint := <-endpointCh:
			snapshot.Health.Endpoint = endpoint
		case remote := <-remoteCh:
			snapshot.Health.Runtime = remote.Runtime
			snapshot.GPU = remote.GPU
		case <-ctx.Done():
			if !snapshot.Health.Endpoint.Refreshed {
				snapshot.Health.Endpoint = endpointHealth{Refreshed: true, Meta: sampleMeta{SampledAt: time.Now().UTC(), Error: ctx.Err().Error()}}
			}
			if !snapshot.Health.Runtime.Refreshed {
				snapshot.Health.Runtime = runtimeHealth{Refreshed: true, Meta: sampleMeta{SampledAt: time.Now().UTC(), Error: ctx.Err().Error()}}
				snapshot.GPU = gpuTelemetry{Refreshed: true, Meta: sampleMeta{SampledAt: time.Now().UTC(), Error: ctx.Err().Error()}}
			}
			return snapshot
		}
	}
	return snapshot
}

func localProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func probeEndpointHealth(ctx context.Context) endpointHealth {
	sampledAt := time.Now().UTC()
	result := endpointHealth{Refreshed: true, Meta: sampleMeta{SampledAt: sampledAt}}
	client := &http.Client{Timeout: endpointTelemetryTimeout}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/v1/models", clinePort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		result.Meta.Error = err.Error()
		return result
	}
	started := time.Now()
	resp, err := client.Do(req)
	result.Latency = time.Since(started)
	if err != nil {
		result.Meta.Error = err.Error()
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		result.Meta.Error = fmt.Sprintf("endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
		return result
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&payload); err != nil {
		result.Meta.Error = "decode /v1/models: " + err.Error()
		return result
	}
	for _, model := range payload.Data {
		if model.ID == interactiveModelAlias {
			result.ModelVisible = true
			break
		}
	}
	if !result.ModelVisible {
		result.Meta.Error = "expected model qwen3.8-27b is not advertised"
		return result
	}
	result.Healthy = true
	return result
}

// probeRemoteTelemetry deliberately combines runtime and GPU observations into
// one SSH process. A dashboard refresh therefore costs one read-only SSH round
// trip instead of separate SSH calls for runtime, GPU, memory, power and heat.
func probeRemoteTelemetry(ctx context.Context, paths config.Paths, state sessionstate.State) remoteTelemetrySample {
	sampledAt := time.Now().UTC()
	result := remoteTelemetrySample{
		Runtime: runtimeHealth{Refreshed: true, Meta: sampleMeta{SampledAt: sampledAt}},
		GPU:     gpuTelemetry{Refreshed: true, Meta: sampleMeta{SampledAt: sampledAt}},
	}
	if strings.TrimSpace(state.SSHHost) == "" || state.SSHPort <= 0 {
		errText := "session has no SSH endpoint"
		result.Runtime.Meta.Error = errText
		result.GPU.Meta.Error = errText
		return result
	}
	processName := selectedModelProcessName(state)
	command := fmt.Sprintf(`printf 'STINT_RUNTIME='; if pgrep -x '%s' >/dev/null 2>&1; then echo 1; else echo 0; fi
if command -v nvidia-smi >/dev/null 2>&1; then
  printf 'STINT_GPU='
  nvidia-smi --query-gpu=utilization.gpu,memory.used,memory.total,power.draw,power.limit,temperature.gpu --format=csv,noheader,nounits | head -n 1
else
  echo 'STINT_GPU_ERROR=nvidia-smi unavailable'
fi`, processName)
	output, err := runSSH(ctx, paths, state, command)
	if err != nil {
		errText := compactTelemetryError(err)
		result.Runtime.Meta.Error = errText
		result.GPU.Meta.Error = errText
		return result
	}
	result.Runtime.SSH = true
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "STINT_RUNTIME="):
			result.Runtime.Running = strings.TrimSpace(strings.TrimPrefix(line, "STINT_RUNTIME=")) == "1"
		case strings.HasPrefix(line, "STINT_GPU="):
			gpu, parseErr := parseNvidiaSMILine(strings.TrimPrefix(line, "STINT_GPU="), sampledAt)
			if parseErr != nil {
				result.GPU.Meta.Error = parseErr.Error()
			} else {
				result.GPU = gpu
			}
		case strings.HasPrefix(line, "STINT_GPU_ERROR="):
			result.GPU.Meta.Error = strings.TrimSpace(strings.TrimPrefix(line, "STINT_GPU_ERROR="))
		}
	}
	if !result.Runtime.Running {
		result.Runtime.Meta.Error = fmt.Sprintf("%s is not running", processName)
	}
	if !result.GPU.Available && result.GPU.Meta.Error == "" {
		result.GPU.Meta.Error = "nvidia-smi returned no usable GPU sample"
	}
	return result
}

func parseNvidiaSMILine(line string, sampledAt time.Time) (gpuTelemetry, error) {
	fields := strings.Split(strings.TrimSpace(line), ",")
	if len(fields) != 6 {
		return gpuTelemetry{Refreshed: true, Meta: sampleMeta{SampledAt: sampledAt}}, fmt.Errorf("unexpected nvidia-smi field count %d", len(fields))
	}
	values := make([]*float64, len(fields))
	for i, field := range fields {
		value, err := parseOptionalTelemetryFloat(field)
		if err != nil {
			return gpuTelemetry{Refreshed: true, Meta: sampleMeta{SampledAt: sampledAt}}, fmt.Errorf("parse nvidia-smi field %d: %w", i+1, err)
		}
		values[i] = value
	}
	if values[1] == nil || values[2] == nil {
		return gpuTelemetry{Refreshed: true, Meta: sampleMeta{SampledAt: sampledAt}}, errors.New("nvidia-smi did not report GPU memory")
	}
	return gpuTelemetry{
		Refreshed:          true,
		Available:          true,
		UtilizationPercent: values[0],
		MemoryUsedMiB:      values[1],
		MemoryTotalMiB:     values[2],
		PowerDrawW:         values[3],
		PowerLimitW:        values[4],
		TemperatureC:       values[5],
		Meta:               sampleMeta{SampledAt: sampledAt},
	}, nil
}

func parseOptionalTelemetryFloat(value string) (*float64, error) {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if value == "" || lower == "n/a" || strings.Contains(lower, "not supported") || lower == "[n/a]" {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid numeric value %q", value)
	}
	return &parsed, nil
}

func compactTelemetryError(err error) string {
	text := strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", " ")
	if len(text) > 180 {
		return text[:177] + "..."
	}
	return text
}
