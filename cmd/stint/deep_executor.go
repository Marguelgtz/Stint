package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var errExecutorQuiescenceUnconfirmed = errors.New("executor process-group quiescence is unconfirmed")

// execInput is one bounded coding-agent invocation.
type execInput struct {
	workdir string
	prompt  string
	timeout time.Duration
	// allowedCommands is advisory guidance included in the Hermes prompt.
	allowedCommands []string
	provider        string
	model           string
	reasoning       string
	actionPlan      string
}

// execResult is the observable outcome of an invocation. The invocation
// finishing (process exit) is NOT the same as the task being accepted; the
// coordinator decides acceptance from repository evidence.
type execResult struct {
	exitCode     int
	completed    bool // Hermes process exited successfully; task acceptance is separate
	timedOut     bool
	finishReason string
	outputText   string // final worker report text
	duration     time.Duration
	stderrTail   string
}

func (r execResult) summary() string {
	s := fmt.Sprintf("exit=%d finish=%q in %s",
		r.exitCode, r.finishReason, r.duration.Round(time.Second))
	if r.outputText != "" {
		s += " | " + firstLines(r.outputText, 3)
	}
	return s
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " / ")
}

// executor runs one bounded coding-agent invocation. The interface lets
// tests substitute a fake without spending compute.
type executor interface {
	run(ctx context.Context, in execInput) (execResult, error)
}

func processExitCode(cmd *exec.Cmd) int {
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	return -1
}

func tailLine(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// writeAtomicFile mirrors the session-state persistence convention: write a
// temp file in the target directory and rename over the destination (0600),
// so durable Deep Work files are never observed half-written.
func writeAtomicFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".dwtmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
