package vast

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/core"
)

const DefaultBaseURL = "https://console.vast.ai"
const discoveryPriceCeilingUSD = 0.60
const vastOnDemandType = "on-demand"
const defaultSearchRequestAttempts = 4
const defaultSearchRetryBaseDelay = time.Second
const maxSearchRetryDelay = 8 * time.Second

type SearchOptions struct {
	Hours              float64
	Limit              int
	StorageGB          float64
	MinCUDAMaxGood     float64
	SkipDiscoveryTrace bool
}

type SearchRetryEvent struct {
	Attempt     int
	MaxAttempts int
	Delay       time.Duration
	RateLimited bool
}

type DiscoveryStage struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type DiscoveryEmptyError struct {
	Stages []DiscoveryStage
}

func (e *DiscoveryEmptyError) Error() string {
	parts := make([]string, 0, len(e.Stages))
	for _, stage := range e.Stages {
		parts = append(parts, fmt.Sprintf("%s=%d", stage.Name, stage.Count))
	}
	return "Vast returned zero interactive candidates; discovery bisect: " + strings.Join(parts, ", ")
}

type Provider interface {
	SearchOffers(context.Context, core.Profile, SearchOptions) ([]core.Offer, error)
}

type Client struct {
	APIKey        string
	BaseURL       string
	HTTPClient    *http.Client
	OnSearchRetry func(SearchRetryEvent)
	sleepFn       func(context.Context, time.Duration) error
	jitterFn      func(time.Duration) time.Duration
}

type rawOffer struct {
	ID              int64   `json:"id"`
	GPUName         string  `json:"gpu_name"`
	GPURAM          int     `json:"gpu_ram"`
	GPUMaxPower     float64 `json:"gpu_max_power"`
	DPHTotal        float64 `json:"dph_total"`
	Reliability     float64 `json:"reliability"`
	DLPerf          float64 `json:"dlperf"`
	InetDown        float64 `json:"inet_down"`
	InetUp          float64 `json:"inet_up"`
	InetDownCost    float64 `json:"inet_down_cost"`
	DirectPortCount int     `json:"direct_port_count"`
	Geolocation     string  `json:"geolocation"`
	MachineID       int64   `json:"machine_id"`
	Verification    string  `json:"verification"`
	Rentable        bool    `json:"rentable"`
	Rented          bool    `json:"rented"`
	NumGPUs         int     `json:"num_gpus"`
}

type searchResponse struct {
	Offers []rawOffer `json:"offers"`
}

type APIError struct {
	StatusCode int
	Status     string
	Detail     string
	RetryAfter string
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("Vast API %s: %s", e.Status, e.Detail)
	}
	return "Vast API " + e.Status
}

type searchTransportError struct {
	err error
}

func (e *searchTransportError) Error() string {
	return "search Vast offers: " + e.err.Error()
}

func (e *searchTransportError) Unwrap() error {
	return e.err
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
		"type":     vastOnDemandType,
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
	limit, storageGB, err := validateSearch(profile, options)
	if err != nil {
		return nil, err
	}

	payload := c.discoveryPayload(profile, options.Hours, limit, storageGB)
	payload = applyMinCUDARequirement(options.MinCUDAMaxGood, payload)
	raw, err := c.search(ctx, payload)
	if err != nil {
		return nil, classifySearchError(err)
	}
	if len(raw) == 0 {
		if options.SkipDiscoveryTrace {
			return []core.Offer{}, nil
		}
		stages, traceErr := c.traceDiscovery(ctx, profile, options.Hours, limit, storageGB)
		if traceErr != nil {
			return nil, traceErr
		}
		if options.MinCUDAMaxGood > 0 {
			stages = append(stages, DiscoveryStage{Name: fmt.Sprintf("cuda>=%.1f", options.MinCUDAMaxGood), Count: 0})
		}
		return nil, &DiscoveryEmptyError{Stages: stages}
	}

	offers := make([]core.Offer, 0, len(raw))
	for _, item := range raw {
		offers = append(offers, normalizeOffer(item))
	}
	return offers, nil
}

func validateSearch(profile core.Profile, options SearchOptions) (int, float64, error) {
	if options.Hours <= 0 || math.IsNaN(options.Hours) || math.IsInf(options.Hours, 0) {
		return 0, 0, errors.New("search hours must be greater than zero")
	}
	if options.MinCUDAMaxGood < 0 || math.IsNaN(options.MinCUDAMaxGood) || math.IsInf(options.MinCUDAMaxGood, 0) {
		return 0, 0, errors.New("minimum CUDA compatibility must be zero or greater")
	}
	if len(profile.GPU.PreferredModels) == 0 {
		return 0, 0, errors.New("profile has no preferred GPU models")
	}
	limit := options.Limit
	if limit <= 0 || limit > 250 {
		limit = 250
	}
	storageGB := options.StorageGB
	if storageGB <= 0 {
		storageGB = profile.Session.StorageGB
	}
	return limit, storageGB, nil
}

func (c *Client) discoveryPayload(profile core.Profile, hours float64, limit int, storageGB float64) map[string]any {
	gpuFilter := map[string]any{"eq": profile.GPU.PreferredModels[0]}
	if len(profile.GPU.PreferredModels) > 1 {
		gpuFilter = map[string]any{"in": profile.GPU.PreferredModels}
	}
	return map[string]any{
		"limit":             limit,
		"type":              vastOnDemandType,
		"verified":          map[string]any{"eq": true},
		"rentable":          map[string]any{"eq": true},
		"rented":            map[string]any{"eq": false},
		"gpu_name":          gpuFilter,
		"num_gpus":          map[string]any{"eq": 1},
		"dph_total":         map[string]any{"lte": discoveryPriceCeilingUSD},
		"duration":          map[string]any{"gte": int(math.Ceil(hours * 3600))},
		"allocated_storage": storageGB,
		"order":             [][]string{{"dlperf", "desc"}, {"dph_total", "asc"}},
	}
}

func (c *Client) traceDiscovery(ctx context.Context, profile core.Profile, hours float64, limit int, storageGB float64) ([]DiscoveryStage, error) {
	gpuFilter := map[string]any{"eq": profile.GPU.PreferredModels[0]}
	if len(profile.GPU.PreferredModels) > 1 {
		gpuFilter = map[string]any{"in": profile.GPU.PreferredModels}
	}

	payloads := []struct {
		name    string
		payload map[string]any
	}{
		{
			name: "rentable",
			payload: map[string]any{
				"limit": limit, "type": vastOnDemandType,
				"verified": map[string]any{"eq": true}, "rentable": map[string]any{"eq": true}, "rented": map[string]any{"eq": false},
			},
		},
		{
			name: "gpu",
			payload: map[string]any{
				"limit": limit, "type": vastOnDemandType,
				"verified": map[string]any{"eq": true}, "rentable": map[string]any{"eq": true}, "rented": map[string]any{"eq": false},
				"gpu_name": gpuFilter,
			},
		},
		{
			name: "one_gpu",
			payload: map[string]any{
				"limit": limit, "type": vastOnDemandType,
				"verified": map[string]any{"eq": true}, "rentable": map[string]any{"eq": true}, "rented": map[string]any{"eq": false},
				"gpu_name": gpuFilter, "num_gpus": map[string]any{"eq": 1},
			},
		},
		{
			name: "duration",
			payload: map[string]any{
				"limit": limit, "type": vastOnDemandType,
				"verified": map[string]any{"eq": true}, "rentable": map[string]any{"eq": true}, "rented": map[string]any{"eq": false},
				"gpu_name": gpuFilter, "num_gpus": map[string]any{"eq": 1},
				"duration": map[string]any{"gte": int(math.Ceil(hours * 3600))},
			},
		},
		{
			name: "price",
			payload: map[string]any{
				"limit": limit, "type": vastOnDemandType,
				"verified": map[string]any{"eq": true}, "rentable": map[string]any{"eq": true}, "rented": map[string]any{"eq": false},
				"gpu_name": gpuFilter, "num_gpus": map[string]any{"eq": 1},
				"duration":  map[string]any{"gte": int(math.Ceil(hours * 3600))},
				"dph_total": map[string]any{"lte": discoveryPriceCeilingUSD},
			},
		},
		{
			name:    "storage",
			payload: c.discoveryPayload(profile, hours, limit, storageGB),
		},
	}

	stages := make([]DiscoveryStage, 0, len(payloads))
	for _, item := range payloads {
		raw, err := c.search(ctx, item.payload)
		if err != nil {
			return nil, classifySearchError(err)
		}
		stages = append(stages, DiscoveryStage{Name: item.name, Count: len(raw)})
	}
	return stages, nil
}

func classifySearchError(err error) error {
	if apiErr := (*APIError)(nil); errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized:
			return errors.New("Vast API key was rejected; run: stint auth vast")
		case http.StatusForbidden:
			return errors.New("Vast API key lacks misc/search permission")
		case http.StatusTooManyRequests:
			return errors.New("Vast API rate limit remained active after retries; retry later")
		}
	}
	return err
}

func (c *Client) search(ctx context.Context, payload map[string]any) ([]rawOffer, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode Vast offer search: %w", err)
	}

	for attempt := 1; attempt <= defaultSearchRequestAttempts; attempt++ {
		raw, searchErr := c.searchOnce(ctx, body)
		if searchErr == nil {
			return raw, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt == defaultSearchRequestAttempts || !isTransientSearchError(searchErr) {
			return nil, searchErr
		}

		delay := c.searchRetryDelay(attempt, searchErr)
		if c.OnSearchRetry != nil {
			c.OnSearchRetry(SearchRetryEvent{
				Attempt:     attempt,
				MaxAttempts: defaultSearchRequestAttempts,
				Delay:       delay,
				RateLimited: isRateLimitError(searchErr),
			})
		}
		if sleepErr := c.sleep(ctx, delay); sleepErr != nil {
			return nil, sleepErr
		}
	}
	return nil, errors.New("Vast marketplace search retry loop exited unexpectedly")
}

func (c *Client) searchOnce(ctx context.Context, body []byte) ([]rawOffer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/api/v0/bundles"), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build Vast offer search request: %w", err)
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, &searchTransportError{err: err}
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

func isTransientSearchError(err error) bool {
	var transportErr *searchTransportError
	if errors.As(err, &transportErr) {
		return true
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.StatusCode {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func isRateLimitError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests
}

func (c *Client) searchRetryDelay(attempt int, err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if retryAfter, ok := parseRetryAfter(apiErr.RetryAfter, time.Now()); ok {
			return retryAfter
		}
	}

	delay := defaultSearchRetryBaseDelay << (attempt - 1)
	if delay > maxSearchRetryDelay {
		delay = maxSearchRetryDelay
	}
	return delay + c.retryJitter(delay)
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := when.Sub(now)
	if delay < 0 {
		delay = 0
	}
	return delay, true
}

func (c *Client) retryJitter(delay time.Duration) time.Duration {
	if c.jitterFn != nil {
		return c.jitterFn(delay)
	}
	maxJitter := delay / 4
	if maxJitter <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(maxJitter) + 1))
}

func (c *Client) sleep(ctx context.Context, delay time.Duration) error {
	if c.sleepFn != nil {
		return c.sleepFn(ctx, delay)
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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
	return &APIError{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Detail:     detail,
		RetryAfter: resp.Header.Get("Retry-After"),
	}
}

func FixtureOffers(profile string) []core.Offer {
	if profile == "interactive" {
		return []core.Offer{
			{ID: "fixture-4090-fast", GPUModel: "RTX_4090", GPURAMMB: 24576, GPUMaxPowerW: 450, HourlyUSD: 0.35, Reliability: 0.995, DLPerf: 110, InetDownMBps: 500, DirectPortCount: 2, Verified: true, Rentable: true, Geolocation: "fixture-fast"},
			{ID: "fixture-4090-cheap", GPUModel: "RTX_4090", GPURAMMB: 24576, GPUMaxPowerW: 300, HourlyUSD: 0.31, Reliability: 0.99, DLPerf: 96, InetDownMBps: 40, DirectPortCount: 2, Verified: true, Rentable: true, Geolocation: "fixture-cheap"},
		}
	}
	return []core.Offer{
		{ID: "fixture-3090-a", GPUModel: "RTX_3090", GPURAMMB: 24576, HourlyUSD: 0.14, Reliability: 0.99, DLPerf: 53, InetDownMBps: 100, DirectPortCount: 2, Verified: true, Rentable: true},
		{ID: "fixture-3090-b", GPUModel: "RTX_3090", GPURAMMB: 24576, HourlyUSD: 0.15, Reliability: 0.992, DLPerf: 55, InetDownMBps: 100, DirectPortCount: 2, Verified: true, Rentable: true},
		{ID: "fixture-3090-over", GPUModel: "RTX_3090", GPURAMMB: 24576, HourlyUSD: 0.20, Reliability: 0.999, DLPerf: 58, InetDownMBps: 100, DirectPortCount: 2, Verified: true, Rentable: true},
	}
}
