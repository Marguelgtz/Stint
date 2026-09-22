package deep

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SaveDir persists the session state and the latest-session pointer.
func (s DeepState) SaveDir(stateDir string) error {
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
	data, err := os.ReadFile(filepath.Join(DeepDir(stateDir, sessionID), "deep.json"))
	if err != nil {
		return DeepState{}, fmt.Errorf("read deep state %s: %w", sessionID, err)
	}
	var state DeepState
	if err := unmarshal(data, &state); err != nil {
		return DeepState{}, err
	}
	if state.SessionID != sessionID {
		return DeepState{}, fmt.Errorf("deep state session id mismatch")
	}
	return state, nil
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
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install %s: %w", filepath.Base(path), err)
	}
	return os.Chmod(path, 0o600)
}
