// infer_probe.go implements the read-only live-inference observation used by
// `stint status --refresh` and the dashboard. It polls the local tunnel
// (127.0.0.1:8409) for the runtime's /metrics and /slots endpoints over two
// epochs and derives concurrent activity, per-lane prompt depth, and token
// rates. It never sends an inference request and never mutates the remote
// session.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// inferenceProbeInterval is the gap between the two scraped epochs; token
// rates are derived from counter deltas across that window.
var inferenceProbeInterval = 1200 * time.Millisecond

// inferenceFetchTimeout bounds each /metrics or /slots fetch of one epoch.
var inferenceFetchTimeout = 1200 * time.Millisecond

// Runtime-agnostic Prometheus metric names. llama.cpp (b10472) and NInfer
// both publish the llamacpp:* series; NInfer additionally publishes the
// ninfer:* series.
const (
	metricPromptTokensTotal    = "llamacpp:prompt_tokens_total"
	metricPromptCachedTotal    = "llamacpp:prompt_tokens_cached_total"
	metricPredictedTokensTotal = "llamacpp:tokens_predicted_total"
	metricRequestsProcessing   = "llamacpp:requests_processing"
	metricRequestsDeferred     = "llamacpp:requests_deferred"

	metricNInferPrefixCacheHit = "ninfer:prefix_cache_hit_tokens_total"
	metricNInferDraftTokens    = "ninfer:draft_tokens_total"
	metricNInferDraftAccepted  = "ninfer:draft_accepted_tokens_total"
	metricLlamaSpecDrafts      = "llamacpp:spec_decode_num_draft_tokens_total"
	metricLlamaSpecAccepted    = "llamacpp:spec_decode_num_accepted_tokens_total"
)

// inferenceEpoch is one scrape of /metrics and /slots through the tunnel.
type inferenceEpoch struct {
	At            time.Time
	Counters      map[string]float64
	Lanes         []inferenceLane
	MetricsStatus int
	SlotsStatus   int
	MetricsErr    string
	SlotsErr      string
}

func (e inferenceEpoch) metricsOK() bool {
	return e.MetricsErr == "" && e.MetricsStatus == http.StatusOK
}
func (e inferenceEpoch) slotsOK() bool { return e.SlotsErr == "" && e.SlotsStatus == http.StatusOK }
func (e inferenceEpoch) usable() bool  { return e.metricsOK() || e.slotsOK() }

func probeInference(ctx context.Context) inferenceTelemetry {
	return probeInferenceBase(ctx, fmt.Sprintf("http://127.0.0.1:%d", clinePort))
}

// probeInferenceBase runs the two-epoch observation against any base URL so
// tests can point it at an httptest server; probeInference pins it to the
// local tunnel both runtimes publish through.
func probeInferenceBase(ctx context.Context, base string) inferenceTelemetry {
	sampledAt := time.Now().UTC()
	result := inferenceTelemetry{Refreshed: true, Meta: sampleMeta{SampledAt: sampledAt}}

	first := fetchInferenceEpoch(ctx, base)
	if !first.usable() {
		result.UnavailableReason = inferenceUnavailableReason(first)
		return result
	}
	inferFromEpoch(&result, first)

	select {
	case <-ctx.Done():
		return result
	case <-time.After(inferenceProbeInterval):
	}
	second := fetchInferenceEpoch(ctx, base)
	if !second.usable() {
		return result
	}
	inferFromEpoch(&result, second)
	applyInferenceRates(&result, first, second)
	return result
}
func fetchInferenceEpoch(ctx context.Context, base string) inferenceEpoch {
	epoch := inferenceEpoch{At: time.Now().UTC(), Counters: make(map[string]float64)}
	client := &http.Client{Timeout: inferenceFetchTimeout}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		body, status, err := inferenceHTTPGet(ctx, client, base+"/metrics")
		if err != nil {
			epoch.MetricsErr = err.Error()
			return
		}
		epoch.MetricsStatus = status
		switch {
		case status == http.StatusOK:
			epoch.Counters = parsePrometheusText(body)
		case status == http.StatusNotImplemented:
			epoch.MetricsErr = "metrics endpoint disabled"
		default:
			epoch.MetricsErr = fmt.Sprintf("metrics returned status %d", status)
		}
	}()
	go func() {
		defer wg.Done()
		body, status, err := inferenceHTTPGet(ctx, client, base+"/slots")
		if err != nil {
			epoch.SlotsErr = err.Error()
			return
		}
		epoch.SlotsStatus = status
		switch {
		case status == http.StatusOK:
			epoch.Lanes = parseSlotLanes(body)
		case status == http.StatusNotImplemented:
			epoch.SlotsErr = "slots endpoint disabled"
		default:
			epoch.SlotsErr = fmt.Sprintf("slots returned status %d", status)
		}
	}()
	wg.Wait()
	return epoch
}

func inferenceHTTPGet(ctx context.Context, client *http.Client, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// inferFromEpoch fills concurrency, per-lane state, and ratio fields from a
// single usable epoch. Agents means active inference work only; retained or
// resident lanes do not count as active. Counter-based fields prefer llama.cpp
// metric names and fall back to NInfer names so one probe works on both
// runtimes.
func inferFromEpoch(result *inferenceTelemetry, epoch inferenceEpoch) {
	result.Available = true
	result.Lanes = epoch.Lanes
	result.Processing = 0
	result.Deferred = 0
	result.Agents = 0
	result.ResidentDepth = 0
	for _, lane := range epoch.Lanes {
		depth := lane.NPrompt
		if depth < 0 {
			depth = 0
		}
		if lane.Processing {
			result.Processing++
			result.Agents++
		}
		if depth > result.ResidentDepth {
			result.ResidentDepth = depth
		}
	}
	if epoch.metricsOK() {
		result.Processing = int(counterOrDefault(epoch.Counters, metricRequestsProcessing, float64(result.Processing)))
		// requests_processing is the engine's authoritative current activity
		// gauge when available. Do not turn retained context into "active"
		// agents just because it remains resident in a lane.
		result.Agents = result.Processing
		result.Deferred = int(counterOrDefault(epoch.Counters, metricRequestsDeferred, 0))
	}
	result.CacheReuseRatio = inferenceRatio(epoch.Counters,
		[]string{metricPromptCachedTotal, metricNInferPrefixCacheHit},
		[]string{metricPromptTokensTotal})
	result.SpecAcceptRatio = inferenceRatio(epoch.Counters,
		[]string{metricNInferDraftAccepted, metricLlamaSpecAccepted},
		[]string{metricNInferDraftTokens, metricLlamaSpecDrafts})
}

// applyInferenceRates converts cumulative counter deltas between two epochs
// into tokens/second. Rates stay nil when a runtime does not publish the
// counter, so absence is distinguishable from a zero rate.
func applyInferenceRates(result *inferenceTelemetry, first, second inferenceEpoch) {
	elapsed := second.At.Sub(first.At).Seconds()
	if elapsed <= 0 {
		return
	}
	if rate := counterDeltaRate(first.Counters, second.Counters, metricPredictedTokensTotal, elapsed); rate != nil {
		result.DecodeTokensSec = rate
	}
	if rate := counterDeltaRate(first.Counters, second.Counters, metricPromptTokensTotal, elapsed); rate != nil {
		result.PrefillTokensSec = rate
	}
}

func inferenceUnavailableReason(epoch inferenceEpoch) string {
	switch {
	case epoch.MetricsStatus == http.StatusNotImplemented && epoch.SlotsStatus == http.StatusNotImplemented:
		return "runtime serves neither /metrics nor /slots (start llama.cpp with --metrics --slots)"
	case strings.Contains(epoch.MetricsErr, "connection refused") || strings.Contains(epoch.SlotsErr, "connection refused"):
		return "inference endpoint unreachable through the tunnel"
	}
	return "inference endpoints unavailable: metrics=" + epoch.MetricsErr + " slots=" + epoch.SlotsErr
}

func counterDeltaRate(prev, cur map[string]float64, name string, elapsed float64) *float64 {
	start, okStart := prev[name]
	end, okCur := cur[name]
	if !okStart || !okCur || elapsed <= 0 {
		return nil
	}
	rate := (end - start) / elapsed
	if rate < 0 {
		return nil
	}
	return &rate
}

// inferenceRatio returns numerator/denominator, taking the first positive
// counter for each side in priority order (so a zero-valued shared llamacpp
// series cannot shadow a NInfer-only series), or nil when no side is
// positive. The denominator clamps at 1.0 so a partial-cache window can
// never report reuse above 100%.
func inferenceRatio(counters map[string]float64, numeratorNames []string, denominatorNames ...[]string) *float64 {
	var numerator float64
	for _, name := range numeratorNames {
		if value, ok := counters[name]; ok && value > 0 {
			numerator = value
			break
		}
	}
	if numerator <= 0 {
		return nil
	}
	var denominator float64
	for _, group := range denominatorNames {
		for _, name := range group {
			if value, ok := counters[name]; ok && value > 0 {
				denominator = value
				break
			}
		}
		if denominator > 0 {
			break
		}
	}
	if denominator <= 0 {
		return nil
	}
	ratio := numerator / denominator
	if ratio > 1 {
		ratio = 1
	}
	return &ratio
}

func counterOrDefault(counters map[string]float64, name string, fallback float64) float64 {
	if value, ok := counters[name]; ok {
		return value
	}
	return fallback
}

// parsePrometheusText parses the Prometheus text exposition format into flat
// metric names. It accepts both llama.cpp's full output (with # HELP and
// # TYPE comments) and NInfer's flat `name value` subset, and strips label
// blocks. Invalid samples are skipped rather than failing the probe.
func parsePrometheusText(body []byte) map[string]float64 {
	out := make(map[string]float64)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.LastIndexAny(line, " \t")
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		if brace := strings.IndexByte(name, '{'); brace >= 0 {
			name = strings.TrimSpace(name[:brace])
		}
		if name == "" {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(line[idx+1:]), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		out[name] = value
	}
	return out
}

// parseSlotLanes decodes the /slots JSON array. Both llama.cpp and NInfer
// publish an array of per-lane objects; unknown fields (llama.cpp's
// prompt/generated text, NInfer's checkpoints) are ignored.
func parseSlotLanes(body []byte) []inferenceLane {
	var lanes []inferenceLane
	if err := json.Unmarshal(body, &lanes); err != nil {
		return nil
	}
	return lanes
}
