package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
)

const historyDirName = "sessions"

const (
	DispositionDestroyedManual       = "DESTROYED_MANUAL"
	DispositionDestroyedWatchdog     = "DESTROYED_WATCHDOG"
	DispositionDestroyedStartupAbort = "DESTROYED_STARTUP_ABORT"
	DispositionInstanceDisappeared   = "INSTANCE_DISAPPEARED"
	DispositionDestroyUnconfirmed    = "DESTROY_UNCONFIRMED"
)

type Archive struct {
	State             State     `json:"state"`
	Disposition       string    `json:"disposition"`
	DestroyConfirmed  bool      `json:"destroyConfirmed"`
	DestroyAttempts   int       `json:"destroyAttempts,omitempty"`
	DestroyLastError  string    `json:"destroyLastError,omitempty"`
	ArchivedAt        time.Time `json:"archivedAt"`
}

func SessionDir(paths config.Paths, instanceID int64) string {
	return filepath.Join(paths.StateDir, historyDirName, fmt.Sprintf("%d", instanceID))
}

func SessionLogPath(paths config.Paths, instanceID int64, name string) string {
	return filepath.Join(SessionDir(paths, instanceID), name)
}

func EnsureSessionDir(paths config.Paths, instanceID int64) error {
	if instanceID <= 0 {
		return errors.New("cannot create session directory without instance id")
	}
	return os.MkdirAll(SessionDir(paths, instanceID), 0o700)
}

func ArchiveState(paths config.Paths, state State, disposition string, destroyConfirmed bool, attempts int, lastErr string) error {
	if err := EnsureSessionDir(paths, state.InstanceID); err != nil {
		return err
	}
	archive := Archive{
		State: state,
		Disposition: disposition,
		DestroyConfirmed: destroyConfirmed,
		DestroyAttempts: attempts,
		DestroyLastError: lastErr,
		ArchivedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(archive, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session archive: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(SessionDir(paths, state.InstanceID), "session.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write session archive: %w", err)
	}
	return nil
}

func LoadLastArchive(paths config.Paths) (Archive, error) {
	root := filepath.Join(paths.StateDir, historyDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		return Archive{}, err
	}
	type candidate struct {
		archive Archive
		when    time.Time
	}
	var candidates []candidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(root, entry.Name(), "session.json"))
		if readErr != nil {
			continue
		}
		var archive Archive
		if json.Unmarshal(data, &archive) != nil {
			continue
		}
		when := archive.ArchivedAt
		if when.IsZero() {
			when = archive.State.UpdatedAt
		}
		candidates = append(candidates, candidate{archive: archive, when: when})
	}
	if len(candidates) == 0 {
		return Archive{}, os.ErrNotExist
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].when.After(candidates[j].when) })
	return candidates[0].archive, nil
}
