package main

import (
	"math"
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
