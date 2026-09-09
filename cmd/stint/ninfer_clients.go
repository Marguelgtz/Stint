package main

import (
	"fmt"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const (
	defaultNInferClients = 1
	maxNInferClients     = 2
)

// validateNInferClients intentionally keeps the first multi-agent surface
// narrow. One client preserves today's behavior; two clients maps directly to
// NInfer's two generation lanes while both lanes continue sharing one dynamic
// KV pool.
func validateNInferClients(clients int) error {
	if clients < defaultNInferClients || clients > maxNInferClients {
		return fmt.Errorf("--clients must be 1 or 2, got %d", clients)
	}
	return nil
}

func validateClientsForRuntime(runtime string, clients int) error {
	if err := validateNInferClients(clients); err != nil {
		return err
	}
	if clients > 1 && runtime != runtimeNInfer {
		return fmt.Errorf("--clients %d requires NInfer; selected runtime is %s", clients, runtime)
	}
	return nil
}

// clientsForState keeps sessions created before the clients field compatible:
// an absent/zero value means the historical single-lane behavior.
func clientsForState(state sessionstate.State) int {
	if state.Clients <= 0 {
		return defaultNInferClients
	}
	return state.Clients
}

// Auto fallback to llama.cpp is safe only when the requested capacity can be
// preserved. A two-client session must fail rather than silently restarting as
// a single-lane llama.cpp server.
func allowLlamaFallbackForState(state sessionstate.State) bool {
	return state.RuntimeRequest == runtimeAuto && clientsForState(state) == defaultNInferClients
}
