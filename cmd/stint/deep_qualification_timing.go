package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

type qualificationTimingRecord struct {
	SchemaVersion        int           `json:"schemaVersion"`
	RunID                string        `json:"runId"`
	EpochID              string        `json:"epochId,omitempty"`
	Phase                string        `json:"phase"`
	TaskID               string        `json:"taskId,omitempty"`
	Attempt              int           `json:"attempt,omitempty"`
	StartedAtUTC         time.Time     `json:"startedAtUtc"`
	EndedAtUTC           time.Time     `json:"endedAtUtc"`
	DurationMilliseconds int64         `json:"durationMilliseconds"`
	DurationSource       string        `json:"durationSource"`
	ProviderRuntime      time.Duration `json:"providerRuntime,omitempty"`
	PacketBytes          int           `json:"packetBytes,omitempty"`
}

func (c *deepCoordinator) timingEnabled() bool {
	return os.Getenv("STINT_ONBOX_QUALIFICATION_EXPORT") == "1" && c != nil && c.state != nil && c.state.RunID != ""
}

func (c *deepCoordinator) recordTiming(phase, taskID string, attempt int, started time.Time) {
	c.recordTimingWithPacket(phase, taskID, attempt, started, 0)
}

func (c *deepCoordinator) recordTimingWithPacket(phase, taskID string, attempt int, started time.Time, packetBytes int) {
	if !c.timingEnabled() || started.IsZero() {
		return
	}
	endedAt := time.Now()
	record := qualificationTimingRecord{
		SchemaVersion: 1, RunID: c.state.RunID, EpochID: c.state.ExecutionEpochID,
		Phase: phase, TaskID: taskID, Attempt: attempt,
		StartedAtUTC: started.UTC(), EndedAtUTC: endedAt.UTC(),
		DurationMilliseconds: max(0, time.Since(started).Milliseconds()),
		DurationSource:       "process-local-monotonic", PacketBytes: packetBytes,
	}
	c.appendTiming(record)
}

func (c *deepCoordinator) recordProviderRuntime(taskID string, attempt int, runtime time.Duration) {
	if !c.timingEnabled() || runtime < 0 {
		return
	}
	now := time.Now().UTC()
	record := qualificationTimingRecord{
		SchemaVersion: 1, RunID: c.state.RunID, EpochID: c.state.ExecutionEpochID,
		Phase: "executor.provider_runtime", TaskID: taskID, Attempt: attempt,
		StartedAtUTC: now.Add(-runtime), EndedAtUTC: now,
		DurationMilliseconds: max(0, runtime.Milliseconds()), DurationSource: "executor-receipt",
		ProviderRuntime: runtime,
	}
	c.appendTiming(record)
}

func (c *deepCoordinator) recordMeasuredTiming(phase, taskID string, attempt int, duration time.Duration, source string) {
	if !c.timingEnabled() || duration < 0 {
		return
	}
	ended := time.Now().UTC()
	started := ended.Add(-duration)
	c.appendTiming(qualificationTimingRecord{
		SchemaVersion: 1, RunID: c.state.RunID, EpochID: c.state.ExecutionEpochID,
		Phase: phase, TaskID: taskID, Attempt: attempt,
		StartedAtUTC: started, EndedAtUTC: ended,
		DurationMilliseconds: duration.Milliseconds(), DurationSource: source,
	})
}

func (c *deepCoordinator) appendTiming(record qualificationTimingRecord) {
	dir := deep.DeepDir(c.stateDir, c.state.SessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	path := filepath.Join(dir, "qualification-timings.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_ = file.Chmod(0o600)
	_ = json.NewEncoder(file).Encode(record)
	_ = file.Sync()
	_ = file.Close()
}

func (c *deepCoordinator) captureVerificationSubject(phase string, dir string, paths []string, taskID string, attempt int) (verificationSnapshot, error) {
	started := time.Now()
	c.installQualificationTiming(taskID, attempt)
	subject, err := c.git.verificationSubject(dir, paths)
	c.recordTiming(phase, taskID, attempt, started)
	return subject, err
}

func (c *deepCoordinator) installQualificationTiming(taskID string, attempt int) {
	if runner, ok := c.git.(*gitRunner); ok {
		runner.timingTaskID = taskID
		runner.timingAttempt = attempt
		runner.timing = c.recordTiming
	}
}
