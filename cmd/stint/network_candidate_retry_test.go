package main

import (
	"testing"

	"github.com/Marguelgtz/Stint/internal/core"
)

func TestSelectNetworkCandidatesUsesRankedDistinctMachines(t *testing.T) {
	profile := core.BuiltinProfiles["interactive"]
	offers := []core.Offer{
		testNetworkCandidateOffer("best", 101, 120),
		testNetworkCandidateOffer("same-machine", 101, 115),
		testNetworkCandidateOffer("second", 202, 110),
		testNetworkCandidateOffer("third", 303, 100),
	}

	got := selectNetworkCandidates(profile, offers, 3)
	if len(got) != 3 {
		t.Fatalf("candidates = %d, want 3", len(got))
	}
	want := []string{"best", "second", "third"}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("candidate %d = %q, want %q", i, got[i].ID, want[i])
		}
	}
}

func TestSelectNetworkCandidatesFallsBackToOfferIdentity(t *testing.T) {
	profile := core.BuiltinProfiles["interactive"]
	offers := []core.Offer{
		testNetworkCandidateOffer("one", 0, 120),
		testNetworkCandidateOffer("two", 0, 110),
	}

	got := selectNetworkCandidates(profile, offers, 2)
	if len(got) != 2 || got[0].ID != "one" || got[1].ID != "two" {
		t.Fatalf("candidates = %#v", got)
	}
}

func TestStartupCandidatePoolReplacesStaleOfferWithoutSpendingAttempt(t *testing.T) {
	profile := core.BuiltinProfiles["interactive"]
	pool := newStartupCandidatePool(profile, []core.Offer{
		testNetworkCandidateOffer("a", 101, 120),
		testNetworkCandidateOffer("b", 202, 110),
		testNetworkCandidateOffer("c", 303, 100),
	}, 3)

	first, ok := pool.next()
	if !ok || first.ID != "a" {
		t.Fatalf("first candidate = %#v, ok=%v", first, ok)
	}
	if pool.attemptsUsed() != 0 || pool.nextAttemptNumber() != 1 {
		t.Fatalf("stale pre-rental candidate spent the paid budget: used=%d next=%d", pool.attemptsUsed(), pool.nextAttemptNumber())
	}
	if pool.missingSlots() != 1 {
		t.Fatalf("missing slots = %d, want 1 after removing stale candidate", pool.missingSlots())
	}

	added := pool.add([]core.Offer{
		testNetworkCandidateOffer("a-new-ask", 101, 125),
		testNetworkCandidateOffer("b", 202, 110),
		testNetworkCandidateOffer("c", 303, 100),
		testNetworkCandidateOffer("replacement", 404, 105),
	})
	if len(added) != 1 || added[0].ID != "replacement" {
		t.Fatalf("replacement candidates = %#v", added)
	}
	if pool.queued() != 3 {
		t.Fatalf("queue = %d, want replenished size 3", pool.queued())
	}

	next, ok := pool.next()
	if !ok || next.ID != "b" || pool.nextAttemptNumber() != 1 {
		t.Fatalf("next candidate = %#v, ok=%v, attempt=%d; stale ask should keep attempt 1", next, ok, pool.nextAttemptNumber())
	}
}

func TestStartupCandidatePoolDefersRefreshWhileQueuedCandidatesRemain(t *testing.T) {
	profile := core.BuiltinProfiles["interactive"]
	pool := newStartupCandidatePool(profile, []core.Offer{
		testNetworkCandidateOffer("a", 101, 120),
		testNetworkCandidateOffer("b", 202, 110),
		testNetworkCandidateOffer("c", 303, 100),
	}, 3)

	if _, ok := pool.next(); !ok {
		t.Fatal("missing first candidate")
	}
	if pool.missingSlots() != 1 {
		t.Fatalf("missing slots = %d, want 1", pool.missingSlots())
	}
	if pool.needsRefill() {
		t.Fatal("pool requested a marketplace refresh while unseen candidates were still queued")
	}

	if _, ok := pool.next(); !ok {
		t.Fatal("missing second candidate")
	}
	if pool.needsRefill() {
		t.Fatal("pool requested a marketplace refresh with one queued candidate remaining")
	}
	if _, ok := pool.next(); !ok {
		t.Fatal("missing third candidate")
	}
	if !pool.needsRefill() {
		t.Fatal("pool did not request a refresh after exhausting the current marketplace snapshot")
	}
}

func TestStartupCandidatePoolPaidFailureConsumesAttempt(t *testing.T) {
	profile := core.BuiltinProfiles["interactive"]
	pool := newStartupCandidatePool(profile, []core.Offer{
		testNetworkCandidateOffer("a", 101, 120),
		testNetworkCandidateOffer("b", 202, 110),
		testNetworkCandidateOffer("c", 303, 100),
	}, 3)

	if _, ok := pool.next(); !ok {
		t.Fatal("missing first candidate")
	}
	pool.recordPaidAttempt()
	if pool.attemptsUsed() != 1 || pool.nextAttemptNumber() != 2 {
		t.Fatalf("paid failure accounting = used %d next %d, want 1/2", pool.attemptsUsed(), pool.nextAttemptNumber())
	}
	if pool.missingSlots() != 0 {
		t.Fatalf("full initial queue should already cover remaining paid budget; missing=%d", pool.missingSlots())
	}
	if added := pool.add([]core.Offer{testNetworkCandidateOffer("replacement", 404, 130)}); len(added) != 0 {
		t.Fatalf("paid failure incorrectly expanded configured budget: %#v", added)
	}
}

func TestStartupCandidatePoolRefillCanFillInitiallyShortMarketplace(t *testing.T) {
	profile := core.BuiltinProfiles["interactive"]
	pool := newStartupCandidatePool(profile, []core.Offer{
		testNetworkCandidateOffer("a", 101, 120),
	}, 3)
	if pool.queued() != 1 || pool.missingSlots() != 2 {
		t.Fatalf("initial pool queue/missing = %d/%d, want 1/2", pool.queued(), pool.missingSlots())
	}

	if _, ok := pool.next(); !ok {
		t.Fatal("missing first candidate")
	}
	pool.recordPaidAttempt()
	added := pool.add([]core.Offer{
		testNetworkCandidateOffer("a-new-ask", 101, 130),
		testNetworkCandidateOffer("b", 202, 115),
		testNetworkCandidateOffer("c", 303, 110),
		testNetworkCandidateOffer("d", 404, 105),
	})
	if len(added) != 2 || added[0].ID != "b" || added[1].ID != "c" {
		t.Fatalf("refill = %#v, want ranked unseen b/c", added)
	}
	if pool.queued() != 2 || pool.missingSlots() != 0 {
		t.Fatalf("refilled queue/missing = %d/%d, want 2/0", pool.queued(), pool.missingSlots())
	}
}

func TestValidateNetworkCandidateAttempts(t *testing.T) {
	if err := validateNetworkCandidateAttempts(1); err != nil {
		t.Fatalf("minimum attempts rejected: %v", err)
	}
	if err := validateNetworkCandidateAttempts(defaultNetworkCandidateAttempts); err != nil {
		t.Fatalf("default attempts rejected: %v", err)
	}
	if err := validateNetworkCandidateAttempts(0); err == nil {
		t.Fatal("zero attempts accepted")
	}
	if err := validateNetworkCandidateAttempts(maxNetworkCandidateAttempts + 1); err == nil {
		t.Fatal("attempt count above hard cap accepted")
	}
}

func testNetworkCandidateOffer(id string, machineID int64, dlperf float64) core.Offer {
	return core.Offer{
		ID:              id,
		GPUModel:        "RTX_4090",
		GPURAMMB:        24576,
		HourlyUSD:       0.33,
		Reliability:     0.995,
		DLPerf:          dlperf,
		InetDownMBps:    1200,
		DirectPortCount: 2,
		MachineID:       machineID,
		Verified:        true,
		Rentable:        true,
	}
}
