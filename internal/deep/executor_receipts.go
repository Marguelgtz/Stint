package deep

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const ExecutorReceiptSchemaVersion = 1

// ExecutorReceipt is a private recovery outbox record written by the Hermes
// supervisor after it observes the child result and confirms process-group
// quiescence. The RunEvent journal remains canonical; this receipt only lets a
// later coordinator reconstruct a result event if it crashed before appending
// one. It intentionally contains no prompt, environment, or worker output.
type ExecutorReceipt struct {
	SchemaVersion                     int    `json:"schemaVersion"`
	ExecutorRunID                     string `json:"executorRunId"`
	EndedAtUnixNano                   int64  `json:"endedAtUnixNano"`
	DurationMillis                    int64  `json:"durationMilliseconds"`
	ExitCode                          int    `json:"exitCode"`
	Launched                          bool   `json:"launched"`
	Completed                         bool   `json:"completed"`
	TimedOut                          bool   `json:"timedOut"`
	Canceled                          bool   `json:"canceled"`
	ProcessQuiescent                  bool   `json:"processQuiescent"`
	RepositoryAfterHeadCommit         string `json:"repositoryAfterHeadCommit,omitempty"`
	RepositoryAfterTreeSHA            string `json:"repositoryAfterTreeSha,omitempty"`
	RepositoryAfterObservedAtUnixNano int64  `json:"repositoryAfterObservedAtUnixNano,omitempty"`
	RepositoryAfterError              string `json:"repositoryAfterError,omitempty"`
}

func ExecutorReceiptPath(stateDir, sessionID, runID string) (string, error) {
	if sessionID == "" || sessionID == "." || sessionID == ".." || len(sessionID) > 128 {
		return "", errors.New("executor receipt session identity is invalid")
	}
	for _, r := range sessionID {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' && r != '.' {
			return "", errors.New("executor receipt session identity contains unsupported path characters")
		}
	}
	if len(runID) != 32 {
		return "", errors.New("executor receipt run identity is invalid")
	}
	if _, err := hex.DecodeString(runID); err != nil {
		return "", errors.New("executor receipt run identity is not hexadecimal")
	}
	return filepath.Join(DeepDir(stateDir, sessionID), "executor-receipts", runID+".json"), nil
}

func ValidateExecutorReceipt(receipt ExecutorReceipt) error {
	if receipt.SchemaVersion != ExecutorReceiptSchemaVersion || len(receipt.ExecutorRunID) != 32 {
		return errors.New("executor receipt schema or run identity is invalid")
	}
	if _, err := hex.DecodeString(receipt.ExecutorRunID); err != nil {
		return errors.New("executor receipt run identity is not hexadecimal")
	}
	if receipt.EndedAtUnixNano <= 0 || receipt.DurationMillis < 0 || receipt.DurationMillis > int64((7*24*time.Hour)/time.Millisecond) {
		return errors.New("executor receipt end time or duration is invalid")
	}
	if receipt.ExitCode < -1 || receipt.ExitCode > 255 || !receipt.ProcessQuiescent {
		return errors.New("executor receipt exit or quiescence facts are invalid")
	}
	terminalFacts := 0
	if receipt.Completed {
		terminalFacts++
	}
	if receipt.TimedOut {
		terminalFacts++
	}
	if receipt.Canceled {
		terminalFacts++
	}
	if terminalFacts > 1 {
		return errors.New("executor receipt has conflicting terminal outcomes")
	}
	if receipt.Completed && (!receipt.Launched || receipt.ExitCode != 0) {
		return errors.New("executor receipt completion facts contradict its exit status")
	}
	if receipt.TimedOut && (!receipt.Launched || (receipt.ExitCode != -1 && receipt.ExitCode != 124)) {
		return errors.New("executor receipt timeout facts contradict its exit status")
	}
	if receipt.Canceled && (!receipt.Launched || receipt.ExitCode != -1) {
		return errors.New("executor receipt cancellation facts contradict its exit status")
	}
	if !receipt.Launched && (terminalFacts > 0 || receipt.ExitCode <= 0) {
		return errors.New("pre-launch executor receipt has invocation result facts")
	}
	if receipt.Launched && terminalFacts == 0 && receipt.ExitCode == 0 {
		return errors.New("failed executor receipt has a zero exit status")
	}
	if len(receipt.RepositoryAfterHeadCommit) > 128 || len(receipt.RepositoryAfterTreeSHA) > 128 ||
		len(receipt.RepositoryAfterError) > 512 || strings.ContainsAny(receipt.RepositoryAfterError, "\x00\r\n") {
		return errors.New("executor receipt repository identity or error exceeds its bounds")
	}
	hasHead, hasTree := receipt.RepositoryAfterHeadCommit != "", receipt.RepositoryAfterTreeSHA != ""
	if hasHead != hasTree {
		return errors.New("executor receipt has a partial repository identity")
	}
	if hasHead {
		if receipt.RepositoryAfterObservedAtUnixNano <= 0 || receipt.RepositoryAfterError != "" ||
			strings.ContainsAny(receipt.RepositoryAfterHeadCommit, "\x00\r\n") || strings.ContainsAny(receipt.RepositoryAfterTreeSHA, "\x00\r\n") {
			return errors.New("executor receipt repository identity facts conflict")
		}
	} else {
		if receipt.RepositoryAfterObservedAtUnixNano <= 0 || receipt.RepositoryAfterError == "" {
			return errors.New("executor receipt must identify the resulting repository state or its capture failure")
		}
	}
	return nil
}

func EncodeExecutorReceipt(receipt ExecutorReceipt) ([]byte, error) {
	if err := ValidateExecutorReceipt(receipt); err != nil {
		return nil, err
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return nil, fmt.Errorf("encode executor receipt: %w", err)
	}
	return append(data, '\n'), nil
}

func DecodeExecutorReceipt(data []byte) (ExecutorReceipt, error) {
	var receipt ExecutorReceipt
	if len(data) == 0 || len(data) > 2048 {
		return ExecutorReceipt{}, errors.New("executor receipt is empty or exceeds its size limit")
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		return ExecutorReceipt{}, fmt.Errorf("decode executor receipt: %w", err)
	}
	if err := ValidateExecutorReceipt(receipt); err != nil {
		return ExecutorReceipt{}, err
	}
	return receipt, nil
}

func PersistExecutorReceipt(stateDir, sessionID string, receipt ExecutorReceipt) error {
	path, err := ExecutorReceiptPath(stateDir, sessionID, receipt.ExecutorRunID)
	if err != nil {
		return err
	}
	data, err := EncodeExecutorReceipt(receipt)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create executor receipt directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure executor receipt directory: %w", err)
	}
	if err := writeAtomic(path, data); err != nil {
		return fmt.Errorf("persist executor receipt: %w", err)
	}
	return nil
}

func LoadExecutorReceipt(stateDir, sessionID, runID string) (ExecutorReceipt, bool, error) {
	path, err := ExecutorReceiptPath(stateDir, sessionID, runID)
	if err != nil {
		return ExecutorReceipt{}, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ExecutorReceipt{}, false, nil
	}
	if err != nil {
		return ExecutorReceipt{}, false, fmt.Errorf("read executor receipt: %w", err)
	}
	receipt, err := DecodeExecutorReceipt(data)
	if err != nil {
		return ExecutorReceipt{}, false, err
	}
	if receipt.ExecutorRunID != runID {
		return ExecutorReceipt{}, false, errors.New("executor receipt identity differs from its path")
	}
	return receipt, true, nil
}

func executorRunFromReceipt(start ExecutorRun, receipt ExecutorReceipt, repositoryAtRecovery *VerificationSubject, repositoryAtRecoveryError string, observedAt time.Time) (ExecutorRun, error) {
	if err := ValidateExecutorReceipt(receipt); err != nil {
		return ExecutorRun{}, err
	}
	if receipt.ExecutorRunID != start.ID || observedAt.IsZero() ||
		(repositoryAtRecovery == nil) == (strings.TrimSpace(repositoryAtRecoveryError) == "") {
		return ExecutorRun{}, errors.New("executor receipt does not match a durable start or observation time")
	}
	endedAt := time.Unix(0, receipt.EndedAtUnixNano).UTC()
	run := start
	run.EndedAt = endedAt
	run.EndTimeSource = ExecutorEndTimeSupervisor
	run.ExitCode = receipt.ExitCode
	run.Completed = receipt.Completed
	run.DurationMilliseconds = receipt.DurationMillis
	if receipt.RepositoryAfterHeadCommit != "" {
		run.RepositoryAfter = &VerificationSubject{HeadCommit: receipt.RepositoryAfterHeadCommit, TreeSHA: receipt.RepositoryAfterTreeSHA}
		run.RepositoryAfterObservedAt = time.Unix(0, receipt.RepositoryAfterObservedAtUnixNano).UTC()
	}
	run.RepositoryAfterError = receipt.RepositoryAfterError
	if repositoryAtRecovery != nil {
		subject := *repositoryAtRecovery
		run.RepositoryAtRecovery = &subject
		run.RepositoryAtRecoveryAt = observedAt.UTC()
	} else {
		run.RepositoryAtRecoveryAt = observedAt.UTC()
		run.RepositoryAtRecoveryError = strings.TrimSpace(repositoryAtRecoveryError)
	}
	switch {
	case !receipt.Launched:
		run.Outcome = ExecutorOutcomeFailed
		run.FinishReason = fmt.Sprintf("setup failed before invocation (exit %d)", receipt.ExitCode)
		run.Error = "executor setup failed before invocation"
	case receipt.TimedOut:
		run.Outcome = ExecutorOutcomeTimedOut
		run.FinishReason = "timed out"
		run.Error = "context deadline exceeded"
	case receipt.Canceled:
		run.Outcome = ExecutorOutcomeCanceled
		run.FinishReason = "context canceled"
		run.Error = "context canceled"
	case receipt.Completed:
		run.Outcome = ExecutorOutcomeSucceeded
		run.FinishReason = "completed"
		run.Error = ""
	default:
		run.Outcome = ExecutorOutcomeFailed
		run.FinishReason = fmt.Sprintf("exit %d", receipt.ExitCode)
		if receipt.ExitCode >= 0 {
			run.Error = fmt.Sprintf("exit status %d", receipt.ExitCode)
		} else {
			run.Error = "executor ended without an exit status"
		}
	}
	run.ResultSummary = fmt.Sprintf("exit=%d finish=%q in %s", run.ExitCode, run.FinishReason, (time.Duration(run.DurationMilliseconds) * time.Millisecond).Round(time.Second))
	if err := validateExecutorRun(run, false); err != nil {
		return ExecutorRun{}, fmt.Errorf("invalid reconciled executor result: %w", err)
	}
	return run, nil
}
