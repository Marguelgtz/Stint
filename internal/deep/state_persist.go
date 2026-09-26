package deep

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SaveDir persists the session state and the latest-session pointer.
func (s DeepState) SaveDir(stateDir string) error {
	if s.SessionID == "" {
		return fmt.Errorf("deep state session id is empty")
	}
	return withRunStateLock(stateDir, s.SessionID, func(dir string) error {
		current, err := readStateFileIfPresent(dir, s.SessionID)
		if err != nil {
			return err
		}
		if current != nil {
			recovered, _, err := recoverProjectionLocked(stateDir, dir, *current, true)
			if err != nil {
				return err
			}
			if recovered.RunEventSchemaVersion != 0 || recovered.RunEventWatermark != 0 {
				if !missionOutcomeProjectionValid(s) {
					return fmt.Errorf("journal-backed mission outcome must match its current deterministic evidence")
				}
				if s.RunEventSchemaVersion != recovered.RunEventSchemaVersion || s.RunEventWatermark != recovered.RunEventWatermark ||
					s.RunID != recovered.RunID || s.ExecutionEpochID != recovered.ExecutionEpochID ||
					!sameLifecycleProjection(s, recovered) {
					return fmt.Errorf("journal-backed lifecycle state must be changed through a RunEvent transition")
				}
			}
		}
		return writeProjectionLocked(stateDir, s)
	})
}

// SaveMissionCopy keeps the original mission text next to the state.
func SaveMissionCopy(stateDir, sessionID, missionPath string) error {
	data, err := os.ReadFile(missionPath)
	if err != nil {
		return fmt.Errorf("read mission %s: %w", missionPath, err)
	}
	return writeAtomic(filepath.Join(DeepDir(stateDir, sessionID), "mission.md"), data)
}

// LoadState reads a session's state by ID.
func LoadState(stateDir, sessionID string) (DeepState, error) {
	var state DeepState
	err := withRunStateLock(stateDir, sessionID, func(dir string) error {
		loaded, err := readStateFile(dir, sessionID)
		if err != nil {
			return err
		}
		state, _, err = recoverProjectionLocked(stateDir, dir, loaded, true)
		return err
	})
	if err != nil {
		return DeepState{}, err
	}
	return state, nil
}

func readStateFileIfPresent(dir, sessionID string) (*DeepState, error) {
	data, err := os.ReadFile(filepath.Join(dir, "deep.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read deep state %s: %w", sessionID, err)
	}
	var state DeepState
	if err := unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.SessionID != sessionID {
		return nil, fmt.Errorf("deep state session id mismatch")
	}
	return &state, nil
}

// writeProjectionLocked persists deep.json and the latest-session pointer.
// Callers hold the session's run-event lock. Journal transitions invoke it
// only after the event file has been synced.
func writeProjectionLocked(stateDir string, s DeepState) error {
	dir := DeepDir(stateDir, s.SessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create deep state dir: %w", err)
	}
	s.UpdatedAt = time.Now().UTC()
	data, err := marshalIndent(s)
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(dir, "deep.json"), data); err != nil {
		return err
	}
	return writeAtomic(LatestFile(stateDir), []byte(s.SessionID+"\n"))
}

// LoadLatestState resolves the latest session pointer and loads its state.
func LoadLatestState(stateDir string) (DeepState, error) {
	data, err := os.ReadFile(LatestFile(stateDir))
	if err != nil {
		return DeepState{}, fmt.Errorf("no deep session recorded: %w", err)
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return DeepState{}, fmt.Errorf("no deep session recorded")
	}
	return LoadState(stateDir, id)
}

// AppendLog records a coordinator line (best effort: observability must
// never fail the run).
func AppendLog(stateDir string, s DeepState, format string, args ...any) {
	line := fmt.Sprintf("%s %s\n", time.Now().UTC().Format("2006-01-02T15:04:05Z"), fmt.Sprintf(format, args...))
	path := filepath.Join(DeepDir(stateDir, s.SessionID), "coordinator.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".deep-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install %s: %w", filepath.Base(path), err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure %s: %w", filepath.Base(path), err)
	}
	if err := syncDirectory(dir); err != nil {
		return fmt.Errorf("sync %s directory: %w", filepath.Base(path), err)
	}
	return nil
}

func syncDirectory(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func missionOutcomeProjectionValid(state DeepState) bool {
	if state.RunEventSchemaVersion == 0 {
		return true
	}
	if state.Phase == PhaseLanded {
		return state.MissionOutcome == DetermineMissionOutcome(state)
	}
	if state.Phase == PhaseInitializing || state.Phase == PhaseExecuting || state.Phase == PhaseLanding {
		return state.MissionOutcome == MissionOutcomePending
	}
	return true
}
