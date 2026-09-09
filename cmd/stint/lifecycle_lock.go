package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Marguelgtz/Stint/internal/config"
)

const lifecycleLockFile = "lifecycle.lock"

// acquireLifecycleLock prevents start, resume, and down from mutating the
// session and tunnel concurrently. The kernel releases the lock if the owning
// process exits, so an interrupted startup cannot leave a stale lock behind.
func acquireLifecycleLock(paths config.Paths) (func(), error) {
	if err := paths.Ensure(); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(paths.StateDir, lifecycleLockFile)
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lifecycle lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, errors.New("another Stint start, resume, or down command is already running; wait for it to finish or interrupt it before retrying")
		}
		return nil, fmt.Errorf("lock Stint lifecycle: %w", err)
	}

	release := func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}
	return release, nil
}
