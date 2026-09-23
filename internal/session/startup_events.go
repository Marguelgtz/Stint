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

const (
	StartupPhaseInstanceCreated             = "INSTANCE_CREATED"
	StartupPhaseSSHMetadataAvailable        = "SSH_METADATA_AVAILABLE"
	StartupPhaseSSHAuthenticated            = "SSH_AUTHENTICATED"
	StartupPhaseNetworkQualified            = "NETWORK_QUALIFIED"
	StartupPhaseNetworkQualificationSkipped = "NETWORK_QUALIFICATION_SKIPPED"
	StartupPhaseRuntimePreparing            = "RUNTIME_PREPARING"
	StartupPhaseRuntimeReady                = "RUNTIME_READY"
	StartupPhaseModelPreparing              = "MODEL_PREPARING"
	StartupPhaseModelStarted                = "MODEL_STARTED"
	StartupPhaseModelLoading                = "MODEL_LOADING"
	StartupPhaseReady                       = "READY"
)

// StartupEvent is an append-only observation of a saved lifecycle boundary.
// Session State remains authoritative if this log cannot be written.
type StartupEvent struct {
	RecordedAt                   time.Time `json:"recordedAt"`
	RentalElapsedMillis          *int64    `json:"rentalElapsedMillis,omitempty"`
	InstanceID                   int64     `json:"instanceId"`
	OfferID                      string    `json:"offerId,omitempty"`
	GPUModel                     string    `json:"gpuModel,omitempty"`
	Runtime                      string    `json:"runtime,omitempty"`
	RuntimeDeployment            string    `json:"runtimeDeployment,omitempty"`
	RuntimeSourceCommit          string    `json:"runtimeSourceCommit,omitempty"`
	ModelArtifactRevision        string    `json:"modelArtifactRevision,omitempty"`
	ModelArtifactSHA256          string    `json:"modelArtifactSha256,omitempty"`
	ModelArtifactSizeBytes       int64     `json:"modelArtifactSizeBytes,omitempty"`
	ModelArtifactFormat          string    `json:"modelArtifactFormat,omitempty"`
	RuntimeBundleTag             string    `json:"runtimeBundleTag,omitempty"`
	RuntimeBundleSHA256          string    `json:"runtimeBundleSha256,omitempty"`
	RuntimeAcquisitionStartedAt  time.Time `json:"runtimeAcquisitionStartedAt,omitempty"`
	RuntimeAcquiredAt            time.Time `json:"runtimeAcquiredAt,omitempty"`
	RuntimeVerifiedAt            time.Time `json:"runtimeVerifiedAt,omitempty"`
	RuntimeAcquisitionMillis     int64     `json:"runtimeAcquisitionMillis,omitempty"`
	RuntimeVerificationMillis    int64     `json:"runtimeVerificationMillis,omitempty"`
	ModelAcquisitionStartedAt    time.Time `json:"modelAcquisitionStartedAt,omitempty"`
	ModelAcquisitionMillis       int64     `json:"modelAcquisitionMillis,omitempty"`
	ModelAcquiredAt              time.Time `json:"modelAcquiredAt,omitempty"`
	ReadyAt                      time.Time `json:"readyAt,omitempty"`
	ReadyElapsedFromRentalMillis int64     `json:"readyElapsedFromRentalMillis,omitempty"`
	Status                       string    `json:"status"`
	Phase                        string    `json:"phase"`
	Checkpoint                   string    `json:"checkpoint,omitempty"`
}

func StartupEventsPath(paths config.Paths) string {
	return filepath.Join(paths.StateDir, startupEventsFileName)
}

func LoadStartupEvents(paths config.Paths) ([]StartupEvent, error) {
	file, err := os.Open(StartupEventsPath(paths))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	events := make([]StartupEvent, 0)
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
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

func startupPhaseForState(state State) string {
	switch state.Status {
	case StatusBooting:
		return StartupPhaseInstanceCreated
	case StatusSSHConnecting:
		return StartupPhaseSSHMetadataAvailable
	case StatusSSHReady:
		if state.StartupPhase != "" {
			return state.StartupPhase
		}
		return StartupPhaseSSHAuthenticated
	case StatusRuntimeBootstrap:
		return StartupPhaseRuntimePreparing
	case StatusRuntimeReady:
		return StartupPhaseRuntimeReady
	case StatusModelStarting:
		return StartupPhaseModelPreparing
	case StatusModelStarted:
		return StartupPhaseModelStarted
	case StatusModelLoading:
		return StartupPhaseModelLoading
	case StatusReady:
		return StartupPhaseReady
	default:
		return ""
	}
}

func appendStartupEvent(paths config.Paths, state State) error {
	phase := startupPhaseForState(state)
	if phase == "" {
		return nil
	}
	event := StartupEvent{
		RecordedAt:                   state.UpdatedAt,
		InstanceID:                   state.InstanceID,
		OfferID:                      state.OfferID,
		GPUModel:                     state.GPUModel,
		Runtime:                      state.Runtime,
		RuntimeDeployment:            state.RuntimeDeployment,
		RuntimeSourceCommit:          state.RuntimeSourceCommit,
		ModelArtifactRevision:        state.ModelArtifactRevision,
		ModelArtifactSHA256:          state.ModelArtifactSHA256,
		ModelArtifactSizeBytes:       state.ModelArtifactSizeBytes,
		ModelArtifactFormat:          state.ModelArtifactFormat,
		RuntimeBundleTag:             state.RuntimeBundleTag,
		RuntimeBundleSHA256:          state.RuntimeBundleSHA256,
		RuntimeAcquisitionStartedAt:  state.RuntimeAcquisitionStartedAt,
		RuntimeAcquiredAt:            state.RuntimeAcquiredAt,
		RuntimeVerifiedAt:            state.RuntimeVerifiedAt,
		RuntimeAcquisitionMillis:     state.RuntimeAcquisitionMillis,
		RuntimeVerificationMillis:    state.RuntimeVerificationMillis,
		ModelAcquisitionStartedAt:    state.ModelAcquisitionStartedAt,
		ModelAcquisitionMillis:       state.ModelAcquisitionMillis,
		ModelAcquiredAt:              state.ModelAcquiredAt,
		ReadyAt:                      state.ReadyAt,
		ReadyElapsedFromRentalMillis: state.ReadyElapsedFromRentalMillis,
		Status:                       state.Status,
		Phase:                        phase,
		Checkpoint:                   state.Checkpoint,
	}
	if !state.RentalStartedAt.IsZero() && !state.UpdatedAt.Before(state.RentalStartedAt) {
		elapsed := state.UpdatedAt.Sub(state.RentalStartedAt).Milliseconds()
		event.RentalElapsedMillis = &elapsed
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
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("secure startup event log: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("append startup event: %w", err)
	}
	return nil
}
