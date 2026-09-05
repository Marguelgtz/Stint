package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestDownWaitsForAnonymousLifecycleOwner(t *testing.T) {
	paths := testLifecyclePaths(t)
	if err := paths.Ensure(); err != nil {
		t.Fatalf("ensure paths: %v", err)
	}
	lockPath := filepath.Join(paths.StateDir, lifecycleLockFile)
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open anonymous lifecycle lock: %v", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		t.Fatalf("lock anonymous lifecycle owner: %v", err)
	}

	oldArgs := os.Args
	os.Args = []string{"stint", "down", "--yes"}
	defer func() { os.Args = oldArgs }()

	released := make(chan struct{})
	go func() {
		time.Sleep(25 * time.Millisecond)
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		close(released)
	}()

	releaseDown, err := acquireLifecycleLock(paths)
	if err != nil {
		t.Fatalf("down should wait for anonymous owner instead of failing: %v", err)
	}
	releaseDown()
	<-released
}

func TestDownWaitsForVerifiedNonInterruptibleOwner(t *testing.T) {
	paths := testLifecyclePaths(t)
	releaseExtend, err := acquireLifecycleLockOnce(paths, lifecycleOperationExtend)
	if err != nil {
		t.Fatalf("acquire extend lifecycle lock: %v", err)
	}

	oldArgs := os.Args
	os.Args = []string{"stint", "down", "--yes"}
	defer func() { os.Args = oldArgs }()

	released := make(chan struct{})
	go func() {
		time.Sleep(25 * time.Millisecond)
		releaseExtend()
		close(released)
	}()

	releaseDown, err := acquireLifecycleLock(paths)
	if err != nil {
		t.Fatalf("down should wait for non-interruptible owner instead of failing: %v", err)
	}
	releaseDown()
	<-released
}
