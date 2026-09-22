package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Marguelgtz/Stint/internal/deep"
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

func TestAddActionPlanTaskAvoidsMissionIDCollision(t *testing.T) {
	tasks := []deep.Task{
		{ID: "STINT-PLAN-001", Objective: "mission task one"},
		{ID: "STINT-PLAN-002", Objective: "mission task two"},
		{ID: "T-001", Objective: "normal task"},
	}
	got := addActionPlanTask(tasks, "deep-work/plan.md")
	if len(got) != len(tasks)+1 || got[0].ID != "STINT-PLAN-003" {
		t.Fatalf("planning task = %+v, want a unique STINT-PLAN-003 task", got[0])
	}
	seen := map[string]bool{}
	for _, task := range got {
		if seen[task.ID] {
			t.Errorf("duplicate task id %q", task.ID)
		}
		seen[task.ID] = true
	}
}

func TestFirstEndpointModelFromJSON(t *testing.T) {
	model, err := firstEndpointModelFromJSON(`{"data":[{"id":"qwen3.8-27b"}]}`)
	if err != nil || model != "qwen3.8-27b" {
		t.Fatalf("model=%q err=%v", model, err)
	}
	if _, err := firstEndpointModelFromJSON(`{"data":[]}`); err == nil || !strings.Contains(err.Error(), "no models") {
		t.Fatalf("empty model response err=%v, want missing-model error", err)
	}
}

func TestVerificationToolNamesFindsCompoundCommands(t *testing.T) {
	commands := []string{
		"go test ./... && make verify",
		"test -s build/result && python3 scripts/check.py | jq .",
		"FOO=bar go test ./cmd/stint",
	}
	got := verificationToolNames(commands)
	want := []string{"go", "jq", "make", "python3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("verification tools = %v, want %v", got, want)
	}
}

func TestVerificationToolNamesIgnoresShellBuiltins(t *testing.T) {
	got := verificationToolNames([]string{"test -f out && echo ready", "printf ok"})
	if len(got) != 0 {
		t.Fatalf("verification tools = %v, want no external tools", got)
	}
}
