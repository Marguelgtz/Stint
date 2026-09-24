package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestDeepStartCanProvisionComputeThenLaunchDetachedRun(t *testing.T) {
	fixture := newDeepProductionFixture(t)
	if err := sessionstate.Clear(fixture.paths); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	fixture.session.StartedAt = now
	fixture.session.RentalStartedAt = now
	fixture.session.Deadline = now.Add(3 * time.Hour)
	fixture.deadline = fixture.session.Deadline

	var gotProvisionArgs []string
	provisionCalls := 0
	provisioner := func(_ context.Context, binary string, args []string, out, errOut io.Writer) error {
		provisionCalls++
		if binary != fixture.stintBinary {
			t.Fatalf("provision binary = %q, want %q", binary, fixture.stintBinary)
		}
		gotProvisionArgs = append([]string(nil), args...)
		if err := sessionstate.Save(fixture.paths, fixture.session); err != nil {
			t.Fatal(err)
		}
		_, _ = out.Write([]byte("READY\n"))
		return nil
	}

	launcherCalls := 0
	runner := func(_ context.Context, launcher string, _ []string, out, _ io.Writer) ([]byte, error) {
		launcherCalls++
		if launcher != fixture.launcherPath {
			t.Fatalf("launcher = %q, want %q", launcher, fixture.launcherPath)
		}
		_, _ = out.Write([]byte("ONBOX_SUPERVISOR_RUNNING pid=321\n"))
		return []byte("ONBOX_SUPERVISOR_RUNNING pid=321\n{" +
			`"status":"RUNNING","session":"deep-self-provision","deadline":"` + fixture.deadline.Format(time.RFC3339) + `"}` + "\n"), nil
	}

	var stdout, stderr bytes.Buffer
	args := []string{
		"--repo", fixture.repo,
		"--mission", fixture.mission,
		"--github-token-file", fixture.tokenPath,
		"--hours", "3",
		"--runtime", "ninfer",
		"--ninfer-deployment", "release-bundle",
		"--ninfer-config", "native",
		"--clients", "2",
		"--max-hourly-usd", "0.45",
		"--max-cost-usd", "1.35",
		"--yes",
	}
	if err := runDeepStartWithProvisioner(args, fixture.paths, fixture.launcherPath, fixture.stintBinary, runner, provisioner, &stdout, &stderr, now); err != nil {
		t.Fatal(err)
	}
	if provisionCalls != 1 || launcherCalls != 1 {
		t.Fatalf("calls provision=%d launcher=%d, want 1/1", provisionCalls, launcherCalls)
	}
	joined := strings.Join(gotProvisionArgs, " ")
	for _, want := range []string{
		"interactive",
		"--hours 3",
		"--runtime ninfer",
		"--ninfer-deployment release-bundle",
		"--ninfer-config native",
		"--clients 2",
		"--max-hourly-usd 0.45",
		"--max-cost-usd 1.35",
		"--yes",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("provision args %q missing %q", joined, want)
		}
	}
	if !strings.Contains(stdout.String(), "provisioning compute for Deep Work") ||
		!strings.Contains(stdout.String(), "is detached and RUNNING") {
		t.Fatalf("stdout missing one-command lifecycle evidence:\n%s", stdout.String())
	}
}

func TestDeepStartRequiresExplicitHoursBeforeRenting(t *testing.T) {
	fixture := newDeepProductionFixture(t)
	if err := sessionstate.Clear(fixture.paths); err != nil {
		t.Fatal(err)
	}
	provisionCalled := false
	provisioner := func(context.Context, string, []string, io.Writer, io.Writer) error {
		provisionCalled = true
		return nil
	}
	runner := func(context.Context, string, []string, io.Writer, io.Writer) ([]byte, error) {
		t.Fatal("launcher must not run")
		return nil, nil
	}
	err := runDeepStartWithProvisioner(
		[]string{"--repo", fixture.repo, "--mission", fixture.mission, "--github-token-file", fixture.tokenPath, "--runtime", "ninfer"},
		fixture.paths, fixture.launcherPath, fixture.stintBinary, runner, provisioner,
		io.Discard, io.Discard, time.Now().UTC(),
	)
	if err == nil || !strings.Contains(err.Error(), "explicit --hours") {
		t.Fatalf("error = %v, want explicit --hours requirement", err)
	}
	if provisionCalled {
		t.Fatal("compute provisioner ran without an explicit paid-duration cap")
	}
}

func TestDeepStartValidatesMissionBeforeProvisioning(t *testing.T) {
	fixture := newDeepProductionFixture(t)
	if err := sessionstate.Clear(fixture.paths); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.mission, []byte("not a valid mission\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	provisionCalled := false
	provisioner := func(context.Context, string, []string, io.Writer, io.Writer) error {
		provisionCalled = true
		return nil
	}
	err := runDeepStartWithProvisioner(
		[]string{"--repo", fixture.repo, "--mission", fixture.mission, "--github-token-file", fixture.tokenPath, "--hours", "3"},
		fixture.paths, fixture.launcherPath, fixture.stintBinary,
		func(context.Context, string, []string, io.Writer, io.Writer) ([]byte, error) {
			t.Fatal("launcher must not run")
			return nil, nil
		},
		provisioner, io.Discard, io.Discard, time.Now().UTC(),
	)
	if err == nil || !strings.Contains(err.Error(), "validate mission") {
		t.Fatalf("error = %v, want mission validation failure", err)
	}
	if provisionCalled {
		t.Fatal("compute provisioner ran before mission validation")
	}
}

func TestDeepStartDoesNotProvisionWhenLaunchPreflightInputIsMissing(t *testing.T) {
	tests := []struct {
		name      string
		flag      string
		value     string
		wantError string
	}{
		{name: "mission", flag: "--mission", value: "missing-mission.md", wantError: "read mission"},
		{name: "repository", flag: "--repo", value: "missing-repository", wantError: "resolve repository path"},
		{name: "action plan", flag: "--action-plan", value: "plans/missing.md", wantError: "action-plan seed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDeepProductionFixture(t)
			if err := sessionstate.Clear(fixture.paths); err != nil {
				t.Fatal(err)
			}
			args := []string{
				"--repo", fixture.repo,
				"--mission", fixture.mission,
				"--github-token-file", fixture.tokenPath,
				"--hours", "3",
				test.flag, test.value,
			}
			provisionCalls := 0
			err := runDeepStartWithProvisioner(
				args, fixture.paths, fixture.launcherPath, fixture.stintBinary,
				func(context.Context, string, []string, io.Writer, io.Writer) ([]byte, error) {
					t.Fatal("launcher must not run for invalid local inputs")
					return nil, nil
				},
				func(context.Context, string, []string, io.Writer, io.Writer) error {
					provisionCalls++
					return nil
				},
				io.Discard, io.Discard, time.Now().UTC(),
			)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
			if provisionCalls != 0 {
				t.Fatalf("provisioner called %d times for invalid %s input", provisionCalls, test.name)
			}
		})
	}
}

func TestDeepStartDoesNotSilentlyIgnoreProvisionFlagsOnReadySession(t *testing.T) {
	fixture := newDeepProductionFixture(t)
	provisioner := func(context.Context, string, []string, io.Writer, io.Writer) error {
		t.Fatal("provisioner must not run when READY compute exists")
		return nil
	}
	runner := func(context.Context, string, []string, io.Writer, io.Writer) ([]byte, error) {
		t.Fatal("launcher must not run when provisioning flags conflict with existing READY compute")
		return nil, nil
	}
	err := runDeepStartWithProvisioner(
		[]string{"--repo", fixture.repo, "--mission", fixture.mission, "--github-token-file", fixture.tokenPath, "--hours", "3"},
		fixture.paths, fixture.launcherPath, fixture.stintBinary, runner, provisioner,
		io.Discard, io.Discard, time.Now().UTC(),
	)
	if err == nil || !strings.Contains(err.Error(), "READY Stint compute session already exists") {
		t.Fatalf("error = %v, want explicit existing-session conflict", err)
	}
}

func TestDeepStartProvisionFailureDoesNotLaunchMission(t *testing.T) {
	fixture := newDeepProductionFixture(t)
	if err := sessionstate.Clear(fixture.paths); err != nil {
		t.Fatal(err)
	}
	provisionErr := errors.New("no qualifying offers remain")
	launcherCalled := false
	err := runDeepStartWithProvisioner(
		[]string{"--repo", fixture.repo, "--mission", fixture.mission, "--github-token-file", fixture.tokenPath, "--hours", "3"},
		fixture.paths, fixture.launcherPath, fixture.stintBinary,
		func(context.Context, string, []string, io.Writer, io.Writer) ([]byte, error) {
			launcherCalled = true
			return nil, nil
		},
		func(context.Context, string, []string, io.Writer, io.Writer) error {
			return provisionErr
		},
		io.Discard, io.Discard, time.Now().UTC(),
	)
	if err == nil || !strings.Contains(err.Error(), provisionErr.Error()) {
		t.Fatalf("error = %v, want provisioning failure", err)
	}
	if launcherCalled {
		t.Fatal("detached launcher ran after compute provisioning failed")
	}
}
