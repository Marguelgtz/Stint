package core

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

type Objective string

const (
	ObjectiveLatency                Objective = "latency"
	ObjectiveValidatedWorkPerDollar Objective = "validated_work_per_dollar"
)

type GPUPolicy struct {
	PreferredModels []string `json:"preferredModels"`
	MaxHourlyUSD    float64  `json:"maxHourlyUsd"`
	MinReliability  float64  `json:"minReliability"`
}

type Profile struct {
	Name      string    `json:"name"`
	Objective Objective `json:"objective"`
	Workers   int       `json:"workers"`
	GPU       GPUPolicy `json:"gpu"`
}

type Offer struct {
	ID          string  `json:"id"`
	GPUModel    string  `json:"gpuModel"`
	HourlyUSD   float64 `json:"hourlyUsd"`
	Reliability float64 `json:"reliability"`
	DLPerf      float64 `json:"dlperf,omitempty"`
}

type PlannedWorker struct {
	Offer               Offer   `json:"offer"`
	EstimatedSessionUSD float64 `json:"estimatedSessionUsd"`
}

type SessionPlan struct {
	Profile           Profile         `json:"profile"`
	Hours             float64         `json:"hours"`
	Workers           []PlannedWorker `json:"workers"`
	EstimatedTotalUSD float64         `json:"estimatedTotalUsd"`
}

var BuiltinProfiles = map[string]Profile{
	"interactive": {
		Name:      "interactive",
		Objective: ObjectiveLatency,
		Workers:   1,
		GPU: GPUPolicy{PreferredModels: []string{"RTX_4090", "RTX_5090"}, MaxHourlyUSD: 0.40, MinReliability: 0.985},
	},
	"deep": {
		Name:      "deep",
		Objective: ObjectiveValidatedWorkPerDollar,
		Workers:   2,
		GPU: GPUPolicy{PreferredModels: []string{"RTX_3090"}, MaxHourlyUSD: 0.18, MinReliability: 0.98},
	},
}

func roundMoney(v float64) float64 { return math.Round(v*100) / 100 }

func preferredRank(profile Profile, model string) int {
	for i, preferred := range profile.GPU.PreferredModels {
		if preferred == model { return i }
	}
	return 1 << 30
}

func SelectOffers(profile Profile, offers []Offer) []Offer {
	qualified := make([]Offer, 0, len(offers))
	for _, offer := range offers {
		if preferredRank(profile, offer.GPUModel) == 1<<30 { continue }
		if offer.HourlyUSD > profile.GPU.MaxHourlyUSD || offer.Reliability < profile.GPU.MinReliability { continue }
		qualified = append(qualified, offer)
	}

	sort.SliceStable(qualified, func(i, j int) bool {
		a, b := qualified[i], qualified[j]
		rankA, rankB := preferredRank(profile, a.GPUModel), preferredRank(profile, b.GPUModel)
		if rankA != rankB { return rankA < rankB }
		if profile.Objective == ObjectiveLatency && a.DLPerf != b.DLPerf { return a.DLPerf > b.DLPerf }
		if a.HourlyUSD != b.HourlyUSD { return a.HourlyUSD < b.HourlyUSD }
		return a.ID < b.ID
	})

	if len(qualified) > profile.Workers { qualified = qualified[:profile.Workers] }
	return qualified
}

func CreateSessionPlan(profile Profile, hours float64, offers []Offer) (SessionPlan, error) {
	if hours <= 0 || math.IsNaN(hours) || math.IsInf(hours, 0) { return SessionPlan{}, errors.New("hours must be greater than zero") }
	if profile.Workers <= 0 { return SessionPlan{}, errors.New("profile workers must be greater than zero") }
	selected := SelectOffers(profile, offers)
	if len(selected) < profile.Workers { return SessionPlan{}, fmt.Errorf("need %d qualifying worker(s), found %d", profile.Workers, len(selected)) }
	workers := make([]PlannedWorker, 0, len(selected))
	total := 0.0
	for _, offer := range selected {
		cost := roundMoney(offer.HourlyUSD * hours)
		total += cost
		workers = append(workers, PlannedWorker{Offer: offer, EstimatedSessionUSD: cost})
	}
	return SessionPlan{Profile: profile, Hours: hours, Workers: workers, EstimatedTotalUSD: roundMoney(total)}, nil
}
