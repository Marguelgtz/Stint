package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// execInput is one bounded coding-agent invocation.
type execInput struct {
	workdir     string
	prompt      string
	timeout     time.Duration
	autoApprove bool
	// allowedCommands is the session's command allow-list policy; it is
	// named in the prompt (COMMAND POLICY) and is enforced by the CLI's
	// approval mode (denied outside the list while auto-approval is off).
	allowedCommands []string
	provider        string
	model           string
	apiKey          string
	clineConfig     string
}

// execResult is the observable outcome of an invocation. The invocation
// finishing (process exit) is NOT the same as the task being accepted; the
// coordinator decides acceptance from repository evidence.
type execResult struct {
	exitCode     int
	completed    bool // run_result finishReason "completed" (or done + exit 0)
	finishReason string
	iterations   int
	inputTokens  int
	outputTokens int
	outputText   string // final worker report text
	doneSeen     bool
	duration     time.Duration
	eventCount   int
	stderrTail   string
}

func (r execResult) summary() string {
	s := fmt.Sprintf("exit=%d finish=%q iterations=%d tokens_in=%d tokens_out=%d in %s",
		r.exitCode, r.finishReason, r.iterations, r.inputTokens, r.outputTokens, r.duration.Round(time.Second))
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

// clineExecutor drives the official Cline CLI headlessly:
//
//	cline -c <workdir> --json --auto-approve <bool> -t <sec> --retries 2
//	      [-P <provider> -m <model> [-k <key>] [--config <dir>]] '<prompt>'
//
// The prompt is always passed explicitly: --json refuses interactive mode
// without a prompt or piped stdin (F-DW-002).
type clineExecutor struct {
	binary string
	// invoke is the subprocess seam; tests may replace it.
	invoke func(ctx context.Context, dir string, argv []string) (stdout, stderr string, exitCode int)
}

func newClineExecutor(binary string) *clineExecutor {
	e := &clineExecutor{binary: binary}
	e.invoke = func(ctx context.Context, dir string, argv []string) (string, string, int) {
		cmd := exec.Command(e.binary, argv...)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader("")
		// Run in its own process group so a context expiry kills the whole
		// tree (cline is a CLI that may spawn children); otherwise orphans
		// keep the output pipes open and Wait would hang.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Start(); err != nil {
			return "", err.Error(), -1
		}
		var kill chan struct{}
		if ctx.Done() != nil {
			kill = make(chan struct{})
			go func() {
				select {
				case <-ctx.Done():
					for i := 0; i < 50; i++ { // retry: group may still be starting
						if syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) == nil {
							break
						}
						time.Sleep(20 * time.Millisecond)
					}
				case <-kill:
				}
			}()
		}
		_ = cmd.Wait()
		if kill != nil {
			close(kill)
		}
		return out.String(), errb.String(), processExitCode(cmd)
	}
	return e
}

func processExitCode(cmd *exec.Cmd) int {
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	return -1
}

func (e *clineExecutor) argv(in execInput) []string {
	argv := []string{
		"-c", in.workdir,
		"--json",
		"--auto-approve", fmt.Sprintf("%t", in.autoApprove),
		"-t", fmt.Sprintf("%d", int(in.timeout.Seconds())),
		"--retries", "2",
	}
	if in.provider != "" {
		argv = append(argv, "-P", in.provider, "-m", in.model)
	}
	if in.apiKey != "" {
		argv = append(argv, "-k", in.apiKey)
	}
	if in.clineConfig != "" {
		argv = append(argv, "--config", in.clineConfig)
	}
	argv = append(argv, in.prompt)
	return argv
}

// run starts one Cline invocation and parses its event stream. The context
// deadline and in.timeout are hard bounds (cline's own -t is a softer bound
// on the model side; the fake or a wedged CLI must still be killable).
func (e *clineExecutor) run(ctx context.Context, in execInput) (execResult, error) {
	if in.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, in.timeout)
		defer cancel()
	}
	start := time.Now()
	stdout, stderr, exitCode := e.invoke(ctx, in.workdir, e.argv(in))
	res := execResult{exitCode: exitCode, duration: time.Since(start), stderrTail: tailLine(stderr, 5)}
	for _, ev := range parseClineEvents(stdout) {
		res.eventCount++
		switch ev.Type {
		case "run_result":
			res.completed = ev.FinishReason == "completed"
			res.finishReason = ev.FinishReason
			res.iterations = ev.Iterations
			res.outputText = ev.Text
			if ev.Usage != nil {
				res.inputTokens = ev.Usage.InputTokens
				res.outputTokens = ev.Usage.OutputTokens
			}
		case "done":
			res.doneSeen = true
		}
	}
	if !res.completed && res.doneSeen && exitCode == 0 {
		res.completed = true
		if res.finishReason == "" {
			res.finishReason = "done"
		}
	}
	return res, nil
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
