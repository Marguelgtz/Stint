package vast

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/core"
)

const DefaultBaseURL = "https://console.vast.ai"

type Provider interface {
	SearchOffers(context.Context, core.Profile) ([]core.Offer, error)
}

type Client struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:  strings.TrimSpace(apiKey),
		BaseURL: DefaultBaseURL,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *Client) VerifyAuth(ctx context.Context) error {
	if c.APIKey == "" {
		return errors.New("Vast API key is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/api/v1/instances"), nil)
	if err != nil {
		return fmt.Errorf("build Vast auth request: %w", err)
	}
	c.authorize(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("contact Vast: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError("Vast authentication failed", resp)
	}
	return nil
}

func (c *Client) endpoint(path string) string {
	return strings.TrimRight(c.BaseURL, "/") + path
}

func (c *Client) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func responseError(prefix string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	detail := strings.TrimSpace(string(body))
	if detail != "" {
		return fmt.Errorf("%s (%s): %s", prefix, resp.Status, detail)
	}
	return fmt.Errorf("%s (%s)", prefix, resp.Status)
}

// SearchOffers remains intentionally non-mutating and is implemented in Phase 2.
func (c *Client) SearchOffers(context.Context, core.Profile) ([]core.Offer, error) {
	return nil, errors.New("live Vast offer search is not implemented until Phase 2")
}

func FixtureOffers(profile string) []core.Offer {
	if profile == "interactive" {
		return []core.Offer{{ID: "fixture-4090-fast", GPUModel: "RTX_4090", HourlyUSD: 0.35, Reliability: 0.995, DLPerf: 110}, {ID: "fixture-4090-cheap", GPUModel: "RTX_4090", HourlyUSD: 0.31, Reliability: 0.99, DLPerf: 96}}
	}
	return []core.Offer{{ID: "fixture-3090-a", GPUModel: "RTX_3090", HourlyUSD: 0.14, Reliability: 0.99, DLPerf: 53}, {ID: "fixture-3090-b", GPUModel: "RTX_3090", HourlyUSD: 0.15, Reliability: 0.992, DLPerf: 55}, {ID: "fixture-3090-over", GPUModel: "RTX_3090", HourlyUSD: 0.20, Reliability: 0.999, DLPerf: 58}}
}
