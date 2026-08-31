package main

import (
	"strings"
	"testing"
	"time"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestDashboardDisplayStatusRecoverable(t *testing.T) {
	snapshot := sessionSnapshot{
		Session: sessionInfo{InstanceID: 42, Status: sessionstate.StatusRecoverable, Checkpoint: sessionstate.CheckpointRuntimeReady},
		Time:    sessionTimeSnapshot{Deadline: time.Now().Add(time.Hour)},
	}
	if got := dashboardDisplayStatus(snapshot); got != sessionstate.StatusRecoverable {
		t.Fatalf("status = %q, want RECOVERABLE", got)
	}
	if !dashboardSessionRecoverable(snapshot) {
		t.Fatal("expected recoverable snapshot")
	}
	if notice := dashboardRecoveryNotice(snapshot); !strings.Contains(notice, "press r to resume") {
		t.Fatalf("notice = %q", notice)
	}
}

func TestDashboardDisplayStatusDegradedFromPassiveTelemetry(t *testing.T) {
	snapshot := sessionSnapshot{
		Session: sessionInfo{InstanceID: 42, Status: sessionstate.StatusReady},
		Time:    sessionTimeSnapshot{Deadline: time.Now().Add(time.Hour)},
		Health: sessionHealth{
			Endpoint: endpointHealth{Refreshed: true, Healthy: false},
			Runtime:  runtimeHealth{Refreshed: true, SSH: true, Running: true},
		},
	}
	if got := dashboardDisplayStatus(snapshot); got != "DEGRADED" {
		t.Fatalf("status = %q, want DEGRADED", got)
	}
	if dashboardSessionRecoverable(snapshot) {
		t.Fatal("degraded telemetry must not invent authoritative recoverability")
	}
}

func TestDashboardDisplayStatusExpiredWins(t *testing.T) {
	snapshot := sessionSnapshot{
		Session: sessionInfo{InstanceID: 42, Status: sessionstate.StatusRecoverable},
		Time:    sessionTimeSnapshot{Expired: true},
	}
	if got := dashboardDisplayStatus(snapshot); got != "EXPIRED" {
		t.Fatalf("status = %q, want EXPIRED", got)
	}
	if dashboardSessionRecoverable(snapshot) {
		t.Fatal("expired session must not offer resume")
	}
}
