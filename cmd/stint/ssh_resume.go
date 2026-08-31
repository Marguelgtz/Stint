package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const (
	sshConnectProbeTimeout = 12 * time.Second
	sshConnectHeartbeat    = 10 * time.Second
)

// waitForSSHResponsive keeps resume visibly alive and bounds each SSH probe.
// Without the per-probe timeout, the resilient SSH wrapper can spend roughly a
// minute retrying a single probe, making the outer four-minute timeout appear
// frozen and allowing one attempt to consume most of the retry window.
func waitForSSHResponsive(ctx context.Context, paths config.Paths, state sessionstate.State, timeout time.Duration) error {
	return waitForSSHWithProbe(ctx, timeout, os.Stdout, func(probeCtx context.Context) error {
		_, err := runSSH(probeCtx, paths, state, "echo stint-ssh-ready")
		return err
	})
}

func waitForSSHWithProbe(ctx context.Context, timeout time.Duration, out io.Writer, probe func(context.Context) error) error {
	startedAt := time.Now()
	deadline := startedAt.Add(timeout)
	lastHeartbeat := time.Time{}
	attempt := 0

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New("timed out waiting for SSH connection")
		}

		attempt++
		probeTimeout := sshConnectProbeTimeout
		if remaining < probeTimeout {
			probeTimeout = remaining
		}
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		err := probe(probeCtx)
		cancel()
		if err == nil {
			fmt.Fprintf(out, "  SSH connection   ready after %s\n", formatWaitDuration(time.Since(startedAt)))
			return nil
		}

		now := time.Now()
		if !now.Before(deadline) {
			return errors.New("timed out waiting for SSH connection")
		}
		if lastHeartbeat.IsZero() || now.Sub(lastHeartbeat) >= sshConnectHeartbeat {
			fmt.Fprintf(out, "  SSH connection   waiting %s / %s (attempt %d)\n", formatWaitDuration(now.Sub(startedAt)), formatWaitDuration(timeout), attempt)
			lastHeartbeat = now
		}

		sleepFor := 2 * time.Second
		if left := time.Until(deadline); left < sleepFor {
			sleepFor = left
		}
		if sleepFor <= 0 {
			return errors.New("timed out waiting for SSH connection")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepFor):
		}
	}
}
