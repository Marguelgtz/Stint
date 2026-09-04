package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testInferenceTiming speeds up the probe for tests and restores the
// production timings afterwards.
func testInferenceTiming(t *testing.T) {
	t.Helper()
	oldInterval, oldTimeout := inferenceProbeInterval, inferenceFetchTimeout
	inferenceProbeInterval = 30 * time.Millisecond
	inferenceFetchTimeout = 500 * time.Millisecond
	t.Cleanup(func() {
		inferenceProbeInterval, inferenceFetchTimeout = oldInterval, oldTimeout
	})
}

// llamaMetricsFixture is a llama.cpp b10472 style /metrics body: Prometheus
// text with # HELP / # TYPE comments, flat llamacpp:* counters, and one
// labeled series that must have its label block stripped.
func llamaMetricsFixture(epoch int) string {
	var b strings.Builder
	b.WriteString("# HELP llamacpp:prompt_tokens_total Number of processed prompt tokens\n")
	b.WriteString("# TYPE llamacpp:prompt_tokens_total counter\n")
	b.WriteString(fmt.Sprintf("llamacpp:prompt_tokens_total %d\n", 1000+epoch*2000))
	b.WriteString(fmt.Sprintf("llamacpp:prompt_tokens_cached_total %d\n", 800+epoch*1600))
	b.WriteString(fmt.Sprintf("llamacpp:tokens_predicted_total %d\n", 500+epoch*1000))
	b.WriteString(fmt.Sprintf("llamacpp:requests_processing %d\n", 1))
	b.WriteString("llamacpp:requests_deferred 0\n")
	b.WriteString("# TYPE llamacpp:spec_decode_num_draft_tokens_total counter\n")
	b.WriteString(fmt.Sprintf("llamacpp:spec_decode_num_draft_tokens_total %d\n", 900+epoch*300))
	b.WriteString(fmt.Sprintf("llamacpp:spec_decode_num_accepted_tokens_total %d\n", 630+epoch*210))
	b.WriteString("llamacpp:server_load{slot=\"0\"} 1.0\n")
	return b.String()
}

// ninferMetricsFixture is the NInfer flat exposition subset: bare
// `name value` lines without # HELP/# TYPE comments, publishing the ninfer:*
// series next to the shared llamacpp:* series.
func ninferMetricsFixture(epoch int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("llamacpp:prompt_tokens_total %d\n", 2048+epoch*4096))
	b.WriteString("llamacpp:prompt_tokens_cached_total 0\n")
	b.WriteString(fmt.Sprintf("llamacpp:tokens_predicted_total %d\n", 716+epoch*1432))
	b.WriteString(fmt.Sprintf("llamacpp:requests_processing %d\n", 2))
	b.WriteString(fmt.Sprintf("llamacpp:requests_deferred %d\n", 1))
	b.WriteString(fmt.Sprintf("ninfer:prefix_cache_hit_tokens_total %d\n", 1536+epoch*3072))
	b.WriteString(fmt.Sprintf("ninfer:draft_tokens_total %d\n", 1000+epoch*500))
	b.WriteString(fmt.Sprintf("ninfer:draft_accepted_tokens_total %d\n", 700+epoch*350))
	return b.String()
}

// llamaSlotsFixture mirrors llama.cpp b10472 /slots: one busy slot, one idle
// slot, including fields the lane parser must ignore.
const llamaSlotsFixture = `[
  {"id": 0, "n_ctx": 16384, "n_batch": 2048, "speculative": true, "state": 1,
   "is_processing": true, "n_prompt_tokens": 34210, "n_prompt_tokens_cache": 29871,
   "prompt": "a very long cline conversation", "is_child": false},
  {"id": 1, "n_ctx": 16384, "n_batch": 2048, "speculative": false, "state": 0,
   "is_processing": false, "is_child": false}
]`

// ninferSlotsFixture mirrors the real NInfer /slots schema (captured on a
// live instance, 2026-09-03): lane objects add retained (context held after
// request completion), speculative, and session_digest — a per-completion
// history fingerprint, not a stable agent identity — and checkpoints is an
// array (empty while the run has no checkpoints). n_ctx matches this
// branch's default "coding" preset; other presets launch different contexts.
const ninferSlotsFixture = `[
  {"id": 0, "n_ctx": 126976, "speculative": true, "is_processing": true, "retained": true,
   "session_digest": "a1b2c3", "n_prompt_tokens": 45000, "n_prompt_tokens_cache": 41000,
   "checkpoints": []},
  {"id": 1, "n_ctx": 126976, "speculative": true, "is_processing": false, "retained": true,
   "session_digest": "d4e5f6", "n_prompt_tokens": 0, "checkpoints": []},
  {"id": 2, "n_ctx": 126976, "speculative": true, "is_processing": false, "n_prompt_tokens": 0, "checkpoints": []}
]`

type inferenceFixtureServer struct {
	metricsHandler func(epoch int) (string, int)
	slotsHandler   func(epoch int) (string, int)
}

// newInferenceFixtureServer serves /metrics and /slots with per-call epoch
// state so the two probe epochs can return different counter values.
func newInferenceFixtureServer(t *testing.T, fixture inferenceFixtureServer) *httptest.Server {
	t.Helper()
	var metricsEpoch, slotsEpoch int32
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		epoch := int(atomic.AddInt32(&metricsEpoch, 1) - 1)
		body, status := fixture.metricsHandler(epoch)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/slots", func(w http.ResponseWriter, r *http.Request) {
		epoch := int(atomic.AddInt32(&slotsEpoch, 1) - 1)
		body, status := fixture.slotsHandler(epoch)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestParsePrometheusTextLlamaCPP(t *testing.T) {
	counters := parsePrometheusText([]byte(llamaMetricsFixture(1)))
	if counters[metricPromptTokensTotal] != 3000 {
		t.Fatalf("prompt_tokens_total = %v, want 3000", counters[metricPromptTokensTotal])
	}
	if counters[metricRequestsProcessing] != 1 {
		t.Fatalf("requests_processing = %v, want 1", counters[metricRequestsProcessing])
	}
	if counters[metricLlamaSpecDrafts] != 1200 || counters[metricLlamaSpecAccepted] != 840 {
		t.Fatalf("spec counters = %v / %v, want 1200 / 840", counters[metricLlamaSpecDrafts], counters[metricLlamaSpecAccepted])
	}
	if _, ok := counters["llamacpp:server_load"]; !ok || counters["llamacpp:server_load"] != 1.0 {
		t.Fatal("labeled series was not reduced to its metric name")
	}
	if _, ok := counters["# TYPE llamacpp:spec_decode_num_draft_tokens_total"]; ok {
		t.Fatal("comment lines must be ignored")
	}
}

func TestParsePrometheusTextNInfer(t *testing.T) {
	counters := parsePrometheusText([]byte(ninferMetricsFixture(0)))
	if counters[metricNInferPrefixCacheHit] != 1536 {
		t.Fatalf("prefix cache hits = %v, want 1536", counters[metricNInferPrefixCacheHit])
	}
	if counters[metricNInferDraftTokens] != 1000 || counters[metricNInferDraftAccepted] != 700 {
		t.Fatalf("draft counters = %v / %v, want 1000 / 700", counters[metricNInferDraftTokens], counters[metricNInferDraftAccepted])
	}
	if counters[metricPromptTokensTotal] != 2048 {
		t.Fatalf("shared llamacpp counter = %v, want 2048", counters[metricPromptTokensTotal])
	}
}

func TestParsePrometheusTextSkipsGarbage(t *testing.T) {
	counters := parsePrometheusText([]byte("not a sample line\n# comment\n\nllamacpp:ok 4.5\nllamacpp:bad NaN\nllamacpp:broken hello"))
	if counters["llamacpp:ok"] != 4.5 {
		t.Fatalf("valid sample lost: %v", counters)
	}
	if _, ok := counters["llamacpp:bad"]; ok {
		t.Fatal("NaN value must be skipped")
	}
	if _, ok := counters["llamacpp:broken"]; ok {
		t.Fatal("non-numeric value must be skipped")
	}
}

func TestParseSlotLanesLlamaCPP(t *testing.T) {
	lanes := parseSlotLanes([]byte(llamaSlotsFixture))
	if len(lanes) != 2 {
		t.Fatalf("lanes = %+v, want 2", lanes)
	}
	busy, idle := lanes[0], lanes[1]
	if !busy.Processing || busy.ID != 0 || busy.NPrompt != 34210 || busy.NCached != 29871 || !busy.Speculative {
		t.Fatalf("busy lane = %+v", busy)
	}
	if idle.Processing || idle.ID != 1 || idle.NPrompt != 0 {
		t.Fatalf("idle lane = %+v", idle)
	}
}

func TestParseSlotLanesNInfer(t *testing.T) {
	lanes := parseSlotLanes([]byte(ninferSlotsFixture))
	if len(lanes) != 3 {
		t.Fatalf("lanes = %+v, want 3", lanes)
	}
	agent, resident, free := lanes[0], lanes[1], lanes[2]
	if !agent.Processing || !agent.Retained || agent.Session != "a1b2c3" || agent.NPrompt != 45000 {
		t.Fatalf("agent lane = %+v", agent)
	}
	if resident.Processing || !resident.Retained || resident.Session != "d4e5f6" {
		t.Fatalf("resident lane = %+v", resident)
	}
	if free.Processing || free.Retained {
		t.Fatalf("free lane = %+v", free)
	}
}

func TestParseSlotLanesRejectsInvalidJSON(t *testing.T) {
	if lanes := parseSlotLanes([]byte(`{"not":"an array"}`)); lanes != nil {
		t.Fatalf("lanes = %+v, want nil", lanes)
	}
	if lanes := parseSlotLanes(nil); lanes != nil {
		t.Fatalf("lanes = %+v, want nil", lanes)
	}
}

// TestRefreshBudgets pins the per-consumer refresh budget relationship:
// the dashboard refresh gets more headroom than the CLI so its two-epoch
// inference observation completes on slow tunnels, while staying under the
// refresh cadence so consecutive refreshes never overlap.
func TestRefreshBudgets(t *testing.T) {
	if statusRefreshBudget <= 0 || dashboardRefreshBudget <= 0 {
		t.Fatalf("refresh budgets must be positive: status=%v dashboard=%v", statusRefreshBudget, dashboardRefreshBudget)
	}
	if dashboardRefreshBudget <= statusRefreshBudget {
		t.Fatalf("dashboard budget %v must exceed the status budget %v", dashboardRefreshBudget, statusRefreshBudget)
	}
	if dashboardRefreshBudget >= dashboardRefreshInterval {
		t.Fatalf("dashboard budget %v must stay under the %v refresh cadence", dashboardRefreshBudget, dashboardRefreshInterval)
	}
}

func TestProbeInferenceLlamaCPPBothEndpoints(t *testing.T) {
	testInferenceTiming(t)
	server := newInferenceFixtureServer(t, inferenceFixtureServer{
		metricsHandler: func(epoch int) (string, int) { return llamaMetricsFixture(epoch), http.StatusOK },
		slotsHandler:   func(int) (string, int) { return llamaSlotsFixture, http.StatusOK },
	})
	result := probeInferenceBase(context.Background(), server.URL)
	if !result.Available || result.UnavailableReason != "" {
		t.Fatalf("probe = %+v, want available", result)
	}
	if result.Agents != 1 || result.Processing != 1 {
		t.Fatalf("agents/processing = %d/%d, want 1/1", result.Agents, result.Processing)
	}
	if result.ResidentDepth != 34210 {
		t.Fatalf("resident depth = %d, want 34210", result.ResidentDepth)
	}
	// Absolute rates depend on the (test-shortened) epoch gap; the deltas are
	// deterministic, so assert presence and the 2:1 prefill-to-decode shape.
	if result.DecodeTokensSec == nil || *result.DecodeTokensSec <= 0 {
		t.Fatalf("decode rate = %v, want a positive rate", result.DecodeTokensSec)
	}
	if result.PrefillTokensSec == nil || *result.PrefillTokensSec <= *result.DecodeTokensSec {
		t.Fatalf("prefill rate = %v, want above decode rate %v", result.PrefillTokensSec, result.DecodeTokensSec)
	}
	if result.CacheReuseRatio == nil || *result.CacheReuseRatio < 0.79 || *result.CacheReuseRatio > 0.81 {
		t.Fatalf("cache reuse = %v, want 0.80", result.CacheReuseRatio)
	}
	if result.SpecAcceptRatio == nil || *result.SpecAcceptRatio < 0.69 || *result.SpecAcceptRatio > 0.71 {
		t.Fatalf("spec accept = %v, want about 0.70", result.SpecAcceptRatio)
	}
	if len(result.Lanes) != 2 || !result.Lanes[0].Processing {
		t.Fatalf("lanes = %+v", result.Lanes)
	}
	if result.Meta.SampledAt.IsZero() {
		t.Fatalf("sampledAt not set: %+v", result.Meta)
	}
}

func TestProbeInferenceNInferBothEndpoints(t *testing.T) {
	testInferenceTiming(t)
	server := newInferenceFixtureServer(t, inferenceFixtureServer{
		metricsHandler: func(epoch int) (string, int) { return ninferMetricsFixture(epoch), http.StatusOK },
		slotsHandler:   func(int) (string, int) { return ninferSlotsFixture, http.StatusOK },
	})
	result := probeInferenceBase(context.Background(), server.URL)
	if !result.Available {
		t.Fatalf("probe = %+v, want available", result)
	}
	// Lane 0 is processing and lane 1 is retained (resident, not processing);
	// lane 2 is fully idle and counts for no agent.
	if result.Processing != 2 || result.Agents != 2 {
		t.Fatalf("processing/agents = %d/%d, want 2/2", result.Processing, result.Agents)
	}
	if result.Deferred != 1 {
		t.Fatalf("deferred = %d, want 1", result.Deferred)
	}
	if result.ResidentDepth != 45000 {
		t.Fatalf("resident depth = %d, want 45000", result.ResidentDepth)
	}
	if result.CacheReuseRatio == nil || *result.CacheReuseRatio < 0.42 || *result.CacheReuseRatio > 0.44 {
		// NInfer's re-published prompt counter counts non-cached tokens
		// only, so reuse must be hits/(hits+non-cached): 4608/(4608+6144).
		t.Fatalf("ninfer cache reuse = %v, want about 0.43", result.CacheReuseRatio)
	}
	if result.SpecAcceptRatio == nil || *result.SpecAcceptRatio < 0.69 || *result.SpecAcceptRatio > 0.71 {
		t.Fatalf("ninfer spec accept = %v, want about 0.70", result.SpecAcceptRatio)
	}
	if result.DecodeTokensSec == nil || *result.DecodeTokensSec <= 0 {
		t.Fatalf("decode rate = %v, want a positive rate", result.DecodeTokensSec)
	}
	if result.PrefillTokensSec == nil || *result.PrefillTokensSec <= 0 {
		t.Fatalf("prefill rate = %v, want a positive rate", result.PrefillTokensSec)
	}
}

func TestProbeInferenceMetricsDisabledFallsBackToSlots(t *testing.T) {
	testInferenceTiming(t)
	server := newInferenceFixtureServer(t, inferenceFixtureServer{
		metricsHandler: func(int) (string, int) { return "Not Implemented", http.StatusNotImplemented },
		slotsHandler:   func(int) (string, int) { return llamaSlotsFixture, http.StatusOK },
	})
	result := probeInferenceBase(context.Background(), server.URL)
	if !result.Available || result.UnavailableReason != "" {
		t.Fatalf("probe = %+v, want available via slots", result)
	}
	if result.Agents != 1 || result.ResidentDepth != 34210 {
		t.Fatalf("lane-derived values lost: %+v", result)
	}
	if result.DecodeTokensSec != nil || result.CacheReuseRatio != nil {
		t.Fatalf("counter-derived values must stay absent without /metrics: %+v", result)
	}
}

func TestProbeInferenceNeitherEndpointServed(t *testing.T) {
	testInferenceTiming(t)
	server := newInferenceFixtureServer(t, inferenceFixtureServer{
		metricsHandler: func(int) (string, int) { return "Not Implemented", http.StatusNotImplemented },
		slotsHandler:   func(int) (string, int) { return "Not Implemented", http.StatusNotImplemented },
	})
	result := probeInferenceBase(context.Background(), server.URL)
	if result.Available {
		t.Fatalf("probe = %+v, want unavailable", result)
	}
	if !strings.Contains(result.UnavailableReason, "--metrics --slots") {
		t.Fatalf("reason = %q, want llama.cpp launch hint", result.UnavailableReason)
	}
}

func TestProbeInferenceEndpointUnreachable(t *testing.T) {
	testInferenceTiming(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	result := probeInferenceBase(context.Background(), base)
	if result.Available {
		t.Fatalf("probe = %+v, want unavailable", result)
	}
	if !strings.Contains(result.UnavailableReason, "unreachable") {
		t.Fatalf("reason = %q, want unreachable hint", result.UnavailableReason)
	}
}

func TestProbeInferenceSecondEpochUnavailableKeepsFirstEpoch(t *testing.T) {
	testInferenceTiming(t)
	server := newInferenceFixtureServer(t, inferenceFixtureServer{
		// Both endpoints stop serving after the first epoch so the second
		// epoch is entirely unusable.
		metricsHandler: func(epoch int) (string, int) {
			if epoch > 0 {
				return "broken", http.StatusServiceUnavailable
			}
			return llamaMetricsFixture(0), http.StatusOK
		},
		slotsHandler: func(epoch int) (string, int) {
			if epoch > 0 {
				return "broken", http.StatusServiceUnavailable
			}
			return llamaSlotsFixture, http.StatusOK
		},
	})
	result := probeInferenceBase(context.Background(), server.URL)
	if !result.Available {
		t.Fatalf("probe = %+v, want available from first epoch", result)
	}
	if result.Agents != 1 || result.ResidentDepth != 34210 {
		t.Fatalf("first-epoch data lost: %+v", result)
	}
	if result.DecodeTokensSec != nil {
		t.Fatalf("rates must stay nil without a usable second epoch: %+v", result)
	}
}
