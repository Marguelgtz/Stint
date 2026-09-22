package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/core"
	"github.com/Marguelgtz/Stint/internal/router"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestFallbackCandidateExceedingSessionCostIsRejected(t *testing.T) {
	profile, err := router.ResolveProfile("interactive")
	if err != nil {
		t.Fatal(err)
	}
	const requestedHours = 7.0
	initial := core.Offer{HourlyUSD: 0.30}
	fallback := core.Offer{HourlyUSD: profile.GPU.MaxHourlyUSD}
	if initial.HourlyUSD*requestedHours > profile.Session.MaxCostUSD {
		t.Fatal("test setup: initial candidate must fit the session ceiling")
	}
	if fallback.HourlyUSD > profile.GPU.MaxHourlyUSD {
		t.Fatal("test setup: fallback must satisfy the hourly ceiling")
	}
	if fallback.HourlyUSD*requestedHours <= profile.Session.MaxCostUSD {
		t.Fatal("test setup: fallback must exceed the requested-session ceiling")
	}
	if !candidateWithinSessionBudget(profile, initial, requestedHours) {
		t.Fatal("initial candidate should fit the full-session budget")
	}
	if candidateWithinSessionBudget(profile, fallback, requestedHours) {
		t.Fatal("fallback satisfying hourly policy but exceeding the full-session budget was accepted")
	}
}

func TestProviderStartupTimeoutAllowsCandidateFailover(t *testing.T) {
	if providerStartupTimeout > 6*time.Minute {
		t.Fatalf("provider startup timeout = %s, want at most 6m so another paid candidate can be tried", providerStartupTimeout)
	}
}

func TestCheckpointIsRecoverable(t *testing.T) {
	tests := []struct {
		checkpoint string
		want       bool
	}{
		{checkpoint: "", want: false},
		{checkpoint: sessionstate.CheckpointInstanceCreated, want: true},
		{checkpoint: sessionstate.CheckpointSSHReady, want: true},
		{checkpoint: sessionstate.CheckpointRuntimeReady, want: true},
		{checkpoint: sessionstate.CheckpointModelStarted, want: true},
		{checkpoint: sessionstate.CheckpointReady, want: true},
	}
	for _, tt := range tests {
		if got := checkpointIsRecoverable(tt.checkpoint); got != tt.want {
			t.Fatalf("checkpointIsRecoverable(%q) = %v, want %v", tt.checkpoint, got, tt.want)
		}
	}
}

func TestRemoteModelLaunchUsesPIDTracking(t *testing.T) {
	command := remoteModelLaunchCommand()
	if strings.Contains(command, "\npkill -f ") {
		t.Fatal("remote model launch must not use pkill -f; it can kill the SSH shell that contains the pattern")
	}
	for _, required := range []string{
		"/workspace/stint/llama.pid",
		"pgrep -x llama-server",
		"nohup bash -c",
		"exec /workspace/stint/llama.cpp/build/bin/llama-server",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("remote model launch missing %q", required)
		}
	}
}
