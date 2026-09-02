package deep

import (
	"fmt"
	"os"
	"time"
)

// readAll is a seam over os.ReadFile so tests can supply mission content
// without touching the filesystem when the parser is exercised directly.
func readAll(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read mission %s: %w", path, err)
	}
	return string(data), nil
}

// Status is a task's lifecycle state.
type Status string

const (
	StatusQueued     Status = "queued"
	StatusActive     Status = "active"
	StatusVerified   Status = "verified"
	StatusIncomplete Status = "incomplete"
	StatusBlocked    Status = "blocked"
	StatusNeedsHuman Status = "needs_human"
	StatusDropped    Status = "dropped"
)

// Terminal reports whether a status will not be selected for execution again.
// Blocked and needs_human tasks stay parked; they surface in the handoff.
func (s Status) Terminal() bool {
	switch s {
	case StatusVerified, StatusBlocked, StatusNeedsHuman, StatusDropped:
		return true
	}
	return false
}

func (s Status) String() string { return string(s) }

// Task is one unit of Deep Work. IDs come from the mission (or from
// coordinator discovery, marked via Source).
type Task struct {
	ID         string     `json:"id"`
	Objective  string     `json:"objective"`
	Acceptance string     `json:"acceptance,omitempty"`
	Status     Status     `json:"status"`
	Attempts   int        `json:"attempts"`
	Blocker    string     `json:"blocker,omitempty"`
	LastResult string     `json:"lastResult,omitempty"`
	Findings   []string   `json:"findings,omitempty"`
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
	Source     string     `json:"source,omitempty"`
}
