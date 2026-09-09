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
