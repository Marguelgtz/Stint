package main

import (
	"context"
	"errors"
	"math"
	"net/http"
	"testing"
	"time"
)

func TestAveragePerf(t *testing.T) {
	samples := []perfSample{
		{TTFT: time.Second, Total: 5 * time.Second, PromptTokens: 100, CompletionTokens: 200, DecodeTokensSec: 50},
		{TTFT: 3 * time.Second, Total: 9 * time.Second, PromptTokens: 300, CompletionTokens: 400, DecodeTokensSec: 70},
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
}

func TestAveragePerfEmpty(t *testing.T) {
	if got := averagePerf(nil); got != (perfSample{}) {
		t.Fatalf("empty average = %#v, want zero value", got)
	}
}

func TestBenchmarkCompletionWithRetryRecovers(t *testing.T) {
	calls := 0
	want := perfSample{TTFT: time.Second, Total: 2 * time.Second, CompletionTokens: 10, DecodeTokensSec: 9}
	benchmark := func(context.Context, *http.Client, int) (perfSample, error) {
		calls++
		if calls < 3 {
			return perfSample{}, errors.New("EOF")
		}
		return want, nil
	}

	got, attempts, err := benchmarkCompletionWithRetry(context.Background(), &http.Client{}, 256, benchmark)
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

func TestBenchmarkCompletionWithRetryStopsAfterMaxAttempts(t *testing.T) {
	calls := 0
	benchmark := func(context.Context, *http.Client, int) (perfSample, error) {
		calls++
		return perfSample{}, errors.New("EOF")
	}

	_, attempts, err := benchmarkCompletionWithRetry(context.Background(), &http.Client{}, 256, benchmark)
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
	benchmark := func(context.Context, *http.Client, int) (perfSample, error) {
		calls++
		cancel()
		return perfSample{}, errors.New("EOF")
	}

	_, attempts, err := benchmarkCompletionWithRetry(ctx, &http.Client{}, 256, benchmark)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if attempts != 1 || calls != 1 {
		t.Fatalf("attempts/calls = %d/%d, want 1/1", attempts, calls)
	}
}
