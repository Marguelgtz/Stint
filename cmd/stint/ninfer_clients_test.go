package main

import (
	"strings"
	"testing"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestValidateNInferClients(t *testing.T) {
	for _, clients := range []int{1, 2} {
		if err := validateNInferClients(clients); err != nil {
			t.Fatalf("validateNInferClients(%d): %v", clients, err)
		}
	}
	for _, clients := range []int{0, 3, -1} {
		if err := validateNInferClients(clients); err == nil {
			t.Fatalf("validateNInferClients(%d) unexpectedly succeeded", clients)
		}
	}
}

func TestClientsForStatePreservesLegacySingleLane(t *testing.T) {
	if got := clientsForState(sessionstate.State{}); got != 1 {
		t.Fatalf("legacy clients = %d, want 1", got)
	}
	if got := clientsForState(sessionstate.State{Clients: 2}); got != 2 {
		t.Fatalf("persisted clients = %d, want 2", got)
	}
}

func TestTwoClientsRequireNInfer(t *testing.T) {
	if err := validateClientsForRuntime(runtimeNInfer, 2); err != nil {
		t.Fatalf("NInfer clients=2 rejected: %v", err)
	}
	if err := validateClientsForRuntime(runtimeLlamaCpp, 2); err == nil || !strings.Contains(err.Error(), "requires NInfer") {
		t.Fatalf("llama.cpp clients=2 error = %v, want requires NInfer", err)
	}
}

func TestLlamaFallbackRequiresSingleClient(t *testing.T) {
	if !allowLlamaFallbackForState(sessionstate.State{RuntimeRequest: runtimeAuto, Clients: 1}) {
		t.Fatal("auto single-client session should allow llama.cpp fallback")
	}
	if allowLlamaFallbackForState(sessionstate.State{RuntimeRequest: runtimeAuto, Clients: 2}) {
		t.Fatal("auto two-client session must not silently fall back to llama.cpp")
	}
	if allowLlamaFallbackForState(sessionstate.State{RuntimeRequest: runtimeNInfer, Clients: 1}) {
		t.Fatal("explicit NInfer session must not fall back to llama.cpp")
	}
}
