package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
)

const lifecycleLockFile = "lifecycle.lock"

const (
	lifecycleOperationStart   = "start"
	lifecycleOperationResume  = "resume"
	lifecycleOperationDown    = "down"
	lifecycleOperationExtend  = "extend"
	lifecycleOperationShorten = "shorten"
	lifecycleOperationUnknown = "lifecycle"

	lifecyclePreemptPollInterval = 100 * time.Millisecond
	lifecyclePreemptTimeout      = 60 * time.Second
)

type lifecycleLockOwner struct {
	PID        int       `json:"pid"`
	Operation  string    `json:"operation"`
	StartedAt  time.Time `json:"startedAt"`
	Executable string    `json:"executable,omitempty"`
}

type lifecycleBusyError struct {
	Owner         lifecycleLockOwner
	OwnerVerified bool
	LockPath      string
}

func (e *lifecycleBusyError) Error() string {
	if e.OwnerVerified && e.Owner.PID > 0 && e.Owner.Operation != "" {
		return fmt.Sprintf(
			"another Stint %s lifecycle command is already running (pid %d); wait for it to finish or run `stint down` to stop an active start/resume",
			e.Owner.Operation,
			e.Owner.PID,
		)
	}
	return "another Stint session lifecycle command is already running; wait for it to finish or interrupt it before retrying"
}

// acquireLifecycleLock prevents paid-session lifecycle mutations (start,
// resume, extend, shorten, down) from changing session state, the tunnel, or
// the deadline concurrently. The kernel flock remains the authority; metadata
// written into the lock file exists only so contention can identify the owner
// and `stint down` can safely interrupt an in-flight start/resume.
func acquireLifecycleLock(paths config.Paths) (func(), error) {
	operation := currentLifecycleOperation()
	release, err := acquireLifecycleLockOnce(paths, operation)
	if err == nil || operation != lifecycleOperationDown {
		return release, err
	}

	var busy *lifecycleBusyError
	if !errors.As(err, &busy) || !busy.OwnerVerified || !lifecycleOperationInterruptible(busy.Owner.Operation) {
		return nil, err
	}
	if !lifecycleOwnerStillHoldsLock(busy.Owner, busy.LockPath) {
		// The metadata can outlive a process after an ungraceful exit, and the
		// deadline watchdog also uses the same kernel lock without owner
		// metadata. Never signal a PID unless that exact process still has this
		// lock file open.
		return nil, err
	}

	if !downAssumesYes() && !confirmLifecycleInterrupt(busy.Owner) {
		return nil, fmt.Errorf("down cancelled; active Stint %s (pid %d) is still running", busy.Owner.Operation, busy.Owner.PID)
	}
	if err := interruptLifecycleOwner(busy.Owner, busy.LockPath); err != nil {
		return nil, err
	}
	fmt.Printf("Stopping active Stint %s (pid %d) before teardown...\n", busy.Owner.Operation, busy.Owner.PID)

	return waitForLifecycleLock(paths, lifecycleOperationDown, lifecyclePreemptTimeout)
}

func acquireLifecycleLockOnce(paths config.Paths, operation string) (func(), error) {
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
			owner, _ := readLifecycleLockOwner(lockPath)
			return nil, &lifecycleBusyError{
				Owner:         owner,
				OwnerVerified: lifecycleOwnerStillHoldsLock(owner, lockPath),
				LockPath:      lockPath,
			}
		}
		return nil, fmt.Errorf("lock Stint lifecycle: %w", err)
	}

	owner := lifecycleLockOwner{
		PID:        os.Getpid(),
		Operation:  normalizeLifecycleOperation(operation),
		StartedAt:  time.Now().UTC(),
		Executable: currentExecutablePath(),
	}
	if err := writeLifecycleLockOwner(file, owner); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, err
	}

	release := func() {
		// Clear diagnostics while the flock is still held. Clearing after unlock
		// could erase metadata written by the next owner.
		_ = file.Truncate(0)
		_, _ = file.Seek(0, 0)
		_ = file.Sync()
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}
	return release, nil
}

func writeLifecycleLockOwner(file *os.File, owner lifecycleLockOwner) error {
	data, err := json.Marshal(owner)
	if err != nil {
		return fmt.Errorf("encode lifecycle lock owner: %w", err)
	}
	data = append(data, '\n')
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("truncate lifecycle lock metadata: %w", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("seek lifecycle lock metadata: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write lifecycle lock metadata: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync lifecycle lock metadata: %w", err)
	}
	return nil
}

func readLifecycleLockOwner(lockPath string) (lifecycleLockOwner, error) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return lifecycleLockOwner{}, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return lifecycleLockOwner{}, errors.New("lifecycle lock owner metadata is empty")
	}
	var owner lifecycleLockOwner
	if err := json.Unmarshal(data, &owner); err != nil {
		return lifecycleLockOwner{}, fmt.Errorf("parse lifecycle lock owner: %w", err)
	}
	return owner, nil
}

func currentLifecycleOperation() string {
	if len(os.Args) < 2 {
		return lifecycleOperationUnknown
	}
	return normalizeLifecycleOperation(os.Args[1])
}

func normalizeLifecycleOperation(operation string) string {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case lifecycleOperationStart:
		return lifecycleOperationStart
	case lifecycleOperationResume:
		return lifecycleOperationResume
	case lifecycleOperationDown:
		return lifecycleOperationDown
	case lifecycleOperationExtend:
		return lifecycleOperationExtend
	case lifecycleOperationShorten:
		return lifecycleOperationShorten
	default:
		return lifecycleOperationUnknown
	}
}

func lifecycleOperationInterruptible(operation string) bool {
	return operation == lifecycleOperationStart || operation == lifecycleOperationResume
}

func downAssumesYes() bool {
	for _, arg := range os.Args[2:] {
		if arg == "--yes" {
			return true
		}
	}
	return false
}

func confirmLifecycleInterrupt(owner lifecycleLockOwner) bool {
	fmt.Printf(
		"Stint %s is still running (pid %d). Interrupt it so `stint down` can continue? [y/N] ",
		owner.Operation,
		owner.PID,
	)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

func interruptLifecycleOwner(owner lifecycleLockOwner, lockPath string) error {
	if !lifecycleOperationInterruptible(owner.Operation) {
		return fmt.Errorf("refusing to interrupt non-preemptible Stint lifecycle operation %q", owner.Operation)
	}
	if !lifecycleOwnerStillHoldsLock(owner, lockPath) {
		return errors.New("lifecycle owner changed before it could be interrupted; retry `stint down`")
	}
	process, err := os.FindProcess(owner.PID)
	if err != nil {
		return fmt.Errorf("find lifecycle owner pid %d: %w", owner.PID, err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("interrupt Stint %s pid %d: %w", owner.Operation, owner.PID, err)
	}
	return nil
}

func waitForLifecycleLock(paths config.Paths, operation string, timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		release, err := acquireLifecycleLockOnce(paths, operation)
		if err == nil {
			return release, nil
		}
		lastErr = err
		var busy *lifecycleBusyError
		if !errors.As(err, &busy) {
			return nil, err
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf(
				"active lifecycle command did not release the lock after graceful termination; Stint did not force-kill it: %w",
				lastErr,
			)
		}
		time.Sleep(lifecyclePreemptPollInterval)
	}
}

func lifecycleOwnerStillHoldsLock(owner lifecycleLockOwner, lockPath string) bool {
	if owner.PID <= 0 {
		return false
	}
	if owner.Executable != "" {
		procExecutable, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", owner.PID))
		if err != nil || cleanExecutablePath(procExecutable) != cleanExecutablePath(owner.Executable) {
			return false
		}
	}

	want, err := filepath.Abs(lockPath)
	if err != nil {
		return false
	}
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", owner.PID))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		target, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", owner.PID, entry.Name()))
		if err != nil {
			continue
		}
		target = strings.TrimSuffix(target, " (deleted)")
		absoluteTarget, err := filepath.Abs(target)
		if err == nil && filepath.Clean(absoluteTarget) == filepath.Clean(want) {
			return true
		}
	}
	return false
}

func currentExecutablePath() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	return cleanExecutablePath(executable)
}

func cleanExecutablePath(path string) string {
	path = strings.TrimSuffix(path, " (deleted)")
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	return filepath.Clean(path)
}
