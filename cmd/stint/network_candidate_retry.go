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

// startupCandidatePool separates marketplace candidates from the paid-attempt
// budget. Offers may disappear between search and rental, so a stale ask must
// not consume one of the user's configured machine attempts. Once Vast creates
// an instance, recordPaidAttempt advances the budget even if provider startup,
// SSH, or measured-network qualification later rejects that machine.
type startupCandidatePool struct {
	profile      core.Profile
	limit        int
	paidAttempts int
	queue        []core.Offer
	seen         map[string]struct{}
}

func newStartupCandidatePool(profile core.Profile, offers []core.Offer, limit int) *startupCandidatePool {
	pool := &startupCandidatePool{
		profile: profile,
		limit:   limit,
		seen:    make(map[string]struct{}),
	}
	pool.add(offers)
	return pool
}

func networkCandidateKey(offer core.Offer) string {
	if offer.MachineID > 0 {
		return fmt.Sprintf("machine:%d", offer.MachineID)
	}
	return "offer:" + offer.ID
}

// add ranks fresh marketplace offers and fills only the currently missing
// candidate slots. Every queued or previously rejected machine stays in seen,
// preventing a refresh from cycling back to a host Stint already considered.
func (p *startupCandidatePool) add(offers []core.Offer) []core.Offer {
	missing := p.missingSlots()
	if missing <= 0 {
		return nil
	}

	ranked := core.RankOffers(p.profile, offers)
	added := make([]core.Offer, 0, missing)
	for _, offer := range ranked {
		key := networkCandidateKey(offer)
		if _, exists := p.seen[key]; exists {
			continue
		}
		p.seen[key] = struct{}{}
		p.queue = append(p.queue, offer)
		added = append(added, offer)
		if len(added) == missing {
			break
		}
	}
	return added
}

func (p *startupCandidatePool) next() (core.Offer, bool) {
	if len(p.queue) == 0 {
		return core.Offer{}, false
	}
	offer := p.queue[0]
	p.queue = p.queue[1:]
	return offer, true
}

func (p *startupCandidatePool) peek() (core.Offer, bool) {
	if len(p.queue) == 0 {
		return core.Offer{}, false
	}
	return p.queue[0], true
}

func (p *startupCandidatePool) recordPaidAttempt() {
	if p.paidAttempts < p.limit {
		p.paidAttempts++
	}
}

func (p *startupCandidatePool) canTryPaid() bool {
	return p.paidAttempts < p.limit
}

func (p *startupCandidatePool) nextAttemptNumber() int {
	return p.paidAttempts + 1
}

func (p *startupCandidatePool) attemptsUsed() int {
	return p.paidAttempts
}

func (p *startupCandidatePool) attemptLimit() int {
	return p.limit
}

func (p *startupCandidatePool) queued() int {
	return len(p.queue)
}

func (p *startupCandidatePool) missingSlots() int {
	remainingAttempts := p.limit - p.paidAttempts
	if remainingAttempts <= len(p.queue) {
		return 0
	}
	return remainingAttempts - len(p.queue)
}

// selectNetworkCandidates retains the existing pure selection helper for plan
// and unit-test callers. The paid start path uses startupCandidatePool directly
// so later marketplace refreshes can add unseen replacement machines.
func selectNetworkCandidates(profile core.Profile, offers []core.Offer, attempts int) []core.Offer {
	if attempts <= 0 {
		return nil
	}
	pool := newStartupCandidatePool(profile, offers, attempts)
	return append([]core.Offer(nil), pool.queue...)
}
