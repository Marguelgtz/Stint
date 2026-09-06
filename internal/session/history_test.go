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
