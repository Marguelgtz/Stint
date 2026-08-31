package main

import "testing"

func TestStartHelpIncludesClientsFlag(t *testing.T) {
	cmd, ok := findCommand("start")
	if !ok {
		t.Fatal("start command missing")
	}
	for _, flag := range cmd.flags {
		if flag.name == "--clients" {
			if flag.defaultVal != "1" {
				t.Fatalf("--clients default = %q, want 1", flag.defaultVal)
			}
			return
		}
	}
	t.Fatal("start help missing --clients")
}
