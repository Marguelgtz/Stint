package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Marguelgtz/Stint/internal/deep"
)

const qualificationFaultExitCode = 86

// qualificationFaultAfterReceipt is deliberately specific to the planned
// EXPORT-001 attempt. The supervisor and watchdog are separate processes; this
// hook exits only the coordinator after its Hermes supervisor has quiesced.
func qualificationFaultAfterReceipt(c *deepCoordinator, task deep.Task, run deep.ExecutorRun, result execResult, runErr error) (bool, error) {
	if os.Getenv("STINT_QUALIFICATION_FAULT_V1") != "after-receipt" || task.ID != "EXPORT-001" || task.Attempts != 1 {
		return false, nil
	}
	if c.state.RunEventSchemaVersion != deep.RunEventSchemaVersion || run.TaskID != task.ID || run.Attempt != 1 || run.ID == "" {
		return false, errors.New("qualification receipt fault requires the journaled EXPORT-001 attempt 1")
	}
	if _, ok := c.executor.(*localHermesExecutor); !ok {
		return false, errors.New("qualification receipt fault requires the production Hermes executor")
	}
	if runErr != nil || !result.completed || result.exitCode != 0 || result.timedOut {
		return false, nil
	}
	receipt, found, err := deep.LoadExecutorReceipt(c.stateDir, c.state.SessionID, run.ID)
	if err != nil {
		return false, fmt.Errorf("read qualification completion receipt: %w", err)
	}
	if !found || receipt.ExecutorRunID != run.ID || !receipt.ProcessQuiescent || !receipt.Launched || !receipt.Completed || receipt.ExitCode != 0 {
		return false, errors.New("qualification receipt fault requires a validated, successful, quiescent Hermes receipt")
	}
	if result.repositoryAfter == nil || result.repositoryAfterError != "" ||
		receipt.RepositoryAfterHeadCommit != result.repositoryAfter.HeadCommit || receipt.RepositoryAfterTreeSHA != result.repositoryAfter.TreeSHA {
		return false, errors.New("qualification receipt does not match the executor result Git identity")
	}
	events, err := deep.ReadRunEvents(c.stateDir, c.state.SessionID)
	if err != nil {
		return false, fmt.Errorf("read qualification run journal: %w", err)
	}
	started, completed := false, false
	for _, event := range events {
		if event.ExecutorRun == nil || event.ExecutorRun.ID != run.ID {
			continue
		}
		switch event.Type {
		case deep.RunEventExecutorStarted:
			started = true
		case deep.RunEventExecutorResult, deep.RunEventExecutorReconciled:
			completed = true
		}
	}
	if !started || completed {
		return false, errors.New("qualification fault boundary is not between executor.started and its canonical result")
	}
	markerDir := filepath.Join(deep.DeepDir(c.stateDir, c.state.SessionID), "qualification", "fault-markers")
	if err := os.MkdirAll(markerDir, 0o700); err != nil {
		return false, err
	}
	marker := filepath.Join(markerDir, "EXPORT-001-attempt-1-after-receipt.fired")
	line := fmt.Sprintf("STINT_QUALIFICATION_FAULT_V1 run=%s task=%s attempt=%d executor=%s action=coordinator_exit_after_quiescent_receipt\n",
		c.state.RunID, run.TaskID, run.Attempt, run.ID)
	file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("create qualification one-shot marker: %w", err)
	}
	if _, err := file.WriteString(line); err != nil {
		_ = file.Close()
		_ = os.Remove(marker)
		return false, fmt.Errorf("write qualification one-shot marker: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(marker)
		return false, fmt.Errorf("sync qualification one-shot marker: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(marker)
		return false, fmt.Errorf("close qualification one-shot marker: %w", err)
	}
	if err := syncQualificationDirectory(markerDir); err != nil {
		_ = os.Remove(marker)
		return false, err
	}
	logPath := filepath.Join(deep.DeepDir(c.stateDir, c.state.SessionID), "coordinator.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		_ = os.Remove(marker)
		return false, fmt.Errorf("open coordinator log for qualification marker: %w", err)
	}
	if _, err := logFile.WriteString(line); err != nil {
		_ = logFile.Close()
		_ = os.Remove(marker)
		return false, fmt.Errorf("write coordinator qualification marker: %w", err)
	}
	if err := logFile.Sync(); err != nil {
		_ = logFile.Close()
		_ = os.Remove(marker)
		return false, fmt.Errorf("sync coordinator qualification marker: %w", err)
	}
	if err := logFile.Close(); err != nil {
		_ = os.Remove(marker)
		return false, fmt.Errorf("close coordinator qualification marker: %w", err)
	}
	return true, nil
}

func syncQualificationDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open qualification marker directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync qualification marker directory: %w", err)
	}
	return nil
}
