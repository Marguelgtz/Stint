package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const (
	watchdogDeadlinePollInterval = time.Second
	watchdogDestroyRetryInterval = 15 * time.Second
)

type deadlineWatchdogDeps struct {
	load       func() (sessionstate.State, error)
	now        func() time.Time
	wait       func(context.Context, time.Duration) error
	lock       func() (func(), error)
	destroy    func(sessionstate.State) error
	recordFail func(sessionstate.State, error)
}

// runDynamicWatchdog is the deadline-aware replacement for the original
// one-shot watchdog. It captures the instance it is responsible for, then
// re-reads session.json while waiting so extend/shorten operations take effect
// without killing and replacing the watchdog process.
func runDynamicWatchdog(args []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	var expectedInstanceID int64
	if len(args) > 1 {
		return errors.New("internal watchdog accepts at most one instance id")
	}
	if len(args) == 1 {
		expectedInstanceID, err = strconv.ParseInt(args[0], 10, 64)
		if err != nil || expectedInstanceID <= 0 {
			return fmt.Errorf("invalid watchdog instance id %q", args[0])
		}
	} else {
		state, loadErr := sessionstate.Load(paths)
		if errors.Is(loadErr, os.ErrNotExist) {
			return nil
		}
		if loadErr != nil {
			return loadErr
		}
		expectedInstanceID = state.InstanceID
	}

	deps := deadlineWatchdogDeps{
		load: func() (sessionstate.State, error) {
			return sessionstate.Load(paths)
		},
		now: time.Now,
		wait: func(ctx context.Context, duration time.Duration) error {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
		lock: func() (func(), error) {
			return acquireLifecycleLockBlocking(paths)
		},
		destroy: func(state sessionstate.State) error {
			return destroyExpiredSession(paths, state)
		},
		recordFail: func(state sessionstate.State, destroyErr error) {
			state.LastError = "deadline destroy failed: " + destroyErr.Error()
			_ = sessionstate.Save(paths, state)
		},
	}

	return watchSessionDeadline(context.Background(), expectedInstanceID, watchdogDeadlinePollInterval, watchdogDestroyRetryInterval, deps)
}

func watchSessionDeadline(
	ctx context.Context,
	expectedInstanceID int64,
	pollInterval time.Duration,
	destroyRetryInterval time.Duration,
	deps deadlineWatchdogDeps,
) error {
	if expectedInstanceID <= 0 {
		return errors.New("watchdog requires a positive instance id")
	}
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	if destroyRetryInterval <= 0 {
		destroyRetryInterval = 15 * time.Second
	}

	for {
		state, err := deps.load()
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("load session state: %w", err)
		}
		if state.InstanceID != expectedInstanceID {
			// A watchdog from an older session must never touch a replacement
			// instance that reused the same local state path.
			return nil
		}

		remaining := sessionstate.Remaining(state, deps.now())
		if remaining > 0 {
			wait := pollInterval
			if remaining < wait {
				wait = remaining
			}
			if err := deps.wait(ctx, wait); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil
				}
				return err
			}
			continue
		}

		// The apparent deadline has arrived. Serialize with start/resume/down and
		// deadline mutation, then re-read state under the lock. This closes the
		// race where an extension commits at the same instant the old deadline
		// expires.
		release, err := deps.lock()
		if err != nil {
			return fmt.Errorf("lock lifecycle at deadline: %w", err)
		}
		fresh, loadErr := deps.load()
		if errors.Is(loadErr, os.ErrNotExist) {
			release()
			return nil
		}
		if loadErr != nil {
			release()
			return fmt.Errorf("reload session state at deadline: %w", loadErr)
		}
		if fresh.InstanceID != expectedInstanceID {
			release()
			return nil
		}
		if sessionstate.Remaining(fresh, deps.now()) > 0 {
			release()
			continue
		}

		destroyErr := deps.destroy(fresh)
		release()
		if destroyErr == nil {
			return nil
		}
		if deps.recordFail != nil {
			deps.recordFail(fresh, destroyErr)
		}
		// Keep the watchdog alive after a provider/API failure. The paid
		// resource remains recorded and we retry until teardown succeeds or
		// another lifecycle operation removes/replaces the session.
		if err := deps.wait(ctx, destroyRetryInterval); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return err
		}
	}
}

func acquireLifecycleLockBlocking(paths config.Paths) (func(), error) {
	if err := paths.Ensure(); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(paths.StateDir, lifecycleLockFile)
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lifecycle lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock Stint lifecycle: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func destroyExpiredSession(paths config.Paths, state sessionstate.State) error {
	credentials, err := config.LoadCredentials(paths)
	if err != nil {
		return err
	}
	killPID(state.TunnelPID)
	client := vast.NewClient(credentials.Vast.APIKey)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := client.DestroyInstance(ctx, state.InstanceID); err != nil {
		return err
	}
	return sessionstate.Clear(paths)
}
