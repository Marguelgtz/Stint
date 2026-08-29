package core

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

type Objective string

const (
	ObjectiveLatency                Objective = "latency"
	ObjectiveValidatedWorkPerDollar Objective = "validated_work_per_dollar"
)

type RejectionReason string

const (
	RejectGPU         RejectionReason = "gpu"
	RejectPrice       RejectionReason = "price"
	RejectReliability RejectionReason = "reliability"
	RejectPorts       RejectionReason = "direct_ports"
	RejectVRAM        RejectionReason = "vram"
	RejectVerified    RejectionReason = "verified"
	RejectRentable    RejectionReason = "rentable"
	RejectRented      RejectionReason = "already_rented"
)

type GPUPolicy struct {
	PreferredModels        []string `json:"preferredModels"`
	MaxHourlyUSD           float64  `json:"maxHourlyUsd"`
	MinReliability         float64  `json:"minReliability"`
	PreferredInetDownMBps  float64  `json:"preferredInetDownMBps"`
	MinDirectPorts         int      `json:"minDirectPorts"`
	MinGPURAMMB            int      `json:"minGpuRamMb"`
	PreferredGPUMaxPowerW  float64  `json:"preferredGpuMaxPowerW"`
	RequireVerified        bool     `json:"requireVerified"`
	RequireRentable        bool     `json:"requireRentable"`
	RequireNotRented       bool     `json:"requireNotRented"`
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

type OfferEvaluation struct {
	Offer      Offer             `json:"offer"`
	Qualified  bool              `json:"qualified"`
	Rejections []RejectionReason `json:"rejections,omitempty"`
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
			// Vast's REST examples use human-readable names with spaces. Local offers
			// are canonicalized before ranking, so both forms compare consistently.
			PreferredModels:       []string{"RTX 4090", "RTX 3090"},
			MaxHourlyUSD:          0.40,
			MinReliability:        0.985,
			PreferredInetDownMBps: 50,
			MinDirectPorts:        1,
			MinGPURAMMB:           24000,
			PreferredGPUMaxPowerW: 350,
			RequireVerified:       true,
			RequireRentable:       true,
			RequireNotRented:      true,
		},
		Session: SessionPolicy{DefaultHours: 5, MaxCostUSD: 2.50, StorageGB: 50},
	},
	"deep": {
		Name:      "deep",
		Objective: ObjectiveValidatedWorkPerDollar,
		Workers:   2,
		GPU: GPUPolicy{
			PreferredModels:       []string{"RTX 3090"},
			MaxHourlyUSD:          0.18,
			MinReliability:        0.98,
			PreferredInetDownMBps: 50,
			MinDirectPorts:        1,
			MinGPURAMMB:           24000,
			RequireVerified:       true,
			RequireRentable:       true,
			RequireNotRented:      true,
		},
		Session: SessionPolicy{DefaultHours: 8, MaxCostUSD: 3.00, StorageGB: 50},
	},
}

func roundMoney(v float64) float64 { return math.Round(v*100) / 100 }

func canonicalModelName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "-", "_")
	return strings.ToUpper(value)
}

func preferredRank(profile Profile, model string) int {
	model = canonicalModelName(model)
	for i, preferred := range profile.GPU.PreferredModels {
		if canonicalModelName(preferred) == model {
			return i
		}
	}
	return 1 << 30
}

func EvaluateOffer(profile Profile, offer Offer) OfferEvaluation {
	rejections := make([]RejectionReason, 0, 8)
	if preferredRank(profile, offer.GPUModel) == 1<<30 {
		rejections = append(rejections, RejectGPU)
	}
	if offer.HourlyUSD <= 0 || offer.HourlyUSD > profile.GPU.MaxHourlyUSD {
		rejections = append(rejections, RejectPrice)
	}
	if offer.Reliability < profile.GPU.MinReliability {
		rejections = append(rejections, RejectReliability)
	}
	if offer.DirectPortCount < profile.GPU.MinDirectPorts {
		rejections = append(rejections, RejectPorts)
	}
	if offer.GPURAMMB < profile.GPU.MinGPURAMMB {
		rejections = append(rejections, RejectVRAM)
	}
	if profile.GPU.RequireVerified && !offer.Verified {
		rejections = append(rejections, RejectVerified)
	}
	if profile.GPU.RequireRentable && !offer.Rentable {
		rejections = append(rejections, RejectRentable)
	}
	if profile.GPU.RequireNotRented && offer.Rented {
		rejections = append(rejections, RejectRented)
	}
	return OfferEvaluation{Offer: offer, Qualified: len(rejections) == 0, Rejections: rejections}
}

func EvaluateOffers(profile Profile, offers []Offer) []OfferEvaluation {
	result := make([]OfferEvaluation, 0, len(offers))
	for _, offer := range offers {
		result = append(result, EvaluateOffer(profile, offer))
	}
	return result
}

func Qualifies(profile Profile, offer Offer) bool {
	return EvaluateOffer(profile, offer).Qualified
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
			if a.InetDownMBps != b.InetDownMBps {
				return a.InetDownMBps > b.InetDownMBps
			}
			if a.HourlyUSD != b.HourlyUSD {
				return a.HourlyUSD < b.HourlyUSD
			}
			if a.GPUMaxPowerW != b.GPUMaxPowerW {
				return a.GPUMaxPowerW > b.GPUMaxPowerW
			}
		} else if a.HourlyUSD != b.HourlyUSD {
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
