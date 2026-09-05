package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func downTestState() sessionstate.State {
	return sessionstate.State{InstanceID: 75893, Deadline: time.Now().Add(42 * time.Minute)}
}

func TestConfirmDestroyAcceptsExactWord(t *testing.T) {
	var out bytes.Buffer
	if !confirmDestroy(strings.NewReader("destroy\n"), &out, downTestState()) {
		t.Fatalf("exact \"destroy\" must confirm: %s", out.String())
	}
	if !strings.Contains(out.String(), "75893") || !strings.Contains(out.String(), "Type \"destroy\" to confirm") {
		t.Fatalf("summary or prompt missing from: %s", out.String())
	}
}

func TestConfirmDestroyRejectsAnythingElse(t *testing.T) {
	for _, input := range []string{"Destroy\n", "no\n", "\n", "destroy extra\n"} {
		var out bytes.Buffer
		if confirmDestroy(strings.NewReader(input), &out, downTestState()) {
			t.Fatalf("input %q must not confirm: %s", input, out.String())
		}
		if !strings.Contains(out.String(), "Aborted") {
			t.Fatalf("input %q: missing abort message: %s", input, out.String())
		}
	}
}

func TestConfirmDestroyNonInteractiveStdinAborts(t *testing.T) {
	var out bytes.Buffer
	if confirmDestroy(strings.NewReader(""), &out, downTestState()) {
		t.Fatalf("EOF stdin must not confirm: %s", out.String())
	}
	if !strings.Contains(out.String(), "--yes") {
		t.Fatalf("missing --yes hint on EOF: %s", out.String())
	}
}

func TestWaitForInstanceGoneConfirmsAfterRetries(t *testing.T) {
	old := destroyGonePollInterval
	destroyGonePollInterval = time.Millisecond
	t.Cleanup(func() { destroyGonePollInterval = old })
	calls := 0
	show := func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("instance still visible")
		}
		return nil
	}
	if err := waitForInstanceGone(context.Background(), show); err != nil {
		t.Fatalf("gave up on a verified-gone instance: %v", err)
	}
	if calls != 3 {
		t.Fatalf("show polled %d times, want 3", calls)
	}
}

func TestWaitForInstanceGoneExpiresUnconfirmed(t *testing.T) {
	old := destroyGonePollInterval
	destroyGonePollInterval = time.Millisecond
	t.Cleanup(func() { destroyGonePollInterval = old })
	show := func(context.Context) error { return errors.New("instance still visible") }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := waitForInstanceGone(ctx, show); err == nil {
		t.Fatal("expected an error when the instance is never confirmed gone")
	}
}