package main

import (
	"testing"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestDoctorClassifiesActiveStartupAsProgress(t *testing.T) {
	got := classifyDoctorInputs(doctorInputs{
		ProviderReachable: true,
		InstancePresent:   true,
		ProviderStatus:    "running",
		ProviderSSHReady:  true,
		Lifecycle: lifecycleDoctorState{
			Busy: true, Verified: true, PID: 123, Operation: lifecycleOperationStart,
		},
		RecordedStatus:  sessionstate.StatusBooting,
		Checkpoint:      sessionstate.CheckpointInstanceCreated,
		SSHMetadata:    false,
		WatchdogRunning: true,
	})
	if got.Diagnosis != doctorStartupInProgress || got.Severity != "PROGRESS" {
		t.Fatalf("classification = %+v, want STARTUP_IN_PROGRESS/PROGRESS", got)
	}
}

func TestDoctorClassifiesAnonymousStartupLockAsSafetyIssue(t *testing.T) {
	got := classifyDoctorInputs(doctorInputs{
		ProviderReachable: true,
		InstancePresent:   true,
		ProviderStatus:    "running",
		Lifecycle:         lifecycleDoctorState{Busy: true, Verified: false},
		RecordedStatus:    sessionstate.StatusBooting,
		WatchdogRunning:   true,
	})
	if got.Diagnosis != doctorLifecycleOwnerUnknown || got.Severity != "SAFETY" {
		t.Fatalf("classification = %+v, want LIFECYCLE_OWNER_UNKNOWN/SAFETY", got)
	}
}

func TestDoctorClassifiesStartupWithoutOwnerAsStalled(t *testing.T) {
	got := classifyDoctorInputs(doctorInputs{
		ProviderReachable: true,
		InstancePresent:   true,
		ProviderStatus:    "running",
		RecordedStatus:    sessionstate.StatusBooting,
		Checkpoint:        sessionstate.CheckpointInstanceCreated,
		WatchdogRunning:   true,
	})
	if got.Diagnosis != doctorStartupStalled || got.Severity != "RECOVERABLE" {
		t.Fatalf("classification = %+v, want STARTUP_STALLED/RECOVERABLE", got)
	}
}

func TestDoctorClassifiesMissingTrackedInstanceAsSafetyIssue(t *testing.T) {
	got := classifyDoctorInputs(doctorInputs{ProviderReachable: true, InstancePresent: false})
	if got.Diagnosis != doctorInstanceMissing || got.Severity != "SAFETY" {
		t.Fatalf("classification = %+v, want INSTANCE_MISSING/SAFETY", got)
	}
}

func TestDoctorClassifiesReadySessionMissingTunnel(t *testing.T) {
	got := classifyDoctorInputs(doctorInputs{
		ProviderReachable: true,
		InstancePresent:   true,
		ProviderStatus:    "running",
		RecordedStatus:    sessionstate.StatusReady,
		SSHMetadata:       true,
		SSHHealthy:        true,
		RuntimeRunning:    true,
		TunnelRunning:     false,
		EndpointHealthy:   false,
		WatchdogRunning:   true,
	})
	if got.Diagnosis != doctorTunnelMissing || got.Severity != "RECOVERABLE" {
		t.Fatalf("classification = %+v, want TUNNEL_PROCESS_MISSING/RECOVERABLE", got)
	}
}

func TestDoctorWatchdogFailurePrecedesStartupProgress(t *testing.T) {
	got := classifyDoctorInputs(doctorInputs{
		ProviderReachable: true,
		InstancePresent:   true,
		ProviderStatus:    "running",
		RecordedStatus:    sessionstate.StatusBooting,
		Lifecycle:         lifecycleDoctorState{Busy: true, Verified: true, Operation: lifecycleOperationStart},
		WatchdogRunning:   false,
	})
	if got.Diagnosis != doctorWatchdogMissing || got.Severity != "SAFETY" {
		t.Fatalf("classification = %+v, want WATCHDOG_MISSING/SAFETY", got)
	}
}
