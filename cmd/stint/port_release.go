package main

import (
	"context"
	"fmt"
	"time"

	localenv "github.com/Marguelgtz/Stint/internal/local"
)

const resumePortReleaseTimeout = 3 * time.Second

func waitForPortAvailable(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if localenv.PortAvailable(port) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("local port %d is still in use after %s", port, timeout.Round(time.Millisecond))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
