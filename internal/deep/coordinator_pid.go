package deep

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// CoordinatorPidFile records which process is coordinating a session. It is
// what lets `stint deep resume` refuse to start a second coordinator for a
// session whose first one is still alive. Crash tolerance comes from a
// liveness probe: a stale pid file (crash, power loss) is harmless.
func CoordinatorPidFile(stateDir, sessionID string) string {
	return filepath.Join(DeepDir(stateDir, sessionID), "coordinator.pid")
}

// WriteCoordinatorPid installs the pid file atomically (0600).
func WriteCoordinatorPid(stateDir, sessionID string, pid int) error {
	return writeAtomic(CoordinatorPidFile(stateDir, sessionID), []byte(strconv.Itoa(pid)+"\n"))
}

// ClearCoordinatorPid removes the pid file; a missing file is not an error.
func ClearCoordinatorPid(stateDir, sessionID string) error {
	err := os.Remove(CoordinatorPidFile(stateDir, sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// CoordinatorAlive probes whether the recorded coordinator process is alive
// (signal 0: no signal is sent). A missing, malformed, or stale file reports
// not alive, with the pid returned when one was recorded.
func CoordinatorAlive(stateDir, sessionID string) (bool, int) {
	data, err := os.ReadFile(CoordinatorPidFile(stateDir, sessionID))
	if err != nil {
		return false, 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false, 0
	}
	// EPERM means the process exists but belongs to another user: alive.
	switch syscall.Kill(pid, 0) {
	case nil, syscall.EPERM:
		return true, pid
	default:
		return false, pid
	}
}
