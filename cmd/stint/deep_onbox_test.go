package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
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

func TestApplyDeepOnBoxOverridesPreservesOmittedSettings(t *testing.T) {
	state := deep.DeepState{
		TaskAttemptCap: 5,
		Exec: &deep.ExecSettings{
			Provider:        "persisted-provider",
			Model:           "persisted-model",
			Reasoning:       deep.ReasoningXHigh,
			ActionPlanPath:  "plans/current.md",
			TaskTimeoutSec:  73,
			AllowedCommands: []string{"go test ./..."},
		},
	}
	if err := applyDeepOnBoxOverrides(&state, &deepOnBoxFlags{}); err != nil {
		t.Fatalf("apply omitted overrides: %v", err)
	}
	if state.TaskAttemptCap != 5 || state.Exec.Provider != "persisted-provider" || state.Exec.Model != "persisted-model" || state.Exec.Reasoning != deep.ReasoningXHigh || state.Exec.ActionPlanPath != "plans/current.md" || state.Exec.TaskTimeoutSec != 73 || strings.Join(state.Exec.AllowedCommands, ",") != "go test ./..." {
		t.Fatalf("omitted flags changed persisted settings: state=%+v exec=%+v", state, state.Exec)
	}
}

func TestApplyDeepOnBoxOverridesUsesExplicitSettings(t *testing.T) {
	state := deep.DeepState{
		TaskAttemptCap: 5,
		Tasks:          []deep.Task{{ID: "STINT-PLAN-001", Status: deep.StatusVerified, Attempts: 2, CheckpointCommit: "abc"}, {ID: "T-001", Status: deep.StatusQueued}},
		Exec:           &deep.ExecSettings{Provider: "old", Model: "old", Reasoning: deep.ReasoningMedium, ActionPlanPath: "plans/old.md", TaskTimeoutSec: 60, AllowedCommands: []string{"old"}},
	}
	f := &deepOnBoxFlags{
		provider: "new-provider", providerSet: true,
		model: "new-model", modelSet: true,
		reasoning: deep.ReasoningXHigh, reasoningSet: true,
		actionPlan: "plans/new.md", actionPlanSet: true,
		taskTimeout: 2 * time.Minute, taskTimeoutSet: true,
		maxAttempts: 7, maxAttemptsSet: true,
		allowCommands: stringSlice{"git status", "go test ./..."}, allowCommandsSet: true,
	}
	if err := applyDeepOnBoxOverrides(&state, f); err != nil {
		t.Fatalf("apply explicit overrides: %v", err)
	}
	if state.TaskAttemptCap != 7 || state.Exec.Provider != "new-provider" || state.Exec.Model != "new-model" || state.Exec.Reasoning != deep.ReasoningXHigh || state.Exec.ActionPlanPath != "plans/new.md" || state.Exec.TaskTimeoutSec != 120 || strings.Join(state.Exec.AllowedCommands, ",") != "git status,go test ./..." {
		t.Fatalf("explicit settings not applied: state=%+v exec=%+v", state, state.Exec)
	}
	plan := state.Tasks[0]
	if plan.Status != deep.StatusQueued || plan.Attempts != 0 || plan.CheckpointCommit != "" || plan.Verify != "test -s 'plans/new.md'" {
		t.Fatalf("retargeted action-plan task retained stale acceptance: %+v", plan)
	}
	if state.Tasks[1].ID != "T-001" || state.Tasks[1].Status != deep.StatusQueued {
		t.Fatalf("unrelated task changed: %+v", state.Tasks[1])
	}
}

func TestOnBoxComputeRebindRequiresMismatchAndPersistsReason(t *testing.T) {
	now := time.Now().UTC()
	state := deep.DeepState{}
	if err := state.BindCompute("vast", 100, now); err != nil {
		t.Fatal(err)
	}
	if onBoxComputeRebindNeeded(&state, 100) {
		t.Fatal("matching instance requires a rebind")
	}
	if !onBoxComputeRebindNeeded(&state, 101) {
		t.Fatal("replacement instance should require an explicit rebind")
	}
	if err := state.RebindCompute("vast", 101, "restored durable worktree on replacement", now.Add(time.Minute)); err != nil {
		t.Fatalf("record rebind: %v", err)
	}
	if got := state.ComputeBinding.Rebinds; len(got) != 1 || got[0].FromInstanceID != 100 || got[0].ToInstanceID != 101 || got[0].Reason != "restored durable worktree on replacement" {
		t.Fatalf("rebind history = %+v", got)
	}
}

func TestPrepareDeepOnBoxResumeRebindsOnlyAfterWorktreeRecovery(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	state := env.state
	state.Exec = &deep.ExecSettings{Worker: workerHermesOnBox, Provider: "persisted", Model: "model", TaskTimeoutSec: 60}
	if err := state.BindCompute("vast", 100, env.clock.now); err != nil {
		t.Fatal(err)
	}
	// A missing checkout is recoverable while its checkpoint branch exists.
	if err := os.RemoveAll(state.WorktreePath); err != nil {
		t.Fatal(err)
	}
	computeDeadline := env.clock.now.Add(4 * time.Hour)
	now := env.clock.now.Add(2 * time.Hour) // past the saved Deep Work deadline
	compute := sessionstate.State{InstanceID: 101, Deadline: computeDeadline}
	f := &deepOnBoxFlags{resume: true, rebindCompute: true, rebindReason: "restored durable volume on replacement"}
	reset, err := prepareDeepOnBoxResume(state, compute, f, env.coord.git, now)
	if err != nil {
		t.Fatalf("prepare replacement compute resume: %v", err)
	}
	if !reset {
		t.Fatal("expired saved deadline was not reset against the active compute session")
	}
	if state.ComputeBinding.InstanceID != 101 || len(state.ComputeBinding.Rebinds) != 1 || state.ComputeBinding.Rebinds[0].FromInstanceID != 100 || state.ComputeBinding.Rebinds[0].ToInstanceID != 101 || state.ComputeBinding.Rebinds[0].Reason != f.rebindReason {
		t.Fatalf("compute binding did not record explicit replacement: %+v", state.ComputeBinding)
	}
	if !state.Deadline.Equal(computeDeadline) || state.Phase != deep.PhaseExecuting {
		t.Fatalf("resumed state deadline/phase = %s/%s", state.Deadline, state.Phase)
	}
	if !env.coord.git.worktreeUsable(state.WorktreePath) {
		t.Fatal("saved branch worktree was not reattached before compute rebind")
	}
}

func TestPrepareDeepOnBoxResumeRefusesImplicitComputeRebind(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	state := env.state
	state.Exec = &deep.ExecSettings{Worker: workerHermesOnBox, Model: "model"}
	if err := state.BindCompute("vast", 100, env.clock.now); err != nil {
		t.Fatal(err)
	}
	compute := sessionstate.State{InstanceID: 101, Deadline: env.clock.now.Add(time.Hour)}
	_, err := prepareDeepOnBoxResume(state, compute, &deepOnBoxFlags{resume: true}, env.coord.git, env.clock.now)
	if err == nil || !strings.Contains(err.Error(), "--rebind-compute --rebind-reason") {
		t.Fatalf("implicit replacement compute resume err = %v", err)
	}
	if state.ComputeBinding.InstanceID != 100 || len(state.ComputeBinding.Rebinds) != 0 {
		t.Fatalf("refused resume mutated compute binding: %+v", state.ComputeBinding)
	}
}

func TestWriteOnBoxReadyWritesRunningHandshake(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime", "RUNNING.json")
	deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	state := deep.DeepState{SessionID: "onbox-test", Deadline: deadline}
	if err := writeOnBoxReady(path, state); err != nil {
		t.Fatalf("write readiness: %v", err)
	}
	var got struct {
		Status   string    `json:"status"`
		Session  string    `json:"session"`
		Deadline time.Time `json:"deadline"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "RUNNING" || got.Session != state.SessionID || !got.Deadline.Equal(deadline) {
		t.Fatalf("readiness payload = %+v", got)
	}
}
