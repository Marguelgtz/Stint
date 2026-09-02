package main

import (
	"strings"
	"testing"
)

func TestStartHelpExplainsStaleOfferReplacement(t *testing.T) {
	cmd, ok := findCommand("start")
	if !ok {
		t.Fatal("start command missing")
	}
	for _, flag := range cmd.flags {
		if flag.name != "--network-candidate-attempts" {
			continue
		}
		if !strings.Contains(flag.purpose, "stale offers are replaced") || !strings.Contains(flag.purpose, "rented Vast machines") {
			t.Fatalf("candidate attempt help = %q", flag.purpose)
		}
		return
	}
	t.Fatal("start help missing --network-candidate-attempts")
}
