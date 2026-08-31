package main

import (
	"strings"
	"testing"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestNInferTwoClientsUsesTwoLanesAndSharedKV(t *testing.T) {
	state := sessionstate.State{Runtime: runtimeNInfer, ContextTokens: 262144, Clients: 2}
	command := remoteModelLaunchCommandForState(state)
	for _, required := range []string{
		"--max-context 262144",
		"--kv-capacity 262144",
		"--max-concurrency 2",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("dual-lane NInfer launch missing %q:\n%s", required, command)
		}
	}
	if strings.Contains(command, "--max-context 131072") || strings.Contains(command, "--kv-capacity 131072") {
		t.Fatalf("dual-lane launch statically divided native context instead of sharing the full KV pool:\n%s", command)
	}
}

func TestLegacyNInferStateRemainsSingleLane(t *testing.T) {
	state := sessionstate.State{Runtime: runtimeNInfer, ContextTokens: 126976}
	command := remoteModelLaunchCommandForState(state)
	if !strings.Contains(command, "--max-concurrency 1") {
		t.Fatalf("legacy NInfer session did not preserve one lane:\n%s", command)
	}
}

func TestLlamaLaunchRemainsUnchangedByClientField(t *testing.T) {
	state := sessionstate.State{Runtime: runtimeLlamaCpp, ContextTokens: interactiveContext, Clients: 2}
	command := remoteModelLaunchCommandForState(state)
	if !strings.Contains(command, "llama-server") {
		t.Fatalf("llama.cpp state did not select llama-server:\n%s", command)
	}
	if strings.Contains(command, "--max-concurrency") {
		t.Fatalf("NInfer client-lane flag leaked into llama.cpp launch:\n%s", command)
	}
}
