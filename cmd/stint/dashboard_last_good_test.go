package main

import (
	"testing"
	"time"
)

func TestProjectInferenceRetainsLastGoodOnFailedProbe(t *testing.T) {
	now := time.Now().UTC()
	good := inferenceTelemetry{
		Refreshed: true, Available: true,
		Agents: 2, ResidentDepth: 40000,
		Meta: sampleMeta{SampledAt: now.Add(-30 * time.Second)},
	}
	failed := inferenceTelemetry{Refreshed: true, Available: false, UnavailableReason: "metrics endpoint disabled"}

	snapshot := func(inf inferenceTelemetry, instance int64) sessionSnapshot {
		return sessionSnapshot{Session: sessionInfo{InstanceID: instance}, Inference: inf}
	}

	c := &dashboardController{}

	got := c.projectInference(snapshot(good, 7))
	if !got.Available || got.Stale {
		t.Fatalf("first good sample: want fresh available, got %+v", got)
	}

	got = c.projectInference(snapshot(failed, 7))
	if !got.Stale || !got.Available || got.Age == "" || got.Agents != 2 || got.Depth != 40000 {
		t.Fatalf("failed probe: want retained stale sample, got %+v", got)
	}

	got = c.projectInference(snapshot(inferenceTelemetry{}, 7))
	if !got.Stale || !got.Available {
		t.Fatalf("local snapshot: want retained stale sample, got %+v", got)
	}

	good.Meta.SampledAt = now
	got = c.projectInference(snapshot(good, 7))
	if !got.Available || got.Stale {
		t.Fatalf("recovered probe: want fresh available sample, got %+v", got)
	}

	got = c.projectInference(snapshot(failed, 8))
	if got.Available || got.Stale {
		t.Fatalf("new session: want plain unavailable sample, got %+v", got)
	}
}

func TestProjectInferenceUnavailableWithoutLastGood(t *testing.T) {
	c := &dashboardController{}
	failed := inferenceTelemetry{Refreshed: true, Available: false, UnavailableReason: "probe failed"}
	s := sessionSnapshot{Session: sessionInfo{InstanceID: 7}, Inference: failed}

	got := c.projectInference(s)
	if got.Available || got.Stale || got.Error != "probe failed" {
		t.Fatalf("want plain unavailable sample, got %+v", got)
	}
}