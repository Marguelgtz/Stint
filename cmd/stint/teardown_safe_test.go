package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestPreserveUnconfirmedDestroyKeepsActiveStateAndArchive(t *testing.T) {
	paths := config.Paths{StateDir: filepath.Join(t.TempDir(), "state")}
	state := sessionstate.State{InstanceID: 42, Status: sessionstate.StatusReady, TunnelPID: 99}
	result := destroyResult{Confirmed: false, Attempts: 2, LastError: errors.New("dns timeout")}

	err := preserveUnconfirmedDestroy(paths, state, result, sessionstate.DispositionDestroyUnconfirmed)
	if err == nil || !strings.Contains(err.Error(), diagnosticDestroyUnconfirmed) {
		t.Fatalf("expected loud unconfirmed-destroy error, got %v", err)
	}
	active, loadErr := sessionstate.Load(paths)
	if loadErr != nil {
		t.Fatalf("load active state: %v", loadErr)
	}
	if active.Status != sessionstate.StatusDestroyUnconfirmed || active.LastError != "dns timeout" {
		t.Fatalf("active state = %+v", active)
	}
	if active.TunnelPID != 0 {
		t.Fatalf("tunnel PID = %d, want 0 after teardown attempt", active.TunnelPID)
	}
	archive, archiveErr := sessionstate.LoadLastArchive(paths)
	if archiveErr != nil {
		t.Fatalf("load archive: %v", archiveErr)
	}
	if archive.DestroyConfirmed || archive.DestroyAttempts != 2 || archive.DestroyLastError != "dns timeout" {
		t.Fatalf("archive = %+v", archive)
	}
}
