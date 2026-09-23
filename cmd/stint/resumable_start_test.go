package main

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/core"
	"github.com/Marguelgtz/Stint/internal/router"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestFallbackCandidateExceedingSessionCostIsRejected(t *testing.T) {
	profile, err := router.ResolveProfile("interactive")
	if err != nil {
		t.Fatal(err)
	}
	const requestedHours = 7.0
	initial := core.Offer{HourlyUSD: 0.30}
	fallback := core.Offer{HourlyUSD: profile.GPU.MaxHourlyUSD}
	if initial.HourlyUSD*requestedHours > profile.Session.MaxCostUSD {
		t.Fatal("test setup: initial candidate must fit the session ceiling")
	}
	if fallback.HourlyUSD > profile.GPU.MaxHourlyUSD {
		t.Fatal("test setup: fallback must satisfy the hourly ceiling")
	}
	if fallback.HourlyUSD*requestedHours <= profile.Session.MaxCostUSD {
		t.Fatal("test setup: fallback must exceed the requested-session ceiling")
	}
	if !candidateWithinSessionBudget(profile, initial, requestedHours) {
		t.Fatal("initial candidate should fit the full-session budget")
	}
	if candidateWithinSessionBudget(profile, fallback, requestedHours) {
		t.Fatal("fallback satisfying hourly policy but exceeding the full-session budget was accepted")
	}
}

func TestApplySessionCostCeilingOnlyLowersProfilePolicy(t *testing.T) {
	profile, err := router.ResolveProfile("interactive")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Session.MaxCostUSD < 2 {
		t.Fatalf("test setup: interactive profile ceiling $%.2f is below smoke ceiling $2.00", profile.Session.MaxCostUSD)
	}

	got, err := applySessionCostCeiling(profile, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session.MaxCostUSD != 2 {
		t.Fatalf("per-run ceiling = $%.2f, want $2.00", got.Session.MaxCostUSD)
	}
	if candidateWithinSessionBudget(got, core.Offer{HourlyUSD: 1.34}, 1.5) {
		t.Fatal("candidate projected above the per-run ceiling was accepted")
	}

	if _, err := applySessionCostCeiling(profile, profile.Session.MaxCostUSD+0.01); err == nil {
		t.Fatal("raising the profile cost ceiling from the command line was accepted")
	}
	for _, invalid := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if _, err := applySessionCostCeiling(profile, invalid); err == nil {
			t.Fatalf("invalid per-run ceiling %v was accepted", invalid)
		}
	}
}

func TestApplySessionHourlyCeilingNeedsSessionCapToRaiseProfileLimit(t *testing.T) {
	profile, err := router.ResolveProfile("interactive")
	if err != nil {
		t.Fatal(err)
	}
	profile, err = applySessionCostCeiling(profile, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applySessionHourlyCeiling(profile, 1.33, false); err == nil {
		t.Fatal("raising the profile's hourly ceiling without an explicit session cap was accepted")
	}

	got, err := applySessionHourlyCeiling(profile, 1.33, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.GPU.MaxHourlyUSD != 1.33 {
		t.Fatalf("per-run hourly ceiling = $%.2f, want $1.33", got.GPU.MaxHourlyUSD)
	}
	if !candidateWithinSessionBudget(got, core.Offer{HourlyUSD: 1.33}, 1.5) {
		t.Fatal("$1.33/hour candidate should fit the $2.00 session ceiling for 1.5 hours")
	}
	if candidateWithinSessionBudget(got, core.Offer{HourlyUSD: 1.34}, 1.5) {
		t.Fatal("candidate above the full-session ceiling was accepted")
	}
	for _, invalid := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if _, err := applySessionHourlyCeiling(profile, invalid, true); err == nil {
			t.Fatalf("invalid hourly ceiling %v was accepted", invalid)
		}
	}
}

func TestRunStartResumableValidateOnlyParsesSmokeArgumentsWithoutConfig(t *testing.T) {
	t.Setenv("HOME", filepath.Join(t.TempDir(), "no-home"))
	err := runStartResumable([]string{
		"interactive", "--hours", "1.5", "--runtime", "ninfer", "--ninfer-config", "native", "--clients", "2",
		"--min-measured-download-mbps", "30", "--min-network-mbps", "300",
		"--network-candidate-attempts", "1", "--max-hourly-usd", "1.33", "--max-cost-usd", "2", "--yes", "--validate-only",
	})
	if err != nil {
		t.Fatalf("validate-only rejected the live smoke arguments: %v", err)
	}
}

func TestRunStartResumableValidateOnlyRejectsBadCostBeforeConfig(t *testing.T) {
	t.Setenv("HOME", filepath.Join(t.TempDir(), "no-home"))
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "cannot raise profile ceiling",
			args: []string{"interactive", "--max-cost-usd", "3", "--validate-only"},
			want: "exceeds the interactive profile ceiling",
		},
		{
			name: "hourly raise requires explicit session cap",
			args: []string{"interactive", "--max-hourly-usd", "1.33", "--validate-only"},
			want: "requires an explicit --max-cost-usd session cap",
		},
		{
			name: "rejects unknown option",
			args: []string{"interactive", "--not-a-start-option", "--validate-only"},
			want: "flag provided but not defined",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runStartResumable(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("runStartResumable(%v) error = %v, want containing %q", tc.args, err, tc.want)
			}
		})
	}
}

func TestProviderStartupTimeoutAllowsCandidateFailover(t *testing.T) {
	if providerStartupTimeout > 6*time.Minute {
		t.Fatalf("provider startup timeout = %s, want at most 6m so another paid candidate can be tried", providerStartupTimeout)
	}
}

func TestCheckpointIsRecoverable(t *testing.T) {
	tests := []struct {
		checkpoint string
		want       bool
	}{
		{checkpoint: "", want: false},
		{checkpoint: sessionstate.CheckpointInstanceCreated, want: true},
		{checkpoint: sessionstate.CheckpointSSHReady, want: true},
		{checkpoint: sessionstate.CheckpointRuntimeReady, want: true},
		{checkpoint: sessionstate.CheckpointModelStarted, want: true},
		{checkpoint: sessionstate.CheckpointReady, want: true},
	}
	for _, tt := range tests {
		if got := checkpointIsRecoverable(tt.checkpoint); got != tt.want {
			t.Fatalf("checkpointIsRecoverable(%q) = %v, want %v", tt.checkpoint, got, tt.want)
		}
	}
}

func TestRemoteModelLaunchUsesPIDTracking(t *testing.T) {
	command := remoteModelLaunchCommand()
	if strings.Contains(command, "\npkill -f ") {
		t.Fatal("remote model launch must not use pkill -f; it can kill the SSH shell that contains the pattern")
	}
	for _, required := range []string{
		"/workspace/stint/llama.pid",
		"pgrep -x llama-server",
		"nohup bash -c",
		"exec /workspace/stint/llama.cpp/build/bin/llama-server",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("remote model launch missing %q", required)
		}
	}
}
