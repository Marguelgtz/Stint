package main

import "time"

func startupDuration(startedAt, servingAt time.Time) time.Duration {
	if startedAt.IsZero() || servingAt.IsZero() || servingAt.Before(startedAt) {
		return 0
	}
	return servingAt.Sub(startedAt)
}

func formatStartupDuration(startedAt, servingAt time.Time) string {
	return startupDuration(startedAt, servingAt).Round(time.Second).String()
}
