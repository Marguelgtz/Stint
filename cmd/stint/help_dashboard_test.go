package main

import "testing"

func TestDashIsPrimaryDashboardCommand(t *testing.T) {
	cmd, ok := findCommand("dash")
	if !ok {
		t.Fatal("dash command is not registered")
	}
	if cmd.name != "dash" {
		t.Fatalf("dash resolved to %q", cmd.name)
	}
	if cmd.usage != "stint dash [flags]" {
		t.Fatalf("dash usage = %q", cmd.usage)
	}

	alias, ok := findCommand("dashboard")
	if !ok {
		t.Fatal("dashboard compatibility alias is not registered")
	}
	if alias.name != "dash" {
		t.Fatalf("dashboard alias resolved to %q, want dash", alias.name)
	}
}
