package main

import (
	"strings"
	"testing"
	"time"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

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
