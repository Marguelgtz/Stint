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
	client := &http.Client{Timeout: 3 * time.Minute, Transport: transport}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	sample, _, err := benchmarkCompletionWithRetry(ctx, client, 128, benchmarkCompletion)
	if err != nil {
		return perfSample{}, err
	}
	if err := savePerformanceSample(paths, state, sample, time.Now().UTC()); err != nil {
		return perfSample{}, err
	}
	return sample, nil
}
