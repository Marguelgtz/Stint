package deep

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const maxExecutorRunSummaryBytes = 512

type ExecutorOutcome string

const (
	ExecutorOutcomeStarted               ExecutorOutcome = "started"
	ExecutorOutcomeSucceeded             ExecutorOutcome = "succeeded"
	ExecutorOutcomeFailed                ExecutorOutcome = "failed"
	ExecutorOutcomeTimedOut              ExecutorOutcome = "timed_out"
	ExecutorOutcomeCanceled              ExecutorOutcome = "canceled"
	ExecutorOutcomeQuiescenceUnconfirmed ExecutorOutcome = "quiescence_unconfirmed"
	ExecutorOutcomeUnknown               ExecutorOutcome = "unknown"
)

// ExecutorRuntime records bounded identity for the runtime that actually
// received the invocation, never prompts or environment dumps. Compute
// binding is carried separately on the run.
type ExecutorRuntime struct {
	Worker    string `json:"worker,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

// ExecutorRun is the bounded durable record for one executor invocation. A
// start event contains launch context; the result event repeats that context
// with the observed result, so either event remains attributable on its own.
type ExecutorRun struct {
	ID                       string               `json:"id"`
	StartEventID             string               `json:"startEventId"`
	StartedInEpochID         string               `json:"startedInEpochId"`
	TaskID                   string               `json:"taskId"`
	Attempt                  int                  `json:"attempt"`
	StartedAt                time.Time            `json:"startedAt"`
	ConfiguredTimeoutSeconds int                  `json:"configuredTimeoutSeconds"`
	EffectiveTimeoutSeconds  int                  `json:"effectiveTimeoutSeconds"`
	RemainingDeadlineSeconds int                  `json:"remainingDeadlineSeconds,omitempty"`
	TimeoutDecision          string               `json:"timeoutDecision,omitempty"`
	Runtime                  ExecutorRuntime      `json:"runtime"`
	ComputeProvider          string               `json:"computeProvider,omitempty"`
	ComputeInstance          int64                `json:"computeInstanceId,omitempty"`
	RepositoryBefore         *VerificationSubject `json:"repositoryBefore,omitempty"`
	Outcome                  ExecutorOutcome      `json:"outcome"`
	EndedAt                  time.Time            `json:"endedAt,omitempty"`
	ExitCode                 int                  `json:"exitCode,omitempty"`
	Completed                bool                 `json:"completed,omitempty"`
	FinishReason             string               `json:"finishReason,omitempty"`
	Error                    string               `json:"error,omitempty"`
	ResultSummary            string               `json:"resultSummary,omitempty"`
	DurationMilliseconds     int64                `json:"durationMilliseconds,omitempty"`
	RepositoryAfter          *VerificationSubject `json:"repositoryAfter,omitempty"`
	RepositoryAfterError     string               `json:"repositoryAfterError,omitempty"`
	ArtifactRefs             []string             `json:"artifactRefs,omitempty"`
}

func NewExecutorRunID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate executor run identity: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// BeginExecutorRun durably projects the active attempt and appends the
// executor.started fact before the caller launches any executor process.
func BeginExecutorRun(stateDir string, state *DeepState, run ExecutorRun) (ExecutorRun, error) {
	if state == nil || state.RunID == "" || state.ExecutionEpochID == "" || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return ExecutorRun{}, errors.New("executor start requires a journaled run epoch")
	}
	if state.Phase != PhaseExecuting {
		return ExecutorRun{}, fmt.Errorf("cannot start executor in phase %q", state.Phase)
	}
	if state.ExecutionQuiescenceUnconfirmed {
		return ExecutorRun{}, errors.New("cannot start executor while process quiescence is unconfirmed")
	}
	if run.ID == "" || run.StartedAt.IsZero() || run.TaskID == "" || run.Attempt < 1 {
		return ExecutorRun{}, errors.New("executor start requires identity, task, attempt, and timestamp")
	}
	run.StartEventID = executorEventID(run.ID, "started")
	run.StartedInEpochID = state.ExecutionEpochID
	run.StartedAt = run.StartedAt.UTC()
	run.Outcome = ExecutorOutcomeStarted
	if err := validateExecutorRun(run, true); err != nil {
		return ExecutorRun{}, err
	}
	prepared := *state
	prepared.Tasks = append([]Task(nil), state.Tasks...)
	task, ok := findTask(&prepared, run.TaskID)
	if !ok {
		return ExecutorRun{}, fmt.Errorf("executor start references unknown task %q", run.TaskID)
	}
	if run.Attempt != task.Attempts+1 {
		return ExecutorRun{}, fmt.Errorf("executor start attempt %d does not follow task attempt %d", run.Attempt, task.Attempts)
	}
	if task.Status.Terminal() {
		return ExecutorRun{}, fmt.Errorf("cannot start executor for terminal task %q", run.TaskID)
	}
	if task.ExecutorRunID != "" && !task.ExecutorRunProcessed {
		return ExecutorRun{}, fmt.Errorf("cannot start another executor for task %q before its prior result is processed", run.TaskID)
	}
	if binding := state.ComputeBinding; binding != nil {
		run.ComputeProvider, run.ComputeInstance = binding.Provider, binding.InstanceID
	}
	event := RunEvent{
		EventID: run.StartEventID, RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: run.StartedAt, Actor: "deep-coordinator", Type: RunEventExecutorStarted,
		FromPhase: state.Phase, ToPhase: state.Phase, ExecutorRun: &run,
	}
	applyExecutorStarted(task, run)
	event.TaskSummary = summarizeRunTasks(prepared.Tasks)
	if err := appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked); err != nil {
		return ExecutorRun{}, err
	}
	return run, nil
}

// CompleteExecutorRun durably records an observed result before verification
// begins. A result with unknown quiescence also projects the hard execution
// block so no verifier or later executor can run against a moving worktree.
func CompleteExecutorRun(stateDir string, state *DeepState, run ExecutorRun) error {
	if state == nil || state.RunID == "" || state.ExecutionEpochID == "" || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return errors.New("executor result requires a journaled run epoch")
	}
	if run.ID == "" || run.StartEventID != executorEventID(run.ID, "started") || run.TaskID == "" || run.Attempt < 1 || run.EndedAt.IsZero() {
		return errors.New("executor result requires its start identity, task, attempt, and end timestamp")
	}
	if run.Outcome != ExecutorOutcomeSucceeded && run.Outcome != ExecutorOutcomeFailed && run.Outcome != ExecutorOutcomeTimedOut &&
		run.Outcome != ExecutorOutcomeCanceled && run.Outcome != ExecutorOutcomeQuiescenceUnconfirmed {
		return fmt.Errorf("invalid executor result outcome %q", run.Outcome)
	}
	run.EndedAt = run.EndedAt.UTC()
	if err := validateExecutorRun(run, false); err != nil {
		return err
	}
	if _, ok := findTask(state, run.TaskID); !ok {
		return fmt.Errorf("executor result references unknown task %q", run.TaskID)
	}
	event := RunEvent{
		EventID: executorEventID(run.ID, "result"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: run.EndedAt, Actor: "deep-coordinator", Type: RunEventExecutorResult,
		FromPhase: state.Phase, ToPhase: state.Phase, ExecutorRun: &run,
	}
	projected := *state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if err := applyExecutorResult(&projected, run); err != nil {
		return err
	}
	event.TaskSummary = summarizeRunTasks(projected.Tasks)
	return appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked)
}

// RecoverUnmatchedExecutorRun turns a start lacking a result into an explicit
// hard recovery block. The missing result does not prove the process stopped
// or ended; EndedAt remains unset and the event timestamp records when
// recovery observed the missing result.
func RecoverUnmatchedExecutorRun(stateDir string, state *DeepState, at time.Time) (*ExecutorRun, error) {
	if state == nil || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return nil, nil
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		return nil, err
	}
	starts := map[string]ExecutorRun{}
	for _, event := range events {
		switch event.Type {
		case RunEventExecutorStarted:
			if event.ExecutorRun != nil {
				starts[event.ExecutorRun.ID] = *event.ExecutorRun
			}
		case RunEventExecutorResult, RunEventExecutorRecoveryRequired:
			if event.ExecutorRun != nil {
				delete(starts, event.ExecutorRun.ID)
			}
		}
	}
	if len(starts) == 0 {
		return nil, nil
	}
	if len(starts) != 1 {
		return nil, fmt.Errorf("journal contains %d unmatched executor starts; refusing recovery", len(starts))
	}
	var run ExecutorRun
	for _, run = range starts {
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	run.Outcome = ExecutorOutcomeUnknown
	reason := "coordinator resumed without a durable executor result; invocation outcome and process quiescence are unknown"
	event := RunEvent{
		EventID: executorEventID(run.ID, "recovery-required"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: at.UTC(), Actor: "deep-coordinator", Type: RunEventExecutorRecoveryRequired,
		FromPhase: state.Phase, ToPhase: state.Phase, Reason: reason, ExecutorRun: &run,
	}
	projected := *state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if task, ok := findTask(&projected, run.TaskID); !ok || task.ExecutorRunID != run.ID || task.Attempts != run.Attempt {
		return nil, errors.New("unmatched executor start does not match the projected task attempt")
	} else {
		task.Status = StatusNeedsHuman
		task.Blocker = "executor invocation has no durable result; process quiescence is unknown, so verification and retries are stopped"
	}
	projected.ExecutionQuiescenceUnconfirmed = true
	projected.ExecutionQuiescenceTaskID = run.TaskID
	event.TaskSummary = summarizeRunTasks(projected.Tasks)
	if err := appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked); err != nil {
		return nil, err
	}
	return &run, nil
}

// ReadRunEvents returns the validated canonical history in run-sequence order.
func ReadRunEvents(stateDir, sessionID string) ([]RunEvent, error) {
	var events []RunEvent
	err := withRunStateLock(stateDir, sessionID, func(dir string) error {
		var err error
		events, _, err = readRunEventsLocked(dir, sessionID)
		return err
	})
	return events, err
}

// LoadExecutorRun returns the latest durable representation of one invocation.
func LoadExecutorRun(stateDir, sessionID, id string) (ExecutorRun, bool, error) {
	events, err := ReadRunEvents(stateDir, sessionID)
	if err != nil {
		return ExecutorRun{}, false, err
	}
	var found ExecutorRun
	ok := false
	for _, event := range events {
		if event.ExecutorRun != nil && event.ExecutorRun.ID == id {
			found, ok = *event.ExecutorRun, true
		}
	}
	return found, ok, nil
}

func applyExecutorStarted(task *Task, run ExecutorRun) {
	task.Status = StatusActive
	task.Attempts = run.Attempt
	task.ExecutorRunID = run.ID
	task.Blocker = ""
	task.ExecutionError = ""
	task.LastResult = ""
	task.ExecutorRunProcessed = false
	task.VerificationCommand = ""
	task.VerificationResult = ""
	task.VerificationOutput = ""
	task.VerificationSubject = nil
	task.VerificationBookkeeping = nil
	task.CheckpointCommit = ""
	task.CheckpointTreeSHA = ""
	task.VerifiedAt = nil
	task.ConfiguredTimeoutSec = run.ConfiguredTimeoutSeconds
	task.EffectiveTimeoutSec = run.EffectiveTimeoutSeconds
	task.TimeoutDecision = run.TimeoutDecision
}

func applyExecutorResult(state *DeepState, run ExecutorRun) error {
	task, ok := findTask(state, run.TaskID)
	if !ok || task.ExecutorRunID != run.ID || task.Attempts != run.Attempt {
		return fmt.Errorf("executor result %q does not match the projected task attempt", run.ID)
	}
	task.LastResult = executorProjectedLastResult(run)
	task.ExecutionError = run.Error
	task.ExecutorRunProcessed = false
	if run.Outcome == ExecutorOutcomeQuiescenceUnconfirmed {
		task.Status = StatusNeedsHuman
		task.Blocker = "executor process quiescence is unconfirmed; verification and further work are stopped"
		state.ExecutionQuiescenceUnconfirmed = true
		state.ExecutionQuiescenceTaskID = task.ID
	}
	return nil
}

func findTask(state *DeepState, id string) (*Task, bool) {
	for i := range state.Tasks {
		if state.Tasks[i].ID == id {
			return &state.Tasks[i], true
		}
	}
	return nil, false
}

func executorEventID(id, action string) string { return "executor/" + id + "/" + action }

func validateExecutorRun(run ExecutorRun, starting bool) error {
	if err := validateExecutorStartFacts(run); err != nil {
		return err
	}
	if starting {
		if run.Outcome != ExecutorOutcomeStarted || !run.EndedAt.IsZero() || run.Error != "" || run.ResultSummary != "" ||
			run.RepositoryAfter != nil || run.RepositoryAfterError != "" || len(run.ArtifactRefs) != 0 {
			return errors.New("executor start contains result-only fields")
		}
		return nil
	}
	if run.EndedAt.IsZero() || run.EndedAt.Before(run.StartedAt) || run.DurationMilliseconds < 0 ||
		run.DurationMilliseconds > int64((7*24*time.Hour)/time.Millisecond) ||
		len(run.FinishReason) > maxRunEventTextBytes || len(run.Error) > maxRunEventTextBytes ||
		len(run.ResultSummary) > maxExecutorRunSummaryBytes || len(run.RepositoryAfterError) > maxRunEventTextBytes {
		return errors.New("executor result facts exceed limits or are invalid")
	}
	if len(run.ArtifactRefs) > 8 {
		return errors.New("executor result has too many artifact references")
	}
	for _, ref := range run.ArtifactRefs {
		if ref == "" || len(ref) > 256 || strings.ContainsAny(ref, "\x00\r\n") {
			return errors.New("executor artifact reference is empty or invalid")
		}
	}
	if run.RepositoryAfter != nil && (run.RepositoryAfter.HeadCommit == "" || run.RepositoryAfter.TreeSHA == "" ||
		len(run.RepositoryAfter.HeadCommit) > 128 || len(run.RepositoryAfter.TreeSHA) > 128) {
		return errors.New("executor post-run repository identity is invalid")
	}
	if run.Outcome == ExecutorOutcomeQuiescenceUnconfirmed && run.RepositoryAfter != nil {
		return errors.New("unconfirmed executor quiescence cannot claim a stable post-run repository identity")
	}
	if run.Outcome == ExecutorOutcomeSucceeded && (!run.Completed || run.ExitCode != 0 || run.Error != "") {
		return errors.New("successful executor outcome disagrees with completion facts")
	}
	if run.Outcome == ExecutorOutcomeTimedOut && run.Error == "" {
		return errors.New("timed-out executor outcome requires a timeout error")
	}
	return nil
}

func validateExecutorRecovery(run ExecutorRun) error {
	if run.Outcome != ExecutorOutcomeUnknown || !run.EndedAt.IsZero() || run.ExitCode != 0 || run.Completed ||
		run.FinishReason != "" || run.Error != "" || run.ResultSummary != "" || run.DurationMilliseconds != 0 ||
		run.RepositoryAfter != nil || run.RepositoryAfterError != "" || len(run.ArtifactRefs) != 0 {
		return errors.New("executor recovery record contains unobserved terminal facts")
	}
	started := run
	started.Outcome = ExecutorOutcomeStarted
	return validateExecutorRun(started, true)
}

func validateExecutorStartFacts(run ExecutorRun) error {
	if run.ID == "" || len(run.ID) > 128 || strings.ContainsAny(run.ID, "\x00\r\n") ||
		run.StartEventID != executorEventID(run.ID, "started") || run.StartedInEpochID == "" ||
		run.TaskID == "" || len(run.TaskID) > 128 || strings.ContainsAny(run.TaskID, "\x00\r\n") ||
		run.Attempt < 1 || run.Attempt > 1_000_000 || run.StartedAt.IsZero() {
		return errors.New("executor run identity or start facts are invalid")
	}
	if run.ConfiguredTimeoutSeconds < 0 || run.ConfiguredTimeoutSeconds > 7*24*60*60 ||
		run.EffectiveTimeoutSeconds < 1 || run.EffectiveTimeoutSeconds > 7*24*60*60 ||
		run.RemainingDeadlineSeconds < 0 || run.RemainingDeadlineSeconds > 7*24*60*60 {
		return errors.New("executor run timeout context is invalid")
	}
	if len(run.TimeoutDecision) > maxRunEventTextBytes || strings.ContainsRune(run.TimeoutDecision, '\x00') {
		return errors.New("executor timeout decision exceeds its limit or contains NUL")
	}
	for _, value := range []string{run.Runtime.Worker, run.Runtime.Provider, run.Runtime.Model, run.Runtime.Reasoning, run.ComputeProvider} {
		if len(value) > 128 || strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("executor runtime identity is invalid")
		}
	}
	if run.ComputeInstance < 0 || (run.ComputeProvider == "") != (run.ComputeInstance == 0) {
		return errors.New("executor compute identity is incomplete")
	}
	if run.RepositoryBefore != nil && (run.RepositoryBefore.HeadCommit == "" || run.RepositoryBefore.TreeSHA == "" ||
		len(run.RepositoryBefore.HeadCommit) > 128 || len(run.RepositoryBefore.TreeSHA) > 128) {
		return errors.New("executor pre-run repository identity is invalid")
	}
	return nil
}
