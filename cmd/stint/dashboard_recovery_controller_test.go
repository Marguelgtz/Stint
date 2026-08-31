package main

import (
	"testing"
	"time"

	dash "github.com/Marguelgtz/Stint/internal/dashboard"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestDashboardRecoverableRShowsResumeConfirmation(t *testing.T) {
	controller := dashboardController{
		model: dash.Model{Session: dash.Session{InstanceID: 42, Status: sessionstate.StatusRecoverable, Remaining: 30 * time.Minute}},
		snapshot: sessionSnapshot{
			Session: sessionInfo{InstanceID: 42, Status: sessionstate.StatusRecoverable, Checkpoint: sessionstate.CheckpointRuntimeReady},
			Time:    sessionTimeSnapshot{Deadline: time.Now().Add(30 * time.Minute), Remaining: 30 * time.Minute},
		},
	}

	quit, changed, action := controller.handleKey('r')
	if quit || !changed || action.Kind != dashboardActionNone {
		t.Fatalf("r = quit %v changed %v action %v", quit, changed, action.Kind)
	}
	if controller.modalMode != dashboardModalResumeConfirm || controller.model.Modal == nil {
		t.Fatalf("resume modal not opened: mode=%v modal=%v", controller.modalMode, controller.model.Modal)
	}

	quit, changed, action = controller.handleKey('\r')
	if quit || !changed || action.Kind != dashboardActionResume {
		t.Fatalf("resume confirm = quit %v changed %v action %v", quit, changed, action.Kind)
	}
}

func TestDashboardReadyRRemainsRefresh(t *testing.T) {
	controller := dashboardController{
		model:     dash.Model{Session: dash.Session{InstanceID: 42, Status: sessionstate.StatusReady}},
		snapshot:  sessionSnapshot{Session: sessionInfo{InstanceID: 42, Status: sessionstate.StatusReady}},
		refreshCh: make(chan dashboardLoadResult, 1),
	}
	_, changed, action := controller.handleKey('r')
	if !changed || action.Kind != dashboardActionNone {
		t.Fatalf("ready r = changed %v action %v", changed, action.Kind)
	}
	if controller.modalMode != dashboardModalNone {
		t.Fatalf("ready refresh unexpectedly opened modal: %v", controller.modalMode)
	}
	if !controller.refreshing {
		t.Fatal("ready r should start passive refresh")
	}
}

func TestDashboardBenchmarkDisabledOutsideReady(t *testing.T) {
	controller := dashboardController{
		model: dash.Model{Session: dash.Session{InstanceID: 42, Status: sessionstate.StatusRecoverable}},
	}
	_, changed, action := controller.handleKey('b')
	if !changed || action.Kind != dashboardActionNone {
		t.Fatalf("recoverable benchmark = changed %v action %v", changed, action.Kind)
	}
	if controller.modalMode != dashboardModalNone {
		t.Fatal("benchmark modal must stay closed while recoverable")
	}
	if controller.model.Notice == "" {
		t.Fatal("expected benchmark-unavailable notice")
	}
}
