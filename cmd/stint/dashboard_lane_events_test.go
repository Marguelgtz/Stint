package main

import (
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/dashboard"
)

func laneSnapshot(instanceID int64, lanes ...inferenceLane) sessionSnapshot {
	return sessionSnapshot{
		Session: sessionInfo{InstanceID: instanceID},
		Inference: inferenceTelemetry{
			Refreshed: true, Available: true, Lanes: lanes,
			Meta: sampleMeta{SampledAt: time.Now().UTC()},
		},
	}
}

func TestObserveLaneEventsRecordsTransitions(t *testing.T) {
	c := &dashboardController{}

	// First observation: one active lane, one idle lane.
	c.observeLaneEvents(laneSnapshot(7,
		inferenceLane{ID: 0, Processing: true, NPrompt: 10260, NCached: 200},
		inferenceLane{ID: 1, NPrompt: 0},
	))
	if len(c.model.LaneEvents) != 2 {
		t.Fatalf("first observation = %d events, want 2 (one per lane): %+v", len(c.model.LaneEvents), c.model.LaneEvents)
	}

	// No change: no new events.
	c.observeLaneEvents(laneSnapshot(7,
		inferenceLane{ID: 0, Processing: true, NPrompt: 10260, NCached: 200},
		inferenceLane{ID: 1, NPrompt: 0},
	))
	if len(c.model.LaneEvents) != 2 {
		t.Fatalf("unchanged lanes must not emit events: got %d", len(c.model.LaneEvents))
	}

	// Lane 0 finishes and retains context: active -> idle retained.
	c.observeLaneEvents(laneSnapshot(7,
		inferenceLane{ID: 0, Retained: true, NPrompt: 45000, NCached: 43000},
		inferenceLane{ID: 1, NPrompt: 0},
	))
	if len(c.model.LaneEvents) != 3 {
		t.Fatalf("transition = %d events, want 3: %+v", len(c.model.LaneEvents), c.model.LaneEvents)
	}
	newest := c.model.LaneEvents[0]
	if newest.LaneID != 0 || newest.Kind != "active -> idle retained" {
		t.Fatalf("newest event = %+v, want lane 0 active -> idle retained", newest)
	}
}

func TestObserveLaneEventsDisplacementShowsEviction(t *testing.T) {
	c := &dashboardController{}
	// Lane 1 holds a large resident history.
	c.observeLaneEvents(laneSnapshot(7, inferenceLane{ID: 1, Retained: true, NPrompt: 130000, NCached: 120000}))
	// A fresh probe session displaces lane 1's history: it goes resident -> idle.
	c.observeLaneEvents(laneSnapshot(7, inferenceLane{ID: 1, NPrompt: 0}))
	newest := c.model.LaneEvents[0]
	if newest.LaneID != 1 || newest.Kind != "idle retained -> idle" {
		t.Fatalf("displacement event = %+v, want lane 1 idle retained -> idle", newest)
	}
}

func TestObserveLaneEventsResetsOnNewInstance(t *testing.T) {
	c := &dashboardController{}
	c.observeLaneEvents(laneSnapshot(7, inferenceLane{ID: 0, Processing: true, NPrompt: 100}))
	c.observeLaneEvents(laneSnapshot(8, inferenceLane{ID: 0, Processing: true, NPrompt: 500}))
	// The new instance's lanes are unrelated to the old session's: the log
	// was reset and re-seeded with the new session's first observations.
	if len(c.model.LaneEvents) != 1 {
		t.Fatalf("new instance should reset the log to its own observations, got %d: %+v", len(c.model.LaneEvents), c.model.LaneEvents)
	}
	if c.model.LaneEvents[0].Kind != "observed active" {
		t.Fatalf("first event of new instance = %+v, want observed active", c.model.LaneEvents[0])
	}
}

func TestObserveLaneEventsIgnoresUnavailableProbe(t *testing.T) {
	c := &dashboardController{}
	c.observeLaneEvents(laneSnapshot(7, inferenceLane{ID: 0, Processing: true, NPrompt: 100}))
	bad := laneSnapshot(7, inferenceLane{ID: 0, Processing: false, NPrompt: 0})
	bad.Inference.Available = false
	bad.Inference.UnavailableReason = "probe failed"
	c.observeLaneEvents(bad)
	if len(c.model.LaneEvents) != 1 {
		t.Fatalf("unavailable probe must not emit events, got %d: %+v", len(c.model.LaneEvents), c.model.LaneEvents)
	}
}

func TestObserveLaneEventsBoundedLog(t *testing.T) {
	c := &dashboardController{}
	for i := 0; i < dashboard.MaxLaneEvents+50; i++ {
		// Alternate a lane between active and idle to force a transition each
		// call (each call observes one lane).
		active := i%2 == 0
		c.observeLaneEvents(laneSnapshot(7, inferenceLane{ID: 0, Processing: active, NPrompt: 100}))
	}
	if len(c.model.LaneEvents) != dashboard.MaxLaneEvents {
		t.Fatalf("log = %d events, want bounded to %d", len(c.model.LaneEvents), dashboard.MaxLaneEvents)
	}
}
