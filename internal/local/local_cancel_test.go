//go:build !windows

package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSSHWrapperCancellationReapsSSHChild(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "ssh.pid")
	fakeSSH := filepath.Join(dir, "ssh")
	// exec keeps the same PID so this behaves like a long-running ssh client,
	// rather than a shell that owns another long-running descendant.
	fakeScript := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$$\" > %q\nexec sleep 30\n", pidFile)
	if err := os.WriteFile(fakeSSH, []byte(fakeScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	wrapper, err := SSHExecutable()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		cmd := exec.CommandContext(ctx, wrapper, "ignored")
		_, runErr := cmd.CombinedOutput()
		done <- runErr
	}()

	var childPID int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, readErr := os.ReadFile(pidFile)
		if readErr == nil {
			childPID, err = strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("parse fake ssh pid: %v", err)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if childPID == 0 {
		t.Fatal("fake ssh child never started")
	}
	defer syscall.Kill(childPID, syscall.SIGKILL) // best-effort cleanup if the assertion fails

	cancel()
	select {
	case runErr := <-done:
		if runErr == nil {
			t.Fatal("expected canceled wrapper command to fail")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SSH wrapper did not return promptly after context cancellation")
	}

	reapDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(reapDeadline) {
		err = syscall.Kill(childPID, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("fake ssh child %d survived wrapper cancellation", childPID)
}
