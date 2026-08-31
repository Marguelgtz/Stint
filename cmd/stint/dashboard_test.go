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

func TestDashboardNavigationAndExit(t *testing.T) {
	controller := dashboardController{model: dash.Model{View: dash.Home}}
	quit, changed, action := controller.handleKey('2')
	if quit || !changed || action.Kind != dashboardActionNone || controller.model.View != dash.Performance {
		t.Fatalf("performance navigation: quit=%v changed=%v action=%v view=%v", quit, changed, action.Kind, controller.model.View)
	}
	quit, changed, action = controller.handleKey('3')
	if quit || !changed || action.Kind != dashboardActionNone || controller.model.View != dash.Config {
		t.Fatalf("config navigation: quit=%v changed=%v action=%v view=%v", quit, changed, action.Kind, controller.model.View)
	}
	quit, _, _ = controller.handleKey('q')
	if !quit {
		t.Fatal("q should exit dashboard")
	}
}

func TestDashboardBenchmarkRequiresExplicitConfirmation(t *testing.T) {
	controller := dashboardController{model: dash.Model{View: dash.Home}}
	_, changed, action := controller.handleKey('b')
	if !changed || action.Kind != dashboardActionNone || controller.modalMode != dashboardModalBenchmark || controller.model.Modal == nil {
		t.Fatalf("benchmark key did not open confirmation: mode=%v action=%v", controller.modalMode, action.Kind)
	}
	_, changed, action = controller.handleKey('\r')
	if !changed || action.Kind != dashboardActionBenchmark || controller.model.Modal != nil {
		t.Fatalf("benchmark confirmation = changed %v action %v modal=%v", changed, action.Kind, controller.model.Modal)
	}
}

func TestDashboardDeadlineChoiceAndCustomInput(t *testing.T) {
	controller := dashboardController{model: dash.Model{View: dash.Home, Session: dash.Session{InstanceID: 9}}}
	_, changed, _ := controller.handleKey('+')
	if !changed || controller.modalMode != dashboardModalDeadlineChoice || controller.deadlineDirection != deadlineExtend {
		t.Fatalf("extend key did not open choice: mode=%v direction=%v", controller.modalMode, controller.deadlineDirection)
	}
	_, changed, _ = controller.handleKey('4')
	if !changed || controller.modalMode != dashboardModalDeadlineCustom {
		t.Fatalf("custom choice did not open input: mode=%v", controller.modalMode)
	}
	for _, key := range []byte{'1', 'h', '3', '0', 'm'} {
		_, _, _ = controller.handleKey(key)
	}
	if controller.customDuration != "1h30m" {
		t.Fatalf("custom duration = %q", controller.customDuration)
	}
	_, changed, _ = controller.handleKey(27)
	if !changed || controller.model.Modal != nil || controller.modalMode != dashboardModalNone {
		t.Fatal("escape should cancel modal")
	}
}

func TestDashboardDownRequiresUppercaseConfirmation(t *testing.T) {
	controller := dashboardController{model: dash.Model{Session: dash.Session{InstanceID: 42, Remaining: time.Hour}}}
	_, _, action := controller.handleKey('d')
	if action.Kind != dashboardActionNone || controller.modalMode != dashboardModalDownConfirm {
		t.Fatalf("down should open guarded modal: action=%v mode=%v", action.Kind, controller.modalMode)
	}
	_, _, action = controller.handleKey('d')
	if action.Kind != dashboardActionNone {
		t.Fatal("lowercase d must not confirm destruction")
	}
	_, changed, action := controller.handleKey('D')
	if !changed || action.Kind != dashboardActionDown {
		t.Fatalf("uppercase D should confirm down: changed=%v action=%v", changed, action.Kind)
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
