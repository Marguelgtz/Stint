package core

import "testing"

func TestInteractivePrefersPerformanceWithinBudget(t *testing.T) {
	profile := BuiltinProfiles["interactive"]
	offers := []Offer{{ID: "cheap", GPUModel: "RTX_4090", HourlyUSD: 0.31, Reliability: 0.99, DLPerf: 96}, {ID: "fast", GPUModel: "RTX_4090", HourlyUSD: 0.35, Reliability: 0.995, DLPerf: 110}}
	selected := SelectOffers(profile, offers)
	if len(selected) != 1 || selected[0].ID != "fast" { t.Fatalf("expected fastest qualifying 4090, got %#v", selected) }
}

func TestDeepPrefersCheapestQualifyingWorkers(t *testing.T) {
	profile := BuiltinProfiles["deep"]
	offers := []Offer{{ID: "a", GPUModel: "RTX_3090", HourlyUSD: 0.14, Reliability: 0.99, DLPerf: 53}, {ID: "b", GPUModel: "RTX_3090", HourlyUSD: 0.15, Reliability: 0.992, DLPerf: 55}, {ID: "over", GPUModel: "RTX_3090", HourlyUSD: 0.20, Reliability: 0.999, DLPerf: 58}}
	selected := SelectOffers(profile, offers)
	if len(selected) != 2 || selected[0].ID != "a" || selected[1].ID != "b" { t.Fatalf("expected cheapest qualifying pair, got %#v", selected) }
}

func TestSessionPlanRoundsCost(t *testing.T) {
	profile := BuiltinProfiles["interactive"]
	plan, err := CreateSessionPlan(profile, 5, []Offer{{ID: "fast", GPUModel: "RTX_4090", HourlyUSD: 0.35, Reliability: 0.995, DLPerf: 110}})
	if err != nil { t.Fatal(err) }
	if plan.EstimatedTotalUSD != 1.75 { t.Fatalf("expected 1.75, got %.2f", plan.EstimatedTotalUSD) }
}
