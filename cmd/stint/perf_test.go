package main

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAveragePerf(t *testing.T) {
	samples := []perfSample{
		{TTFT: time.Second, Total: 5 * time.Second, PromptTokens: 100, CompletionTokens: 200, DecodeTokensSec: 50, DecodeAvailable: true},
		{TTFT: 3 * time.Second, Total: 9 * time.Second, PromptTokens: 300, CompletionTokens: 400, DecodeTokensSec: 70, DecodeAvailable: true},
	}
	got := averagePerf(samples)
	if got.TTFT != 2*time.Second {
		t.Fatalf("TTFT = %s, want 2s", got.TTFT)
	}
	if got.Total != 7*time.Second {
		t.Fatalf("total = %s, want 7s", got.Total)
	}
	if got.PromptTokens != 200 || got.CompletionTokens != 300 {
		t.Fatalf("tokens = %d/%d, want 200/300", got.PromptTokens, got.CompletionTokens)
	}
	if math.Abs(got.DecodeTokensSec-60) > 0.001 {
		t.Fatalf("decode = %.3f, want 60", got.DecodeTokensSec)
	}
	if !got.DecodeAvailable {
		t.Fatal("decode should be available when all runs have verified timing")
	}
}

func TestAveragePerfEmpty(t *testing.T) {
	if got := averagePerf(nil); got != (perfSample{}) {
		t.Fatalf("empty average = %#v, want zero value", got)
	}
}

func TestAveragePerfHidesRateWhenAnyRunLacksVerifiedTiming(t *testing.T) {
	got := averagePerf([]perfSample{
		{DecodeTokensSec: 100, DecodeAvailable: true},
		{DecodeUnavailableReason: "stream did not expose all completion tokens"},
	})
	if got.DecodeAvailable || got.DecodeTokensSec != 0 || got.DecodeUnavailableReason == "" {
		t.Fatalf("mixed decode average = %+v, want unavailable", got)
	}
}

func TestBenchmarkCompletionWithRetryRecovers(t *testing.T) {
	calls := 0
	want := perfSample{TTFT: time.Second, Total: 2 * time.Second, CompletionTokens: 10, DecodeTokensSec: 9, DecodeAvailable: true}
	benchmark := func(context.Context, *http.Client, string, int) (perfSample, error) {
		calls++
		if calls < 3 {
			return perfSample{}, errors.New("EOF")
		}
		return want, nil
	}

	got, attempts, err := benchmarkCompletionWithRetry(context.Background(), &http.Client{}, "prompt", 256, benchmark)
	if err != nil {
		t.Fatalf("benchmarkCompletionWithRetry() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if got != want {
		t.Fatalf("sample = %#v, want %#v", got, want)
	}
}

func TestReadPerfStreamMeasuresTokenUpdates(t *testing.T) {
	started := time.Unix(0, 0)
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"R"}}]}`,
		`data: {"choices":[{"delta":{"content":"E"}}]}`,
		`data: {"choices":[{"delta":{"content":"A"}}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}}`,
		`data: [DONE]`,
	}, "\n") + "\n"
	clock := steppedPerfClock(started, 100*time.Millisecond)
	sample, err := readPerfStreamAt(strings.NewReader(stream), started, clock)
	if err != nil {
		t.Fatalf("readPerfStreamAt() error = %v", err)
	}
	if sample.TTFT != 100*time.Millisecond || sample.Total != 400*time.Millisecond {
		t.Fatalf("TTFT/total = %s/%s, want 100ms/400ms", sample.TTFT, sample.Total)
	}
	if !sample.DecodeAvailable || math.Abs(sample.DecodeTokensSec-10) > 0.001 {
		t.Fatalf("decode = %.3f available=%t, want 10 tok/s and available", sample.DecodeTokensSec, sample.DecodeAvailable)
	}
}

func TestReadPerfStreamDoesNotInventRateForFinalBufferedText(t *testing.T) {
	started := time.Unix(0, 0)
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"READY"}}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":7413,"completion_tokens":45,"total_tokens":7458}}`,
		`data: [DONE]`,
	}, "\n") + "\n"
	clock := steppedPerfClock(started, 100*time.Millisecond)
	sample, err := readPerfStreamAt(strings.NewReader(stream), started, clock)
	if err != nil {
		t.Fatalf("readPerfStreamAt() error = %v", err)
	}
	if sample.DecodeAvailable || sample.DecodeTokensSec != 0 {
		t.Fatalf("decode = %.3f available=%t, want unavailable", sample.DecodeTokensSec, sample.DecodeAvailable)
	}
	if !strings.Contains(sample.DecodeUnavailableReason, "1 generation updates for 45 completion tokens") {
		t.Fatalf("decode unavailable reason = %q", sample.DecodeUnavailableReason)
	}
	if sample.TTFT != 100*time.Millisecond || sample.Total != 200*time.Millisecond {
		t.Fatalf("TTFT/total = %s/%s, want 100ms/200ms", sample.TTFT, sample.Total)
	}
}

func TestReadPerfStreamRejectsReasoningOnlyCompletion(t *testing.T) {
	started := time.Unix(0, 0)
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"think"}}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`,
		`data: [DONE]`,
	}, "\n") + "\n"
	_, err := readPerfStreamAt(strings.NewReader(stream), started, steppedPerfClock(started, 100*time.Millisecond))
	if err == nil || !strings.Contains(err.Error(), "without user-visible content") {
		t.Fatalf("error = %v, want no-user-visible-content error", err)
	}
}

func steppedPerfClock(start time.Time, step time.Duration) func() time.Time {
	var n time.Duration
	return func() time.Time {
		n += step
		return start.Add(n)
	}
}

func TestBenchmarkCompletionWithRetryStopsAfterMaxAttempts(t *testing.T) {
	calls := 0
	benchmark := func(context.Context, *http.Client, string, int) (perfSample, error) {
		calls++
		return perfSample{}, errors.New("EOF")
	}

	_, attempts, err := benchmarkCompletionWithRetry(context.Background(), &http.Client{}, "prompt", 256, benchmark)
	if err == nil {
		t.Fatal("benchmarkCompletionWithRetry() error = nil, want error")
	}
	if attempts != perfMaxAttempts || calls != perfMaxAttempts {
		t.Fatalf("attempts/calls = %d/%d, want %d/%d", attempts, calls, perfMaxAttempts, perfMaxAttempts)
	}
}

func TestBenchmarkCompletionWithRetryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	benchmark := func(context.Context, *http.Client, string, int) (perfSample, error) {
		calls++
		cancel()
		return perfSample{}, errors.New("EOF")
	}

	_, attempts, err := benchmarkCompletionWithRetry(ctx, &http.Client{}, "prompt", 256, benchmark)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if attempts != 1 || calls != 1 {
		t.Fatalf("attempts/calls = %d/%d, want 1/1", attempts, calls)
	}
}

func TestBuildPerfPromptDeterministic(t *testing.T) {
	for _, target := range []int{512, 8192, 131072} {
		first := buildPerfPrompt(target)
		second := buildPerfPrompt(target)
		if first != second {
			t.Fatalf("buildPerfPrompt(%d) is not deterministic", target)
		}
		if strings.TrimSpace(first) == "" {
			t.Fatalf("buildPerfPrompt(%d) produced an empty prompt", target)
		}
	}
}

func TestBuildPerfPromptRequestsVisibleCompletion(t *testing.T) {
	prompt := buildPerfPrompt(8192)
	if !strings.HasSuffix(prompt, "Answer with exactly one word: READY.") {
		t.Fatal("benchmark prompt must request a short visible completion")
	}
}

func TestBuildPerfPromptScalesWithTarget(t *testing.T) {
	small := buildPerfPrompt(1024)
	large := buildPerfPrompt(32768)
	if len(large) <= len(small) {
		t.Fatalf("32k prompt (%d bytes) is not larger than 1k prompt (%d bytes)", len(large), len(small))
	}
	if first := buildPerfPrompt(1024); !strings.HasPrefix(first, "The following is a synthetic reference text") {
		t.Fatalf("prompt should start with the reference preamble, got %q", first[:60])
	}
	// The builder targets target*10/13 words; keep the word count inside a
	// generous band of that expectation.
	words := float64(len(strings.Fields(large)))
	want := float64(32768) * perfWordsPerToken
	if words < want/2 || words > want*3/2 {
		t.Fatalf("32k prompt has %.0f words, want roughly %.0f", words, want)
	}
}

func TestValidatePerfDepth(t *testing.T) {
	if err := validatePerfDepth(16384, 8192, 256); err != nil {
		t.Fatalf("agent-depth validation = %v, want nil", err)
	}
	if err := validatePerfDepth(262144, 200000, 2048); err != nil {
		t.Fatalf("native-depth validation = %v, want nil", err)
	}
	if err := validatePerfDepth(16384, 16384, 256); err == nil {
		t.Fatal("context overflow should fail validation")
	}
	if err := validatePerfDepth(0, 8192, 256); err == nil {
		t.Fatal("missing context should fail validation")
	}
	if err := validatePerfDepth(16384, 10, 256); err == nil {
		t.Fatal("prompt below the minimum should fail validation")
	}
	if err := validatePerfDepth(16384, 8192, 10); err == nil {
		t.Fatal("completion below the minimum should fail validation")
	}
}

func TestPerfBenchmarkTimeoutScalesWithDepth(t *testing.T) {
	shallow := perfBenchmarkTimeout(512, 128)
	if shallow != 3*time.Minute {
		t.Fatalf("shallow timeout = %s, want the 3m floor", shallow)
	}
	mid := perfBenchmarkTimeout(131072, 2048)
	if mid <= shallow {
		t.Fatalf("mid-depth timeout %s is not larger than the floor %s", mid, shallow)
	}
	deep := perfBenchmarkTimeout(perfMaxPromptTokens, perfMaxCompletionTokens)
	if deep <= mid {
		t.Fatalf("deep timeout %s is not larger than mid %s", deep, mid)
	}
	if deep > perfBenchmarkTimeoutCap {
		t.Fatalf("timeout %s exceeds the cap", deep)
	}
	// Beyond the validated depth range the bound must clamp to the cap.
	if capped := perfBenchmarkTimeout(3_000_000, perfMaxCompletionTokens); capped != perfBenchmarkTimeoutCap {
		t.Fatalf("extreme-depth timeout = %s, want the cap %s", capped, perfBenchmarkTimeoutCap)
	}
}

func TestPerfCompletionPayload(t *testing.T) {
	payload := perfCompletionPayload("hello world", 128)
	if payload["model"] != interactiveModelAlias {
		t.Fatalf("model = %v, want %s", payload["model"], interactiveModelAlias)
	}
	if payload["max_tokens"] != 128 {
		t.Fatalf("max_tokens = %v, want 128", payload["max_tokens"])
	}
	messages, ok := payload["messages"].([]map[string]string)
	if !ok || len(messages) != 1 || messages[0]["role"] != "user" || messages[0]["content"] != "hello world" {
		t.Fatalf("messages = %#v, want single user message with the prompt", payload["messages"])
	}
}
