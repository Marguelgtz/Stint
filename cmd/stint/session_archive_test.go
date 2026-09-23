package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
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
	archivePath, err := ArchiveSession(paths, state, at)
	if err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}
	if archivePath != filepath.Join("archive", "sessions", "75893.2026-09-05T02-15-00.000000000Z.json") {
		t.Fatalf("archive path = %q", archivePath)
	}

	dir := sessionArchiveDir(paths)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("archive has %d entries, want 1", len(entries))
	}
	got := entries[0].Name()
	want := "75893.2026-09-05T02-15-00.000000000Z.json"
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

func TestArchiveSessionDoesNotOverwriteExistingArchive(t *testing.T) {
	paths := archiveTestPaths(t)
	state := sessionstate.State{InstanceID: 75893, HourlyUSD: 0.37}
	if err := sessionstate.Save(paths, state); err != nil {
		t.Fatalf("Save: %v", err)
	}
	at := time.Date(2026, 9, 5, 2, 15, 0, 0, time.UTC)
	first, err := ArchiveSession(paths, state, at)
	if err != nil {
		t.Fatalf("first ArchiveSession: %v", err)
	}
	if err := sessionstate.Save(paths, sessionstate.State{InstanceID: state.InstanceID, Status: "changed"}); err != nil {
		t.Fatalf("replace active state: %v", err)
	}
	if _, err := ArchiveSession(paths, sessionstate.State{InstanceID: state.InstanceID}, at); err == nil {
		t.Fatal("second ArchiveSession unexpectedly overwrote an existing archive")
	}
	data, err := os.ReadFile(filepath.Join(paths.StateDir, first))
	if err != nil {
		t.Fatalf("read first archive: %v", err)
	}
	var got sessionstate.State
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse first archive: %v", err)
	}
	if got.Status == "changed" {
		t.Fatal("the existing archive was overwritten")
	}
}

func TestArchiveSessionFailureKeepsMissingStateVisible(t *testing.T) {
	paths := archiveTestPaths(t)
	state := sessionstate.State{InstanceID: 1}
	// No session.json on disk: archival fails, so lifecycle code must not clear
	// or overwrite the active-session record.
	if _, err := ArchiveSession(paths, state, time.Now().UTC()); err == nil {
		t.Fatal("ArchiveSession unexpectedly succeeded without active state")
	}
	if _, err := os.ReadDir(sessionArchiveDir(paths)); err == nil {
		t.Fatalf("archive dir should not exist when there is no state file")
	}
}

func TestArchiveSessionFailsClosedOnUnwritableDir(t *testing.T) {
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
	// A caller must retain active state if it cannot durably archive it.
	if _, err := ArchiveSession(paths, state, time.Now().UTC()); err == nil {
		t.Fatal("ArchiveSession unexpectedly succeeded through a blocking file")
	}
	if _, err := os.Stat(blocker); err != nil {
		t.Fatalf("blocker removed: %v", err)
	}
}

func TestArchiveSessionRejectsInstanceWithoutID(t *testing.T) {
	paths := archiveTestPaths(t)
	if _, err := ArchiveSession(paths, sessionstate.State{}, time.Now().UTC()); err == nil {
		t.Fatal("ArchiveSession unexpectedly accepted a state without an instance id")
	}
	if _, err := os.ReadDir(sessionArchiveDir(paths)); err == nil {
		t.Fatalf("archive dir should not exist for a state with no instance id")
	}
}

func destroyTestClient(t *testing.T, instanceVisible bool) *vast.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete:
			if r.URL.Path != "/api/v0/instances/75893" {
				t.Errorf("destroy path = %q", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"success":true,"msg":"destroy requested"}`))
		case http.MethodGet:
			if r.URL.Path != "/api/v1/instances/" {
				t.Errorf("inventory path = %q", r.URL.Path)
			}
			if instanceVisible {
				_, _ = w.Write([]byte(`{"success":true,"instances_found":1,"total_instances":1,"instances":[{"id":75893,"actual_status":"running"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"success":true,"instances_found":0,"total_instances":0,"instances":[]}`))
			}
		default:
			t.Errorf("unexpected Vast method %s", r.Method)
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	return &vast.Client{APIKey: "fixture", BaseURL: server.URL, HTTPClient: server.Client()}
}

func TestDestroyAndArchiveKeepsActiveStateUntilVastConfirmsGone(t *testing.T) {
	paths := archiveTestPaths(t)
	state := sessionstate.State{InstanceID: 75893, Status: sessionstate.StatusReady, Deadline: time.Now().Add(time.Hour)}
	if err := sessionstate.Save(paths, state); err != nil {
		t.Fatal(err)
	}
	old := destroyGonePollInterval
	destroyGonePollInterval = time.Millisecond
	t.Cleanup(func() { destroyGonePollInterval = old })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if err := destroyAndArchiveSession(ctx, paths, destroyTestClient(t, true), state); err == nil {
		t.Fatal("destroy succeeded while Vast still listed the instance")
	}
	got, err := sessionstate.Load(paths)
	if err != nil {
		t.Fatalf("active session state was cleared: %v", err)
	}
	if got.InstanceID != state.InstanceID || got.Status != sessionstate.StatusReady {
		t.Fatalf("retained state = %#v, want the original active state", got)
	}
	if _, err := os.ReadDir(sessionArchiveDir(paths)); err == nil {
		t.Fatal("unverified destroy unexpectedly produced a completion archive")
	}
}

func TestDestroyAndArchiveClearsOnlyAfterGoneAndDurableArchive(t *testing.T) {
	paths := archiveTestPaths(t)
	state := sessionstate.State{InstanceID: 75893, Status: sessionstate.StatusReady, Deadline: time.Now().Add(time.Hour)}
	if err := sessionstate.Save(paths, state); err != nil {
		t.Fatal(err)
	}
	old := destroyGonePollInterval
	destroyGonePollInterval = time.Millisecond
	t.Cleanup(func() { destroyGonePollInterval = old })
	if err := destroyAndArchiveSession(context.Background(), paths, destroyTestClient(t, false), state); err != nil {
		t.Fatalf("destroyAndArchiveSession: %v", err)
	}
	if _, err := sessionstate.Load(paths); !os.IsNotExist(err) {
		t.Fatalf("active state error = %v, want file removed after archive", err)
	}
	entries, err := os.ReadDir(sessionArchiveDir(paths))
	if err != nil || len(entries) != 1 {
		t.Fatalf("archive entries = %v, err = %v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(sessionArchiveDir(paths), entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var archived sessionstate.State
	if err := json.Unmarshal(data, &archived); err != nil {
		t.Fatal(err)
	}
	if archived.Status != "STOPPED" {
		t.Fatalf("archived state = %q, want STOPPED", archived.Status)
	}
}

func TestDestroyAndArchiveFinalizesWhenDeleteRetryReportsAlreadyGone(t *testing.T) {
	paths := archiveTestPaths(t)
	state := sessionstate.State{InstanceID: 75893, Status: sessionstate.StatusReady}
	if err := sessionstate.Save(paths, state); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete:
			http.Error(w, "instance already destroyed", http.StatusNotFound)
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"success":true,"instances_found":0,"total_instances":0,"instances":[]}`))
		default:
			t.Errorf("unexpected Vast method %s", r.Method)
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	client := &vast.Client{APIKey: "fixture", BaseURL: server.URL, HTTPClient: server.Client()}
	if err := destroyAndArchiveSession(context.Background(), paths, client, state); err != nil {
		t.Fatalf("destroyAndArchiveSession: %v", err)
	}
	if _, err := sessionstate.Load(paths); !os.IsNotExist(err) {
		t.Fatalf("active state error = %v, want cleared after inventory confirmed absence", err)
	}
	entries, err := os.ReadDir(sessionArchiveDir(paths))
	if err != nil || len(entries) != 1 {
		t.Fatalf("archive entries = %v, err = %v", entries, err)
	}
}

func TestDestroyAndArchiveRetainsStoppedStateWhenArchiveFails(t *testing.T) {
	paths := archiveTestPaths(t)
	state := sessionstate.State{InstanceID: 75893, Status: sessionstate.StatusReady, Deadline: time.Now().Add(time.Hour)}
	if err := sessionstate.Save(paths, state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.StateDir, "archive"), []byte("blocker"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := destroyAndArchiveSession(context.Background(), paths, destroyTestClient(t, false), state); err == nil {
		t.Fatal("expected archive failure")
	}
	got, err := sessionstate.Load(paths)
	if err != nil {
		t.Fatalf("active state was cleared after archive failure: %v", err)
	}
	if got.Status != "STOPPED" {
		t.Fatalf("retained state status = %q, want STOPPED for provider-confirmed teardown", got.Status)
	}
}
