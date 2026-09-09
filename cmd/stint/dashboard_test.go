package main

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	dash "github.com/Marguelgtz/Stint/internal/dashboard"
)

func TestDashboardTickUpdatesOnlyDerivedValues(t *testing.T) {
	started := time.Date(2026, 8, 31, 8, 0, 0, 0, time.UTC)
	controller := dashboardController{
		snapshot: sessionSnapshot{
			Session:     sessionInfo{InstanceID: 9, Status: "READY", Runtime: "ninfer", Model: interactiveModelAlias, ContextTokens: 172032},
			Time:        sessionTimeSnapshot{StartedAt: started, Deadline: started.Add(time.Hour), ScheduledDuration: time.Hour},
			Cost:        sessionCostSnapshot{HourlyUSD: 0.40, ScheduledUSD: 0.40},
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

func TestDashboardArrowNavigationWraps(t *testing.T) {
	controller := dashboardController{model: dash.Model{View: dash.Home}}

	_, changed, _ := controller.handleKey(dashboardKeyPrevious)
	if !changed || controller.model.View != dash.Logs {
		t.Fatalf("previous from Home = %v, changed=%v; want Logs", controller.model.View, changed)
	}
	_, changed, _ = controller.handleKey(dashboardKeyNext)
	if !changed || controller.model.View != dash.Home {
		t.Fatalf("next from Logs = %v, changed=%v; want Home", controller.model.View, changed)
	}
	_, _, _ = controller.handleKey(dashboardKeyNext)
	_, _, _ = controller.handleKey(dashboardKeyNext)
	if controller.model.View != dash.Config {
		t.Fatalf("two next presses = %v; want Config", controller.model.View)
	}
}

func TestDashboardArrowInputSequences(t *testing.T) {
	keys := make(chan byte, 8)
	errs := make(chan error, 1)
	readDashboardInput(strings.NewReader("\x1b[C\x1b[D\x1b[A\x1b[B"), keys, errs)

	if err := <-errs; !errors.Is(err, io.EOF) {
		t.Fatalf("input parser error = %v, want EOF", err)
	}
	want := []byte{dashboardKeyNext, dashboardKeyPrevious, dashboardKeyPrevious, dashboardKeyNext}
	if len(keys) != len(want) {
		t.Fatalf("parsed %d keys, want %d", len(keys), len(want))
	}
	for i, expected := range want {
		if got := <-keys; got != expected {
			t.Fatalf("key %d = %#x, want %#x", i, got, expected)
		}
	}
}

func TestDashboardStandaloneEscapeIsPreserved(t *testing.T) {
	keys := make(chan byte, 2)
	errs := make(chan error, 1)
	readDashboardInput(strings.NewReader("\x1b"), keys, errs)

	if err := <-errs; !errors.Is(err, io.EOF) {
		t.Fatalf("input parser error = %v, want EOF", err)
	}
	if len(keys) != 1 {
		t.Fatalf("parsed %d keys, want one Escape", len(keys))
	}
	if got := <-keys; got != 27 {
		t.Fatalf("standalone Escape = %#x, want 0x1b", got)
	}
}

func TestDashboardBenchmarkRequiresExplicitConfirmation(t *testing.T) {
	controller := dashboardController{model: dash.Model{View: dash.Home, Session: dash.Session{InstanceID: 42, Status: "READY"}}}
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
			Time:    sessionTimeSnapshot{StartedAt: started, Deadline: started.Add(time.Hour), Elapsed: 10 * time.Minute, Remaining: 50 * time.Minute, ScheduledDuration: time.Hour},
			Cost:    sessionCostSnapshot{HourlyUSD: 0.392, EstimatedSpentUSD: 0.065, ScheduledUSD: 0.392},
		},
	}
	controller.projectSnapshot()
	if controller.model.Session.InstanceID != 42 || controller.model.Session.Runtime != "ninfer" || controller.model.Session.Context != 172032 {
		t.Fatalf("projection changed session identity: %+v", controller.model.Session)
	}
}

func TestDashboardProjectionProjectsInferenceDomain(t *testing.T) {
	decode, prefill, reuse, spec := 63.25, 1204.5, 0.87, 0.71
	controller := dashboardController{
		snapshot: sessionSnapshot{
			Session: sessionInfo{InstanceID: 42, Status: "READY", Runtime: "ninfer"},
			Inference: inferenceTelemetry{
				Refreshed: true, Available: true, Processing: 1, Deferred: 2, Agents: 2, ResidentDepth: 45000,
				DecodeTokensSec: &decode, PrefillTokensSec: &prefill, CacheReuseRatio: &reuse, SpecAcceptRatio: &spec,
				Lanes: []inferenceLane{{ID: 0, NCTX: 172032, Processing: true, NPrompt: 45000}},
			},
		},
	}
	controller.projectSnapshot()
	got := controller.model.Inference
	if !got.Refreshed || !got.Available || got.Agents != 2 || got.Depth != 45000 {
		t.Fatalf("inference projection = %+v", got)
	}
	if got.Decode != "63.2 tok/s" || got.Prefill != "1204.5 tok/s" || got.Queue != "2 queued" {
		t.Fatalf("inference strings = %+v", got)
	}
	if got.CacheReuse != "87%" || got.Speculative != "71% accepted" {
		t.Fatalf("inference ratios = %+v", got)
	}
	if got.Lanes != "0: 45000 tok" {
		t.Fatalf("inference lanes = %q", got.Lanes)
	}
}

func TestDashboardProjectionInferenceUnavailable(t *testing.T) {
	controller := dashboardController{
		snapshot: sessionSnapshot{
			Session:   sessionInfo{InstanceID: 42, Status: "READY", Runtime: "llama.cpp"},
			Inference: inferenceTelemetry{Refreshed: true, UnavailableReason: "runtime serves neither /metrics nor /slots (start llama.cpp with --metrics --slots)"},
		},
	}
	controller.projectSnapshot()
	got := controller.model.Inference
	// Refreshed stays true: the probe ran, the engine merely served neither endpoint.
	if !got.Refreshed || got.Available || got.Agents != 0 {
		t.Fatalf("unavailable inference must project as idle: %+v", got)
	}
	if !strings.Contains(got.Error, "--metrics --slots") {
		t.Fatalf("unavailable reason not projected: %+v", got)
	}
}
