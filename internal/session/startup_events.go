package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
)

const startupEventsFileName = "startup-events.jsonl"

type StartupEvent struct {
	RecordedAt    time.Time `json:"recordedAt"`
	ElapsedMillis int64     `json:"elapsedMillis,omitempty"`
	InstanceID    int64     `json:"instanceId"`
	OfferID       string    `json:"offerId,omitempty"`
	GPUModel      string    `json:"gpuModel,omitempty"`
	Runtime       string    `json:"runtime,omitempty"`
	Status        string    `json:"status"`
	Checkpoint    string    `json:"checkpoint,omitempty"`
}

func StartupEventsPath(paths config.Paths) string {
	return filepath.Join(paths.StateDir, startupEventsFileName)
}

func appendStartupEvent(paths config.Paths, state State) error {
	if !isStartupStatus(state.Status) {
		return nil
	}
	event := StartupEvent{
		RecordedAt: state.UpdatedAt,
		InstanceID: state.InstanceID,
		OfferID:    state.OfferID,
		GPUModel:   state.GPUModel,
		Runtime:    state.Runtime,
		Status:     state.Status,
		Checkpoint: state.Checkpoint,
	}
	if !state.StartedAt.IsZero() && !state.UpdatedAt.Before(state.StartedAt) {
		event.ElapsedMillis = state.UpdatedAt.Sub(state.StartedAt).Milliseconds()
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode startup event: %w", err)
	}
	data = append(data, '\n')
	file, err := os.OpenFile(StartupEventsPath(paths), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open startup event log: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write startup event: %w", err)
	}
	return file.Chmod(0o600)
}

func LoadStartupEvents(paths config.Paths) ([]StartupEvent, error) {
	file, err := os.Open(StartupEventsPath(paths))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var events []StartupEvent
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		var event StartupEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("parse startup event line %d: %w", line, err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read startup event log: %w", err)
	}
	return events, nil
}

func isStartupStatus(status string) bool {
	switch status {
	case StatusRenting,
		StatusBooting,
		StatusSSHConnecting,
		StatusSSHReady,
		StatusRuntimeBootstrap,
		StatusRuntimeReady,
		StatusModelStarting,
		StatusModelStarted,
		StatusModelLoading,
		StatusReady,
		StatusRecoverable:
		return true
	default:
		return false
	}
}
