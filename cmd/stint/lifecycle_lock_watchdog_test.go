package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
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

func TestDownYesPreemptsRecordedWatchdogHoldingLifecycleLock(t *testing.T) {
	paths := testLifecyclePaths(t)
	if err := paths.Ensure(); err != nil {
		t.Fatalf("ensure paths: %v", err)
	}
	lockPath := filepath.Join(paths.StateDir, lifecycleLockFile)

	cmd := exec.Command(os.Args[0], "-test.run=TestLifecycleWatchdogLockHelperProcess")
	cmd.Env = append(os.Environ(),
		"STINT_WATCHDOG_LOCK_HELPER=1",
		"STINT_WATCHDOG_LOCK_PATH="+lockPath,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("helper stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start watchdog helper: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	ready, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || ready != "ready\n" {
		t.Fatalf("watchdog helper did not acquire lock: ready=%q err=%v", ready, err)
	}

	state := sessionstate.State{
		InstanceID:  1,
		Status:      sessionstate.StatusRecoverable,
		WatchdogPID: cmd.Process.Pid,
	}
	if err := sessionstate.Save(paths, state); err != nil {
		t.Fatalf("save session state: %v", err)
	}

	oldArgs := os.Args
	os.Args = []string{"stint", "down", "--yes"}
	defer func() { os.Args = oldArgs }()

	releaseDown, err := acquireLifecycleLock(paths)
	if err != nil {
		t.Fatalf("down --yes should preempt recorded watchdog and acquire lock: %v", err)
	}
	releaseDown()
}

func TestLifecycleWatchdogLockHelperProcess(t *testing.T) {
	if os.Getenv("STINT_WATCHDOG_LOCK_HELPER") != "1" {
		return
	}
	lockPath := os.Getenv("STINT_WATCHDOG_LOCK_PATH")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		os.Exit(2)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		os.Exit(3)
	}
	_, _ = os.Stdout.WriteString("ready\n")
	for {
		time.Sleep(time.Second)
	}
}
