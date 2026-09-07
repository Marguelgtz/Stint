package main

import (
	"math"
	"testing"
)

func TestInferenceCacheReuseByRuntime(t *testing.T) {
	for _, tc := range []struct {
		name     string
		counters map[string]float64
		want     float64 // negative means unavailable
	}{
		{"ninfer", map[string]float64{metricNInferPrefixCacheHit: 75, metricPromptTokensTotal: 25}, .75},
		{"ninfer zero hits", map[string]float64{metricNInferPrefixCacheHit: 0, metricPromptTokensTotal: 25}, 0},
		{"ninfer all cached", map[string]float64{metricNInferPrefixCacheHit: 75, metricPromptTokensTotal: 0}, 1},
		{"ninfer idle", map[string]float64{metricNInferPrefixCacheHit: 0, metricPromptTokensTotal: 0}, -1},
		{"ninfer missing noncached", map[string]float64{metricNInferPrefixCacheHit: 75}, -1},
		{"llama", map[string]float64{metricPromptCachedTotal: 75, metricPromptTokensTotal: 100}, .75},
		{"absent", nil, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var result inferenceTelemetry
			inferFromEpoch(&result, inferenceEpoch{Counters: tc.counters})
			got := result.CacheReuseRatio
			if tc.want < 0 {
				if got != nil {
					t.Fatalf("want unavailable, got %v", *got)
				}
			} else if got == nil || math.Abs(*got-tc.want) > 1e-9 {
				t.Fatalf("ratio = %v, want %v", got, tc.want)
			}
		})
	}
}
