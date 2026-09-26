package deep

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	RunEventSchemaVersion = 1
	maxRunEventLineBytes  = 16 * 1024
	maxRunEventIDBytes    = 256
	maxRunEventTextBytes  = 512
	runEventFileName      = "run-events.jsonl"
	runEventLockName      = "run-events.lock"
	runEventTailName      = "run-events.incomplete-tail"
)

type RunEventType string

const (
	RunEventEpochStarted   RunEventType = "run.epoch_started"
	RunEventLandingStarted RunEventType = "run.landing_started"
	RunEventLanded         RunEventType = "run.landed"
)

type RunEventBoundary string

const (
	RunEventBoundaryNewRun       RunEventBoundary = "new_run"
	RunEventBoundaryResume       RunEventBoundary = "resume"
	RunEventBoundaryLegacyResume RunEventBoundary = "legacy_resume"
)

// RunTaskSummary preserves bounded task-state context at lifecycle boundaries
// without copying objectives, prompts, or attempt output into the journal.
type RunTaskSummary struct {
	Total      int `json:"total"`
	Queued     int `json:"queued"`
	Active     int `json:"active"`
	Verified   int `json:"verified"`
	Incomplete int `json:"incomplete"`
	Blocked    int `json:"blocked"`
	NeedsHuman int `json:"needsHuman"`
	Dropped    int `json:"dropped"`
	Other      int `json:"other"`
}

// RunEvent is a bounded, versioned fact in one Deep Work run's append-only
// history. B1 records run/epoch and landing lifecycle transitions; executor
// and verifier facts are added by the later execution-record layer.
type RunEvent struct {
	SchemaVersion    int              `json:"schemaVersion"`
	EventID          string           `json:"eventId"`
	RunID            string           `json:"runId"`
	EpochID          string           `json:"epochId"`
	Sequence         uint64           `json:"sequence"`
	OccurredAt       time.Time        `json:"occurredAt"`
	Actor            string           `json:"actor"`
	Type             RunEventType     `json:"type"`
	Boundary         RunEventBoundary `json:"boundary,omitempty"`
	FromPhase        Phase            `json:"fromPhase,omitempty"`
	ToPhase          Phase            `json:"toPhase,omitempty"`
	Reason           string           `json:"reason,omitempty"`
	Deadline         time.Time        `json:"deadline,omitempty"`
	LandBefore       time.Time        `json:"landBefore,omitempty"`
	ComputeProvider  string           `json:"computeProvider,omitempty"`
	ComputeInstance  int64            `json:"computeInstanceId,omitempty"`
	TaskSummary      *RunTaskSummary  `json:"taskSummary,omitempty"`
	CheckpointCommit string           `json:"checkpointCommit,omitempty"`
	CheckpointTree   string           `json:"checkpointTreeSha,omitempty"`
}

// NewExecutionEpochID returns a random opaque identity for one execution
// period. It is generated before the epoch event is appended and then
// recovered from that event if projection persistence is interrupted.
func NewExecutionEpochID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate execution epoch identity: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// BeginNewRun writes a non-executing initialization projection, then records
// the first epoch before exposing the executing phase. Retrying an interrupted
// startup reuses the prepared IDs and deduplicates the same event.
func BeginNewRun(stateDir string, state *DeepState, at time.Time) error {
	if state == nil || state.SessionID == "" {
		return errors.New("new Deep Work run requires a session identity")
	}
	if state.RunID == "" {
		prepared := *state
		prepared.RunID = prepared.SessionID
		epoch, err := NewExecutionEpochID()
		if err != nil {
			return err
		}
		prepared.ExecutionEpochID = epoch
		prepared.RunEventSchemaVersion = RunEventSchemaVersion
		prepared.RunEventWatermark = 0
		prepared.Phase = PhaseInitializing
		err = withRunStateLock(stateDir, prepared.SessionID, func(dir string) error {
			existing, err := readStateFileIfPresent(dir, prepared.SessionID)
			if err != nil {
				return err
			}
			if existing != nil {
				return errors.New("session state already exists; use BeginResumeEpoch for an existing run")
			}
			if _, err := os.Stat(filepath.Join(dir, runEventFileName)); err == nil {
				return errors.New("orphaned run event journal exists for new session identity")
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("inspect existing run event journal: %w", err)
			}
			return writeProjectionLocked(stateDir, prepared)
		})
		if err != nil {
			return fmt.Errorf("persist run initialization boundary: %w", err)
		}
		*state = prepared
	}
	if state.RunID != state.SessionID || state.ExecutionEpochID == "" || state.RunEventSchemaVersion != RunEventSchemaVersion {
		return errors.New("prepared Deep Work run has invalid journal identity")
	}
	if state.Phase == PhaseExecuting && state.RunEventWatermark > 0 {
		return nil
	}
	if state.Phase != PhaseInitializing || state.RunEventWatermark != 0 {
		return fmt.Errorf("cannot start journaled run from phase %q at event %d", state.Phase, state.RunEventWatermark)
	}
	if at.IsZero() {
		at = state.StartedAt
	}
	event := RunEvent{
		EventID:     epochStartedEventID(state.RunID, state.ExecutionEpochID),
		RunID:       state.RunID,
		EpochID:     state.ExecutionEpochID,
		OccurredAt:  at.UTC(),
		Actor:       "deep-coordinator",
		Type:        RunEventEpochStarted,
		Boundary:    RunEventBoundaryNewRun,
		FromPhase:   PhaseInitializing,
		ToPhase:     PhaseExecuting,
		Deadline:    state.Deadline.UTC(),
		LandBefore:  state.LandBefore.UTC(),
		TaskSummary: summarizeRunTasks(state.Tasks),
	}
	setRunEventComputeIdentity(&event, state.ComputeBinding)
	return appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked)
}

// BeginResumeEpoch starts one new execution epoch while retaining the same
// run identity and run-wide event sequence. A legacy state begins at an
// explicit compatibility boundary; no events are invented for its past.
func BeginResumeEpoch(stateDir string, state *DeepState, fromPhase Phase, at time.Time) error {
	if state == nil || state.SessionID == "" {
		return errors.New("resuming a Deep Work run requires a session identity")
	}
	if fromPhase == PhaseInitializing {
		return BeginNewRun(stateDir, state, at)
	}
	if state.Phase != fromPhase {
		return fmt.Errorf("resume phase changed before epoch start: loaded %q, current %q", fromPhase, state.Phase)
	}
	runID := state.RunID
	boundary := RunEventBoundaryResume
	if runID == "" {
		runID = state.SessionID
		boundary = RunEventBoundaryLegacyResume
	}
	epoch, err := NewExecutionEpochID()
	if err != nil {
		return err
	}
	toPhase := PhaseExecuting
	reason := ""
	if fromPhase == PhaseLanding {
		toPhase = PhaseLanding
		reason = strings.TrimSpace(state.LandingReason)
		if reason == "" {
			reason = "resumed interrupted landing"
		}
	}
	event := RunEvent{
		EventID:     epochStartedEventID(runID, epoch),
		RunID:       runID,
		EpochID:     epoch,
		OccurredAt:  at.UTC(),
		Actor:       "deep-coordinator",
		Type:        RunEventEpochStarted,
		Boundary:    boundary,
		FromPhase:   fromPhase,
		ToPhase:     toPhase,
		Reason:      reason,
		Deadline:    state.Deadline.UTC(),
		LandBefore:  state.LandBefore.UTC(),
		TaskSummary: summarizeRunTasks(state.Tasks),
	}
	setRunEventComputeIdentity(&event, state.ComputeBinding)
	return appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked)
}

func setRunEventComputeIdentity(event *RunEvent, binding *ComputeBinding) {
	if binding == nil {
		return
	}
	event.ComputeProvider = binding.Provider
	event.ComputeInstance = binding.InstanceID
}

func summarizeRunTasks(tasks []Task) *RunTaskSummary {
	summary := &RunTaskSummary{Total: len(tasks)}
	for _, task := range tasks {
		switch task.Status {
		case StatusQueued:
			summary.Queued++
		case StatusActive:
			summary.Active++
		case StatusVerified:
			summary.Verified++
		case StatusIncomplete:
			summary.Incomplete++
		case StatusBlocked:
			summary.Blocked++
		case StatusNeedsHuman:
			summary.NeedsHuman++
		case StatusDropped:
			summary.Dropped++
		default:
			summary.Other++
		}
	}
	return summary
}

// BeginLanding records the landing boundary before landing work proceeds.
func BeginLanding(stateDir string, state *DeepState, reason string, at time.Time) error {
	if state == nil || state.RunID == "" || state.ExecutionEpochID == "" {
		return errors.New("landing transition requires a journaled run epoch")
	}
	if state.Phase != PhaseExecuting {
		return fmt.Errorf("cannot begin landing from phase %q", state.Phase)
	}
	if strings.TrimSpace(reason) == "" || len(reason) > maxRunEventTextBytes {
		return errors.New("landing reason is empty or exceeds the journal limit")
	}
	event := RunEvent{
		EventID:     landingEventID(state.RunID, state.ExecutionEpochID, "started"),
		RunID:       state.RunID,
		EpochID:     state.ExecutionEpochID,
		OccurredAt:  at.UTC(),
		Actor:       "deep-coordinator",
		Type:        RunEventLandingStarted,
		FromPhase:   PhaseExecuting,
		ToPhase:     PhaseLanding,
		Reason:      reason,
		TaskSummary: summarizeRunTasks(state.Tasks),
	}
	return appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked)
}

// CompleteLanding records the operational landing only after the checkpoint
// side effect has been established. The event ID is stable within an epoch,
// and checkpointSubject reuses an already-created matching Git commit.
func CompleteLanding(stateDir string, state *DeepState, checkpointCommit, checkpointTree string, at time.Time) error {
	if state == nil || state.RunID == "" || state.ExecutionEpochID == "" {
		return errors.New("landing completion requires a journaled run epoch")
	}
	if state.Phase != PhaseLanding {
		return fmt.Errorf("cannot complete landing from phase %q", state.Phase)
	}
	if strings.TrimSpace(checkpointCommit) == "" || strings.TrimSpace(checkpointTree) == "" {
		return errors.New("landing event requires checkpoint commit and tree identities")
	}
	event := RunEvent{
		EventID:          landingEventID(state.RunID, state.ExecutionEpochID, "completed"),
		RunID:            state.RunID,
		EpochID:          state.ExecutionEpochID,
		OccurredAt:       at.UTC(),
		Actor:            "deep-coordinator",
		Type:             RunEventLanded,
		FromPhase:        PhaseLanding,
		ToPhase:          PhaseLanded,
		Reason:           state.LandingReason,
		TaskSummary:      summarizeRunTasks(state.Tasks),
		CheckpointCommit: strings.TrimSpace(checkpointCommit),
		CheckpointTree:   strings.TrimSpace(checkpointTree),
	}
	return appendAndProjectRunEvent(stateDir, state, event, writeProjectionLocked)
}

func epochStartedEventID(runID, epochID string) string {
	return runID + "/" + epochID + "/epoch-started"
}

func landingEventID(runID, epochID, action string) string {
	return runID + "/" + epochID + "/landing-" + action
}

type projectionWriter func(string, DeepState) error

func appendAndProjectRunEvent(stateDir string, state *DeepState, event RunEvent, project projectionWriter) error {
	if project == nil {
		project = writeProjectionLocked
	}
	return withRunStateLock(stateDir, state.SessionID, func(dir string) error {
		current, err := readStateFile(dir, state.SessionID)
		if err != nil {
			return err
		}
		current, _, err = recoverProjectionLocked(stateDir, dir, current, true)
		if err != nil {
			return err
		}
		if state.RunEventWatermark != current.RunEventWatermark || state.RunID != current.RunID ||
			state.ExecutionEpochID != current.ExecutionEpochID || state.RunEventSchemaVersion != current.RunEventSchemaVersion ||
			!sameLifecycleProjection(*state, current) {
			return errors.New("stale Deep Work projection; reload before writing a run event")
		}
		events, _, err := readRunEventsLocked(dir, state.SessionID)
		if err != nil {
			return err
		}
		if uint64(len(events)) != current.RunEventWatermark {
			return fmt.Errorf("projection watermark %d does not match event history %d", current.RunEventWatermark, len(events))
		}
		if event.Type == RunEventEpochStarted && event.Boundary != RunEventBoundaryNewRun {
			// Resume may update bounded runtime, compute, deadline, and task
			// configuration before beginning the new epoch. Persist that
			// pre-transition projection first so replaying the epoch event after
			// a crash cannot lose those facts. The lifecycle fields were checked
			// against current above, and the lock remains held across both writes.
			if !missionOutcomeProjectionValid(*state) {
				return errors.New("resume context has an invalid mission outcome projection")
			}
			prepared := *state
			// Deadline fields belong to the epoch event itself. Keep the prior
			// values at the old watermark; replay installs the new values with
			// the new epoch event.
			prepared.Deadline = current.Deadline
			prepared.LandBefore = current.LandBefore
			if err := project(stateDir, prepared); err != nil {
				return fmt.Errorf("persist resume context before run epoch event: %w", err)
			}
		}
		event.SchemaVersion = RunEventSchemaVersion
		event.Sequence = uint64(len(events)) + 1
		if err := validateRunEvent(event); err != nil {
			return fmt.Errorf("invalid run event: %w", err)
		}
		if err := validateEventTransition(events, event, state.SessionID); err != nil {
			return err
		}
		if err := appendRunEventLocked(dir, event); err != nil {
			return err
		}
		projected := *state
		projected.PreviousLandings = append([]LandingRecord(nil), state.PreviousLandings...)
		if err := applyRunEvent(&projected, event); err != nil {
			return fmt.Errorf("apply persisted run event %s: %w", event.EventID, err)
		}
		if err := project(stateDir, projected); err != nil {
			return fmt.Errorf("run event %s is durable but deep.json projection update failed: %w", event.EventID, err)
		}
		*state = projected
		return nil
	})
}

func withRunStateLock(stateDir, sessionID string, fn func(string) error) error {
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("Deep Work state requires a session identity")
	}
	dir := DeepDir(stateDir, sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create Deep Work state directory: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(dir, runEventLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open Deep Work state lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock Deep Work state: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return fn(dir)
}

func readRunEventsLocked(dir, sessionID string) ([]RunEvent, bool, error) {
	path := filepath.Join(dir, runEventFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read run event journal: %w", err)
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lastLineEnd := bytes.LastIndexByte(data, '\n') + 1
		tail := data[lastLineEnd:]
		if len(tail) > maxRunEventLineBytes {
			return nil, true, fmt.Errorf("incomplete run event tail exceeds %d bytes", maxRunEventLineBytes)
		}
		if err := writeAtomic(filepath.Join(dir, runEventTailName), tail); err != nil {
			return nil, true, fmt.Errorf("quarantine incomplete run event tail: %w", err)
		}
		f, err := os.OpenFile(path, os.O_WRONLY, 0o600)
		if err != nil {
			return nil, true, fmt.Errorf("open journal to truncate incomplete tail: %w", err)
		}
		truncateErr := f.Truncate(int64(lastLineEnd))
		syncErr := f.Sync()
		closeErr := f.Close()
		if truncateErr != nil {
			return nil, true, fmt.Errorf("truncate incomplete run event tail: %w", truncateErr)
		}
		if syncErr != nil {
			return nil, true, fmt.Errorf("sync truncated run event journal: %w", syncErr)
		}
		if closeErr != nil {
			return nil, true, fmt.Errorf("close truncated run event journal: %w", closeErr)
		}
		if err := syncDirectory(dir); err != nil {
			return nil, true, fmt.Errorf("sync journal directory after tail recovery: %w", err)
		}
		data = data[:lastLineEnd]
	}
	if len(data) == 0 {
		return nil, true, nil
	}
	lines := bytes.Split(data[:len(data)-1], []byte{'\n'})
	events := make([]RunEvent, 0, len(lines))
	for i, line := range lines {
		if len(line) == 0 || len(line) > maxRunEventLineBytes {
			return nil, true, fmt.Errorf("invalid complete run event line %d", i+1)
		}
		var event RunEvent
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&event); err != nil {
			return nil, true, fmt.Errorf("decode complete run event line %d: %w", i+1, err)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return nil, true, fmt.Errorf("complete run event line %d contains trailing data", i+1)
		}
		if err := validateRunEvent(event); err != nil {
			return nil, true, fmt.Errorf("invalid complete run event line %d: %w", i+1, err)
		}
		if err := validateEventTransition(events, event, sessionID); err != nil {
			return nil, true, fmt.Errorf("invalid run event history at line %d: %w", i+1, err)
		}
		events = append(events, event)
	}
	return events, true, nil
}

func validateRunEvent(event RunEvent) error {
	if event.SchemaVersion != RunEventSchemaVersion {
		return fmt.Errorf("unsupported schema version %d", event.SchemaVersion)
	}
	if event.EventID == "" || len(event.EventID) > maxRunEventIDBytes || strings.ContainsAny(event.EventID, "\x00\r\n") {
		return errors.New("event identity is empty or invalid")
	}
	if event.RunID == "" || len(event.RunID) > 128 || strings.ContainsAny(event.RunID, "\x00\r\n") {
		return errors.New("run identity is empty or invalid")
	}
	if event.EpochID == "" || len(event.EpochID) > 128 || strings.ContainsAny(event.EpochID, "\x00\r\n") {
		return errors.New("epoch identity is empty or invalid")
	}
	if event.Sequence == 0 || event.OccurredAt.IsZero() {
		return errors.New("event sequence and timestamp are required")
	}
	if event.Actor == "" || len(event.Actor) > 64 || strings.ContainsAny(event.Actor, "\x00\r\n") {
		return errors.New("event actor is empty or invalid")
	}
	if len(event.Reason) > maxRunEventTextBytes || strings.ContainsRune(event.Reason, '\x00') {
		return errors.New("event reason exceeds its limit or contains NUL")
	}
	if event.TaskSummary == nil || !validRunTaskSummary(*event.TaskSummary) {
		return errors.New("event task-state summary is missing or invalid")
	}
	switch event.Type {
	case RunEventEpochStarted:
		if event.ToPhase != PhaseExecuting && event.ToPhase != PhaseLanding {
			return errors.New("epoch-start event has invalid target phase")
		}
		if event.Deadline.IsZero() || event.LandBefore.IsZero() {
			return errors.New("epoch-start event requires deadline boundaries")
		}
		if event.ToPhase == PhaseLanding && event.Reason == "" {
			return errors.New("resumed landing epoch requires a reason")
		}
		if event.Boundary != RunEventBoundaryNewRun && event.Boundary != RunEventBoundaryResume && event.Boundary != RunEventBoundaryLegacyResume {
			return errors.New("epoch-start event has invalid boundary")
		}
		if event.Boundary == RunEventBoundaryNewRun {
			if event.FromPhase != PhaseInitializing || event.ToPhase != PhaseExecuting || event.Reason != "" {
				return errors.New("new-run event must start initializing execution without a resume reason")
			}
		} else {
			switch event.FromPhase {
			case PhaseExecuting, PhaseLanding, PhaseLanded, PhaseStopped:
			default:
				return errors.New("resume event has invalid source phase")
			}
			want := PhaseExecuting
			if event.FromPhase == PhaseLanding {
				want = PhaseLanding
			}
			if event.ToPhase != want {
				return errors.New("resume event has invalid target phase")
			}
		}
		if event.ComputeProvider == "" || event.ComputeInstance == 0 {
			if event.ComputeProvider != "" || event.ComputeInstance != 0 {
				return errors.New("epoch event compute identity is incomplete")
			}
		} else if len(event.ComputeProvider) > 64 || strings.ContainsAny(event.ComputeProvider, "\x00\r\n") || event.ComputeInstance < 0 {
			return errors.New("epoch event compute identity is invalid")
		}
	case RunEventLandingStarted:
		if event.FromPhase != PhaseExecuting || event.ToPhase != PhaseLanding || event.Reason == "" {
			return errors.New("landing-start event has invalid phase or reason")
		}
	case RunEventLanded:
		if event.FromPhase != PhaseLanding || event.ToPhase != PhaseLanded || event.CheckpointCommit == "" || event.CheckpointTree == "" ||
			len(event.CheckpointCommit) > 128 || len(event.CheckpointTree) > 128 ||
			strings.ContainsAny(event.CheckpointCommit, "\x00\r\n") || strings.ContainsAny(event.CheckpointTree, "\x00\r\n") {
			return errors.New("landed event has invalid phase or checkpoint identity")
		}
	default:
		return fmt.Errorf("unknown run event type %q", event.Type)
	}
	if event.Type != RunEventEpochStarted && (event.Boundary != "" || !event.Deadline.IsZero() || !event.LandBefore.IsZero() || event.ComputeProvider != "" || event.ComputeInstance != 0) {
		return errors.New("non-epoch event contains epoch-only context")
	}
	if event.Type != RunEventLanded && (event.CheckpointCommit != "" || event.CheckpointTree != "") {
		return errors.New("non-landed event contains checkpoint identity")
	}
	return nil
}

func validRunTaskSummary(summary RunTaskSummary) bool {
	const maxRunTaskCount = 1_000_000
	counts := []int{summary.Total, summary.Queued, summary.Active, summary.Verified, summary.Incomplete,
		summary.Blocked, summary.NeedsHuman, summary.Dropped, summary.Other}
	var sum int
	for _, count := range counts {
		if count < 0 || count > maxRunTaskCount {
			return false
		}
	}
	for _, count := range counts[1:] {
		sum += count
	}
	return summary.Total <= maxRunTaskCount && sum == summary.Total
}

func validateEventTransition(prior []RunEvent, event RunEvent, sessionID string) error {
	if event.RunID != sessionID {
		return fmt.Errorf("event run %q does not match session %q", event.RunID, sessionID)
	}
	if event.Sequence != uint64(len(prior))+1 {
		return fmt.Errorf("event sequence %d follows %d", event.Sequence, len(prior))
	}
	for _, old := range prior {
		if old.EventID == event.EventID {
			return fmt.Errorf("duplicate event identity %q", event.EventID)
		}
	}
	if len(prior) == 0 {
		if event.Type != RunEventEpochStarted || (event.Boundary != RunEventBoundaryNewRun && event.Boundary != RunEventBoundaryLegacyResume) {
			return errors.New("first journal event must begin a new or legacy-resume epoch")
		}
		if event.Boundary == RunEventBoundaryLegacyResume {
			want := PhaseExecuting
			if event.FromPhase == PhaseLanding {
				want = PhaseLanding
			}
			if event.ToPhase != want {
				return errors.New("legacy-resume event has an invalid target phase")
			}
		}
		return nil
	}
	last := prior[len(prior)-1]
	phase := last.ToPhase
	if event.Type == RunEventEpochStarted {
		if event.EpochID == last.EpochID || event.Boundary != RunEventBoundaryResume || event.FromPhase != phase {
			return errors.New("subsequent epoch event must use a new epoch identity and resume boundary")
		}
		want := PhaseExecuting
		if phase == PhaseLanding {
			want = PhaseLanding
		}
		if event.ToPhase != want {
			return errors.New("resume event has an invalid target phase")
		}
		return nil
	}
	if event.EpochID != last.EpochID {
		return errors.New("lifecycle event epoch does not match current run epoch")
	}
	if last.Type == RunEventLanded {
		return errors.New("lifecycle event follows terminal landing without a new epoch")
	}
	if event.FromPhase != phase {
		return fmt.Errorf("event %q expects phase %q after prior event phase %q", event.Type, event.FromPhase, phase)
	}
	if event.Type == RunEventLandingStarted && phase != PhaseExecuting {
		return errors.New("landing-start event does not follow an executing epoch")
	}
	if event.Type == RunEventLanded && phase != PhaseLanding {
		return errors.New("landed event does not follow a landing-start event")
	}
	return nil
}

func appendRunEventLocked(dir string, event RunEvent) error {
	path := filepath.Join(dir, runEventFileName)
	info, statErr := os.Stat(path)
	created := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !created {
		return fmt.Errorf("inspect run event journal: %w", statErr)
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode run event: %w", err)
	}
	if len(data) > maxRunEventLineBytes {
		return fmt.Errorf("run event is %d bytes; maximum is %d", len(data), maxRunEventLineBytes)
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open run event journal: %w", err)
	}
	if info == nil {
		if err := f.Chmod(0o600); err != nil {
			_ = f.Close()
			return fmt.Errorf("secure run event journal: %w", err)
		}
	}
	n, writeErr := f.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("append durable run event: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close run event journal: %w", closeErr)
	}
	if created {
		if err := syncDirectory(dir); err != nil {
			return fmt.Errorf("sync run event directory: %w", err)
		}
	}
	return nil
}

func readStateFile(dir, sessionID string) (DeepState, error) {
	data, err := os.ReadFile(filepath.Join(dir, "deep.json"))
	if err != nil {
		return DeepState{}, fmt.Errorf("read deep state %s: %w", sessionID, err)
	}
	var state DeepState
	if err := unmarshal(data, &state); err != nil {
		return DeepState{}, err
	}
	if state.SessionID != sessionID {
		return DeepState{}, errors.New("deep state session id mismatch")
	}
	return state, nil
}

func recoverProjectionLocked(stateDir, dir string, state DeepState, persist bool) (DeepState, bool, error) {
	events, journalExists, err := readRunEventsLocked(dir, state.SessionID)
	if err != nil {
		return DeepState{}, journalExists, err
	}
	if state.RunEventSchemaVersion > RunEventSchemaVersion {
		return DeepState{}, journalExists, fmt.Errorf("unsupported run event schema version %d", state.RunEventSchemaVersion)
	}
	if state.RunEventSchemaVersion != 0 && !missionOutcomeProjectionValid(state) {
		return DeepState{}, journalExists, errors.New("deep.json mission outcome disagrees with its current deterministic evidence")
	}
	if state.RunEventWatermark > uint64(len(events)) {
		return DeepState{}, journalExists, fmt.Errorf("deep.json event watermark %d is ahead of durable journal sequence %d", state.RunEventWatermark, len(events))
	}
	if len(events) == 0 {
		if state.RunEventWatermark != 0 {
			return DeepState{}, journalExists, errors.New("deep.json claims run events but the journal has no complete history")
		}
		if state.RunEventSchemaVersion != 0 && state.Phase != PhaseInitializing {
			return DeepState{}, journalExists, errors.New("journal-enabled run has no durable epoch-start event")
		}
		if state.RunEventSchemaVersion != 0 && (state.RunID != state.SessionID || state.ExecutionEpochID == "") {
			return DeepState{}, journalExists, errors.New("prepared journal run has invalid run or epoch identity")
		}
		if state.RunEventSchemaVersion == 0 && (state.RunID != "" || state.ExecutionEpochID != "") {
			return DeepState{}, journalExists, errors.New("legacy projection contains journal identities without a journal event")
		}
		return state, journalExists, nil
	}
	if state.RunEventSchemaVersion == 0 {
		if state.RunEventWatermark != 0 || state.RunID != "" || state.ExecutionEpochID != "" || events[0].Boundary != RunEventBoundaryLegacyResume {
			return DeepState{}, journalExists, errors.New("legacy state can enter a journal only at an explicit legacy-resume boundary")
		}
	} else if state.RunEventSchemaVersion != RunEventSchemaVersion {
		return DeepState{}, journalExists, fmt.Errorf("unsupported run event schema version %d", state.RunEventSchemaVersion)
	}
	if state.RunEventWatermark > 0 {
		if err := validateProjectionAtWatermark(state, events[state.RunEventWatermark-1]); err != nil {
			return DeepState{}, journalExists, err
		}
	}
	changed := false
	for i := state.RunEventWatermark; i < uint64(len(events)); i++ {
		if err := applyRunEvent(&state, events[i]); err != nil {
			return DeepState{}, journalExists, fmt.Errorf("replay run event %d: %w", i+1, err)
		}
		changed = true
	}
	if state.RunEventSchemaVersion != RunEventSchemaVersion || state.RunEventWatermark != uint64(len(events)) {
		return DeepState{}, journalExists, errors.New("run event replay did not reach the journal watermark")
	}
	if state.RunID != state.SessionID || state.ExecutionEpochID != events[len(events)-1].EpochID {
		return DeepState{}, journalExists, errors.New("deep.json run or epoch identity differs from journal history")
	}
	if changed && persist {
		if err := writeProjectionLocked(stateDir, state); err != nil {
			return DeepState{}, journalExists, fmt.Errorf("replay run event journal into deep.json: %w", err)
		}
	}
	return state, journalExists, nil
}

func validateProjectionAtWatermark(state DeepState, event RunEvent) error {
	if state.RunEventSchemaVersion != RunEventSchemaVersion || state.RunID != event.RunID || state.ExecutionEpochID != event.EpochID {
		return errors.New("deep.json run identity, epoch, or journal schema differs from its watermark event")
	}
	switch event.Type {
	case RunEventEpochStarted:
		if state.Phase != event.ToPhase || state.Deadline != event.Deadline || state.LandBefore != event.LandBefore {
			return fmt.Errorf("deep.json phase %q disagrees with epoch event phase %q", state.Phase, event.ToPhase)
		}
		if event.ToPhase == PhaseLanding && state.LandingReason != event.Reason {
			return errors.New("deep.json resumed landing reason disagrees with its epoch event")
		}
		if event.FromPhase == PhaseLanded && (state.LandingReason != "" || state.LandingCommit != "" ||
			state.LandingCheckpointTreeSHA != "" || state.MissionOutcome != MissionOutcomePending ||
			state.LandedAt != nil || state.LandingHandoff != "") {
			return errors.New("deep.json did not clear the prior landing when it advanced to a new epoch")
		}
	case RunEventLandingStarted:
		if state.Phase != PhaseLanding || state.LandingReason != event.Reason {
			return errors.New("deep.json landing transition disagrees with its watermark event")
		}
	case RunEventLanded:
		if state.Phase != PhaseLanded || state.LandingCommit != event.CheckpointCommit ||
			state.LandingCheckpointTreeSHA != event.CheckpointTree || state.LandingReason != event.Reason ||
			state.LandedAt == nil || !state.LandedAt.Equal(event.OccurredAt) ||
			state.MissionOutcome != DetermineMissionOutcome(state) {
			return errors.New("deep.json landing result disagrees with its watermark event")
		}
	}
	return nil
}

func applyRunEvent(state *DeepState, event RunEvent) error {
	if event.RunID != state.SessionID || event.Sequence != state.RunEventWatermark+1 {
		return errors.New("run event identity or sequence does not follow projection")
	}
	switch event.Type {
	case RunEventEpochStarted:
		if state.Phase != event.FromPhase {
			return fmt.Errorf("epoch event expects phase %q, found %q", event.FromPhase, state.Phase)
		}
		if event.FromPhase == PhaseLanded {
			if !state.ReopenAfterLanding(event.OccurredAt) {
				return errors.New("could not reopen prior landing for new epoch")
			}
		} else {
			state.Phase = event.ToPhase
			state.MissionOutcome = MissionOutcomePending
			if event.ToPhase == PhaseLanding {
				state.LandingReason = event.Reason
			}
			if event.ToPhase == PhaseExecuting {
				state.LandedAt = nil
			}
		}
		state.RunID = event.RunID
		state.ExecutionEpochID = event.EpochID
		state.RunEventSchemaVersion = event.SchemaVersion
		if !event.Deadline.IsZero() {
			state.Deadline = event.Deadline.UTC()
		}
		if !event.LandBefore.IsZero() {
			state.LandBefore = event.LandBefore.UTC()
		}
	case RunEventLandingStarted:
		if state.Phase != event.FromPhase || event.EpochID != state.ExecutionEpochID {
			return errors.New("landing-start event does not follow the current epoch phase")
		}
		state.Phase = event.ToPhase
		state.LandingReason = event.Reason
	case RunEventLanded:
		if state.Phase != event.FromPhase || event.EpochID != state.ExecutionEpochID {
			return errors.New("landed event does not follow the current landing phase")
		}
		landedAt := event.OccurredAt.UTC()
		state.Phase = event.ToPhase
		state.LandedAt = &landedAt
		state.LandingCommit = event.CheckpointCommit
		state.LandingCheckpointTreeSHA = event.CheckpointTree
		state.MissionOutcome = DetermineMissionOutcome(*state)
	default:
		return fmt.Errorf("cannot apply run event type %q", event.Type)
	}
	state.RunEventWatermark = event.Sequence
	return nil
}

func sameLifecycleProjection(a, b DeepState) bool {
	if a.Phase != b.Phase || a.LandingReason != b.LandingReason || a.LandingCommit != b.LandingCommit ||
		a.LandingCheckpointTreeSHA != b.LandingCheckpointTreeSHA ||
		!sameOptionalTime(a.LandedAt, b.LandedAt) || len(a.PreviousLandings) != len(b.PreviousLandings) {
		return false
	}
	for i := range a.PreviousLandings {
		if !sameLandingRecord(a.PreviousLandings[i], b.PreviousLandings[i]) {
			return false
		}
	}
	return true
}

func sameLandingRecord(a, b LandingRecord) bool {
	return a.At.Equal(b.At) && a.Reason == b.Reason && a.Commit == b.Commit &&
		a.CheckpointTreeSHA == b.CheckpointTreeSHA && a.Verification == b.Verification &&
		a.MissionOutcome == b.MissionOutcome && a.VerificationOutcome == b.VerificationOutcome &&
		sameVerificationSubject(a.VerificationSubject, b.VerificationSubject) &&
		a.HandoffSHA256 == b.HandoffSHA256
}

func sameVerificationSubject(a, b *VerificationSubject) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sameOptionalTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
