package deep

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

const maxAcceptanceArtifacts = 1

type AcceptanceRuntime struct {
	Worker          string `json:"worker,omitempty"`
	Location        string `json:"location"`
	Shell           string `json:"shell"`
	Protocol        string `json:"protocol"`
	ComputeProvider string `json:"computeProvider,omitempty"`
	ComputeInstance int64  `json:"computeInstanceId,omitempty"`
}

// AcceptanceRun records one objective-specific deterministic check against a
// previously journaled exact TaskCheckpoint. Its command result and decision
// are separate: passing the command is necessary, but repository-change
// policy and exact-state stability also participate in the decision.
type AcceptanceRun struct {
	ID               string `json:"id"`
	StartEventID     string `json:"startEventId"`
	StartedInEpochID string `json:"startedInEpochId"`
	// TaskID is the stable Objective / Work Unit identity, not an atomic action ID.
	TaskID             string                      `json:"taskId"`
	Attempt            int                         `json:"attempt"`
	ContractSHA256     string                      `json:"contractSha256"`
	CommandSHA256      string                      `json:"commandSha256"`
	RepositoryChange   RepositoryChangeExpectation `json:"repositoryChange"`
	RepositoryBaseline VerificationSubject         `json:"repositoryBaseline"`
	// SubjectBefore is the checkpoint HEAD/tree that the acceptance command
	// actually exercises. It differs from Checkpoint.VerificationSubject when
	// Stint had to create a semantic commit after executor/verifier evidence.
	SubjectBefore            VerificationSubject    `json:"subjectBefore"`
	CheckpointEventID        string                 `json:"checkpointEventId"`
	Checkpoint               TaskCheckpoint         `json:"checkpoint"`
	Runtime                  AcceptanceRuntime      `json:"runtime"`
	StartedAt                time.Time              `json:"startedAt"`
	TimeoutSeconds           int                    `json:"timeoutSeconds"`
	RemainingDeadlineSeconds int                    `json:"remainingDeadlineSeconds,omitempty"`
	CheckOutcome             AcceptanceCheckOutcome `json:"checkOutcome"`
	Decision                 AcceptanceOutcome      `json:"decision"`
	Reason                   string                 `json:"reason,omitempty"`
	EndedAt                  time.Time              `json:"endedAt,omitempty"`
	HasExitCode              bool                   `json:"hasExitCode,omitempty"`
	ExitCode                 int                    `json:"exitCode,omitempty"`
	Error                    string                 `json:"error,omitempty"`
	QuiescenceUnconfirmed    bool                   `json:"quiescenceUnconfirmed,omitempty"`
	SubjectAfter             *VerificationSubject   `json:"subjectAfter,omitempty"`
	SubjectAfterError        string                 `json:"subjectAfterError,omitempty"`
	DurationMilliseconds     int64                  `json:"durationMilliseconds,omitempty"`
	ArtifactRefs             []string               `json:"artifactRefs,omitempty"`
}

func NewAcceptanceRunID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate acceptance run identity: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func AcceptanceCommandIdentity(command string) string { return VerificationCommandIdentity(command) }

func acceptanceEventID(id, action string) string { return "acceptance/" + id + "/" + action }

// BeginAcceptanceRun persists the acceptance command identity and exact
// checkpoint subject before the check is invoked.
func BeginAcceptanceRun(stateDir string, state *DeepState, run AcceptanceRun) (AcceptanceRun, error) {
	if state == nil || state.RunID == "" || state.ExecutionEpochID == "" || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return AcceptanceRun{}, errors.New("acceptance start requires a journaled run epoch")
	}
	if state.Phase != PhaseExecuting || state.ExecutionQuiescenceUnconfirmed {
		return AcceptanceRun{}, errors.New("acceptance start requires an executing, quiescent run")
	}
	if err := ValidateMissionAcceptanceContract(state.MissionDefinition()); err != nil || state.AcceptanceContractVersion != DeterministicAcceptanceContractVersion {
		return AcceptanceRun{}, errors.New("acceptance start requires a valid explicit deterministic acceptance contract")
	}
	task, ok := findTask(state, run.TaskID)
	if !ok {
		return AcceptanceRun{}, errors.New("acceptance start must identify a verified Work Unit checkpoint")
	}
	if task.Status != taskCheckpointTaskStatus(run.Checkpoint) || task.Attempts != run.Attempt ||
		task.ExecutorRunID != run.Checkpoint.ExecutorRunID || task.VerificationRunID != run.Checkpoint.VerificationRunID ||
		task.VerificationSubject == nil || *task.VerificationSubject != run.Checkpoint.VerificationSubject ||
		task.CheckpointCommit != run.Checkpoint.Commit || task.CheckpointTreeSHA != run.Checkpoint.TreeSHA {
		return AcceptanceRun{}, errors.New("acceptance start must identify a verified Work Unit checkpoint")
	}
	if run.ID == "" || run.StartedAt.IsZero() {
		return AcceptanceRun{}, errors.New("acceptance start requires identity and timestamp")
	}
	run.StartEventID = acceptanceEventID(run.ID, "started")
	run.StartedInEpochID = state.ExecutionEpochID
	run.StartedAt = run.StartedAt.UTC()
	run.CheckOutcome = AcceptanceCheckStarted
	run.Decision = AcceptanceNotEvaluated
	if binding := state.ComputeBinding; binding != nil {
		run.Runtime.ComputeProvider = binding.Provider
		run.Runtime.ComputeInstance = binding.InstanceID
	}
	if !acceptanceCommandFactsMatch(*state, run) {
		return AcceptanceRun{}, errors.New("acceptance command identity does not match the durable Work Unit contract")
	}
	if err := validateAcceptanceRun(run, true); err != nil {
		return AcceptanceRun{}, err
	}
	event := RunEvent{
		EventID: run.StartEventID, RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: run.StartedAt, Actor: "deep-coordinator", Type: RunEventAcceptanceStarted,
		FromPhase: state.Phase, ToPhase: state.Phase, AcceptanceRun: &run,
		TaskSummary: summarizeRunTasks(state.Tasks),
	}
	if err := appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked); err != nil {
		return AcceptanceRun{}, err
	}
	return run, nil
}

// CompleteAcceptanceRun derives the decision from typed command facts,
// repository-change policy, and the checkpoint subject. Callers cannot submit
// an accepted decision directly.
func CompleteAcceptanceRun(stateDir string, state *DeepState, run AcceptanceRun) error {
	if state == nil || state.RunID == "" || state.ExecutionEpochID == "" || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return errors.New("acceptance result requires a journaled run epoch")
	}
	if run.ID == "" || run.StartEventID != acceptanceEventID(run.ID, "started") || run.EndedAt.IsZero() {
		return errors.New("acceptance result requires its start identity and end timestamp")
	}
	if !acceptanceCommandFactsMatch(*state, run) {
		return errors.New("acceptance result command identity differs from the durable Work Unit contract")
	}
	if !run.CheckOutcome.recorded() {
		return fmt.Errorf("invalid completed acceptance-check outcome %q", run.CheckOutcome)
	}
	run.EndedAt = run.EndedAt.UTC()
	if run.EndedAt.Before(run.StartedAt) {
		return errors.New("acceptance result ends before its durable start")
	}
	run.Decision = deriveAcceptanceOutcome(run)
	if err := validateAcceptanceRun(run, false); err != nil {
		return err
	}
	if _, err := acceptanceArtifactOutput(DeepDir(stateDir, state.SessionID), run); err != nil {
		return err
	}
	if events, err := ReadRunEvents(stateDir, state.SessionID); err != nil {
		return err
	} else {
		for _, event := range events {
			if event.EventID != acceptanceEventID(run.ID, "result") {
				continue
			}
			if event.Type != RunEventAcceptanceResult || event.AcceptanceRun == nil || !reflect.DeepEqual(*event.AcceptanceRun, run) {
				return errors.New("acceptance result identity already has different journal facts")
			}
			fresh, err := LoadState(stateDir, state.SessionID)
			if err != nil {
				return fmt.Errorf("reload durable acceptance result projection: %w", err)
			}
			*state = fresh
			return nil
		}
	}
	projected := *state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if err := applyAcceptanceResult(&projected, run); err != nil {
		return err
	}
	event := RunEvent{
		EventID: acceptanceEventID(run.ID, "result"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: run.EndedAt, Actor: "deep-coordinator", Type: RunEventAcceptanceResult,
		FromPhase: state.Phase, ToPhase: state.Phase, AcceptanceRun: &run,
		TaskSummary: summarizeRunTasks(projected.Tasks),
	}
	return appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked)
}

func (outcome AcceptanceCheckOutcome) recorded() bool {
	switch outcome {
	case AcceptanceCheckPassed, AcceptanceCheckFailed, AcceptanceCheckTimedOut,
		AcceptanceCheckExecutionErr, AcceptanceCheckCanceled:
		return true
	default:
		return false
	}
}

func acceptanceCommandFactsMatch(state DeepState, run AcceptanceRun) bool {
	task, ok := findTask(&state, run.TaskID)
	return ok && state.AcceptanceContractVersion == DeterministicAcceptanceContractVersion &&
		run.ContractSHA256 == state.AcceptanceContractSHA256 && run.CommandSHA256 == AcceptanceCommandIdentity(task.AcceptanceCheck) &&
		run.RepositoryChange == task.RepositoryChange
}

func acceptedRepositoryChange(expectation RepositoryChangeExpectation, baseline, checkpoint VerificationSubject) bool {
	changed := baseline.TreeSHA != checkpoint.TreeSHA
	switch expectation {
	case RepositoryChangeRequired:
		return changed
	case RepositoryChangeOptional:
		return true
	case RepositoryChangeForbidden:
		return !changed
	default:
		return false
	}
}

func deriveAcceptanceOutcome(run AcceptanceRun) AcceptanceOutcome {
	if run.QuiescenceUnconfirmed || run.SubjectAfterError != "" || run.SubjectAfter == nil ||
		run.SubjectBefore != TaskCheckpointSubject(run.Checkpoint) || *run.SubjectAfter != run.SubjectBefore {
		return AcceptanceUnresolved
	}
	switch run.CheckOutcome {
	case AcceptanceCheckTimedOut, AcceptanceCheckExecutionErr, AcceptanceCheckCanceled, AcceptanceCheckUnknown:
		return AcceptanceUnresolved
	case AcceptanceCheckFailed:
		return AcceptanceNotSatisfied
	case AcceptanceCheckPassed:
		if !acceptedRepositoryChange(run.RepositoryChange, run.RepositoryBaseline, run.Checkpoint.VerificationSubject) {
			return AcceptanceNotSatisfied
		}
		return AcceptanceAccepted
	default:
		return AcceptanceUnresolved
	}
}

func validateAcceptanceRun(run AcceptanceRun, starting bool) error {
	if run.ID == "" || len(run.ID) != 32 || run.TaskID == "" || len(run.TaskID) > 128 ||
		strings.ContainsAny(run.TaskID, "\x00\r\n") || run.Attempt < 1 || run.Attempt > 1_000_000 ||
		run.ContractSHA256 == "" || run.CommandSHA256 == "" {
		return errors.New("acceptance run identity or contract provenance is invalid")
	}
	if _, err := hex.DecodeString(run.ID); err != nil {
		return errors.New("acceptance run identity is not hexadecimal")
	}
	if _, err := hex.DecodeString(run.ContractSHA256); err != nil {
		return errors.New("acceptance contract identity is not hexadecimal")
	}
	if _, err := hex.DecodeString(run.CommandSHA256); err != nil {
		return errors.New("acceptance command identity is not hexadecimal")
	}
	if run.StartEventID != acceptanceEventID(run.ID, "started") || run.StartedInEpochID == "" || len(run.StartedInEpochID) > 128 ||
		strings.ContainsAny(run.StartedInEpochID, "\x00\r\n") || run.StartedAt.IsZero() ||
		run.RepositoryBaseline.HeadCommit == "" || run.RepositoryBaseline.TreeSHA == "" ||
		len(run.RepositoryBaseline.HeadCommit) > 128 || len(run.RepositoryBaseline.TreeSHA) > 128 {
		return errors.New("acceptance run start identity or repository baseline is invalid")
	}
	if run.CheckpointEventID == "" || run.CheckpointEventID != taskCheckpointRecordEventID(run.Checkpoint) ||
		run.Checkpoint.TaskID != run.TaskID || run.Checkpoint.Attempt != run.Attempt ||
		run.SubjectBefore != TaskCheckpointSubject(run.Checkpoint) {
		return errors.New("acceptance run checkpoint identity does not match its Work Unit")
	}
	if err := validateTaskCheckpoint(run.Checkpoint); err != nil {
		return fmt.Errorf("acceptance run checkpoint is invalid: %w", err)
	}
	switch run.RepositoryChange {
	case RepositoryChangeRequired, RepositoryChangeOptional, RepositoryChangeForbidden:
	default:
		return errors.New("acceptance run has an invalid repository-change expectation")
	}
	for _, value := range []string{run.Runtime.Worker, run.Runtime.Location, run.Runtime.Shell, run.Runtime.Protocol, run.Runtime.ComputeProvider} {
		if len(value) > 128 || strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("acceptance runtime identity is invalid")
		}
	}
	if run.Runtime.Location == "" || run.Runtime.Shell == "" || run.Runtime.Protocol == "" || run.Runtime.ComputeInstance < 0 ||
		(run.Runtime.ComputeProvider == "") != (run.Runtime.ComputeInstance == 0) {
		return errors.New("acceptance runtime identity is incomplete")
	}
	if run.TimeoutSeconds < 0 || run.TimeoutSeconds > 7*24*60*60 || run.RemainingDeadlineSeconds < 0 || run.RemainingDeadlineSeconds > 7*24*60*60 {
		return errors.New("acceptance timeout context is invalid")
	}
	if len(run.Reason) > maxRunEventTextBytes || len(run.Error) > maxRunEventTextBytes || len(run.SubjectAfterError) > maxRunEventTextBytes {
		return errors.New("acceptance result text exceeds limits")
	}
	if len(run.ArtifactRefs) > maxAcceptanceArtifacts {
		return errors.New("acceptance result has too many artifact references")
	}
	for _, ref := range run.ArtifactRefs {
		if !validAcceptanceArtifactRef(run.ID, ref) {
			return errors.New("acceptance result has an invalid artifact reference")
		}
	}
	if starting {
		if run.CheckOutcome != AcceptanceCheckStarted || run.Decision != AcceptanceNotEvaluated || !run.EndedAt.IsZero() ||
			run.HasExitCode || run.ExitCode != 0 || run.Error != "" || run.Reason != "" || run.QuiescenceUnconfirmed ||
			run.SubjectAfter != nil || run.SubjectAfterError != "" || run.DurationMilliseconds != 0 || len(run.ArtifactRefs) != 0 {
			return errors.New("acceptance start contains result-only facts")
		}
		return nil
	}
	if !run.CheckOutcome.recorded() || run.EndedAt.IsZero() || run.EndedAt.Before(run.StartedAt) || run.DurationMilliseconds < 0 ||
		run.DurationMilliseconds > int64((7*24*time.Hour)/time.Millisecond) {
		return errors.New("acceptance result outcome or timestamps are invalid")
	}
	if run.SubjectAfter != nil && (run.SubjectAfter.HeadCommit == "" || run.SubjectAfter.TreeSHA == "" ||
		len(run.SubjectAfter.HeadCommit) > 128 || len(run.SubjectAfter.TreeSHA) > 128) {
		return errors.New("acceptance post-check repository subject is invalid")
	}
	if (run.SubjectAfter == nil) == (run.SubjectAfterError == "") && !run.QuiescenceUnconfirmed {
		return errors.New("acceptance result must record a post-check subject or why it is unavailable")
	}
	switch run.CheckOutcome {
	case AcceptanceCheckPassed:
		if !run.HasExitCode || run.ExitCode != 0 || run.Error != "" {
			return errors.New("passed acceptance check disagrees with exit facts")
		}
	case AcceptanceCheckFailed:
		if !run.HasExitCode || run.ExitCode == 0 {
			return errors.New("failed acceptance check requires a known nonzero exit code")
		}
	case AcceptanceCheckTimedOut:
		if strings.TrimSpace(run.Error) == "" || (run.HasExitCode && run.ExitCode == 0) {
			return errors.New("timed-out acceptance check requires an error and cannot have exit code zero")
		}
	case AcceptanceCheckCanceled, AcceptanceCheckExecutionErr:
		if strings.TrimSpace(run.Error) == "" || run.HasExitCode {
			return errors.New("acceptance execution error requires an error and cannot claim an exit code")
		}
	}
	if run.QuiescenceUnconfirmed && run.CheckOutcome != AcceptanceCheckExecutionErr && run.CheckOutcome != AcceptanceCheckTimedOut && run.CheckOutcome != AcceptanceCheckCanceled {
		return errors.New("unconfirmed acceptance-check quiescence has an incompatible outcome")
	}
	if run.Decision != deriveAcceptanceOutcome(run) {
		return errors.New("acceptance decision does not follow from its typed evidence")
	}
	return nil
}

func PersistAcceptanceOutput(stateDir, sessionID, runID string, output []byte) (string, error) {
	if len(output) == 0 {
		return "", nil
	}
	output = []byte(strings.ToValidUTF8(string(output), "\uFFFD"))
	if len(output) > 4096 {
		output = output[len(output)-4096:]
		for !utf8.Valid(output) {
			output = output[1:]
		}
	}
	if len(runID) != 32 {
		return "", errors.New("invalid acceptance artifact run identity")
	}
	if _, err := hex.DecodeString(runID); err != nil {
		return "", errors.New("invalid acceptance artifact run identity")
	}
	ref := filepath.ToSlash(filepath.Join("artifacts", "acceptance", runID+".txt"))
	if err := writeAtomic(filepath.Join(DeepDir(stateDir, sessionID), filepath.FromSlash(ref)), output); err != nil {
		return "", fmt.Errorf("persist bounded acceptance output artifact: %w", err)
	}
	return ref, nil
}

func acceptanceArtifactOutput(dir string, run AcceptanceRun) (string, error) {
	if len(run.ArtifactRefs) == 0 {
		return "", nil
	}
	if len(run.ArtifactRefs) != 1 || !validAcceptanceArtifactRef(run.ID, run.ArtifactRefs[0]) {
		return "", errors.New("acceptance output artifact references are invalid")
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(run.ArtifactRefs[0])))
	if err != nil {
		return "", fmt.Errorf("read acceptance-check output artifact: %w", err)
	}
	if len(data) > 4096 || !utf8.Valid(data) {
		return "", errors.New("acceptance-check output artifact exceeds its limit or is not valid UTF-8")
	}
	return string(data), nil
}

func ReadAcceptanceOutput(stateDir, sessionID string, run AcceptanceRun) (string, error) {
	return acceptanceArtifactOutput(DeepDir(stateDir, sessionID), run)
}

func AcceptanceOutputExcerpt(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	value := strings.TrimSpace(strings.Join(lines, "\n"))
	if len(value) > 2048 {
		value = value[len(value)-2048:]
		for !utf8.ValidString(value) {
			value = value[1:]
		}
	}
	return value
}

func validAcceptanceArtifactRef(id, ref string) bool {
	return ref == filepath.ToSlash(filepath.Join("artifacts", "acceptance", id+".txt")) &&
		!strings.ContainsAny(ref, "\x00\r\n")
}

func applyAcceptanceStarted(state *DeepState, run AcceptanceRun) error {
	task, ok := findTask(state, run.TaskID)
	if !ok || task.Attempts != run.Attempt || task.Status != taskCheckpointTaskStatus(run.Checkpoint) || task.ExecutorRunID != run.Checkpoint.ExecutorRunID ||
		task.VerificationRunID != run.Checkpoint.VerificationRunID || task.VerificationSubject == nil ||
		*task.VerificationSubject != run.Checkpoint.VerificationSubject || task.CheckpointCommit != run.Checkpoint.Commit ||
		task.CheckpointTreeSHA != run.Checkpoint.TreeSHA {
		return errors.New("acceptance start does not match the current Work Unit checkpoint")
	}
	task.AcceptanceOutcome = AcceptanceNotEvaluated
	task.AcceptanceRunID = run.ID
	task.AcceptanceCheckOutcome = AcceptanceCheckStarted
	task.AcceptanceReason = ""
	task.AcceptanceSubject = &run.SubjectBefore
	task.AcceptanceCheckpointEventID = run.CheckpointEventID
	task.AcceptanceCheckpointCommit = run.Checkpoint.Commit
	task.AcceptanceCheckpointTreeSHA = run.Checkpoint.TreeSHA
	task.AcceptanceOutput = ""
	return nil
}

func taskCheckpointTaskStatus(checkpoint TaskCheckpoint) Status {
	if checkpoint.Basis == TaskCheckpointBasisExecutor {
		return StatusCheckpointed
	}
	return StatusVerified
}

func applyAcceptanceResult(state *DeepState, run AcceptanceRun) error {
	task, ok := findTask(state, run.TaskID)
	if !ok || task.AcceptanceRunID != run.ID || task.Attempts != run.Attempt || task.AcceptanceSubject == nil ||
		*task.AcceptanceSubject != run.SubjectBefore || task.AcceptanceCheckpointEventID != run.CheckpointEventID {
		return errors.New("acceptance result does not match its projected start and checkpoint")
	}
	task.AcceptanceOutcome = run.Decision
	task.AcceptanceCheckOutcome = run.CheckOutcome
	task.AcceptanceReason = run.Reason
	switch run.Decision {
	case AcceptanceAccepted:
		task.Status = StatusAccepted
		task.Blocker = ""
	case AcceptanceNotSatisfied:
		task.Status = StatusIncomplete
		task.Blocker = run.Reason
	case AcceptanceUnresolved:
		task.Status = StatusNeedsHuman
		task.Blocker = run.Reason
	}
	if run.QuiescenceUnconfirmed {
		state.ExecutionQuiescenceUnconfirmed = true
		state.ExecutionQuiescenceTaskID = task.ID
	}
	return nil
}

func hydrateAcceptanceProjection(state *DeepState, event RunEvent, dir string) error {
	if event.Type != RunEventAcceptanceResult || event.AcceptanceRun == nil {
		return nil
	}
	output, err := acceptanceArtifactOutput(dir, *event.AcceptanceRun)
	if err != nil {
		return err
	}
	task, ok := findTask(state, event.AcceptanceRun.TaskID)
	if !ok || task.AcceptanceRunID != event.AcceptanceRun.ID {
		return errors.New("cannot hydrate output for an unprojected acceptance result")
	}
	task.AcceptanceOutput = AcceptanceOutputExcerpt(output)
	return nil
}

func acceptanceProjectionMatches(dir string, state DeepState, run AcceptanceRun, starting bool, recovery bool) bool {
	task, ok := findTask(&state, run.TaskID)
	if !ok || task.AcceptanceRunID != run.ID || task.Attempts != run.Attempt || task.AcceptanceSubject == nil ||
		*task.AcceptanceSubject != run.SubjectBefore || task.AcceptanceCheckpointEventID != run.CheckpointEventID ||
		task.AcceptanceCheckpointCommit != run.Checkpoint.Commit || task.AcceptanceCheckpointTreeSHA != run.Checkpoint.TreeSHA {
		return false
	}
	if starting {
		return task.Status == taskCheckpointTaskStatus(run.Checkpoint) && task.AcceptanceOutcome == AcceptanceNotEvaluated && task.AcceptanceCheckOutcome == AcceptanceCheckStarted &&
			task.AcceptanceReason == "" && task.AcceptanceOutput == ""
	}
	if recovery {
		return task.Status == StatusNeedsHuman && task.AcceptanceOutcome == AcceptanceUnresolved && task.AcceptanceCheckOutcome == AcceptanceCheckUnknown &&
			task.AcceptanceReason == run.Reason && task.AcceptanceOutput == "" && state.ExecutionQuiescenceUnconfirmed && state.ExecutionQuiescenceTaskID == task.ID
	}
	if task.AcceptanceOutcome != run.Decision || task.AcceptanceCheckOutcome != run.CheckOutcome || task.AcceptanceReason != run.Reason {
		return false
	}
	wantStatus := StatusIncomplete
	switch run.Decision {
	case AcceptanceAccepted:
		wantStatus = StatusAccepted
	case AcceptanceNotSatisfied:
		wantStatus = StatusIncomplete
	case AcceptanceUnresolved:
		wantStatus = StatusNeedsHuman
	}
	if task.Status != wantStatus {
		return false
	}
	output, err := acceptanceArtifactOutput(dir, run)
	return err == nil && task.AcceptanceOutput == AcceptanceOutputExcerpt(output)
}

func validateAcceptanceRecovery(run AcceptanceRun, reason string) error {
	if run.CheckOutcome != AcceptanceCheckUnknown || run.Decision != AcceptanceUnresolved || run.Reason != reason ||
		!run.EndedAt.IsZero() || run.HasExitCode || run.ExitCode != 0 || run.Error != "" || !run.QuiescenceUnconfirmed ||
		run.SubjectAfter != nil || run.SubjectAfterError == "" || run.DurationMilliseconds != 0 || len(run.ArtifactRefs) != 0 {
		return errors.New("acceptance recovery contains facts that were not durably observed")
	}
	started := run
	started.CheckOutcome = AcceptanceCheckStarted
	started.Decision = AcceptanceNotEvaluated
	started.Reason = ""
	started.QuiescenceUnconfirmed = false
	started.SubjectAfterError = ""
	return validateAcceptanceRun(started, true)
}

func applyAcceptanceRecovery(state *DeepState, run AcceptanceRun, reason string) error {
	task, ok := findTask(state, run.TaskID)
	if !ok || task.AcceptanceRunID != run.ID || task.Attempts != run.Attempt || task.AcceptanceCheckpointEventID != run.CheckpointEventID {
		return errors.New("acceptance-recovery event does not match its projected task attempt")
	}
	task.AcceptanceOutcome = AcceptanceUnresolved
	task.AcceptanceCheckOutcome = AcceptanceCheckUnknown
	task.AcceptanceReason = reason
	task.AcceptanceOutput = ""
	task.Status = StatusNeedsHuman
	task.Blocker = reason
	state.ExecutionQuiescenceUnconfirmed = true
	state.ExecutionQuiescenceTaskID = task.ID
	return nil
}

func validateAcceptanceEventTransition(prior []RunEvent, event RunEvent) error {
	if hasUnmatchedAcceptance(prior) && event.Type != RunEventAcceptanceResult && event.Type != RunEventAcceptanceRecovery {
		return errors.New("an acceptance invocation is unmatched; recover its outcome and quiescence before further work")
	}
	if event.Type != RunEventAcceptanceStarted && event.Type != RunEventAcceptanceResult && event.Type != RunEventAcceptanceRecovery {
		return nil
	}
	run := event.AcceptanceRun
	if run == nil {
		return errors.New("acceptance event has no run record")
	}
	switch event.Type {
	case RunEventAcceptanceStarted:
		if event.EpochID != prior[len(prior)-1].EpochID || run.StartedInEpochID != event.EpochID {
			return errors.New("acceptance start must belong to the active run epoch")
		}
		for _, old := range prior {
			if old.AcceptanceRun == nil {
				continue
			}
			if old.AcceptanceRun.ID == run.ID {
				return fmt.Errorf("acceptance run %q already has journal history", run.ID)
			}
			if old.Type == RunEventAcceptanceStarted {
				if _, closed := acceptanceRunClosed(prior, old.AcceptanceRun.ID); !closed {
					return fmt.Errorf("acceptance run %q is still unmatched", old.AcceptanceRun.ID)
				}
			}
			if old.Type == RunEventAcceptanceStarted && old.AcceptanceRun.CheckpointEventID == run.CheckpointEventID {
				return errors.New("checkpoint already has an acceptance invocation")
			}
		}
		checkpointEvent, ok := checkpointEventByID(prior, run.CheckpointEventID)
		if !ok || checkpointEvent.TaskCheckpoint == nil || *checkpointEvent.TaskCheckpoint != run.Checkpoint {
			return errors.New("acceptance start does not reference its exact prior task checkpoint event")
		}
		latestCheckpoint, ok := latestCheckpointEventByTask(prior, run.TaskID)
		if !ok || latestCheckpoint.EventID != run.CheckpointEventID {
			return errors.New("acceptance start does not reference the current Work Unit checkpoint")
		}
		baseline, ok := firstExecutorRepositoryBaseline(prior, run.TaskID)
		if !ok || baseline != run.RepositoryBaseline {
			return errors.New("acceptance start repository baseline differs from the first Work Unit executor start")
		}
	case RunEventAcceptanceResult:
		start, ok := acceptanceStart(prior, run.ID)
		if !ok || event.EpochID != start.EpochID || !sameAcceptanceStart(*start.AcceptanceRun, *run) {
			return errors.New("acceptance result does not match a start event in the same epoch")
		}
		if _, closed := acceptanceRunClosed(prior, run.ID); closed {
			return errors.New("acceptance result follows an already closed invocation")
		}
		for _, later := range prior[start.Sequence:] {
			if later.ExecutorRun != nil && later.Type == RunEventExecutorStarted && later.ExecutorRun.TaskID == run.TaskID {
				return errors.New("acceptance result follows a newer executor attempt for its Work Unit")
			}
			if later.TaskCheckpoint != nil && later.Type == RunEventTaskCheckpointCreated && later.TaskCheckpoint.TaskID == run.TaskID {
				return errors.New("acceptance result follows a newer task checkpoint for its Work Unit")
			}
		}
	case RunEventAcceptanceRecovery:
		start, ok := acceptanceStart(prior, run.ID)
		if !ok || !sameAcceptanceStart(*start.AcceptanceRun, *run) || event.EpochID != prior[len(prior)-1].EpochID {
			return errors.New("acceptance recovery does not match an invocation in run history")
		}
		if _, closed := acceptanceRunClosed(prior, run.ID); closed {
			return errors.New("acceptance recovery follows an already closed invocation")
		}
	}
	return nil
}

func hasUnmatchedAcceptance(events []RunEvent) bool {
	open := make(map[string]struct{})
	for _, event := range events {
		if event.AcceptanceRun == nil {
			continue
		}
		switch event.Type {
		case RunEventAcceptanceStarted:
			open[event.AcceptanceRun.ID] = struct{}{}
		case RunEventAcceptanceResult, RunEventAcceptanceRecovery:
			delete(open, event.AcceptanceRun.ID)
		}
	}
	return len(open) != 0
}

func checkpointEventByID(events []RunEvent, id string) (RunEvent, bool) {
	for _, event := range events {
		if event.EventID == id && event.Type == RunEventTaskCheckpointCreated {
			return event, true
		}
	}
	return RunEvent{}, false
}

func latestCheckpointEventByTask(events []RunEvent, taskID string) (RunEvent, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == RunEventTaskCheckpointCreated && events[i].TaskCheckpoint != nil && events[i].TaskCheckpoint.TaskID == taskID {
			return events[i], true
		}
	}
	return RunEvent{}, false
}

func firstExecutorRepositoryBaseline(events []RunEvent, taskID string) (VerificationSubject, bool) {
	for _, event := range events {
		if event.Type == RunEventExecutorStarted && event.ExecutorRun != nil && event.ExecutorRun.TaskID == taskID {
			if event.ExecutorRun.RepositoryBefore == nil {
				return VerificationSubject{}, false
			}
			return *event.ExecutorRun.RepositoryBefore, true
		}
	}
	return VerificationSubject{}, false
}

func acceptanceStart(events []RunEvent, id string) (*RunEvent, bool) {
	for i := range events {
		if events[i].Type == RunEventAcceptanceStarted && events[i].AcceptanceRun != nil && events[i].AcceptanceRun.ID == id {
			return &events[i], true
		}
	}
	return nil, false
}

func acceptanceRunClosed(events []RunEvent, id string) (RunEvent, bool) {
	for _, event := range events {
		if (event.Type == RunEventAcceptanceResult || event.Type == RunEventAcceptanceRecovery) &&
			event.AcceptanceRun != nil && event.AcceptanceRun.ID == id {
			return event, true
		}
	}
	return RunEvent{}, false
}

func sameAcceptanceStart(start, result AcceptanceRun) bool {
	return start.ID == result.ID && start.StartEventID == result.StartEventID && start.StartedInEpochID == result.StartedInEpochID &&
		start.TaskID == result.TaskID && start.Attempt == result.Attempt && start.ContractSHA256 == result.ContractSHA256 &&
		start.CommandSHA256 == result.CommandSHA256 && start.RepositoryChange == result.RepositoryChange &&
		start.RepositoryBaseline == result.RepositoryBaseline && start.CheckpointEventID == result.CheckpointEventID &&
		start.SubjectBefore == result.SubjectBefore && start.Checkpoint == result.Checkpoint && start.Runtime == result.Runtime && start.StartedAt.Equal(result.StartedAt) &&
		start.TimeoutSeconds == result.TimeoutSeconds && start.RemainingDeadlineSeconds == result.RemainingDeadlineSeconds
}

func validateAcceptanceHistoryContract(mission Mission, events []RunEvent) error {
	for _, event := range events {
		if event.AcceptanceRun == nil {
			continue
		}
		run := event.AcceptanceRun
		task, ok := missionTaskByID(mission, run.TaskID)
		if !ok || mission.AcceptanceContractVersion != DeterministicAcceptanceContractVersion ||
			run.ContractSHA256 != mission.AcceptanceContractSHA256 || run.CommandSHA256 != AcceptanceCommandIdentity(task.AcceptanceCheck) ||
			run.RepositoryChange != task.RepositoryChange {
			return errors.New("canonical acceptance history conflicts with the persisted mission contract")
		}
	}
	return nil
}

func missionTaskByID(mission Mission, id string) (Task, bool) {
	for _, task := range mission.Tasks {
		if task.ID == id && isAcceptanceContractTask(task) {
			return task, true
		}
	}
	return Task{}, false
}

func validateAcceptanceProjection(dir string, state DeepState, events []RunEvent) error {
	if state.AcceptanceContractVersion != DeterministicAcceptanceContractVersion {
		return nil
	}
	latest := make(map[string]RunEvent)
	for _, event := range events {
		if event.AcceptanceRun != nil && (event.Type == RunEventAcceptanceStarted || event.Type == RunEventAcceptanceResult || event.Type == RunEventAcceptanceRecovery) {
			latest[event.AcceptanceRun.TaskID] = event
		}
	}
	for _, event := range latest {
		run := event.AcceptanceRun
		superseded := false
		for _, later := range events[event.Sequence:] {
			if later.Type == RunEventExecutorStarted && later.ExecutorRun != nil && later.ExecutorRun.TaskID == run.TaskID {
				superseded = true
			}
		}
		if superseded {
			task, ok := findTask(&state, run.TaskID)
			if !ok || !acceptanceProjectionCleared(*task, false) {
				return errors.New("deep.json did not reset superseded acceptance evidence for a newer executor attempt")
			}
			continue
		}
		starting := event.Type == RunEventAcceptanceStarted
		recovery := event.Type == RunEventAcceptanceRecovery
		if !acceptanceProjectionMatches(dir, state, *run, starting, recovery) {
			return errors.New("deep.json acceptance projection contradicts canonical acceptance history")
		}
	}
	for _, task := range state.Tasks {
		if !isAcceptanceContractTask(task) {
			continue
		}
		if _, exists := latest[task.ID]; exists {
			continue
		}
		if !acceptanceProjectionCleared(task, true) {
			return errors.New("deep.json contains acceptance facts with no canonical acceptance event")
		}
	}
	return nil
}

func acceptanceProjectionCleared(task Task, allowUninitialized bool) bool {
	validOutcome := task.AcceptanceOutcome == AcceptanceNotEvaluated
	if allowUninitialized && task.AcceptanceOutcome == "" {
		validOutcome = true
	}
	return validOutcome && task.AcceptanceRunID == "" && task.AcceptanceCheckOutcome == "" && task.AcceptanceReason == "" &&
		task.AcceptanceSubject == nil && task.AcceptanceCheckpointEventID == "" && task.AcceptanceCheckpointCommit == "" &&
		task.AcceptanceCheckpointTreeSHA == "" && task.AcceptanceOutput == ""
}

func LoadAcceptanceRun(stateDir, sessionID, id string) (AcceptanceRun, bool, error) {
	events, err := ReadRunEvents(stateDir, sessionID)
	if err != nil {
		return AcceptanceRun{}, false, err
	}
	var found AcceptanceRun
	ok := false
	for _, event := range events {
		if event.AcceptanceRun != nil && event.AcceptanceRun.ID == id {
			found, ok = *event.AcceptanceRun, true
		}
	}
	return found, ok, nil
}

func LoadUnmatchedAcceptanceRun(stateDir, sessionID string) (*AcceptanceRun, error) {
	events, err := ReadRunEvents(stateDir, sessionID)
	if err != nil {
		return nil, err
	}
	starts := make(map[string]AcceptanceRun)
	for _, event := range events {
		switch event.Type {
		case RunEventAcceptanceStarted:
			if event.AcceptanceRun != nil {
				starts[event.AcceptanceRun.ID] = *event.AcceptanceRun
			}
		case RunEventAcceptanceResult, RunEventAcceptanceRecovery:
			if event.AcceptanceRun != nil {
				delete(starts, event.AcceptanceRun.ID)
			}
		}
	}
	if len(starts) == 0 {
		return nil, nil
	}
	if len(starts) != 1 {
		return nil, fmt.Errorf("journal contains %d unmatched acceptance starts; refusing recovery", len(starts))
	}
	for _, run := range starts {
		copy := run
		return &copy, nil
	}
	return nil, nil
}

func RecoverUnmatchedAcceptanceRun(stateDir string, state *DeepState, at time.Time) (*AcceptanceRun, error) {
	if state == nil || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return nil, nil
	}
	run, err := LoadUnmatchedAcceptanceRun(stateDir, state.SessionID)
	if err != nil || run == nil {
		return run, err
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	recovered := *run
	recovered.CheckOutcome = AcceptanceCheckUnknown
	recovered.Decision = AcceptanceUnresolved
	recovered.Reason = "coordinator resumed without a durable acceptance-check result; outcome and process quiescence are unknown"
	recovered.QuiescenceUnconfirmed = true
	recovered.SubjectAfterError = "acceptance-check process quiescence and post-check repository subject are unknown"
	event := RunEvent{
		EventID: acceptanceEventID(run.ID, "recovery-required/"+state.ExecutionEpochID), RunID: state.RunID,
		EpochID: state.ExecutionEpochID, OccurredAt: at.UTC(), Actor: "deep-coordinator",
		Type: RunEventAcceptanceRecovery, FromPhase: state.Phase, ToPhase: state.Phase,
		Reason: recovered.Reason, AcceptanceRun: &recovered, TaskSummary: summarizeRunTasks(state.Tasks),
	}
	if err := appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked); err != nil {
		return nil, err
	}
	return &recovered, nil
}
