package deep

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const maxVerificationArtifacts = 1

type VerificationPurpose string

const (
	VerificationPurposeTask       VerificationPurpose = "task"
	VerificationPurposeMissionEnd VerificationPurpose = "mission_final"
)

// VerificationRuntime records bounded identity for the verifier execution
// path. It deliberately excludes arbitrary environment values and secrets.
type VerificationRuntime struct {
	Worker          string `json:"worker,omitempty"`
	Location        string `json:"location"`
	Shell           string `json:"shell"`
	Protocol        string `json:"protocol"`
	ComputeProvider string `json:"computeProvider,omitempty"`
	ComputeInstance int64  `json:"computeInstanceId,omitempty"`
}

// VerificationRun is the durable record of one deterministic command being
// exercised against one exact Git-visible subject. It records evidence, not
// task acceptance. Start and result events carry the same immutable start
// facts; a missing result remains explicitly unresolved after recovery.
type VerificationRun struct {
	ID                       string               `json:"id"`
	StartEventID             string               `json:"startEventId"`
	StartedInEpochID         string               `json:"startedInEpochId"`
	Purpose                  VerificationPurpose  `json:"purpose"`
	TaskID                   string               `json:"taskId,omitempty"`
	Attempt                  int                  `json:"attempt,omitempty"`
	CommandSource            string               `json:"commandSource"`
	CommandSHA256            string               `json:"commandSha256"`
	Runtime                  VerificationRuntime  `json:"runtime"`
	StartedAt                time.Time            `json:"startedAt"`
	TimeoutSeconds           int                  `json:"timeoutSeconds"`
	RemainingDeadlineSeconds int                  `json:"remainingDeadlineSeconds,omitempty"`
	Subject                  VerificationSubject  `json:"subject"`
	BookkeepingBefore        map[string]string    `json:"bookkeepingBefore,omitempty"`
	Outcome                  VerificationOutcome  `json:"outcome"`
	EndedAt                  time.Time            `json:"endedAt,omitempty"`
	HasExitCode              bool                 `json:"hasExitCode,omitempty"`
	ExitCode                 int                  `json:"exitCode,omitempty"`
	Error                    string               `json:"error,omitempty"`
	QuiescenceUnconfirmed    bool                 `json:"quiescenceUnconfirmed,omitempty"`
	SubjectAfter             *VerificationSubject `json:"subjectAfter,omitempty"`
	SubjectAfterError        string               `json:"subjectAfterError,omitempty"`
	BookkeepingAfter         map[string]string    `json:"bookkeepingAfter,omitempty"`
	DurationMilliseconds     int64                `json:"durationMilliseconds,omitempty"`
	ArtifactRefs             []string             `json:"artifactRefs,omitempty"`
}

func NewVerificationRunID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate verification run identity: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// VerificationCommandIdentity is a stable non-reversible identity for the
// exact configured command. Commands remain in mission/task compatibility
// state and are not duplicated into the append-only journal.
func VerificationCommandIdentity(command string) string {
	sum := sha256.Sum256([]byte(command))
	return hex.EncodeToString(sum[:])
}

// BeginVerificationRun records the verification subject and invocation facts
// durably before the verifier is called.
func BeginVerificationRun(stateDir string, state *DeepState, run VerificationRun) (VerificationRun, error) {
	if state == nil || state.RunID == "" || state.ExecutionEpochID == "" || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return VerificationRun{}, errors.New("verification start requires a journaled run epoch")
	}
	if run.ID == "" || run.StartedAt.IsZero() || run.CommandSHA256 == "" || run.Subject.HeadCommit == "" || run.Subject.TreeSHA == "" {
		return VerificationRun{}, errors.New("verification start requires identity, timestamp, command identity, and exact subject")
	}
	run.StartEventID = verificationEventID(run.ID, "started")
	run.StartedInEpochID = state.ExecutionEpochID
	run.StartedAt = run.StartedAt.UTC()
	run.Outcome = VerificationStarted
	if binding := state.ComputeBinding; binding != nil {
		run.Runtime.ComputeProvider = binding.Provider
		run.Runtime.ComputeInstance = binding.InstanceID
	}
	if run.Purpose == VerificationPurposeTask {
		task, ok := findTask(state, run.TaskID)
		if !ok || task.Status != StatusActive || task.Attempts != run.Attempt || run.Attempt < 1 {
			return VerificationRun{}, errors.New("task verification start does not match an active task attempt")
		}
	} else if run.Purpose == VerificationPurposeMissionEnd {
		if state.Phase != PhaseLanding || run.TaskID != "" || run.Attempt != 0 {
			return VerificationRun{}, errors.New("mission-final verification must start during landing without a task identity")
		}
	} else {
		return VerificationRun{}, fmt.Errorf("invalid verification purpose %q", run.Purpose)
	}
	if run.CommandSHA256 != expectedVerificationCommandIdentity(*state, run) {
		return VerificationRun{}, errors.New("verification command identity does not match the durable task or mission command")
	}
	if err := validateVerificationRun(run, true); err != nil {
		return VerificationRun{}, err
	}
	event := RunEvent{
		EventID: run.StartEventID, RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: run.StartedAt, Actor: "deep-coordinator", Type: RunEventVerificationStarted,
		FromPhase: state.Phase, ToPhase: state.Phase, VerificationRun: &run,
		TaskSummary: summarizeRunTasks(state.Tasks),
	}
	if err := appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked); err != nil {
		return VerificationRun{}, err
	}
	return run, nil
}

// CompleteVerificationRun records the observed verifier result. Callers must
// establish writer quiescence and capture the post-verification subject
// before calling this method. A result event is persisted before deep.json is
// projected, so projection failures replay without rerunning the verifier.
func CompleteVerificationRun(stateDir string, state *DeepState, run VerificationRun) error {
	if state == nil || state.RunID == "" || state.ExecutionEpochID == "" || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return errors.New("verification result requires a journaled run epoch")
	}
	if run.ID == "" || run.StartEventID != verificationEventID(run.ID, "started") || run.EndedAt.IsZero() {
		return errors.New("verification result requires its start identity and end timestamp")
	}
	if run.Outcome == VerificationStarted || run.Outcome == VerificationUnknown || run.Outcome == VerificationNotRun {
		return fmt.Errorf("invalid completed verification outcome %q", run.Outcome)
	}
	run.EndedAt = run.EndedAt.UTC()
	if err := validateVerificationRun(run, false); err != nil {
		return err
	}
	if run.Purpose == VerificationPurposeTask {
		if _, ok := findTask(state, run.TaskID); !ok {
			return fmt.Errorf("verification result references unknown task %q", run.TaskID)
		}
	}
	projected := *state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if err := applyVerificationResult(&projected, run); err != nil {
		return err
	}
	event := RunEvent{
		EventID: verificationEventID(run.ID, "result"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: run.EndedAt, Actor: "deep-coordinator", Type: RunEventVerificationResult,
		FromPhase: state.Phase, ToPhase: state.Phase, VerificationRun: &run,
		TaskSummary: summarizeRunTasks(projected.Tasks),
	}
	return appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked)
}

// RecoverUnmatchedVerificationRun closes an interrupted verifier start as
// unknown and blocks all later verification/checkpoint work. A missing result
// never proves that verifier descendants stopped or that the command passed.
func RecoverUnmatchedVerificationRun(stateDir string, state *DeepState, at time.Time) (*VerificationRun, error) {
	if state == nil || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return nil, nil
	}
	events, err := ReadRunEvents(stateDir, state.SessionID)
	if err != nil {
		return nil, err
	}
	starts := map[string]VerificationRun{}
	for _, event := range events {
		switch event.Type {
		case RunEventVerificationStarted:
			if event.VerificationRun != nil {
				starts[event.VerificationRun.ID] = *event.VerificationRun
			}
		case RunEventVerificationResult, RunEventVerificationRecovery:
			if event.VerificationRun != nil {
				delete(starts, event.VerificationRun.ID)
			}
		}
	}
	if len(starts) == 0 {
		return nil, nil
	}
	if len(starts) != 1 {
		return nil, fmt.Errorf("journal contains %d unmatched verification starts; refusing recovery", len(starts))
	}
	var run VerificationRun
	for _, run = range starts {
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	run.Outcome = VerificationUnknown
	reason := "coordinator resumed without a durable verification result; command outcome and process quiescence are unknown"
	projected := *state
	projected.Tasks = append([]Task(nil), state.Tasks...)
	if run.Purpose == VerificationPurposeTask {
		task, ok := findTask(&projected, run.TaskID)
		if !ok || task.VerificationRunID != run.ID || task.Attempts != run.Attempt {
			return nil, errors.New("unmatched verification start does not match the projected task attempt")
		}
		task.Status = StatusNeedsHuman
		task.Blocker = "verification invocation has no durable result; process quiescence and outcome are unknown, so checkpointing is stopped"
		task.ExecutorRunProcessed = true
		task.VerificationOutcome = VerificationUnknown
		task.VerificationResult = "verification outcome unknown: " + reason
	} else if run.Purpose == VerificationPurposeMissionEnd {
		projected.LandingVerificationOutcome = VerificationUnknown
		projected.LandingVerifyDone = false
		projected.LandingVerify = "verification outcome unknown: " + reason
	} else {
		return nil, errors.New("unmatched verification start has an invalid purpose")
	}
	projected.ExecutionQuiescenceUnconfirmed = true
	projected.ExecutionQuiescenceTaskID = verificationQuiescenceOwner(run)
	event := RunEvent{
		EventID: verificationEventID(run.ID, "recovery-required"), RunID: state.RunID, EpochID: state.ExecutionEpochID,
		OccurredAt: at.UTC(), Actor: "deep-coordinator", Type: RunEventVerificationRecovery,
		FromPhase: state.Phase, ToPhase: state.Phase, Reason: reason, VerificationRun: &run,
		TaskSummary: summarizeRunTasks(projected.Tasks),
	}
	if err := appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked); err != nil {
		return nil, err
	}
	return &run, nil
}

// LoadVerificationRun returns the latest durable representation of one
// verification invocation.
func LoadVerificationRun(stateDir, sessionID, id string) (VerificationRun, bool, error) {
	events, err := ReadRunEvents(stateDir, sessionID)
	if err != nil {
		return VerificationRun{}, false, err
	}
	var found VerificationRun
	ok := false
	for _, event := range events {
		if event.VerificationRun != nil && event.VerificationRun.ID == id {
			found, ok = *event.VerificationRun, true
		}
	}
	return found, ok, nil
}

// PersistVerificationOutput writes only a bounded excerpt to the private
// session artifact directory. The journal stores a relative, validated ref.
func PersistVerificationOutput(stateDir, sessionID, runID string, output []byte) (string, error) {
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
		return "", errors.New("invalid verification artifact run identity")
	}
	if _, err := hex.DecodeString(runID); err != nil {
		return "", errors.New("invalid verification artifact run identity")
	}
	ref := filepath.ToSlash(filepath.Join("artifacts", "verification", runID+".txt"))
	if err := writeAtomic(filepath.Join(DeepDir(stateDir, sessionID), filepath.FromSlash(ref)), output); err != nil {
		return "", fmt.Errorf("persist bounded verification output artifact: %w", err)
	}
	return ref, nil
}

func verificationArtifactOutput(dir string, run VerificationRun) (string, error) {
	if len(run.ArtifactRefs) == 0 {
		return "", nil
	}
	if len(run.ArtifactRefs) != 1 || !validVerificationArtifactRef(run.ID, run.ArtifactRefs[0]) {
		return "", errors.New("verification output artifact references are invalid")
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(run.ArtifactRefs[0])))
	if err != nil {
		return "", fmt.Errorf("read verifier output artifact: %w", err)
	}
	if len(data) > 4096 {
		return "", errors.New("verifier output artifact exceeds its 4096-byte limit")
	}
	if !utf8.Valid(data) {
		return "", errors.New("verifier output artifact is not valid UTF-8")
	}
	return string(data), nil
}

// ReadVerificationOutput loads the bounded compatibility artifact associated
// with a durable verification result. Raw verifier output is kept out of the
// append-only journal and is read only when a projection or resumed caller
// needs the bounded excerpt.
func ReadVerificationOutput(stateDir, sessionID string, run VerificationRun) (string, error) {
	return verificationArtifactOutput(DeepDir(stateDir, sessionID), run)
}

func hydrateVerificationProjection(state *DeepState, event RunEvent, dir string) error {
	if event.Type != RunEventVerificationResult || event.VerificationRun == nil {
		return nil
	}
	run := *event.VerificationRun
	output, err := verificationArtifactOutput(dir, run)
	if err != nil {
		return err
	}
	switch run.Purpose {
	case VerificationPurposeTask:
		task, ok := findTask(state, run.TaskID)
		if !ok || task.VerificationRunID != run.ID {
			return errors.New("cannot hydrate output for an unprojected task verification")
		}
		task.VerificationOutput = VerificationOutputExcerpt(output)
	case VerificationPurposeMissionEnd:
		if state.LandingVerificationRunID != run.ID {
			return errors.New("cannot hydrate output for an unprojected mission verification")
		}
		state.LandingVerify = landingVerificationSummary(run, output)
	default:
		return fmt.Errorf("cannot hydrate output for verification purpose %q", run.Purpose)
	}
	return nil
}

func verificationEventID(id, action string) string { return "verification/" + id + "/" + action }

func verificationQuiescenceOwner(run VerificationRun) string {
	if run.Purpose == VerificationPurposeMissionEnd {
		return "mission-final-verifier"
	}
	return run.TaskID
}

func expectedVerificationCommandIdentity(state DeepState, run VerificationRun) string {
	var command string
	switch run.Purpose {
	case VerificationPurposeMissionEnd:
		command = state.Verify
	case VerificationPurposeTask:
		task, ok := findTask(&state, run.TaskID)
		if !ok {
			return ""
		}
		command = task.Verify
		if command == "" {
			command = state.Verify
		}
	}
	return VerificationCommandIdentity(command)
}

func applyVerificationStarted(state *DeepState, run VerificationRun) error {
	switch run.Purpose {
	case VerificationPurposeTask:
		task, ok := findTask(state, run.TaskID)
		if !ok || task.Attempts != run.Attempt || task.Status != StatusActive {
			return errors.New("verification-start event does not match an active task attempt")
		}
		task.VerificationRunID = run.ID
		task.VerificationOutcome = VerificationStarted
		task.VerificationCommand = verificationCommandForTask(state, *task)
		task.VerificationResult = ""
		task.VerificationOutput = ""
		task.VerificationSubject = &run.Subject
		task.VerificationBookkeeping = cloneStringMap(run.BookkeepingBefore)
	case VerificationPurposeMissionEnd:
		if state.Phase != PhaseLanding {
			return errors.New("mission-final verification-start event is outside landing")
		}
		state.LandingVerificationRunID = run.ID
		state.LandingVerify = ""
		state.LandingVerifyDone = false
		state.LandingVerificationOutcome = VerificationStarted
		state.LandingVerificationSubject = &run.Subject
		state.LandingVerificationBookkeeping = cloneStringMap(run.BookkeepingBefore)
	default:
		return fmt.Errorf("unknown verification purpose %q", run.Purpose)
	}
	return nil
}

func applyVerificationResult(state *DeepState, run VerificationRun) error {
	switch run.Purpose {
	case VerificationPurposeTask:
		task, ok := findTask(state, run.TaskID)
		if !ok || task.VerificationRunID != run.ID || task.Attempts != run.Attempt {
			return errors.New("verification result does not match the projected task attempt")
		}
		task.VerificationOutcome = run.Outcome
		task.VerificationResult = verificationRunSummary(run)
		task.VerificationOutput = ""
		task.VerificationSubject = &run.Subject
		task.VerificationBookkeeping = cloneStringMap(run.BookkeepingBefore)
		if run.QuiescenceUnconfirmed {
			task.Status = StatusNeedsHuman
			task.Blocker = "verifier process quiescence is unconfirmed; checkpointing and further work are stopped"
			task.ExecutorRunProcessed = true
			state.ExecutionQuiescenceUnconfirmed = true
			state.ExecutionQuiescenceTaskID = task.ID
		}
	case VerificationPurposeMissionEnd:
		if state.LandingVerificationRunID != run.ID {
			return errors.New("mission-final verification result does not match the projected run identity")
		}
		state.LandingVerify = landingVerificationSummary(run, "")
		state.LandingVerifyDone = true
		state.LandingVerificationOutcome = run.Outcome
		state.LandingVerificationSubject = &run.Subject
		state.LandingVerificationBookkeeping = cloneStringMap(run.BookkeepingBefore)
		if run.QuiescenceUnconfirmed {
			state.ExecutionQuiescenceUnconfirmed = true
			state.ExecutionQuiescenceTaskID = "mission-final-verifier"
		}
	default:
		return fmt.Errorf("unknown verification purpose %q", run.Purpose)
	}
	return nil
}

func verificationCommandForTask(state *DeepState, task Task) string {
	if task.Verify != "" {
		return task.Verify
	}
	return state.Verify
}

// VerificationOutputExcerpt is the bounded task-summary output tail projected
// from a durable verifier record.
func VerificationOutputExcerpt(output string) string {
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

func verificationRunSummary(run VerificationRun) string {
	switch run.Outcome {
	case VerificationPassed:
		return "repository verification passed"
	case VerificationFailed:
		if run.HasExitCode {
			return fmt.Sprintf("repository verification failed (exit %d)", run.ExitCode)
		}
		return "repository verification failed"
	case VerificationTimedOut:
		return "repository verification timed out"
	case VerificationInvalid:
		return "repository verification command invalid: " + run.Error
	case VerificationCanceled:
		return "repository verification canceled: " + run.Error
	case VerificationExecutionErr:
		return "repository verification execution error: " + run.Error
	default:
		return "repository verification outcome unknown"
	}
}

func landingVerificationSummary(run VerificationRun, output string) string {
	if run.Outcome == VerificationPassed {
		return "passed"
	}
	final := "FAILED (" + string(run.Outcome) + ")"
	if run.HasExitCode {
		final += fmt.Sprintf(" (exit %d)", run.ExitCode)
	}
	if output = strings.TrimSpace(output); output != "" {
		final += "\n" + output
	}
	if run.Error != "" {
		final += "\nerror: " + run.Error
	}
	return final
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	copy := make(map[string]string, len(input))
	for key, value := range input {
		copy[key] = value
	}
	return copy
}

func validateVerificationRun(run VerificationRun, starting bool) error {
	if run.ID == "" || len(run.ID) != 32 || run.StartEventID != verificationEventID(run.ID, "started") ||
		run.StartedInEpochID == "" || run.StartedAt.IsZero() || run.CommandSHA256 == "" ||
		len(run.CommandSHA256) != 64 || run.Subject.HeadCommit == "" || run.Subject.TreeSHA == "" ||
		len(run.Subject.HeadCommit) > 128 || len(run.Subject.TreeSHA) > 128 {
		return errors.New("verification run identity, timestamp, command, or repository subject is invalid")
	}
	if _, err := hex.DecodeString(run.ID); err != nil {
		return errors.New("verification run identity is not hexadecimal")
	}
	if _, err := hex.DecodeString(run.CommandSHA256); err != nil {
		return errors.New("verification command identity is not hexadecimal")
	}
	if run.Purpose == VerificationPurposeTask {
		if run.TaskID == "" || len(run.TaskID) > 128 || run.Attempt < 1 || run.Attempt > 1_000_000 {
			return errors.New("task verification identity is invalid")
		}
	} else if run.Purpose == VerificationPurposeMissionEnd {
		if run.TaskID != "" || run.Attempt != 0 {
			return errors.New("mission-final verification contains task identity")
		}
	} else {
		return errors.New("verification run has an invalid purpose")
	}
	if run.CommandSource != "task" && run.CommandSource != "mission" {
		return errors.New("verification run has an invalid command source")
	}
	if run.Purpose == VerificationPurposeMissionEnd && run.CommandSource != "mission" {
		return errors.New("mission-final verification must use the mission command")
	}
	for _, value := range []string{run.Runtime.Worker, run.Runtime.Location, run.Runtime.Shell, run.Runtime.Protocol, run.Runtime.ComputeProvider} {
		if len(value) > 128 || strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("verification runtime identity is invalid")
		}
	}
	if run.Runtime.Location == "" || run.Runtime.Shell == "" || run.Runtime.Protocol == "" {
		return errors.New("verification runtime identity is incomplete")
	}
	if run.Runtime.ComputeInstance < 0 || (run.Runtime.ComputeProvider == "") != (run.Runtime.ComputeInstance == 0) {
		return errors.New("verification compute identity is incomplete")
	}
	if run.TimeoutSeconds < 0 || run.TimeoutSeconds > 7*24*60*60 || run.RemainingDeadlineSeconds < 0 || run.RemainingDeadlineSeconds > 7*24*60*60 {
		return errors.New("verification timeout context is invalid")
	}
	if err := validateVerificationBookkeeping(run.BookkeepingBefore); err != nil {
		return fmt.Errorf("invalid initial verifier bookkeeping identity: %w", err)
	}
	if starting {
		if run.Outcome != VerificationStarted || !run.EndedAt.IsZero() || run.HasExitCode || run.ExitCode != 0 || run.Error != "" ||
			run.QuiescenceUnconfirmed || run.SubjectAfter != nil || run.SubjectAfterError != "" || len(run.BookkeepingAfter) > 0 ||
			run.DurationMilliseconds != 0 || len(run.ArtifactRefs) > 0 {
			return errors.New("verification start contains result-only facts")
		}
		return nil
	}
	if run.Outcome == VerificationStarted || run.Outcome == VerificationUnknown || run.Outcome == VerificationNotRun {
		return fmt.Errorf("verification result has a non-terminal outcome %q", run.Outcome)
	}
	if run.EndedAt.IsZero() || run.EndedAt.Before(run.StartedAt) || run.DurationMilliseconds < 0 ||
		run.DurationMilliseconds > int64((7*24*time.Hour)/time.Millisecond) {
		return errors.New("verification result timestamps or duration are invalid")
	}
	if len(run.Error) > maxRunEventTextBytes || len(run.SubjectAfterError) > maxRunEventTextBytes {
		return errors.New("verification result text exceeds limits")
	}
	if run.SubjectAfter != nil && (run.SubjectAfter.HeadCommit == "" || run.SubjectAfter.TreeSHA == "" ||
		len(run.SubjectAfter.HeadCommit) > 128 || len(run.SubjectAfter.TreeSHA) > 128) {
		return errors.New("verification post-run repository subject is invalid")
	}
	if (run.SubjectAfter == nil) == (run.SubjectAfterError == "") && !run.QuiescenceUnconfirmed {
		return errors.New("verification result must record either a post-run subject or why it is unavailable")
	}
	if run.QuiescenceUnconfirmed && run.SubjectAfter == nil && run.SubjectAfterError == "" {
		return errors.New("unconfirmed verifier quiescence requires an unavailable post-run subject reason")
	}
	if err := validateVerificationBookkeeping(run.BookkeepingAfter); err != nil {
		return fmt.Errorf("invalid post-run verifier bookkeeping identity: %w", err)
	}
	if len(run.ArtifactRefs) > maxVerificationArtifacts {
		return errors.New("verification result has too many artifact references")
	}
	for _, ref := range run.ArtifactRefs {
		if !validVerificationArtifactRef(run.ID, ref) {
			return errors.New("verification result has an invalid artifact reference")
		}
	}
	if run.Outcome == VerificationPassed && (!run.HasExitCode || run.ExitCode != 0 || run.Error != "") {
		return errors.New("passed verification outcome disagrees with exit facts")
	}
	if run.Outcome == VerificationFailed && (!run.HasExitCode || run.ExitCode == 0) {
		return errors.New("failed verification outcome requires a nonzero exit code")
	}
	if run.QuiescenceUnconfirmed && run.Outcome != VerificationExecutionErr && run.Outcome != VerificationTimedOut && run.Outcome != VerificationCanceled {
		return errors.New("unconfirmed verifier quiescence has an incompatible outcome")
	}
	return nil
}

func validateVerificationBookkeeping(values map[string]string) error {
	if len(values) > 16 {
		return errors.New("too many bookkeeping identities")
	}
	for path, identity := range values {
		if path == "" || len(path) > 512 || identity == "" || len(identity) > 256 ||
			strings.ContainsAny(path, "\x00\r\n") || strings.ContainsAny(identity, "\x00\r\n") {
			return errors.New("bookkeeping identity is empty or exceeds limits")
		}
	}
	return nil
}

func validVerificationArtifactRef(id, ref string) bool {
	return ref == filepath.ToSlash(filepath.Join("artifacts", "verification", id+".txt")) &&
		!strings.ContainsAny(ref, "\x00\r\n")
}

func validateVerificationRecovery(run VerificationRun) error {
	if run.Outcome != VerificationUnknown || !run.EndedAt.IsZero() || run.HasExitCode || run.ExitCode != 0 || run.Error != "" ||
		run.QuiescenceUnconfirmed || run.SubjectAfter != nil || run.SubjectAfterError != "" || len(run.BookkeepingAfter) > 0 ||
		run.DurationMilliseconds != 0 || len(run.ArtifactRefs) > 0 {
		return errors.New("verification recovery contains unobserved result facts")
	}
	started := run
	started.Outcome = VerificationStarted
	return validateVerificationRun(started, true)
}
