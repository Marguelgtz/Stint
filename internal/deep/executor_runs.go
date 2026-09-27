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

type ExecutorEndTimeSource string

const (
	ExecutorEndTimeCoordinator ExecutorEndTimeSource = "coordinator"
	ExecutorEndTimeSupervisor  ExecutorEndTimeSource = "executor-supervisor"
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
	ID               string `json:"id"`
	StartEventID     string `json:"startEventId"`
	StartedInEpochID string `json:"startedInEpochId"`
	// TaskID is the stable Objective / Work Unit identity, not an atomic action ID.
	TaskID                    string                `json:"taskId"`
	Attempt                   int                   `json:"attempt"`
	StartedAt                 time.Time             `json:"startedAt"`
	ConfiguredTimeoutSeconds  int                   `json:"configuredTimeoutSeconds"`
	EffectiveTimeoutSeconds   int                   `json:"effectiveTimeoutSeconds"`
	RemainingDeadlineSeconds  int                   `json:"remainingDeadlineSeconds,omitempty"`
	TimeoutDecision           string                `json:"timeoutDecision,omitempty"`
	Runtime                   ExecutorRuntime       `json:"runtime"`
	ComputeProvider           string                `json:"computeProvider,omitempty"`
	ComputeInstance           int64                 `json:"computeInstanceId,omitempty"`
	RepositoryBefore          *VerificationSubject  `json:"repositoryBefore,omitempty"`
	Outcome                   ExecutorOutcome       `json:"outcome"`
	EndedAt                   time.Time             `json:"endedAt,omitempty"`
	EndTimeSource             ExecutorEndTimeSource `json:"endTimeSource,omitempty"`
	ExitCode                  int                   `json:"exitCode,omitempty"`
	Completed                 bool                  `json:"completed,omitempty"`
	FinishReason              string                `json:"finishReason,omitempty"`
	Error                     string                `json:"error,omitempty"`
	ResultSummary             string                `json:"resultSummary,omitempty"`
	DurationMilliseconds      int64                 `json:"durationMilliseconds,omitempty"`
	RepositoryAfter           *VerificationSubject  `json:"repositoryAfter,omitempty"`
	RepositoryAfterObservedAt time.Time             `json:"repositoryAfterObservedAt,omitempty"`
	RepositoryAfterError      string                `json:"repositoryAfterError,omitempty"`
	RepositoryAtRecovery      *VerificationSubject  `json:"repositoryAtRecovery,omitempty"`
	RepositoryAtRecoveryAt    time.Time             `json:"repositoryAtRecoveryAt,omitempty"`
	RepositoryAtRecoveryError string                `json:"repositoryAtRecoveryError,omitempty"`
	ArtifactRefs              []string              `json:"artifactRefs,omitempty"`
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
	applyExecutorStarted(task, run, state.AcceptanceContractVersion == DeterministicAcceptanceContractVersion)
	event.TaskSummary = summarizeRunTasks(prepared.Tasks)
	if err := appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked); err != nil {
		return ExecutorRun{}, err
	}
	return run, nil
}

// CompleteExecutorRun durably records an observed result before verification
// begins. A result with unknown quiescence may have no process end time; its
// event timestamp records when Stint observed that uncertainty. Such a result
// also projects the hard execution block so no verifier or later executor can
// run against a moving worktree.
func CompleteExecutorRun(stateDir string, state *DeepState, run ExecutorRun, observedAt time.Time) error {
	if state == nil || state.RunID == "" || state.ExecutionEpochID == "" || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return errors.New("executor result requires a journaled run epoch")
	}
	if run.ID == "" || run.StartEventID != executorEventID(run.ID, "started") || run.TaskID == "" || run.Attempt < 1 ||
		(run.EndedAt.IsZero() && run.Outcome != ExecutorOutcomeQuiescenceUnconfirmed) {
		return errors.New("executor result requires its start identity, task, attempt, and observed end timestamp")
	}
	if run.Outcome != ExecutorOutcomeSucceeded && run.Outcome != ExecutorOutcomeFailed && run.Outcome != ExecutorOutcomeTimedOut &&
		run.Outcome != ExecutorOutcomeCanceled && run.Outcome != ExecutorOutcomeQuiescenceUnconfirmed {
		return fmt.Errorf("invalid executor result outcome %q", run.Outcome)
	}
	run.EndedAt = run.EndedAt.UTC()
	if observedAt.IsZero() {
		return errors.New("executor result requires an observation timestamp")
	}
	observedAt = observedAt.UTC()
	if observedAt.Before(run.StartedAt) {
		return errors.New("executor result observation precedes its durable start")
	}
	if err := validateExecutorRun(run, false); err != nil {
		return err
	}
	if _, ok := findTask(state, run.TaskID); !ok {
		return fmt.Errorf("executor result references unknown task %q", run.TaskID)
	}
	event := RunEvent{
		EventID: executorEventID(run.ID, "result"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: observedAt, Actor: "deep-coordinator", Type: RunEventExecutorResult,
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
	run, err := LoadUnmatchedExecutorRun(stateDir, state.SessionID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, nil
	}
	if run.Outcome == ExecutorOutcomeUnknown {
		// A previous epoch already recorded the unresolved observation. Keep it
		// open for a later recovery receipt rather than appending duplicates.
		return nil, nil
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	unknown := *run
	unknown.Outcome = ExecutorOutcomeUnknown
	reason := "coordinator resumed without a durable executor result; invocation outcome and process quiescence are unknown"
	event := RunEvent{
		EventID: executorEventID(unknown.ID, "recovery-required/"+state.ExecutionEpochID), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: at.UTC(), Actor: "deep-coordinator", Type: RunEventExecutorRecoveryRequired,
		FromPhase: state.Phase, ToPhase: state.Phase, Reason: reason, ExecutorRun: &unknown,
	}
	projected := *state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if task, ok := findTask(&projected, unknown.TaskID); !ok || task.ExecutorRunID != unknown.ID || task.Attempts != unknown.Attempt {
		return nil, errors.New("unmatched executor start does not match the projected task attempt")
	} else {
		task.Status = StatusNeedsHuman
		task.Blocker = "executor invocation has no durable result; process quiescence is unknown, so verification and retries are stopped"
		task.ExecutionError = reason
		task.LastResult = "executor outcome unknown | " + reason
	}
	projected.ExecutionQuiescenceUnconfirmed = true
	projected.ExecutionQuiescenceTaskID = unknown.TaskID
	event.TaskSummary = summarizeRunTasks(projected.Tasks)
	if err := appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked); err != nil {
		return nil, err
	}
	return &unknown, nil
}

// LoadUnmatchedExecutorRun returns the sole executor invocation without a
// terminal result. A recovery_required event remains open so a later durable
// completion receipt can reconcile it without rewriting prior history.
func LoadUnmatchedExecutorRun(stateDir, sessionID string) (*ExecutorRun, error) {
	events, err := ReadRunEvents(stateDir, sessionID)
	if err != nil {
		return nil, err
	}
	unmatched := map[string]ExecutorRun{}
	for _, event := range events {
		if event.ExecutorRun == nil {
			continue
		}
		switch event.Type {
		case RunEventExecutorStarted:
			unmatched[event.ExecutorRun.ID] = *event.ExecutorRun
		case RunEventExecutorRecoveryRequired:
			if start, ok := unmatched[event.ExecutorRun.ID]; ok {
				start.Outcome = ExecutorOutcomeUnknown
				unmatched[event.ExecutorRun.ID] = start
			} else {
				unmatched[event.ExecutorRun.ID] = *event.ExecutorRun
			}
		case RunEventExecutorResult:
			if event.ExecutorRun.Outcome != ExecutorOutcomeQuiescenceUnconfirmed {
				delete(unmatched, event.ExecutorRun.ID)
			}
		case RunEventExecutorReconciled:
			delete(unmatched, event.ExecutorRun.ID)
		}
	}
	if len(unmatched) == 0 {
		return nil, nil
	}
	if len(unmatched) != 1 {
		return nil, fmt.Errorf("journal contains %d unresolved executor invocations; refusing recovery", len(unmatched))
	}
	for _, run := range unmatched {
		copy := run
		return &copy, nil
	}
	return nil, nil
}

// ReconcileExecutorRunReceipt appends a result fact in the current epoch from
// a validated process receipt. The event time is when recovery observed it;
// ExecutorRun.EndedAt and RepositoryAfterObservedAt remain supervisor facts.
func ReconcileExecutorRunReceipt(stateDir string, state *DeepState, receipt ExecutorReceipt, repositoryAtRecovery *VerificationSubject, repositoryAtRecoveryError string, observedAt time.Time) (ExecutorRun, error) {
	if state == nil || state.RunID == "" || state.ExecutionEpochID == "" || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return ExecutorRun{}, errors.New("executor receipt reconciliation requires a journaled run epoch")
	}
	if state.Phase != PhaseExecuting && state.Phase != PhaseLanding {
		return ExecutorRun{}, fmt.Errorf("cannot reconcile executor receipt in phase %q", state.Phase)
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		return ExecutorRun{}, err
	}
	var start *RunEvent
	for i := range events {
		if events[i].Type == RunEventExecutorStarted && events[i].ExecutorRun != nil && events[i].ExecutorRun.ID == receipt.ExecutorRunID {
			start = &events[i]
			break
		}
	}
	if start == nil || start.ExecutorRun == nil {
		return ExecutorRun{}, errors.New("executor receipt has no matching durable start event")
	}
	run, err := executorRunFromReceipt(*start.ExecutorRun, receipt, repositoryAtRecovery, repositoryAtRecoveryError, observedAt)
	if err != nil {
		return ExecutorRun{}, err
	}
	unmatched, err := LoadUnmatchedExecutorRun(stateDir, state.SessionID)
	if err != nil {
		return ExecutorRun{}, err
	}
	if unmatched == nil || unmatched.ID != run.ID || !sameExecutorStart(*start.ExecutorRun, *unmatched) {
		return ExecutorRun{}, errors.New("executor receipt does not match the unresolved invocation")
	}
	event := RunEvent{
		EventID: executorEventID(run.ID, "reconciled"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: observedAt.UTC(), Actor: "deep-coordinator", Type: RunEventExecutorReconciled,
		FromPhase: state.Phase, ToPhase: state.Phase, Reason: "durable executor completion receipt reconciled", ExecutorRun: &run,
	}
	projected := *state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if err := applyExecutorReconciled(&projected, run); err != nil {
		return ExecutorRun{}, err
	}
	event.TaskSummary = summarizeRunTasks(projected.Tasks)
	if err := appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked); err != nil {
		return ExecutorRun{}, err
	}
	return run, nil
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

func applyExecutorStarted(task *Task, run ExecutorRun, acceptanceContract bool) {
	task.Status = StatusActive
	task.Attempts = run.Attempt
	task.ExecutorRunID = run.ID
	task.Blocker = ""
	task.ExecutionError = ""
	task.LastResult = ""
	task.ExecutorRunProcessed = false
	task.VerificationCommand = ""
	task.VerificationRunID = ""
	task.VerificationOutcome = VerificationNotRun
	task.VerificationResult = ""
	task.VerificationOutput = ""
	task.VerificationSubject = nil
	task.VerificationBookkeeping = nil
	task.CheckpointCommit = ""
	task.CheckpointTreeSHA = ""
	task.VerifiedAt = nil
	task.AcceptanceOutcome = ""
	if acceptanceContract {
		task.AcceptanceOutcome = AcceptanceNotEvaluated
	}
	task.AcceptanceRunID = ""
	task.AcceptanceCheckOutcome = ""
	task.AcceptanceReason = ""
	task.AcceptanceSubject = nil
	task.AcceptanceCheckpointEventID = ""
	task.AcceptanceCheckpointCommit = ""
	task.AcceptanceCheckpointTreeSHA = ""
	task.AcceptanceOutput = ""
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

func applyExecutorReconciled(state *DeepState, run ExecutorRun) error {
	if run.Outcome == ExecutorOutcomeQuiescenceUnconfirmed || run.Outcome == ExecutorOutcomeUnknown || run.Outcome == ExecutorOutcomeStarted {
		return errors.New("executor reconciliation requires an observed quiescent terminal result")
	}
	if err := applyExecutorResult(state, run); err != nil {
		return err
	}
	task, _ := findTask(state, run.TaskID)
	if state.ExecutionQuiescenceUnconfirmed && state.ExecutionQuiescenceTaskID != task.ID {
		return errors.New("executor reconciliation cannot clear another invocation's quiescence block")
	}
	state.ExecutionQuiescenceUnconfirmed = false
	state.ExecutionQuiescenceTaskID = ""
	if !executorReceiptRepositoryStateMatches(run) {
		task.Status = StatusNeedsHuman
		task.Blocker = executorReceiptRepositoryConflictBlocker
		task.ExecutorRunProcessed = true
		return nil
	}
	if state.Phase == PhaseLanding {
		task.Status = StatusIncomplete
		task.Blocker = "executor completed during interrupted landing; task verification was deferred"
		task.ExecutorRunProcessed = true
	} else {
		task.Status = StatusActive
		task.Blocker = ""
		task.ExecutorRunProcessed = false
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
			run.EndTimeSource != "" || run.RepositoryAfter != nil || run.RepositoryAfterError != "" ||
			run.RepositoryAtRecovery != nil || !run.RepositoryAtRecoveryAt.IsZero() || run.RepositoryAtRecoveryError != "" || len(run.ArtifactRefs) != 0 {
			return errors.New("executor start contains result-only fields")
		}
		return nil
	}
	if run.EndedAt.IsZero() && run.Outcome != ExecutorOutcomeQuiescenceUnconfirmed {
		return errors.New("executor result is missing its observed process end time")
	}
	if !run.EndedAt.IsZero() && run.EndTimeSource != ExecutorEndTimeSupervisor && run.EndedAt.Before(run.StartedAt) {
		return errors.New("executor end time precedes its start")
	}
	if run.EndedAt.IsZero() && run.EndTimeSource != "" {
		return errors.New("executor end-time source is set without an end time")
	}
	if run.EndTimeSource != "" && run.EndTimeSource != ExecutorEndTimeCoordinator && run.EndTimeSource != ExecutorEndTimeSupervisor {
		return errors.New("executor end-time source is invalid")
	}
	if run.DurationMilliseconds < 0 ||
		run.DurationMilliseconds > int64((7*24*time.Hour)/time.Millisecond) ||
		len(run.FinishReason) > maxRunEventTextBytes || len(run.Error) > maxRunEventTextBytes ||
		len(run.ResultSummary) > maxExecutorRunSummaryBytes || len(run.RepositoryAfterError) > maxRunEventTextBytes ||
		len(run.RepositoryAtRecoveryError) > maxRunEventTextBytes {
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
	if run.RepositoryAtRecovery != nil && (run.RepositoryAtRecovery.HeadCommit == "" || run.RepositoryAtRecovery.TreeSHA == "" ||
		len(run.RepositoryAtRecovery.HeadCommit) > 128 || len(run.RepositoryAtRecovery.TreeSHA) > 128) {
		return errors.New("executor recovery-time repository identity is invalid")
	}
	if run.RepositoryAtRecovery != nil {
		if run.RepositoryAtRecoveryAt.IsZero() || run.RepositoryAtRecoveryError != "" {
			return errors.New("executor recovery-time repository identity facts conflict")
		}
	} else if !run.RepositoryAtRecoveryAt.IsZero() || run.RepositoryAtRecoveryError != "" {
		if run.RepositoryAtRecoveryAt.IsZero() || run.RepositoryAtRecoveryError == "" {
			return errors.New("executor recovery must record either a repository subject or why it is unavailable")
		}
	}
	if !run.RepositoryAfterObservedAt.IsZero() && (run.RepositoryAfter == nil ||
		(run.EndTimeSource != ExecutorEndTimeSupervisor && run.RepositoryAfterObservedAt.Before(run.StartedAt)) ||
		(!run.EndedAt.IsZero() && run.EndTimeSource != ExecutorEndTimeSupervisor && run.RepositoryAfterObservedAt.Before(run.EndedAt))) {
		return errors.New("executor post-run repository observation time is invalid")
	}
	if run.Outcome == ExecutorOutcomeQuiescenceUnconfirmed && run.RepositoryAfter != nil {
		return errors.New("unconfirmed executor quiescence cannot claim a stable post-run repository identity")
	}
	if run.Outcome == ExecutorOutcomeQuiescenceUnconfirmed && (run.RepositoryAtRecovery != nil || !run.RepositoryAtRecoveryAt.IsZero() || run.RepositoryAtRecoveryError != "") {
		return errors.New("unconfirmed executor quiescence cannot claim a recovery-time repository identity")
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
		run.RepositoryAfter != nil || run.RepositoryAfterError != "" || run.RepositoryAtRecovery != nil ||
		!run.RepositoryAtRecoveryAt.IsZero() || run.RepositoryAtRecoveryError != "" || len(run.ArtifactRefs) != 0 {
		return errors.New("executor recovery record contains unobserved terminal facts")
	}
	started := run
	started.Outcome = ExecutorOutcomeStarted
	return validateExecutorRun(started, true)
}

const executorReceiptRepositoryConflictBlocker = "executor result repository state changed or is unidentified; verification and further work are stopped"

func executorReceiptRepositoryStateMatches(run ExecutorRun) bool {
	return run.RepositoryAfter != nil && run.RepositoryAfterError == "" && run.RepositoryAtRecovery != nil &&
		run.RepositoryAtRecoveryError == "" &&
		run.RepositoryAfter.HeadCommit == run.RepositoryAtRecovery.HeadCommit &&
		run.RepositoryAfter.TreeSHA == run.RepositoryAtRecovery.TreeSHA
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
