package main

import (
	"fmt"
	"strings"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func dashboardSessionRecoverable(snapshot sessionSnapshot) bool {
	if snapshot.Session.InstanceID <= 0 || snapshot.Time.Expired {
		return false
	}
	// Checkpoints describe how far lifecycle work progressed, but they are not
	// themselves recovery authority: healthy READY sessions also carry a READY
	// checkpoint. Only lifecycle code may persist RECOVERABLE status.
	return snapshot.Session.Status == sessionstate.StatusRecoverable
}

func dashboardDisplayStatus(snapshot sessionSnapshot) string {
	if snapshot.Session.InstanceID <= 0 {
		return "OFFLINE"
	}
	if snapshot.Time.Expired {
		return "EXPIRED"
	}
	if dashboardSessionRecoverable(snapshot) {
		return sessionstate.StatusRecoverable
	}
	if snapshot.Session.Status == sessionstate.StatusReady && dashboardTelemetryDegraded(snapshot) {
		return "DEGRADED"
	}
	return snapshot.Session.Status
}

func dashboardTelemetryDegraded(snapshot sessionSnapshot) bool {
	endpointObserved := snapshot.Health.Endpoint.Refreshed
	runtimeObserved := snapshot.Health.Runtime.Refreshed
	if endpointObserved && !snapshot.Health.Endpoint.Healthy {
		return true
	}
	if runtimeObserved && (!snapshot.Health.Runtime.SSH || !snapshot.Health.Runtime.Running) {
		return true
	}
	return false
}

func dashboardRecoveryNotice(snapshot sessionSnapshot) string {
	if !dashboardSessionRecoverable(snapshot) {
		return ""
	}
	checkpoint := strings.TrimSpace(snapshot.Session.Checkpoint)
	if checkpoint == "" {
		checkpoint = "saved checkpoint"
	}
	message := fmt.Sprintf("Paid instance preserved at %s · press r to resume", checkpoint)
	if lastErr := strings.TrimSpace(snapshot.Session.LastError); lastErr != "" {
		message += " · " + compactTelemetryErrorString(lastErr)
	}
	return message
}

func compactTelemetryErrorString(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const max = 96
	if len(value) <= max {
		return value
	}
	return value[:max-1] + "…"
}
