package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const performanceSampleFileName = "performance.json"

type performanceRecord struct {
	Version           int       `json:"version"`
	InstanceID        int64     `json:"instanceId"`
	Runtime           string    `json:"runtime"`
	ContextTokens     int       `json:"contextTokens"`
	SampledAt         time.Time `json:"sampledAt"`
	TTFTMilliseconds  float64   `json:"ttftMilliseconds"`
	TotalMilliseconds float64   `json:"totalMilliseconds"`
	PromptTokens      int       `json:"promptTokens,omitempty"`
	CompletionTokens  int       `json:"completionTokens,omitempty"`
	DecodeTokensSec   float64   `json:"decodeTokensSec"`
}

func performanceSamplePath(paths config.Paths) string {
	return filepath.Join(paths.StateDir, performanceSampleFileName)
}

func savePerformanceSample(paths config.Paths, state sessionstate.State, sample perfSample, sampledAt time.Time) error {
	if state.InstanceID <= 0 {
		return errors.New("cannot save performance sample without an active instance")
	}
	if err := paths.Ensure(); err != nil {
		return err
	}
	record := performanceRecord{
		Version:           1,
		InstanceID:        state.InstanceID,
		Runtime:           runtimeForState(state),
		ContextTokens:     contextForState(state),
		SampledAt:         sampledAt.UTC(),
		TTFTMilliseconds:  float64(sample.TTFT) / float64(time.Millisecond),
		TotalMilliseconds: float64(sample.Total) / float64(time.Millisecond),
		PromptTokens:      sample.PromptTokens,
		CompletionTokens:  sample.CompletionTokens,
		DecodeTokensSec:   sample.DecodeTokensSec,
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode performance sample: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(paths.StateDir, ".performance-*")
	if err != nil {
		return fmt.Errorf("create performance sample temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write performance sample: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close performance sample: %w", err)
	}
	if err := os.Rename(tmpName, performanceSamplePath(paths)); err != nil {
		return fmt.Errorf("install performance sample: %w", err)
	}
	return os.Chmod(performanceSamplePath(paths), 0o600)
}

func loadPerformanceSnapshot(paths config.Paths, state sessionstate.State, now time.Time) performanceSnapshot {
	data, err := os.ReadFile(performanceSamplePath(paths))
	if errors.Is(err, os.ErrNotExist) {
		return performanceSnapshot{UnavailableReason: "no benchmark sample; run stint perf"}
	}
	if err != nil {
		return performanceSnapshot{UnavailableReason: "read performance sample: " + err.Error()}
	}
	var record performanceRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return performanceSnapshot{UnavailableReason: "parse performance sample: " + err.Error()}
	}
	if record.Version != 1 {
		return performanceSnapshot{UnavailableReason: fmt.Sprintf("unsupported performance sample version %d", record.Version)}
	}
	if record.InstanceID != state.InstanceID {
		return performanceSnapshot{UnavailableReason: "benchmark sample belongs to a previous instance"}
	}
	if record.Runtime != runtimeForState(state) {
		return performanceSnapshot{UnavailableReason: "benchmark sample belongs to a different runtime"}
	}
	if record.ContextTokens != contextForState(state) {
		return performanceSnapshot{UnavailableReason: "benchmark sample belongs to a different context size"}
	}
	age := now.Sub(record.SampledAt)
	if age < 0 {
		age = 0
	}
	return performanceSnapshot{
		Available:        true,
		TTFT:             time.Duration(record.TTFTMilliseconds * float64(time.Millisecond)),
		TotalLatency:     time.Duration(record.TotalMilliseconds * float64(time.Millisecond)),
		PromptTokens:     record.PromptTokens,
		CompletionTokens: record.CompletionTokens,
		DecodeTokensSec:  record.DecodeTokensSec,
		SampledAt:        record.SampledAt,
		Age:              age,
	}
}
