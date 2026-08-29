package main

import (
	"strings"
	"testing"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

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
	command := remoteModelLaunchCommand(interactiveContext)
	if strings.Contains(command, "\npkill -f ") {
		t.Fatal("remote model launch must not use pkill -f; it can kill the SSH shell that contains the pattern")
	}
	for _, required := range []string{"/workspace/stint/llama.pid", "pgrep -x llama-server", "nohup /workspace/stint/llama.cpp/build/bin/llama-server"} {
		if !strings.Contains(command, required) {
			t.Fatalf("remote model launch missing %q", required)
		}
	}
}

func TestRemoteModelLaunchUsesRequestedContext(t *testing.T) {
	command := remoteModelLaunchCommand(24576)
	if !strings.Contains(command, "-c 24576") {
		t.Fatalf("remote model launch did not use requested context: %s", command)
	}
}

func TestValidateInteractiveContext(t *testing.T) {
	for _, tokens := range []int{16384, 24576, 32768} {
		if err := validateInteractiveContext(tokens); err != nil {
			t.Fatalf("validateInteractiveContext(%d) returned %v", tokens, err)
		}
	}
	for _, tokens := range []int{8192, 65536} {
		if err := validateInteractiveContext(tokens); err == nil {
			t.Fatalf("validateInteractiveContext(%d) unexpectedly succeeded", tokens)
		}
	}
}

func TestEffectiveInteractiveContextPreservesLegacySessions(t *testing.T) {
	if got := effectiveInteractiveContext(0); got != legacyInteractiveContext {
		t.Fatalf("effectiveInteractiveContext(0) = %d, want %d", got, legacyInteractiveContext)
	}
	if got := effectiveInteractiveContext(24576); got != 24576 {
		t.Fatalf("effectiveInteractiveContext(24576) = %d, want 24576", got)
	}
}
