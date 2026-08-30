package main

import (
	"fmt"

	"github.com/Marguelgtz/Stint/internal/core"
)

const (
	defaultNetworkCandidateAttempts = 3
	maxNetworkCandidateAttempts     = 10
)

func validateNetworkCandidateAttempts(attempts int) error {
	if attempts < 1 {
		return fmt.Errorf("--network-candidate-attempts must be at least 1")
	}
	if attempts > maxNetworkCandidateAttempts {
		return fmt.Errorf("--network-candidate-attempts must be %d or fewer", maxNetworkCandidateAttempts)
	}
	return nil
}

// selectNetworkCandidates keeps the core interactive ranking, but limits the
// paid qualification loop to distinct Vast machines. A single host can expose
// multiple offers; retrying another offer from the same machine would not give
// us an independent network path and would only burn another startup cycle.
func selectNetworkCandidates(profile core.Profile, offers []core.Offer, attempts int) []core.Offer {
	if attempts <= 0 {
		return nil
	}

	ranked := core.RankOffers(profile, offers)
	selected := make([]core.Offer, 0, attempts)
	seen := make(map[string]struct{}, attempts)
	for _, offer := range ranked {
		key := fmt.Sprintf("machine:%d", offer.MachineID)
		if offer.MachineID <= 0 {
			key = "offer:" + offer.ID
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		selected = append(selected, offer)
		if len(selected) == attempts {
			break
		}
	}
	return selected
}
