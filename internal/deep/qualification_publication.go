package deep

import (
	"errors"
	"fmt"
)

// QualificationPublicationCheckpoint is a bounded publisher input derived
// from validated journal events and their current durable acceptance state.
type QualificationPublicationCheckpoint struct {
	Sequence               uint64                 `json:"sequence"`
	CheckpointEventID      string                 `json:"checkpointEventId"`
	TaskID                 string                 `json:"taskId"`
	Attempt                int                    `json:"attempt"`
	Commit                 string                 `json:"commit"`
	TreeSHA                string                 `json:"treeSha"`
	AcceptanceOutcome      AcceptanceOutcome      `json:"acceptanceOutcome"`
	AcceptanceCheckOutcome AcceptanceCheckOutcome `json:"acceptanceCheckOutcome,omitempty"`
	SemanticReviewOutcome  ReviewOutcome          `json:"semanticReviewOutcome"`
	TaskStatus             Status                 `json:"taskStatus"`
}

type QualificationPublicationPlan struct {
	RunID       string                               `json:"runId"`
	Checkpoints []QualificationPublicationCheckpoint `json:"checkpoints"`
}

// BuildQualificationPublicationPlan uses journal event order, selecting only
// the exact checkpoint bound to each current accepted decision. LoadState and
// ReadRunEvents must validate the input before calling this builder.
func BuildQualificationPublicationPlan(state DeepState, events []RunEvent) (QualificationPublicationPlan, error) {
	if state.RunID == "" || state.RunEventSchemaVersion != RunEventSchemaVersion || uint64(len(events)) != state.RunEventWatermark {
		return QualificationPublicationPlan{}, errors.New("publication plan requires a validated journaled projection")
	}
	checkpointByID := make(map[string]RunEvent)
	latestCheckpointByTask := make(map[string]RunEvent)
	acceptedByID := make(map[string]AcceptanceRun)
	reviewByID := make(map[string]ReviewCycle)
	for _, event := range events {
		switch event.Type {
		case RunEventTaskCheckpointCreated:
			if event.TaskCheckpoint == nil {
				return QualificationPublicationPlan{}, fmt.Errorf("checkpoint event %d has no checkpoint", event.Sequence)
			}
			checkpointByID[event.EventID] = event
			latestCheckpointByTask[event.TaskCheckpoint.TaskID] = event
		case RunEventAcceptanceResult:
			if event.AcceptanceRun != nil {
				acceptedByID[event.AcceptanceRun.ID] = *event.AcceptanceRun
			}
		case RunEventReviewResult:
			if event.ReviewCycle != nil {
				reviewByID[event.ReviewCycle.ID] = *event.ReviewCycle
			}
		}
	}
	plan := QualificationPublicationPlan{RunID: state.RunID, Checkpoints: make([]QualificationPublicationCheckpoint, 0)}
	for _, event := range events {
		if event.Type != RunEventTaskCheckpointCreated || event.TaskCheckpoint == nil {
			continue
		}
		checkpoint := *event.TaskCheckpoint
		task, ok := findTask(&state, checkpoint.TaskID)
		if !ok {
			return QualificationPublicationPlan{}, fmt.Errorf("checkpoint event %s references missing Work Unit %s", event.EventID, checkpoint.TaskID)
		}
		item := QualificationPublicationCheckpoint{
			Sequence: event.Sequence, CheckpointEventID: event.EventID,
			TaskID: checkpoint.TaskID, Attempt: checkpoint.Attempt,
			Commit: checkpoint.Commit, TreeSHA: checkpoint.TreeSHA,
			TaskStatus: task.Status, AcceptanceOutcome: task.AcceptanceOutcome,
			AcceptanceCheckOutcome: task.AcceptanceCheckOutcome,
			SemanticReviewOutcome:  ReviewOutcome("not_required"),
		}
		if state.AcceptanceContractVersion == DeterministicAcceptanceContractVersion && task.IsAcceptanceContractTask() {
			if task.Status != StatusAccepted || task.AcceptanceOutcome != AcceptanceAccepted ||
				task.AcceptanceCheckpointEventID != event.EventID || task.AcceptanceCheckpointCommit != checkpoint.Commit ||
				task.AcceptanceCheckpointTreeSHA != checkpoint.TreeSHA {
				continue
			}
			acceptance, ok := acceptedByID[task.AcceptanceRunID]
			if !ok || acceptance.Decision != AcceptanceAccepted || acceptance.CheckOutcome != AcceptanceCheckPassed ||
				acceptance.CheckpointEventID != event.EventID || acceptance.Checkpoint != checkpoint {
				return QualificationPublicationPlan{}, fmt.Errorf("accepted Work Unit %s has no matching durable acceptance result", task.ID)
			}
			if HasSemanticReviewContract(state.SemanticReviewContractVersion) {
				item.SemanticReviewOutcome = ReviewOutcome("not_run")
				if task.ReviewCheckpointEventID == event.EventID {
					review, found := reviewByID[task.ReviewCycleID]
					if !found || review.CheckpointEventID != event.EventID || review.Checkpoint != checkpoint {
						return QualificationPublicationPlan{}, fmt.Errorf("Work Unit %s review projection differs from its checkpoint", task.ID)
					}
					item.SemanticReviewOutcome = review.Outcome
				}
			}
		} else {
			latest := latestCheckpointByTask[task.ID]
			if latest.EventID != event.EventID || task.Status != StatusVerified || task.CheckpointCommit != checkpoint.Commit || task.CheckpointTreeSHA != checkpoint.TreeSHA {
				continue
			}
		}
		plan.Checkpoints = append(plan.Checkpoints, item)
	}
	return plan, nil
}

func LoadQualificationPublicationPlan(stateDir, runID string) (QualificationPublicationPlan, error) {
	state, err := LoadState(stateDir, runID)
	if err != nil {
		return QualificationPublicationPlan{}, fmt.Errorf("load publication run projection: %w", err)
	}
	events, err := ReadRunEvents(stateDir, runID)
	if err != nil {
		return QualificationPublicationPlan{}, fmt.Errorf("load publication run journal: %w", err)
	}
	return BuildQualificationPublicationPlan(state, events)
}
