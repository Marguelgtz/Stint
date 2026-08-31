package main

import (
	"strings"
	"testing"

	"github.com/Marguelgtz/Stint/internal/core"
	"github.com/Marguelgtz/Stint/internal/router"
)

func lineWith(out, prefix string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

func planOutputForGPU(t *testing.T, gpuModel string) planOutput {
	t.Helper()
	profile, err := router.ResolveProfile("interactive")
	if err != nil {
		t.Fatal(err)
	}
	return planOutput{
		Live:        true,
		Plan:        core.SessionPlan{Profile: profile, Hours: 1, Workers: []core.PlannedWorker{{Offer: core.Offer{GPUModel: gpuModel}}}, EstimatedTotalUSD: 0.40},
		Diagnostics: planDiagnostics{Candidates: 1, Qualified: 1, RejectedBy: map[core.RejectionReason]int{}},
	}
}

func TestHumanPlanShowsPlannedNInferRuntime(t *testing.T) {
	out := captureOutput(t, func() { printHumanPlan(planOutputForGPU(t, "RTX 4090")) })
	line := lineWith(out, "Runtime (auto)")
	if line == "" || !strings.Contains(line, "ninfer") {
		t.Fatalf("human plan missing NInfer runtime line:\n%s", out)
	}
}

func TestHumanPlanShowsPlannedLlamaRuntime(t *testing.T) {
	out := captureOutput(t, func() { printHumanPlan(planOutputForGPU(t, "RTX 3090")) })
	line := lineWith(out, "Runtime (auto)")
	if line == "" || !strings.Contains(line, "llama.cpp") {
		t.Fatalf("human plan missing llama.cpp runtime line:\n%s", out)
	}
}

func TestStatusShowsNInferConfig(t *testing.T) {
	snapshot := sessionSnapshot{
		Session: sessionInfo{InstanceID: 42, Status: "RUNTIME_READY", GPUModel: "RTX 4090", Runtime: runtimeNInfer, Model: "qwen3.8-27b", ContextTokens: 172032},
	}
	out := captureOutput(t, func() { printSessionSnapshotHuman(snapshot, false) })
	line := lineWith(out, "NInfer config")
	if line == "" || !strings.Contains(line, "precision") {
		t.Fatalf("status missing NInfer config line:\n%s", out)
	}
}

func TestStatusHidesNInferConfigForLlama(t *testing.T) {
	snapshot := sessionSnapshot{
		Session: sessionInfo{InstanceID: 43, Status: "RUNTIME_READY", GPUModel: "RTX 3090", Runtime: runtimeLlamaCpp, Model: "qwen3.8-27b", ContextTokens: 16384},
	}
	out := captureOutput(t, func() { printSessionSnapshotHuman(snapshot, false) })
	if line := lineWith(out, "NInfer config"); line != "" {
		t.Fatalf("status shows NInfer config for llama.cpp runtime:\n%s", out)
	}
}
