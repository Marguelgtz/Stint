package main

import (
	"testing"
	"time"

	dash "github.com/Marguelgtz/Stint/internal/dashboard"
)

func TestDashboardTickUpdatesOnlyDerivedValues(t *testing.T) {
	started := time.Date(2026, 8, 31, 8, 0, 0, 0, time.UTC)
	controller := dashboardController{
		snapshot: sessionSnapshot{
			Session: sessionInfo{InstanceID: 9, Status: "READY", Runtime: "ninfer", Model: interactiveModelAlias, ContextTokens: 172032},
			Time: sessionTimeSnapshot{StartedAt: started, Deadline: started.Add(time.Hour), ScheduledDuration: time.Hour},
			Cost: sessionCostSnapshot{HourlyUSD: 0.40, ScheduledUSD: 0.40},
			Performance: performanceSnapshot{Available: true, SampledAt: started.Add(5 * time.Minute)},
		},
	}
	controller.tick(started.Add(20 * time.Minute))
	if controller.snapshot.Time.Elapsed != 20*time.Minute {
		t.Fatalf("elapsed = %s", controller.snapshot.Time.Elapsed)
	}
	if controller.snapshot.Time.Remaining != 40*time.Minute {
		t.Fatalf("remaining = %s", controller.snapshot.Time.Remaining)
	}
	if controller.snapshot.Cost.EstimatedSpentUSD < 0.133 || controller.snapshot.Cost.EstimatedSpentUSD > 0.134 {
		t.Fatalf("spent = %.4f", controller.snapshot.Cost.EstimatedSpentUSD)
	}
	if controller.snapshot.Performance.Age != 15*time.Minute {
		t.Fatalf("perf age = %s", controller.snapshot.Performance.Age)
	}
}

func TestDashboardNavigationAndExitDoNotMutateLifecycle(t *testing.T) {
	controller := dashboardController{model: dash.Model{View: dash.Home}}
	if quit, changed := controller.handleKey('2'); quit || !changed || controller.model.View != dash.Performance {
		t.Fatalf("performance navigation: quit=%v changed=%v view=%v", quit, changed, controller.model.View)
	}
	if quit, changed := controller.handleKey('3'); quit || !changed || controller.model.View != dash.Config {
		t.Fatalf("config navigation: quit=%v changed=%v view=%v", quit, changed, controller.model.View)
	}
	if quit, _ := controller.handleKey('q'); !quit {
		t.Fatal("q should exit dashboard")
	}
}

func TestDashboardReadOnlySliceKeepsMutationKeysInert(t *testing.T) {
	controller := dashboardController{model: dash.Model{View: dash.Home}}
	for _, key := range []byte{'b', '+', '-', 'd'} {
		quit, changed := controller.handleKey(key)
		if quit || !changed {
			t.Fatalf("key %q: quit=%v changed=%v", key, quit, changed)
		}
		if controller.model.Notice == "" {
			t.Fatalf("key %q did not explain disabled action", key)
		}
	}
}

func TestDashboardProjectionPreservesSnapshotIdentity(t *testing.T) {
	started := time.Now().UTC().Add(-10 * time.Minute)
	controller := dashboardController{
		snapshot: sessionSnapshot{
			Session: sessionInfo{InstanceID: 42, Status: "READY", GPUModel: "RTX 4090", Runtime: "ninfer", Model: interactiveModelAlias, ContextTokens: 172032, Profile: "interactive"},
			Time: sessionTimeSnapshot{StartedAt: started, Deadline: started.Add(time.Hour), Elapsed: 10 * time.Minute, Remaining: 50 * time.Minute, ScheduledDuration: time.Hour},
			Cost: sessionCostSnapshot{HourlyUSD: 0.392, EstimatedSpentUSD: 0.065, ScheduledUSD: 0.392},
		},
	}
	controller.projectSnapshot()
	if controller.model.Session.InstanceID != 42 || controller.model.Session.Runtime != "ninfer" || controller.model.Session.Context != 172032 {
		t.Fatalf("projection changed session identity: %+v", controller.model.Session)
	}
}
