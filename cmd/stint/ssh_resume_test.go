package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestWaitForSSHWithProbeBoundsHungProbe(t *testing.T) {
	started := time.Now()
	var out bytes.Buffer
	err := waitForSSHWithProbe(context.Background(), 60*time.Millisecond, &out, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err == nil || !strings.Contains(err.Error(), "timed out waiting for SSH connection") {
		t.Fatalf("error = %v, want SSH timeout", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("hung SSH probe escaped outer timeout: %s", elapsed)
	}
}

func TestWaitForSSHWithProbeImmediateSuccess(t *testing.T) {
	var out bytes.Buffer
	err := waitForSSHWithProbe(context.Background(), time.Second, &out, func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "SSH connection   ready after") {
		t.Fatalf("output = %q, want ready heartbeat", out.String())
	}
}
