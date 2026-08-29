package vast

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/core"
)

const DefaultBaseURL = "https://console.vast.ai"

type SearchOptions struct {
	Hours     float64
	Limit     int
	StorageGB float64
}

type Provider interface {
	SearchOffers(context.Context, core.Profile, SearchOptions) ([]core.Offer, error)
}

type Client struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

type rawOffer struct {
	ID                int64   `json:"id"`
	GPUName           string  `json:"gpu_name"`
	GPURAM            int     `json:"gpu_ram"`
	GPUMaxPower       float64 `json:"gpu_max_power"`
	DPHTotal          float64 `json:"dph_total"`
	Reliability       float64 `json:"reliability"`
	DLPerf            float64 `json:"dlperf"`
	InetDown          float64 `json:"inet_down"`
	InetUp            float64 `json:"inet_up"`
	InetDownCost      float64 `json:"inet_down_cost"`
	DirectPortCount   int     `json:"direct_port_count"`
	Geolocation       string  `json:"geolocation"`
	MachineID         int64   `json:"machine_id"`
	Verification      string  `json:"verification"`
	Rentable          bool    `json:"rentable"`
	Rented            bool    `json:"rented"`
	NumGPUs           int     `json:"num_gpus"`
}

type searchResponse struct {
	Offers []rawOffer `json:"offers"`
}

type APIError struct {
	StatusCode int
	Status     string
	Detail     string
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("Vast API %s: %s", e.Status, e.Detail)
	}
	return "Vast API " + e.Status
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:  strings.TrimSpace(apiKey),
		BaseURL: DefaultBaseURL,
		HTTPClient: &http.Client{
			Timeout: 20 * time.Second,
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
		return decodeAPIError(resp)
	}
	return nil
}

func (c *Client) VerifySearchAccess(ctx context.Context) error {
	payload := map[string]any{
		"limit":    1,
		"type":     "ondemand",
		"verified": map[string]any{"eq": true},
		"rentable": map[string]any{"eq": true},
		"rented":   map[string]any{"eq": false},
	}
	_, err := c.search(ctx, payload)
	if apiErr := (*APIError)(nil); errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		return errors.New("Vast API key lacks misc/search permission")
	}
	return err
}

func (c *Client) SearchOffers(ctx context.Context, profile core.Profile, options SearchOptions) ([]core.Offer, error) {
	if c.APIKey == "" {
		return nil, errors.New("Vast API key is empty")
	}
	if options.Hours <= 0 || math.IsNaN(options.Hours) || math.IsInf(options.Hours, 0) {
		return nil, errors.New("search hours must be greater than zero")
	}
	limit := options.Limit
	if limit <= 0 || limit > 250 {
		limit = 100
	}
	storageGB := options.StorageGB
	if storageGB <= 0 {
		storageGB = profile.Session.StorageGB
	}
	if len(profile.GPU.PreferredModels) == 0 {
		return nil, errors.New("profile has no preferred GPU models")
	}

	gpuFilter := map[string]any{"eq": profile.GPU.PreferredModels[0]}
	if len(profile.GPU.PreferredModels) > 1 {
		gpuFilter = map[string]any{"in": profile.GPU.PreferredModels}
	}

	payload := map[string]any{
		"limit":               limit,
		"type":                "ondemand",
		"verified":            map[string]any{"eq": true},
		"rentable":            map[string]any{"eq": true},
		"rented":              map[string]any{"eq": false},
		"gpu_name":            gpuFilter,
		"num_gpus":            map[string]any{"eq": 1},
		"reliability":         map[string]any{"gte": profile.GPU.MinReliability},
		"dph_total":           map[string]any{"lte": profile.GPU.MaxHourlyUSD},
		"duration":            map[string]any{"gte": int(math.Ceil(options.Hours * 3600))},
		"allocated_storage":   storageGB,
		"order":               [][]string{{"dlperf", "desc"}, {"dph_total", "asc"}},
	}
	if profile.GPU.MinInetDownMBps > 0 {
		payload["inet_down"] = map[string]any{"gte": profile.GPU.MinInetDownMBps}
	}
	if profile.GPU.MinDirectPorts > 0 {
		payload["direct_port_count"] = map[string]any{"gte": profile.GPU.MinDirectPorts}
	}
	if profile.GPU.MinGPURAMMB > 0 {
		payload["gpu_ram"] = map[string]any{"gte": profile.GPU.MinGPURAMMB}
	}
	if profile.GPU.MinGPUMaxPowerW > 0 {
		payload["gpu_max_power"] = map[string]any{"gte": profile.GPU.MinGPUMaxPowerW}
	}

	raw, err := c.search(ctx, payload)
	if err != nil {
		if apiErr := (*APIError)(nil); errors.As(err, &apiErr) {
			switch apiErr.StatusCode {
			case http.StatusUnauthorized:
				return nil, errors.New("Vast API key was rejected; run: stint auth vast")
			case http.StatusForbidden:
				return nil, errors.New("Vast API key lacks misc/search permission")
			case http.StatusTooManyRequests:
				return nil, errors.New("Vast API rate limit reached; retry later")
			}
		}
		return nil, err
	}

	offers := make([]core.Offer, 0, len(raw))
	for _, item := range raw {
		offers = append(offers, normalizeOffer(item))
	}
	return offers, nil
}

func (c *Client) search(ctx context.Context, payload map[string]any) ([]rawOffer, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode Vast offer search: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/api/v0/bundles"), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build Vast offer search request: %w", err)
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("search Vast offers: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, decodeAPIError(resp)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 4<<20))
	var result searchResponse
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Vast offer search response: %w", err)
	}
	return result.Offers, nil
}

func normalizeOffer(item rawOffer) core.Offer {
	return core.Offer{
		ID:                strconv.FormatInt(item.ID, 10),
		GPUModel:          canonicalGPUName(item.GPUName),
		GPURAMMB:          item.GPURAM,
		GPUMaxPowerW:      item.GPUMaxPower,
		HourlyUSD:         item.DPHTotal,
		Reliability:       item.Reliability,
		DLPerf:            item.DLPerf,
		InetDownMBps:      item.InetDown,
		InetUpMBps:        item.InetUp,
		InetDownCostPerGB: item.InetDownCost,
		DirectPortCount:   item.DirectPortCount,
		Geolocation:       item.Geolocation,
		MachineID:         item.MachineID,
		Verified:          strings.EqualFold(item.Verification, "verified"),
		Rentable:          item.Rentable,
		Rented:            item.Rented,
	}
}

func canonicalGPUName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "-", "_")
	return strings.ToUpper(value)
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
	return &http.Client{Timeout: 20 * time.Second}
}

func decodeAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	detail := strings.TrimSpace(string(body))
	var parsed struct {
		Error string `json:"error"`
		Msg   string `json:"msg"`
	}
	if json.Unmarshal(body, &parsed) == nil {
		if parsed.Msg != "" {
			detail = parsed.Msg
		} else if parsed.Error != "" {
			detail = parsed.Error
		}
	}
	return &APIError{StatusCode: resp.StatusCode, Status: resp.Status, Detail: detail}
}

func FixtureOffers(profile string) []core.Offer {
	if profile == "interactive" {
		return []core.Offer{
			{ID: "fixture-4090-fast", GPUModel: "RTX_4090", GPURAMMB: 24576, GPUMaxPowerW: 450, HourlyUSD: 0.35, Reliability: 0.995, DLPerf: 110, InetDownMBps: 500, DirectPortCount: 2, Verified: true, Rentable: true, Geolocation: "fixture-fast"},
			{ID: "fixture-4090-cheap", GPUModel: "RTX_4090", GPURAMMB: 24576, GPUMaxPowerW: 450, HourlyUSD: 0.31, Reliability: 0.99, DLPerf: 96, InetDownMBps: 500, DirectPortCount: 2, Verified: true, Rentable: true, Geolocation: "fixture-cheap"},
		}
	}
	return []core.Offer{
		{ID: "fixture-3090-a", GPUModel: "RTX_3090", GPURAMMB: 24576, HourlyUSD: 0.14, Reliability: 0.99, DLPerf: 53, InetDownMBps: 100, DirectPortCount: 2, Verified: true, Rentable: true},
		{ID: "fixture-3090-b", GPUModel: "RTX_3090", GPURAMMB: 24576, HourlyUSD: 0.15, Reliability: 0.992, DLPerf: 55, InetDownMBps: 100, DirectPortCount: 2, Verified: true, Rentable: true},
		{ID: "fixture-3090-over", GPUModel: "RTX_3090", GPURAMMB: 24576, HourlyUSD: 0.20, Reliability: 0.999, DLPerf: 58, InetDownMBps: 100, DirectPortCount: 2, Verified: true, Rentable: true},
	}
}
