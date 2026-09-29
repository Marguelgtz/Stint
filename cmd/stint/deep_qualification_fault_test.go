package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

const qualificationFaultChildEnv = "STINT_TEST_QUALIFICATION_FAULT_CHILD"

// The parent test process is the detached supervisor fixture. It keeps a
// separate watchdog child alive while the real coordinator/test process exits
// at the receipt boundary, then starts a resumed coordinator against the same
// durable state and worktree.
func TestQualificationReceiptFaultSupervisedProcessRecovery(t *testing.T) {
	if os.Getenv(qualificationFaultChildEnv) == "1" {
		runQualificationFaultChild(t)
		os.Exit(90)
	}
	env := newTestEnv(t, nil, 3)
	env.state.Tasks = env.state.Tasks[:1]
	env.state.Tasks[0].ID = "EXPORT-001"
	env.state.Verify = "true"
	now := time.Now().UTC().Truncate(time.Second)
	env.state.StartedAt = now
	env.state.Deadline = now.Add(time.Hour)
	env.state.LandBefore = now.Add(50 * time.Minute)
	env.coord.stateDir = t.TempDir()
	if err := deep.BeginNewRun(env.coord.stateDir, env.state, now); err != nil {
		t.Fatalf("begin fixture run: %v", err)
	}
	hermesCalls := filepath.Join(t.TempDir(), "hermes-calls.txt")
	fakeHermes := filepath.Join(t.TempDir(), "hermes")
	program := "#!/bin/sh\nset -eu\nprintf 'called\\n' >>\"$STINT_TEST_HERMES_CALLS\"\nprintf 'exported\\n' > export-result.json\nprintf 'completed\\n'\n"
	if err := os.WriteFile(fakeHermes, []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	watchdog := exec.Command("sleep", "60")
	if err := watchdog.Start(); err != nil {
		t.Fatalf("start independent watchdog fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = watchdog.Process.Kill()
		_, _ = watchdog.Process.Wait()
	})

	cmd := exec.Command(os.Args[0], "-test.run=^TestQualificationReceiptFaultSupervisedProcessRecovery$")
	cmd.Env = append(os.Environ(),
		qualificationFaultChildEnv+"=1",
		"STINT_QUALIFICATION_FAULT_V1=after-receipt",
		"STINT_TEST_STATE_DIR="+env.coord.stateDir,
		"STINT_TEST_SESSION="+env.state.SessionID,
		"STINT_TEST_HERMES="+fakeHermes,
		"STINT_TEST_HERMES_CALLS="+hermesCalls,
	)
	if output, err := cmd.CombinedOutput(); err == nil || exitStatus(err) != qualificationFaultExitCode {
		t.Fatalf("coordinator exit = %v; output:\n%s", err, output)
	}
	if got := readText(t, hermesCalls); strings.Count(got, "called") != 1 {
		t.Fatalf("Hermes invocation count after fault = %d, want one: %q", strings.Count(got, "called"), got)
	}
	if err := syscall.Kill(watchdog.Process.Pid, 0); err != nil {
		t.Fatalf("independent watchdog did not survive coordinator exit: %v", err)
	}
	state, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Type != deep.RunEventExecutorStarted {
		t.Fatalf("fault boundary journal = %+v, want start and no canonical result", events)
	}
	if events[1].ExecutorRun == nil {
		t.Fatal("executor.started event has no executor identity")
	}
	receiptPath, err := deep.ExecutorReceiptPath(env.coord.stateDir, state.SessionID, events[1].ExecutorRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	receiptBytes, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var receipt deep.ExecutorReceipt
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		t.Fatal(err)
	}
	if err := deep.ValidateExecutorReceipt(receipt); err != nil || !strings.Contains(string(receiptBytes), `"hermesExitToQuiescenceMilliseconds"`) {
		t.Fatalf("receipt quiescence timing is absent or invalid: receipt=%+v err=%v", receipt, err)
	}
	marker := filepath.Join(deep.DeepDir(env.coord.stateDir, state.SessionID), "qualification", "fault-markers", "EXPORT-001-attempt-1-after-receipt.fired")
	markerText := readText(t, marker)
	if !strings.Contains(markerText, "STINT_QUALIFICATION_FAULT_V1") || !strings.Contains(markerText, "task=EXPORT-001") || !strings.Contains(markerText, "attempt=1") {
		t.Fatalf("durable one-shot marker = %q", markerText)
	}

	if err := deep.BeginResumeEpoch(env.coord.stateDir, &state, state.Phase, now.Add(time.Second)); err != nil {
		t.Fatalf("begin recovery epoch: %v", err)
	}
	coord := &deepCoordinator{
		stateDir: env.coord.stateDir, state: &state, executor: newLocalHermesExecutor(fakeHermes),
		now: func() time.Time { return now.Add(2 * time.Second) }, taskTimeout: time.Minute,
		verify: runVerifyCmd, git: newGitRunner(), logf: func(string, ...any) {}, out: io.Discard,
	}
	if err := coord.run(context.Background()); err != nil {
		t.Fatalf("resume after receipt fault: %v", err)
	}
	if state.Phase != deep.PhaseLanded || state.Tasks[0].Status != deep.StatusVerified {
		t.Fatalf("recovered run phase/task = %s/%s", state.Phase, state.Tasks[0].Status)
	}
	if got := readText(t, hermesCalls); strings.Count(got, "called") != 1 {
		t.Fatalf("Hermes was reinvoked during recovery: %q", got)
	}
	events, err = deep.ReadRunEvents(env.coord.stateDir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var reconciled, result, newEpoch bool
	for _, event := range events {
		if event.Type == deep.RunEventExecutorReconciled && event.ExecutorRun != nil && event.ExecutorRun.TaskID == "EXPORT-001" {
			reconciled = true
		}
		if event.Type == deep.RunEventExecutorResult && event.ExecutorRun != nil && event.ExecutorRun.TaskID == "EXPORT-001" {
			result = true
		}
		if event.Type == deep.RunEventEpochStarted && event.Boundary == deep.RunEventBoundaryResume {
			newEpoch = true
		}
	}
	if !newEpoch || !reconciled || result {
		t.Fatalf("recovery facts epoch=%t reconciled=%t executor.result=%t", newEpoch, reconciled, result)
	}
}

func runQualificationFaultChild(t *testing.T) {
	stateDir, session := os.Getenv("STINT_TEST_STATE_DIR"), os.Getenv("STINT_TEST_SESSION")
	state, err := deep.LoadState(stateDir, session)
	if err != nil {
		t.Fatalf("load child state: %v", err)
	}
	coord := &deepCoordinator{stateDir: stateDir, state: &state, executor: newLocalHermesExecutor(os.Getenv("STINT_TEST_HERMES")),
		now: time.Now, taskTimeout: time.Minute, verify: runVerifyCmd, git: newGitRunner(), logf: func(string, ...any) {}}
	if err := coord.runTask(context.Background(), 0, time.Now().UTC()); err != nil {
		t.Fatalf("run coordinator task: %v", err)
	}
}

func exitStatus(err error) int {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return -1
	}
	return exit.ExitCode()
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
