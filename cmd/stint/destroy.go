package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/provider/vast"
)

type destroyClient interface {
	DestroyInstance(ctx context.Context, id int64) error
	ShowInstance(ctx context.Context, id int64) (vast.Instance, error)
}

type destroyResult struct {
	Confirmed bool
	Attempts  int
	LastError error
}

var destroyRetryDelays = []time.Duration{
	0,
	5 * time.Second,
	15 * time.Second,
	30 * time.Second,
	60 * time.Second,
}

var watchdogDestroyRetryDelays = []time.Duration{
	0,
	5 * time.Second,
	15 * time.Second,
	30 * time.Second,
	60 * time.Second,
	60 * time.Second,
	60 * time.Second,
	60 * time.Second,
	60 * time.Second,
	60 * time.Second,
}

func destroyAndConfirm(ctx context.Context, client destroyClient, instanceID int64) destroyResult {
	return destroyAndConfirmWithDelays(ctx, client, instanceID, destroyRetryDelays)
}

func destroyAndConfirmWithDelays(ctx context.Context, client destroyClient, instanceID int64, delays []time.Duration) destroyResult {
	result := destroyResult{}
	for i, delay := range delays {
		if delay > 0 {
			select {
			case <-ctx.Done():
				result.LastError = ctx.Err()
				return result
			case <-time.After(delay):
			}
		}
		result.Attempts = i + 1
		destroyErr := client.DestroyInstance(ctx, instanceID)
		if destroyErr != nil && !isInstanceMissingError(destroyErr) {
			result.LastError = destroyErr
		}

		instance, showErr := client.ShowInstance(ctx, instanceID)
		if showErr != nil {
			if isInstanceMissingError(showErr) {
				result.Confirmed = true
				result.LastError = nil
				return result
			}
			result.LastError = showErr
			continue
		}
		status := strings.ToLower(strings.TrimSpace(instance.ActualStatus))
		if status == "destroyed" || status == "deleted" || status == "terminated" {
			result.Confirmed = true
			result.LastError = nil
			return result
		}
		if destroyErr == nil {
			result.LastError = fmt.Errorf("destroy requested but instance %d still reports status %q", instanceID, instance.ActualStatus)
		}
	}
	return result
}

func isInstanceMissingError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *vast.APIError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusGone) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "not found") || strings.Contains(text, "404") || strings.Contains(text, "410")
}
