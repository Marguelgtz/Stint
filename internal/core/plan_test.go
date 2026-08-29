package core

import "testing"

func qualifying4090(id string, hourly, dlperf float64) Offer {
	return Offer{
		ID:              id,
		GPUModel:        "RTX_4090",
		GPURAMMB:        24576,
		GPUMaxPowerW:    450,
		HourlyUSD:       hourly,
		Reliability:     0.995,
		DLPerf:          dlperf,
		InetDownMBps:    500,
		DirectPortCount: 2,
		Verified:        true,
		Rentable:        true,
	}
}

func TestInteractiveRanksLatencyBeforePrice(t *testing.T) {
	profile := BuiltinProfiles["interactive"]
	offers := []Offer{
		qualifying4090("cheap", 0.31, 95),
		qualifying4090("fast", 0.35, 110),
	}
	selected := SelectOffers(profile, offers)
	if len(selected) != 1 || selected[0].ID != "fast" {
		t.Fatalf("selected = %#v", selected)
	}
}

func TestHardConstraintsFailClosed(t *testing.T) {
	profile := BuiltinProfiles["interactive"]
	cases := []Offer{
		func() Offer { o := qualifying4090("price", 0.41, 120); return o }(),
		func() Offer { o := qualifying4090("reliability", 0.35, 120); o.Reliability = 0.98; return o }(),
		func() Offer { o := qualifying4090("network", 0.35, 120); o.InetDownMBps = 99; return o }(),
		func() Offer { o := qualifying4090("ports", 0.35, 120); o.DirectPortCount = 0; return o }(),
		func() Offer { o := qualifying4090("ram", 0.35, 120); o.GPURAMMB = 16000; return o }(),
		func() Offer { o := qualifying4090("power", 0.35, 120); o.GPUMaxPowerW = 300; return o }(),
		func() Offer { o := qualifying4090("unverified", 0.35, 120); o.Verified = false; return o }(),
		func() Offer { o := qualifying4090("not-rentable", 0.35, 120); o.Rentable = false; return o }(),
		func() Offer { o := qualifying4090("rented", 0.35, 120); o.Rented = true; return o }(),
	}
	for _, offer := range cases {
		if Qualifies(profile, offer) {
			t.Fatalf("offer %q unexpectedly qualified", offer.ID)
		}
	}
}

func TestSessionCostCeiling(t *testing.T) {
	profile := BuiltinProfiles["interactive"]
	offers := []Offer{qualifying4090("ok", 0.40, 100)}
	if _, err := CreateSessionPlan(profile, 5, offers); err != nil {
		t.Fatalf("5h plan failed: %v", err)
	}
	if _, err := CreateSessionPlan(profile, 8, offers); err == nil {
		t.Fatal("expected 8h plan to exceed session ceiling")
	}
}

func TestDeterministicTieBreak(t *testing.T) {
	profile := BuiltinProfiles["interactive"]
	a := qualifying4090("a", 0.35, 100)
	b := qualifying4090("b", 0.35, 100)
	ranked := RankOffers(profile, []Offer{b, a})
	if len(ranked) != 2 || ranked[0].ID != "a" || ranked[1].ID != "b" {
		t.Fatalf("ranked = %#v", ranked)
	}
}
