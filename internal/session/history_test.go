package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
)

func TestArchiveStateAndLoadLastArchive(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{StateDir: filepath.Join(root, "state")}
	state := State{InstanceID: 42, Status: StatusRecoverable, UpdatedAt: time.Now().UTC()}
	if err := ArchiveState(paths, state, DispositionDestroyUnconfirmed, false, 3, "dns timeout"); err != nil {
		t.Fatalf("ArchiveState: %v", err)
	}
	archive, err := LoadLastArchive(paths)
	if err != nil {
		t.Fatalf("LoadLastArchive: %v", err)
	}
	if archive.State.InstanceID != 42 {
		t.Fatalf("instance = %d, want 42", archive.State.InstanceID)
	}
	if archive.DestroyConfirmed {
		t.Fatal("destroy should be unconfirmed")
	}
	if archive.DestroyAttempts != 3 || archive.DestroyLastError != "dns timeout" {
		t.Fatalf("unexpected destroy metadata: %+v", archive)
	}
	if _, err := os.Stat(filepath.Join(SessionDir(paths, 42), "session.json")); err != nil {
		t.Fatalf("archive file missing: %v", err)
	}
}

func TestSessionLogPathsAreIsolatedByInstance(t *testing.T) {
	paths := config.Paths{StateDir: filepath.Join(t.TempDir(), "state")}
	first := SessionLogPath(paths, 42, "tunnel.log")
	second := SessionLogPath(paths, 43, "tunnel.log")
	if first == second {
		t.Fatalf("session logs share a path: %s", first)
	}
	if filepath.Dir(first) != SessionDir(paths, 42) || filepath.Dir(second) != SessionDir(paths, 43) {
		t.Fatalf("session logs are not scoped to their instance directories")
	}
}

func TestLoadLastArchiveChoosesNewestArchive(t *testing.T) {
	paths := config.Paths{StateDir: filepath.Join(t.TempDir(), "state")}
	if err := ArchiveState(paths, State{InstanceID: 1}, DispositionDestroyedManual, true, 1, ""); err != nil {
		t.Fatalf("archive first: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := ArchiveState(paths, State{InstanceID: 2}, DispositionDestroyedWatchdog, true, 1, ""); err != nil {
		t.Fatalf("archive second: %v", err)
	}
	archive, err := LoadLastArchive(paths)
	if err != nil {
		t.Fatalf("LoadLastArchive: %v", err)
	}
	if archive.State.InstanceID != 2 {
		t.Fatalf("last archive instance = %d, want 2", archive.State.InstanceID)
	}
}
