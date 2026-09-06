package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
)

func testLifecyclePaths(t *testing.T) config.Paths {
	t.Helper()
	root := t.TempDir()
	return config.Paths{
		ConfigDir: root + "/config",
		StateDir:  root + "/state",
		SSHDir:    root + "/ssh",
	}
}

func TestLifecycleLockRejectsConcurrentMutationAndReleases(t *testing.T) {
	paths := testLifecyclePaths(t)

	releaseFirst, err := acquireLifecycleLockOnce(paths, lifecycleOperationStart)
	if err != nil {
		t.Fatalf("acquire first lifecycle lock: %v", err)
	}

	if _, err := acquireLifecycleLockOnce(paths, lifecycleOperationResume); err == nil {
		releaseFirst()
		t.Fatal("concurrent lifecycle lock unexpectedly succeeded")
	} else {
		var busy *lifecycleBusyError
		if !errors.As(err, &busy) {
			releaseFirst()
			t.Fatalf("concurrent lifecycle lock error type = %T, want *lifecycleBusyError", err)
		}
		if busy.Owner.PID != os.Getpid() {
			releaseFirst()
			t.Fatalf("busy owner pid = %d, want %d", busy.Owner.PID, os.Getpid())
		}
		if busy.Owner.Operation != lifecycleOperationStart {
			releaseFirst()
			t.Fatalf("busy owner operation = %q, want %q", busy.Owner.Operation, lifecycleOperationStart)
		}
		if !strings.Contains(err.Error(), "already running") {
			releaseFirst()
			t.Fatalf("concurrent lifecycle lock error = %q", err)
		}
		if !lifecycleOwnerStillHoldsLock(busy.Owner, busy.LockPath) {
			releaseFirst()
			t.Fatal("busy owner metadata did not resolve to the process holding the lock file")
		}
	}

	releaseFirst()
	releaseSecond, err := acquireLifecycleLockOnce(paths, lifecycleOperationResume)
	if err != nil {
		t.Fatalf("reacquire released lifecycle lock: %v", err)
	}
	releaseSecond()
}

func TestLifecycleLockMetadataClearsOnRelease(t *testing.T) {
	paths := testLifecyclePaths(t)
	release, err := acquireLifecycleLockOnce(paths, lifecycleOperationStart)
	if err != nil {
		t.Fatalf("acquire lifecycle lock: %v", err)
	}
	lockPath := paths.StateDir + "/" + lifecycleLockFile
	owner, err := readLifecycleLockOwner(lockPath)
	if err != nil {
		release()
		t.Fatalf("read owner while held: %v", err)
	}
	if owner.Operation != lifecycleOperationStart || owner.PID != os.Getpid() {
		release()
		t.Fatalf("owner while held = %+v", owner)
	}

	release()
	if _, err := readLifecycleLockOwner(lockPath); err == nil {
		t.Fatal("released lifecycle lock unexpectedly retained owner metadata")
	}
}

func TestLifecycleLocksAreScopedByStateDir(t *testing.T) {
	first := testLifecyclePaths(t)
	second := testLifecyclePaths(t)

	releaseFirst, err := acquireLifecycleLockOnce(first, lifecycleOperationStart)
	if err != nil {
		t.Fatalf("acquire first namespace: %v", err)
	}
	defer releaseFirst()

	releaseSecond, err := acquireLifecycleLockOnce(second, lifecycleOperationStart)
	if err != nil {
		t.Fatalf("independent state namespace should not contend: %v", err)
	}
	releaseSecond()
}

func TestLifecycleOperationInterruptibleOnlyForStartAndResume(t *testing.T) {
	for _, operation := range []string{lifecycleOperationStart, lifecycleOperationResume} {
		if !lifecycleOperationInterruptible(operation) {
			t.Fatalf("operation %q should be interruptible", operation)
		}
	}
	for _, operation := range []string{lifecycleOperationDown, lifecycleOperationExtend, lifecycleOperationShorten, lifecycleOperationUnknown} {
		if lifecycleOperationInterruptible(operation) {
			t.Fatalf("operation %q should not be interruptible", operation)
		}
	}
}

func TestWaitForLifecycleLockReacquiresAfterOwnerReleases(t *testing.T) {
	paths := testLifecyclePaths(t)
	releaseFirst, err := acquireLifecycleLockOnce(paths, lifecycleOperationStart)
	if err != nil {
		t.Fatalf("acquire first lifecycle lock: %v", err)
	}

	released := make(chan struct{})
	go func() {
		time.Sleep(25 * time.Millisecond)
		releaseFirst()
		close(released)
	}()

	releaseSecond, err := waitForLifecycleLock(paths, lifecycleOperationDown, time.Second)
	if err != nil {
		t.Fatalf("wait for lifecycle lock: %v", err)
	}
	releaseSecond()
	<-released
}
