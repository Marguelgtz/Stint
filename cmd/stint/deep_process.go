package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// remoteProcessGroupInvocation returns a POSIX-shell fragment that launches a
// timed command in a fresh session. The child supervisor kills its whole
// process group before exiting, then leaves its original status in a private
// file for the SSH-side wrapper to frame after the group is quiescent.
func remoteProcessGroupInvocation(command string, timeoutSeconds int, name, statusFile, groupFile string) string {
	if timeoutSeconds < 1 {
		timeoutSeconds = 1
	}
	inner := `printf '%s\n' "$$" > "$4"
trap 'kill -KILL -- -$$ 2>/dev/null || true' EXIT HUP INT TERM
if timeout --foreground -k 1 "$1" sh -c "$2"; then status=0; else status=$?; fi
printf '%s\n' "$status" > "$3"
exit "$status"`
	return "setsid --wait sh -c " + shellQuote(inner) + " " + shellQuote(name) +
		" " + shellQuote(fmt.Sprint(timeoutSeconds)) + " " + shellQuote(command) + " " + shellQuote(statusFile) + " " + shellQuote(groupFile)
}

// runQuiescedProcessGroup bounds a Stint-owned command to its own process
// group. Context cancellation reaches the whole group, and after the direct
// command exits any remaining members are killed before the caller can
// capture or verify the worktree.
func runQuiescedProcessGroup(cmd *exec.Cmd) (runErr, quiesceErr error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return killProcessGroup(cmd.Process.Pid)
	}
	if err := cmd.Run(); err != nil {
		runErr = err
	}
	if cmd.Process != nil {
		if err := killProcessGroup(cmd.Process.Pid); err != nil {
			quiesceErr = fmt.Errorf("terminate command process group: %w", err)
		}
	}
	return runErr, quiesceErr
}

func killProcessGroup(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid process group leader PID %d", pid)
	}
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func newPrivateOutputFile(prefix string) (*os.File, error) {
	f, err := os.CreateTemp("", prefix)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, err
	}
	return f, nil
}

func readAndRemoveOutputFile(f *os.File) ([]byte, error) {
	if f == nil {
		return nil, nil
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Seek(0, 0); err != nil {
		_ = f.Close()
		return nil, err
	}
	data, readErr := os.ReadFile(name)
	closeErr := f.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return data, nil
}
