package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
)

func TestStartupEventsRecordPhasesAndRentalElapsedTime(t *testing.T) {
	paths := startupTestPaths(t)
	rentalStartedAt := time.Now().UTC().Add(-2 * time.Minute)
	state := State{
		InstanceID:      42,
		OfferID:         "offer-123",
		GPUModel:        "RTX_4090",
		Runtime:         "ninfer",
		StartedAt:       rentalStartedAt,
		RentalStartedAt: rentalStartedAt,
	}

	transitions := []struct {
		status string
		phase  string
	}{
		{status: StatusBooting, phase: StartupPhaseInstanceCreated},
		{status: StatusSSHConnecting, phase: StartupPhaseSSHMetadataAvailable},
		{status: StatusSSHReady, phase: StartupPhaseSSHAuthenticated},
		{status: StatusSSHReady, phase: StartupPhaseNetworkQualified},
		{status: StatusRuntimeBootstrap, phase: StartupPhaseRuntimePreparing},
		{status: StatusRuntimeReady, phase: StartupPhaseRuntimeReady},
		{status: StatusModelStarting, phase: StartupPhaseModelPreparing},
		{status: StatusModelStarted, phase: StartupPhaseModelStarted},
		{status: StatusModelLoading, phase: StartupPhaseModelLoading},
		{status: StatusReady, phase: StartupPhaseReady},
	}
	for _, transition := range transitions {
		state.Status = transition.status
		state.StartupPhase = ""
		if transition.phase == StartupPhaseNetworkQualified {
			state.StartupPhase = transition.phase
		}
		if err := Save(paths, state); err != nil {
			t.Fatalf("save %s: %v", transition.phase, err)
		}
	}

	events, err := LoadStartupEvents(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != len(transitions) {
		t.Fatalf("got %d startup events, want %d", len(events), len(transitions))
	}
	for i, transition := range transitions {
		if events[i].Phase != transition.phase {
			t.Fatalf("event %d phase = %q, want %q", i, events[i].Phase, transition.phase)
		}
		if events[i].InstanceID != 42 || events[i].OfferID != "offer-123" || events[i].Runtime != "ninfer" {
			t.Fatalf("event %d lost session identity: %+v", i, events[i])
		}
		if events[i].RentalElapsedMillis == nil || *events[i].RentalElapsedMillis <= 0 {
			t.Fatalf("event %d has no rental elapsed time: %+v", i, events[i])
		}
	}
	info, err := os.Stat(StartupEventsPath(paths))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("startup event log mode = %o, want 600", info.Mode().Perm())
	}
}

func TestStartupEventWriteFailureDoesNotBlockStateSave(t *testing.T) {
	paths := startupTestPaths(t)
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(StartupEventsPath(paths), 0o700); err != nil {
		t.Fatal(err)
	}
	state := State{InstanceID: 7, Status: StatusBooting}
	if err := Save(paths, state); err != nil {
		t.Fatalf("best-effort event failure blocked authoritative state save: %v", err)
	}
	loaded, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.InstanceID != state.InstanceID || loaded.Status != StatusBooting {
		t.Fatalf("saved lifecycle state changed: %+v", loaded)
	}
}

func TestClearPreservesStartupEvents(t *testing.T) {
	paths := startupTestPaths(t)
	state := State{InstanceID: 9, Status: StatusReady}
	if err := Save(paths, state); err != nil {
		t.Fatal(err)
	}
	if err := Clear(paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(paths)); !os.IsNotExist(err) {
		t.Fatalf("session snapshot still exists after clear: %v", err)
	}
	events, err := LoadStartupEvents(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Phase != StartupPhaseReady {
		t.Fatalf("startup event history did not survive clear: %+v", events)
	}
}

func TestNonStartupStateDoesNotCreateStartupLog(t *testing.T) {
	paths := startupTestPaths(t)
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Save(paths, State{InstanceID: 11, Status: StatusRecoverable}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(StartupEventsPath(paths)); !os.IsNotExist(err) {
		t.Fatalf("non-startup state created an event log: %v", err)
	}
}

func startupTestPaths(t *testing.T) config.Paths {
	t.Helper()
	root := t.TempDir()
	return config.Paths{
		ConfigDir:     filepath.Join(root, "config"),
		StateDir:      filepath.Join(root, "state"),
		SSHDir:        filepath.Join(root, "config", "ssh"),
		SSHPrivateKey: filepath.Join(root, "config", "ssh", "id_ed25519"),
		SSHPublicKey:  filepath.Join(root, "config", "ssh", "id_ed25519.pub"),
	}
}
