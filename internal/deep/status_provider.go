package deep

import (
	"encoding/json"
	"fmt"
	"time"
)

// DeepStatusReplyMaxBytes is the hard cap on a single status projection reply.
// Replies that exceed it fail clearly instead of being truncated, so a consumer
// never sees a silently partial projection. 64 KiB.
const DeepStatusReplyMaxBytes = 64 * 1024

// statusMaxTaskRows bounds the number of task rows a projection may carry so an
// unbounded task list can never drive the reply past the byte cap. Rows are
// small fixed-size fields, so this is generous; exceeding it fails clearly.
const statusMaxTaskRows = 512

// DeepStatusReply is the allow-listed projection of a selected Deep run's
// validated, durable state. It is the ONLY shape the local stdio MCP provider
// exposes. It deliberately carries none of the mission prose (name, objective,
// success, constraints, verify), acceptance narrative, verifier/acceptance
// commands, worker output, findings, local paths, credentials, or raw journal
// events. `phase` (operational boundary) and `missionOutcome` (completion
// evidence) are separate fields on purpose: a run that is `landed` is not
// necessarily a run whose mission `succeeded`.
type DeepStatusReply struct {
	SchemaVersion            int             `json:"schemaVersion"`
	SessionID                string          `json:"sessionId"`
	RunID                    string          `json:"runId,omitempty"`
	ExecutionEpochID         string          `json:"executionEpochId,omitempty"`
	RunEventWatermark        uint64          `json:"runEventWatermark"`
	Phase                    string          `json:"phase"`
	MissionOutcome           string          `json:"missionOutcome"`
	Deadline                 string          `json:"deadline,omitempty"`
	LandingCommit            string          `json:"landingCommit,omitempty"`
	LandingCheckpointTreeSha string          `json:"landingCheckpointTreeSha,omitempty"`
	MissionReviewOutcome     string          `json:"missionReviewOutcome,omitempty"`
	Tasks                    []StatusTaskRow `json:"tasks,omitempty"`
}

// StatusTaskRow is the allow-listed projection of a single task. Only stable
// identity and outcome fields are exposed; narrative, blockers, outputs,
// commands, and findings are excluded.
type StatusTaskRow struct {
	ID                string `json:"id"`
	Source            string `json:"source,omitempty"`
	Status            string `json:"status"`
	Attempts          int    `json:"attempts"`
	CheckpointCommit  string `json:"checkpointCommit,omitempty"`
	CheckpointTreeSha string `json:"checkpointTreeSha,omitempty"`
	AcceptanceOutcome string `json:"acceptanceOutcome,omitempty"`
	ReviewOutcome     string `json:"reviewOutcome,omitempty"`
}

// ValidateStatusSessionID checks a session identity before any state path is
// opened. Session IDs are timestamp-based (YYYYMMDD-HHMMSS), so only alnum,
// "-", and "_" are admitted; a length bound and a no-dot rule (dots are legal
// in task IDs, so they are deliberately excluded here) keep a crafted
// selection from escaping the state directory via "." / ".." or embedded
// separators. This is a gate only; it does not check that a run with the
// identity exists.
func ValidateStatusSessionID(sessionID string) error {
	if sessionID == "" || len(sessionID) > 128 {
		return fmt.Errorf("deep status session identity is invalid")
	}
	for _, r := range sessionID {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			return fmt.Errorf("deep status session identity contains unsupported characters")
		}
	}
	return nil
}

// BuildStatusReply loads the selected run through the package's validated,
// durable state and journal recovery path (LoadState) and projects it into the
// allow-listed DeepStatusReply, capped at DeepStatusReplyMaxBytes. It is
// read-only with respect to status: it creates no competing status store and
// changes no journal or checkpoint semantics. LoadState performs the same
// journal replay a process restart would, so an observation remains available
// across restarts and epoch recovery.
//
// The session identity is re-validated here as defense in depth; the MCP layer
// validates it before this is reached. Any failure (invalid identity, missing
// state, corrupt/invalid state, or an over-cap projection) returns an error and
// never a success projection.
func BuildStatusReply(stateDir, sessionID string) ([]byte, error) {
	if err := ValidateStatusSessionID(sessionID); err != nil {
		return nil, err
	}
	state, err := LoadState(stateDir, sessionID)
	if err != nil {
		return nil, err
	}
	if len(state.Tasks) > statusMaxTaskRows {
		return nil, fmt.Errorf("deep status projection is bounded to %d task rows; run reports %d", statusMaxTaskRows, len(state.Tasks))
	}
	reply := projectStatusReply(state)
	data, err := json.Marshal(reply)
	if err != nil {
		return nil, fmt.Errorf("encode deep status projection: %w", err)
	}
	if len(data) > DeepStatusReplyMaxBytes {
		return nil, fmt.Errorf("deep status projection of %d bytes exceeds the %d byte cap", len(data), DeepStatusReplyMaxBytes)
	}
	return data, nil
}

// projectStatusReply maps a validated DeepState onto the allow-listed reply.
// Only the listed fields are read; every other DeepState and Task field is
// ignored, which is what keeps prose, commands, outputs, paths, and journal
// events out of the projection.
func projectStatusReply(state DeepState) DeepStatusReply {
	reply := DeepStatusReply{
		SchemaVersion:            1,
		SessionID:                state.SessionID,
		RunID:                    state.RunID,
		ExecutionEpochID:         state.ExecutionEpochID,
		RunEventWatermark:        state.RunEventWatermark,
		Phase:                    string(state.Phase),
		MissionOutcome:           string(DisplayMissionOutcome(state.MissionOutcome, state.Phase)),
		LandingCommit:            state.LandingCommit,
		LandingCheckpointTreeSha: state.LandingCheckpointTreeSHA,
		MissionReviewOutcome:     string(state.MissionReviewOutcome),
	}
	if !state.Deadline.IsZero() {
		reply.Deadline = state.Deadline.UTC().Format(time.RFC3339)
	}
	if len(state.Tasks) > 0 {
		reply.Tasks = make([]StatusTaskRow, 0, len(state.Tasks))
	}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		row := StatusTaskRow{
			ID:                task.ID,
			Source:            task.Source,
			Status:            string(task.Status),
			Attempts:          task.Attempts,
			CheckpointCommit:  task.CheckpointCommit,
			CheckpointTreeSha: task.CheckpointTreeSHA,
			AcceptanceOutcome: string(task.AcceptanceOutcome),
			ReviewOutcome:     string(task.ReviewOutcome),
		}
		reply.Tasks = append(reply.Tasks, row)
	}
	return reply
}
