package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSeedOnBoxActionPlanCopiesIntoWorktree(t *testing.T) {
	fixture := t.TempDir()
	seed := filepath.Join(fixture, "seed.md")
	worktree := filepath.Join(fixture, "worktree")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("# Living plan\n\nSeed evidence.\n")
	if err := os.WriteFile(seed, want, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := seedOnBoxActionPlan(seed, worktree, "deep-work/plan.md"); err != nil {
		t.Fatalf("seed action plan: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(worktree, "deep-work", "plan.md"))
	if err != nil {
		t.Fatalf("read seeded action plan: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("seeded action plan = %q, want %q", got, want)
	}
}

func TestSeedOnBoxActionPlanRejectsOutsideWorktree(t *testing.T) {
	if err := seedOnBoxActionPlan("unused", t.TempDir(), "../escape.md"); err == nil {
		t.Fatal("expected traversal destination to be rejected")
	}
}
