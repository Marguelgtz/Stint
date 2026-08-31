package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
)

const stateFileName = "session.json"

const (
	StatusRenting          = "RENTING"
	StatusBooting          = "BOOTING"
	StatusSSHConnecting    = "SSH_CONNECTING"
	StatusSSHReady         = "SSH_READY"
	StatusRuntimeBootstrap = "RUNTIME_BOOTSTRAP"
	StatusRuntimeReady     = "RUNTIME_READY"
	StatusModelStarting    = "MODEL_STARTING"
	StatusModelStarted     = "MODEL_STARTED"
	StatusModelLoading     = "MODEL_LOADING"
	StatusReady            = "READY"
	StatusRecoverable      = "RECOVERABLE"
)

const (
	CheckpointInstanceCreated = "INSTANCE_CREATED"
	CheckpointSSHReady        = "SSH_READY"
	CheckpointRuntimeReady    = "RUNTIME_READY"
	CheckpointModelStarted    = "MODEL_STARTED"
	CheckpointReady           = "READY"
)

type State struct {
	InstanceID     int64     `json:"instanceId"`
	OfferID        string    `json:"offerId"`
	Profile        string    `json:"profile"`
	GPUModel       string    `json:"gpuModel"`
	RuntimeRequest string    `json:"runtimeRequest,omitempty"`
	Runtime        string    `json:"runtime,omitempty"`
	ContextTokens  int       `json:"contextTokens,omitempty"`
	Clients        int       `json:"clients,omitempty"`
	HourlyUSD      float64   `json:"hourlyUsd"`
	Hours          float64   `json:"hours"`
	StartedAt      time.Time `json:"startedAt"`
	Deadline       time.Time `json:"deadline"`
	SSHHost        string    `json:"sshHost,omitempty"`
	SSHPort        int       `json:"sshPort,omitempty"`
	TunnelPID      int       `json:"tunnelPid,omitempty"`
	WatchdogPID    int       `json:"watchdogPid,omitempty"`
	Status         string    `json:"status"`
	Checkpoint     string    `json:"checkpoint,omitempty"`
	LastError      string    `json:"lastError,omitempty"`
	UpdatedAt      time.Time `json:"updatedAt,omitempty"`
}

func Path(paths config.Paths) string {
	return filepath.Join(paths.StateDir, stateFileName)
}

func Load(paths config.Paths) (State, error) {
	data, err := os.ReadFile(Path(paths))
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("parse session state: %w", err)
	}
	if state.InstanceID <= 0 {
		return State{}, errors.New("session state has no Vast instance id")
	}
	return state, nil
}

func Save(paths config.Paths, state State) error {
	if state.InstanceID <= 0 {
		return errors.New("refusing to save session without Vast instance id")
	}
	if err := paths.Ensure(); err != nil {
		return err
	}
	state.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session state: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(paths.StateDir, ".session-*")
	if err != nil {
		return fmt.Errorf("create session temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure session temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write session state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close session state: %w", err)
	}
	if err := os.Rename(tmpName, Path(paths)); err != nil {
		return fmt.Errorf("install session state: %w", err)
	}
	if err := os.Chmod(Path(paths), 0o600); err != nil {
		return fmt.Errorf("secure session state: %w", err)
	}

	// Startup history is observational and must never become a prerequisite for
	// lifecycle persistence. Record the transition only after session.json is
	// safely installed, and deliberately ignore telemetry-write failures.
	_ = appendStartupEvent(paths, state)
	return nil
}

func Clear(paths config.Paths) error {
	err := os.Remove(Path(paths))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
