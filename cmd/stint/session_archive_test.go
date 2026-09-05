package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func archiveTestPaths(t *testing.T) config.Paths {
	t.Helper()
	dir := t.TempDir()
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths: %v", err)
	}
	paths.StateDir = dir
	return paths
}

func TestArchiveSessionWritesInstanceStampedCopy(t *testing.T) {
	paths := archiveTestPaths(t)
	state := sessionstate.State{InstanceID: 75893, HourlyUSD: 0.37}
	if err := sessionstate.Save(paths, state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	at := time.Date(2026, 9, 5, 2, 15, 0, 0, time.UTC)
	ArchiveSession(paths, state, at)

	dir := sessionArchiveDir(paths)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("archive has %d entries, want 1", len(entries))
	}
	got := entries[0].Name()
	want := "75893.2026-09-05T02-15-00Z.json"
	if got != want {
		t.Fatalf("archive name = %q, want %q", got, want)
	}
	data, err := os.ReadFile(filepath.Join(dir, got))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	// The archive must be a faithful copy of the file that was cleared,
	// including the updatedAt Save() stamped in (not a re-serialization).
	var roundTripped sessionstate.State
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("archive is not valid JSON: %v", err)
	}
	if roundTripped.InstanceID != 75893 {
		t.Fatalf("archived instance = %d, want 75893", roundTripped.InstanceID)
	}
	if roundTripped.UpdatedAt.IsZero() {
		t.Fatalf("archived state lost its updatedAt stamp")
	}
}

func TestArchiveSessionToleratesMissingStateFile(t *testing.T) {
	paths := archiveTestPaths(t)
	state := sessionstate.State{InstanceID: 1}
	// No session.json on disk: the copy must be skipped without panicking or
	// blocking teardown, and must not create the archive dir.
	ArchiveSession(paths, state, time.Now().UTC())
	if _, err := os.ReadDir(sessionArchiveDir(paths)); err == nil {
		t.Fatalf("archive dir should not exist when there is no state file")
	}
}

func TestArchiveSessionDoesNotBlockOnUnwritableDir(t *testing.T) {
	paths := archiveTestPaths(t)
	state := sessionstate.State{InstanceID: 2}
	if err := sessionstate.Save(paths, state); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Make the archive dir impossible to create: a *file* occupies the path.
	blocker := filepath.Join(paths.StateDir, "archive")
	if err := os.WriteFile(blocker, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	// Must return without panicking; teardown continues.
	ArchiveSession(paths, state, time.Now().UTC())
	if _, err := os.Stat(blocker); err != nil {
		t.Fatalf("blocker removed: %v", err)
	}
}

func TestArchiveSessionSkipsInstanceWithoutID(t *testing.T) {
	paths := archiveTestPaths(t)
	// InstanceID <= 0 is a defensive no-op (session state always carries one).
	ArchiveSession(paths, sessionstate.State{}, time.Now().UTC())
	if _, err := os.ReadDir(sessionArchiveDir(paths)); err == nil {
		t.Fatalf("archive dir should not exist for a state with no instance id")
	}
}