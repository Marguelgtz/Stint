package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

// sessionArchiveDir is the per-session archive location under the state
// directory: <state>/archive/sessions/<instanceId>.<timestamp>.json.
func sessionArchiveDir(paths config.Paths) string {
	return filepath.Join(paths.StateDir, "archive", "sessions")
}

// ArchiveSession copies the active session.json before active state is cleared.
// It reads the original bytes so updatedAt and unknown future fields survive.
// Failure is returned to the lifecycle caller, which must retain session.json.
func ArchiveSession(paths config.Paths, state sessionstate.State, at time.Time) (string, error) {
	if state.InstanceID <= 0 {
		return "", fmt.Errorf("session archive requires a positive instance id")
	}
	data, err := os.ReadFile(sessionstate.Path(paths))
	if err != nil {
		return "", fmt.Errorf("read active session for archive: %w", err)
	}
	var archived sessionstate.State
	if err := json.Unmarshal(data, &archived); err != nil {
		return "", fmt.Errorf("parse active session for archive: %w", err)
	}
	if archived.InstanceID != state.InstanceID {
		return "", fmt.Errorf("active session changed before archive: got instance %d, expected %d", archived.InstanceID, state.InstanceID)
	}
	dir := sessionArchiveDir(paths)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create session archive directory %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("secure session archive directory %s: %w", dir, err)
	}
	name := fmt.Sprintf("%d.%s.json", state.InstanceID, at.UTC().Format("2006-01-02T15-04-05.000000000Z"))
	tmp, err := os.CreateTemp(dir, name+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create session archive temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", fmt.Errorf("secure session archive temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write session archive: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", fmt.Errorf("sync session archive: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close session archive: %w", err)
	}
	finalPath := filepath.Join(dir, name)
	if err := os.Link(tmpName, finalPath); err != nil {
		return "", fmt.Errorf("install session archive %s: %w", finalPath, err)
	}
	dirFile, err := os.Open(dir)
	if err != nil {
		return "", fmt.Errorf("open session archive directory for sync: %w", err)
	}
	if err := dirFile.Sync(); err != nil {
		_ = dirFile.Close()
		return "", fmt.Errorf("sync session archive directory: %w", err)
	}
	if err := dirFile.Close(); err != nil {
		return "", fmt.Errorf("close session archive directory: %w", err)
	}
	return filepath.Join("archive", "sessions", name), nil
}

// destroyAndArchiveSession clears active state only after provider inventory
// confirms that the instance is gone and an append-only session copy is
// durable. Callers hold the lifecycle lock while using this helper.
func destroyAndArchiveSession(ctx context.Context, paths config.Paths, client *vast.Client, state sessionstate.State) error {
	probe := instanceGoneProbe(client, state.InstanceID)
	if err := client.DestroyInstance(ctx, state.InstanceID); err != nil {
		// A prior destroy may have succeeded while its response was lost, or a
		// retry may receive 404. Only proceed if an exact inventory check
		// independently confirms that this instance is already gone.
		if probeErr := probe(ctx); probeErr != nil {
			return fmt.Errorf("request Vast destroy for instance %d: %w", state.InstanceID, err)
		}
	} else if err := waitForInstanceGone(ctx, probe); err != nil {
		return err
	}
	return archiveGoneSession(paths, state)
}

// archiveGoneSession finalizes local tracking after an authoritative provider
// response confirms that the exact instance no longer exists.
func archiveGoneSession(paths config.Paths, state sessionstate.State) error {
	state.Status = "STOPPED"
	state.LastError = ""
	if err := sessionstate.Save(paths, state); err != nil {
		return fmt.Errorf("record stopped session: %w", err)
	}
	archivePath, err := ArchiveSession(paths, state, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("archive stopped session: %w", err)
	}
	if err := sessionstate.Clear(paths); err != nil {
		return fmt.Errorf("clear stopped session after archive %s: %w", archivePath, err)
	}
	fmt.Printf("Session archive    %s\n", archivePath)
	return nil
}
