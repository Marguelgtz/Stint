package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
)

func TestSaveAppendsStartupEventsWithoutChangingLifecycleState(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{
		ConfigDir:     filepath.Join(root, "config"),
		StateDir:      filepath.Join(root, "state"),
		SSHDir:        filepath.Join(root, "config", "ssh"),
		SSHPrivateKey: filepath.Join(root, "config", "ssh", "id_ed25519"),
		SSHPublicKey:  filepath.Join(root, "config", "ssh", "id_ed25519.pub"),
	}
	started := time.Now().UTC().Add(-2 * time.Second)
	state := State{
		InstanceID: 42,
		OfferID:    "1234",
		GPUModel:   "RTX_4090",
		Runtime:    "ninfer",
		StartedAt:  started,
		Status:     StatusBooting,
		Checkpoint: CheckpointInstanceCreated,
	}
	if err := Save(paths, state); err != nil {
		t.Fatal(err)
	}
	state.Status = StatusSSHReady
	state.Checkpoint = CheckpointSSHReady
	if err := Save(paths, state); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != StatusSSHReady || loaded.Checkpoint != CheckpointSSHReady {
		t.Fatalf("lifecycle state changed unexpectedly: %#v", loaded)
	}

	events, err := LoadStartupEvents(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("startup events = %d, want 2: %#v", len(events), events)
	}
	if events[0].Status != StatusBooting || events[1].Status != StatusSSHReady {
		t.Fatalf("unexpected startup statuses: %#v", events)
	}
	for _, event := range events {
		if event.InstanceID != 42 || event.OfferID != "1234" || event.Runtime != "ninfer" {
			t.Fatalf("startup identity mismatch: %#v", event)
		}
		if event.ElapsedMillis <= 0 {
			t.Fatalf("elapsedMillis = %d, want > 0", event.ElapsedMillis)
		}
	}

	info, err := os.Stat(StartupEventsPath(paths))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("startup event mode = %o, want 600", got)
	}
}

func TestClearPreservesStartupHistory(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{
		ConfigDir: filepath.Join(root, "config"),
		StateDir:  filepath.Join(root, "state"),
		SSHDir:    filepath.Join(root, "config", "ssh"),
	}
	state := State{InstanceID: 7, StartedAt: time.Now().UTC(), Status: StatusReady}
	if err := Save(paths, state); err != nil {
		t.Fatal(err)
	}
	if err := Clear(paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(paths)); !os.IsNotExist(err) {
		t.Fatalf("session state still exists after Clear: %v", err)
	}
	if _, err := os.Stat(StartupEventsPath(paths)); err != nil {
		t.Fatalf("startup history should survive Clear: %v", err)
	}
}

func TestNonStartupStatusIsNotRecorded(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{StateDir: root}
	state := State{InstanceID: 1, Status: "SOMETHING_ELSE", UpdatedAt: time.Now().UTC()}
	if err := appendStartupEvent(paths, state); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(StartupEventsPath(paths)); !os.IsNotExist(err) {
		t.Fatalf("non-startup status created an event log: %v", err)
	}
}
