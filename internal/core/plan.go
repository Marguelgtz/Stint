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
	PreferredModels   []string `json:"preferredModels"`
	MaxHourlyUSD      float64  `json:"maxHourlyUsd"`
	MinReliability    float64  `json:"minReliability"`
	MinInetDownMBps   float64  `json:"minInetDownMBps"`
	MinDirectPorts    int      `json:"minDirectPorts"`
	MinGPURAMMB       int      `json:"minGpuRamMb"`
	MinGPUMaxPowerW   float64  `json:"minGpuMaxPowerW"`
	RequireVerified   bool     `json:"requireVerified"`
	RequireRentable   bool     `json:"requireRentable"`
	RequireNotRented  bool     `json:"requireNotRented"`
}

type SessionPolicy struct {
	DefaultHours float64 `json:"defaultHours"`
	MaxCostUSD   float64 `json:"maxCostUsd"`
	StorageGB    float64 `json:"storageGb"`
}

type Profile struct {
	Name      string        `json:"name"`
	Objective Objective     `json:"objective"`
	Workers   int           `json:"workers"`
	GPU       GPUPolicy     `json:"gpu"`
	Session   SessionPolicy `json:"session"`
}

type Offer struct {
	ID                string  `json:"id"`
	GPUModel          string  `json:"gpuModel"`
	GPURAMMB          int     `json:"gpuRamMb,omitempty"`
	GPUMaxPowerW      float64 `json:"gpuMaxPowerW,omitempty"`
	HourlyUSD         float64 `json:"hourlyUsd"`
	Reliability       float64 `json:"reliability"`
	DLPerf            float64 `json:"dlperf,omitempty"`
	InetDownMBps      float64 `json:"inetDownMBps,omitempty"`
	InetUpMBps        float64 `json:"inetUpMBps,omitempty"`
	InetDownCostPerGB float64 `json:"inetDownCostPerGb,omitempty"`
	DirectPortCount   int     `json:"directPortCount,omitempty"`
	Geolocation       string  `json:"geolocation,omitempty"`
	MachineID         int64   `json:"machineId,omitempty"`
	Verified          bool    `json:"verified"`
	Rentable          bool    `json:"rentable"`
	Rented            bool    `json:"rented"`
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
		GPU: GPUPolicy{
			PreferredModels:  []string{"RTX_4090"},
			MaxHourlyUSD:     0.40,
			MinReliability:   0.985,
			MinInetDownMBps:  100,
			MinDirectPorts:   1,
			MinGPURAMMB:      24000,
			MinGPUMaxPowerW:  350,
			RequireVerified:  true,
			RequireRentable:  true,
			RequireNotRented: true,
		},
		Session: SessionPolicy{DefaultHours: 5, MaxCostUSD: 2.50, StorageGB: 50},
	},
	"deep": {
		Name:      "deep",
		Objective: ObjectiveValidatedWorkPerDollar,
		Workers:   2,
		GPU: GPUPolicy{
			PreferredModels:  []string{"RTX_3090"},
			MaxHourlyUSD:     0.18,
			MinReliability:   0.98,
			MinInetDownMBps:  50,
			MinDirectPorts:   1,
			MinGPURAMMB:      24000,
			RequireVerified:  true,
			RequireRentable:  true,
			RequireNotRented: true,
		},
		Session: SessionPolicy{DefaultHours: 8, MaxCostUSD: 3.00, StorageGB: 50},
	},
}

func roundMoney(v float64) float64 { return math.Round(v*100) / 100 }

func preferredRank(profile Profile, model string) int {
	for i, preferred := range profile.GPU.PreferredModels {
		if preferred == model {
			return i
		}
	}
	return 1 << 30
}

func Qualifies(profile Profile, offer Offer) bool {
	if preferredRank(profile, offer.GPUModel) == 1<<30 {
		return false
	}
	if offer.HourlyUSD <= 0 || offer.HourlyUSD > profile.GPU.MaxHourlyUSD {
		return false
	}
	if offer.Reliability < profile.GPU.MinReliability {
		return false
	}
	if offer.InetDownMBps < profile.GPU.MinInetDownMBps {
		return false
	}
	if offer.DirectPortCount < profile.GPU.MinDirectPorts {
		return false
	}
	if offer.GPURAMMB < profile.GPU.MinGPURAMMB {
		return false
	}
	if profile.GPU.MinGPUMaxPowerW > 0 && offer.GPUMaxPowerW < profile.GPU.MinGPUMaxPowerW {
		return false
	}
	if profile.GPU.RequireVerified && !offer.Verified {
		return false
	}
	if profile.GPU.RequireRentable && !offer.Rentable {
		return false
	}
	if profile.GPU.RequireNotRented && offer.Rented {
		return false
	}
	return true
}

func RankOffers(profile Profile, offers []Offer) []Offer {
	qualified := make([]Offer, 0, len(offers))
	for _, offer := range offers {
		if Qualifies(profile, offer) {
			qualified = append(qualified, offer)
		}
	}

	sort.SliceStable(qualified, func(i, j int) bool {
		a, b := qualified[i], qualified[j]
		rankA, rankB := preferredRank(profile, a.GPUModel), preferredRank(profile, b.GPUModel)
		if rankA != rankB {
			return rankA < rankB
		}
		if profile.Objective == ObjectiveLatency {
			if a.DLPerf != b.DLPerf {
				return a.DLPerf > b.DLPerf
			}
			if a.Reliability != b.Reliability {
				return a.Reliability > b.Reliability
			}
			if a.GPUMaxPowerW != b.GPUMaxPowerW {
				return a.GPUMaxPowerW > b.GPUMaxPowerW
			}
		}
		if a.HourlyUSD != b.HourlyUSD {
			return a.HourlyUSD < b.HourlyUSD
		}
		return a.ID < b.ID
	})
	return qualified
}

func SelectOffers(profile Profile, offers []Offer) []Offer {
	ranked := RankOffers(profile, offers)
	if len(ranked) > profile.Workers {
		return ranked[:profile.Workers]
	}
	return ranked
}

func CreateSessionPlan(profile Profile, hours float64, offers []Offer) (SessionPlan, error) {
	if hours <= 0 || math.IsNaN(hours) || math.IsInf(hours, 0) {
		return SessionPlan{}, errors.New("hours must be greater than zero")
	}
	if profile.Workers <= 0 {
		return SessionPlan{}, errors.New("profile workers must be greater than zero")
	}
	selected := SelectOffers(profile, offers)
	if len(selected) < profile.Workers {
		return SessionPlan{}, fmt.Errorf("need %d qualifying worker(s), found %d", profile.Workers, len(selected))
	}
	workers := make([]PlannedWorker, 0, len(selected))
	total := 0.0
	for _, offer := range selected {
		cost := roundMoney(offer.HourlyUSD * hours)
		total += cost
		workers = append(workers, PlannedWorker{Offer: offer, EstimatedSessionUSD: cost})
	}
	total = roundMoney(total)
	if profile.Session.MaxCostUSD > 0 && total > profile.Session.MaxCostUSD {
		return SessionPlan{}, fmt.Errorf("estimated session cost $%.2f exceeds profile ceiling $%.2f", total, profile.Session.MaxCostUSD)
	}
	return SessionPlan{Profile: profile, Hours: hours, Workers: workers, EstimatedTotalUSD: total}, nil
}
