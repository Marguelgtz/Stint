package main

import (
	"bytes"
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