package vast

import (
	"context"
	"errors"
	"github.com/Marguelgtz/Stint/internal/core"
)

type Provider interface { SearchOffers(context.Context, core.Profile) ([]core.Offer, error) }

// Client is the future live Vast marketplace adapter. Pre-V0 keeps mutating
// operations out until read-only marketplace ranking is validated.
type Client struct{}

func (Client) SearchOffers(context.Context, core.Profile) ([]core.Offer, error) {
	return nil, errors.New("live Vast offer search is not implemented yet; use fixture offers")
}

func FixtureOffers(profile string) []core.Offer {
	if profile == "interactive" {
		return []core.Offer{{ID: "fixture-4090-fast", GPUModel: "RTX_4090", HourlyUSD: 0.35, Reliability: 0.995, DLPerf: 110}, {ID: "fixture-4090-cheap", GPUModel: "RTX_4090", HourlyUSD: 0.31, Reliability: 0.99, DLPerf: 96}}
	}
	return []core.Offer{{ID: "fixture-3090-a", GPUModel: "RTX_3090", HourlyUSD: 0.14, Reliability: 0.99, DLPerf: 53}, {ID: "fixture-3090-b", GPUModel: "RTX_3090", HourlyUSD: 0.15, Reliability: 0.992, DLPerf: 55}, {ID: "fixture-3090-over", GPUModel: "RTX_3090", HourlyUSD: 0.20, Reliability: 0.999, DLPerf: 58}}
}
