package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

type dashboardBenchmarkResult struct {
	Sample perfSample
	Err    error
}

func runDashboardBenchmark(paths config.Paths) (perfSample, error) {
	state, err := sessionstate.Load(paths)
	if errors.Is(err, os.ErrNotExist) {
		return perfSample{}, errors.New("session ended before benchmark started")
	}
	if err != nil {
		return perfSample{}, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	prompt := buildPerfPrompt(perfDefaultPromptTokens)
	timeout := perfBenchmarkTimeout(perfDefaultPromptTokens, 128)
	client := &http.Client{Timeout: timeout, Transport: transport}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	sample, _, err := benchmarkCompletionWithRetry(ctx, client, prompt, 128, benchmarkCompletion)
	if err != nil {
		return perfSample{}, err
	}
	if err := savePerformanceSample(paths, state, sample, time.Now().UTC()); err != nil {
		return perfSample{}, err
	}
	return sample, nil
}
