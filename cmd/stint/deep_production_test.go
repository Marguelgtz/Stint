package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/deep"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const productionMissionFixture = `# Spark MCP graduation

## Objective
Expose the existing deterministic evaluator through one stdio MCP tool.

## Tasks
- [ ] MCP-001: Add the MCP adapter.
  - acceptance: the server exposes evaluate_change.
  - verify: pnpm test

## GitHub
- mode: engineering
- repository: spark-opp/spark
- base: main
- approval: internal
`

type deepProductionFixture struct {
	paths        config.Paths
	session      sessionstate.State
	repo         string
	mission      string
	tokenPath    string
	launcherPath string
	stintBinary  string
	deadline     time.Time
}

func newDeepProductionFixture(t *testing.T) deepProductionFixture {
	t.Helper()
	root := t.TempDir()
	paths := config.Paths{
		ConfigDir:       filepath.Join(root, "config", "stint"),
		StateDir:        filepath.Join(root, "state", "stint"),
		CredentialsFile: filepath.Join(root, "config", "stint", "credentials.json"),
		SSHDir:          filepath.Join(root, "config", "stint", "ssh"),
		SSHPrivateKey:   filepath.Join(root, "config", "stint", "ssh", "id_ed25519"),
		SSHPublicKey:    filepath.Join(root, "config", "stint", "ssh", "id_ed25519.pub"),
	}
	for _, dir := range []string{paths.ConfigDir, paths.StateDir, paths.SSHDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(paths.SSHPrivateKey, []byte("fixture-private-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.CredentialsFile, []byte(`{"vast":{"api_key":"vast-secret-fixture"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(paths.ConfigDir, "github-token")
	if err := os.WriteFile(tokenPath, []byte("github-secret-fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	runFixtureCommand(t, repo, "git", "init", "-q")
	runFixtureCommand(t, repo, "git", "config", "user.email", "fixture@example.invalid")
	runFixtureCommand(t, repo, "git", "config", "user.name", "Fixture")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runFixtureCommand(t, repo, "git", "add", "README.md")
	runFixtureCommand(t, repo, "git", "commit", "-q", "-m", "fixture commit")
	runFixtureCommand(t, repo, "git", "remote", "add", "origin", "https://github.com/spark-opp/spark.git")
	mission := filepath.Join(root, "mission.md")
	if err := os.WriteFile(mission, []byte(productionMissionFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	session := sessionstate.State{
		InstanceID: 49812, GPUModel: "RTX_4090", Runtime: "ninfer", RuntimeDeployment: "release-bundle",
		Clients: 2, HourlyUSD: 0.55, Hours: 3, StartedAt: time.Now().UTC().Add(-20 * time.Minute),
		RentalStartedAt: time.Now().UTC().Add(-20 * time.Minute), Deadline: deadline,
		SSHHost: "203.0.113.25", SSHPort: 22110, Status: sessionstate.StatusReady,
	}
	if err := sessionstate.Save(paths, session); err != nil {
		t.Fatal(err)
	}
	return deepProductionFixture{
		paths: paths, session: session, repo: repo, mission: mission, tokenPath: tokenPath,
		launcherPath: filepath.Join(root, "scripts", "launch-onbox-deep.sh"),
		stintBinary:  filepath.Join(root, "bin", "stint"), deadline: deadline,
	}
}

func runFixtureCommand(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func productionFlagsFor(fixture deepProductionFixture) *deepProductionStartFlags {
	return &deepProductionStartFlags{
		missionPath: fixture.mission, repoPath: fixture.repo, githubToken: fixture.tokenPath,
		taskTimeout: 15 * time.Minute, maxAttempts: 2, provider: "custom:qwen-stint-{reasoning}",
		model: "qwen3.8-27b", reasoning: "medium",
	}
}

func prepareProductionFixture(t *testing.T, fixture deepProductionFixture) *deepProductionLaunchPlan {
	t.Helper()
	plan, err := prepareDeepProductionLaunch(productionFlagsFor(fixture), fixture.paths, fixture.launcherPath, fixture.stintBinary, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestDeepProductionStartBuildsIdentityContractAndWaitsForDurableRunning(t *testing.T) {
	fixture := newDeepProductionFixture(t)
	t.Setenv("STINT_BOX_HOST", "wrong-host-from-shell")
	t.Setenv("STINT_BOX_PORT", "1")
	t.Setenv("STINT_BOX_KEY", "wrong-key-from-shell")
	t.Setenv("STINT_ONBOX_SKIP_WATCHDOG", "1")
	head, err := runLocalGit(fixture.repo, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head) == "" {
		t.Fatalf("source HEAD = %q, %v", head, err)
	}
	head = strings.TrimSpace(head)
	var stdout, stderr bytes.Buffer
	var missionSnapshot string
	runner := func(_ context.Context, launcher string, env []string, out, errOut io.Writer) ([]byte, error) {
		if launcher != fixture.launcherPath {
			t.Fatalf("launcher = %q", launcher)
		}
		values := envMap(env)
		joinedEnv := strings.Join(env, "\n")
		if strings.Contains(joinedEnv, "github-secret-fixture") || strings.Contains(joinedEnv, "vast-secret-fixture") {
			t.Error("launcher environment contains a plaintext credential")
		}
		want := map[string]string{
			"STINT_BOX_HOST": "203.0.113.25", "STINT_BOX_PORT": "22110", "STINT_BOX_KEY": fixture.paths.SSHPrivateKey,
			"STINT_INSTANCE_ID": "49812", "STINT_ONBOX_CLIENTS": "2", "STINT_ONBOX_MODEL": "qwen3.8-27b",
			"STINT_ONBOX_TASK_TIMEOUT": "15m0s", "STINT_ONBOX_MAX_ATTEMPTS": "2",
			"STINT_GITHUB_REPOSITORY": "spark-opp/spark", "STINT_GITHUB_BASE": "main",
			"STINT_GITHUB_MODE": "engineering", "STINT_GITHUB_APPROVAL": "internal",
			"STINT_VAST_CREDENTIALS": fixture.paths.CredentialsFile,
			"STINT_SOURCE_HEAD":      head, "STINT_SOURCE_ORIGIN": "https://github.com/spark-opp/spark.git",
		}
		for key, expected := range want {
			if values[key] != expected {
				t.Errorf("env %s = %q, want %q", key, values[key], expected)
			}
		}
		if values["STINT_SESSION_JSON"] != sessionstate.Path(fixture.paths) {
			t.Errorf("session file = %q", values["STINT_SESSION_JSON"])
		}
		if values["STINT_GITHUB_TOKEN_FILE"] != fixture.tokenPath {
			t.Errorf("token path = %q", values["STINT_GITHUB_TOKEN_FILE"])
		}
		if values["STINT_ONBOX_SKIP_WATCHDOG"] != "0" || values["STINT_ONBOX_SKIP_GITHUB"] != "0" {
			t.Errorf("production safety flags = watchdog %q github %q", values["STINT_ONBOX_SKIP_WATCHDOG"], values["STINT_ONBOX_SKIP_GITHUB"])
		}
		missionSnapshot = values["STINT_MISSION"]
		content, err := os.ReadFile(missionSnapshot)
		if err != nil || string(content) != productionMissionFixture {
			t.Errorf("mission snapshot content = %q, err %v", content, err)
		}
		_, _ = out.Write([]byte("ONBOX_SUPERVISOR_RUNNING pid=321\n"))
		_, _ = errOut.Write(nil)
		return []byte("ONBOX_SUPERVISOR_RUNNING pid=321\n{" +
			`"status":"RUNNING","session":"deep-20260924-120000","deadline":"` + fixture.deadline.Format(time.RFC3339) + `"}` + "\n"), nil
	}
	args := []string{"--repo", fixture.repo, "--mission", fixture.mission, "--github-token-file", fixture.tokenPath, "--task-timeout", "15m", "--max-attempts", "2"}
	if err := runDeepStartWith(args, fixture.paths, fixture.launcherPath, fixture.stintBinary, runner, &stdout, &stderr, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Source commit") || !strings.Contains(stdout.String(), "Remaining cost") || !strings.Contains(stdout.String(), "is detached and RUNNING") {
		t.Fatalf("run contract/confirmation missing from stdout:\n%s", stdout.String())
	}
	if _, err := os.Stat(missionSnapshot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary mission snapshot was not cleaned up: %v", err)
	}
	binding, err := loadDeepProductionBinding(fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	if binding.InstanceID != fixture.session.InstanceID || binding.DeepSessionID != "deep-20260924-120000" || binding.SourceCommit != head {
		t.Fatalf("saved production dashboard binding = %+v", binding)
	}
	if bytes.Contains(stdout.Bytes(), []byte("github-secret-fixture")) || bytes.Contains(stderr.Bytes(), []byte("vast-secret-fixture")) {
		t.Fatal("a secret was printed during launch")
	}
}

func TestDeepProductionStartRejectsInvalidLocalStateBeforeLauncher(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *deepProductionFixture)
		want  string
	}{
		{name: "missing session", setup: func(t *testing.T, f *deepProductionFixture) {
			if err := sessionstate.Clear(f.paths); err != nil {
				t.Fatal(err)
			}
		}, want: "no active Stint compute session"},
		{name: "provisioning session", setup: func(t *testing.T, f *deepProductionFixture) {
			f.session.Status = sessionstate.StatusRuntimeReady
			if err := sessionstate.Save(f.paths, f.session); err != nil {
				t.Fatal(err)
			}
		}, want: "not READY"},
		{name: "stopped session", setup: func(t *testing.T, f *deepProductionFixture) {
			f.session.Status = "STOPPED"
			if err := sessionstate.Save(f.paths, f.session); err != nil {
				t.Fatal(err)
			}
		}, want: "not READY"},
		{name: "failed session", setup: func(t *testing.T, f *deepProductionFixture) {
			f.session.Status = "FAILED"
			if err := sessionstate.Save(f.paths, f.session); err != nil {
				t.Fatal(err)
			}
		}, want: "not READY"},
		{name: "expired session", setup: func(t *testing.T, f *deepProductionFixture) {
			f.session.Deadline = time.Now().UTC().Add(-time.Minute)
			if err := sessionstate.Save(f.paths, f.session); err != nil {
				t.Fatal(err)
			}
		}, want: "expired"},
		{name: "missing SSH host", setup: func(t *testing.T, f *deepProductionFixture) {
			f.session.SSHHost = ""
			if err := sessionstate.Save(f.paths, f.session); err != nil {
				t.Fatal(err)
			}
		}, want: "missing a valid SSH host"},
		{name: "untracked repository change", setup: func(t *testing.T, f *deepProductionFixture) {
			if err := os.WriteFile(filepath.Join(f.repo, "unrelated.tmp"), []byte("dirty"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, want: "uncommitted changes"},
		{name: "embedded origin credentials", setup: func(t *testing.T, f *deepProductionFixture) {
			runFixtureCommand(t, f.repo, "git", "remote", "set-url", "origin", "https://user:secret@example.com/repo.git")
		}, want: "embedded user credentials"},
		{name: "mission without GitHub policy", setup: func(t *testing.T, f *deepProductionFixture) {
			if err := os.WriteFile(f.mission, []byte("# Invalid\n\n## Objective\nDo something.\n\n## Tasks\n- [ ] T-001: Work\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, want: "explicit ## GitHub policy"},
		{name: "missing token", setup: func(t *testing.T, f *deepProductionFixture) {
			f.tokenPath = filepath.Join(t.TempDir(), "missing-token")
		}, want: "GitHub publisher token is unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newDeepProductionFixture(t)
			tt.setup(t, &fixture)
			called := false
			runner := func(context.Context, string, []string, io.Writer, io.Writer) ([]byte, error) {
				called = true
				return nil, nil
			}
			args := []string{"--repo", fixture.repo, "--mission", fixture.mission, "--github-token-file", fixture.tokenPath}
			err := runDeepStartWith(args, fixture.paths, fixture.launcherPath, fixture.stintBinary, runner, new(bytes.Buffer), new(bytes.Buffer), time.Now().UTC())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("prepare error = %v, want substring %q", err, tt.want)
			}
			if called {
				t.Fatal("production launcher ran despite failed local validation")
			}
		})
	}
}

func TestDeepProductionStartRejectsMissingInvalidMissionAndInvalidRepository(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *deepProductionFixture)
		want   string
	}{
		{name: "missing mission", mutate: func(_ *testing.T, f *deepProductionFixture) {
			f.mission = filepath.Join(filepath.Dir(f.mission), "missing.md")
		}, want: "read mission"},
		{name: "invalid mission", mutate: func(t *testing.T, f *deepProductionFixture) {
			if err := os.WriteFile(f.mission, []byte("not a mission"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, want: "mission"},
		{name: "non-git repository", mutate: func(_ *testing.T, f *deepProductionFixture) { f.repo = t.TempDir() }, want: "invalid repository"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newDeepProductionFixture(t)
			tt.mutate(t, &fixture)
			called := false
			runner := func(context.Context, string, []string, io.Writer, io.Writer) ([]byte, error) {
				called = true
				return nil, nil
			}
			args := []string{"--repo", fixture.repo, "--mission", fixture.mission, "--github-token-file", fixture.tokenPath}
			err := runDeepStartWith(args, fixture.paths, fixture.launcherPath, fixture.stintBinary, runner, new(bytes.Buffer), new(bytes.Buffer), time.Now().UTC())
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.want)) {
				t.Fatalf("prepare error = %v, want substring %q", err, tt.want)
			}
			if called {
				t.Fatal("production launcher ran despite failed local validation")
			}
		})
	}
}

func TestValidateDeepSourceOriginRejectsSecretsAndAllowsCommonGitRemotes(t *testing.T) {
	for _, origin := range []string{
		"https://user:token@example.com/owner/repo.git",
		"ssh://git:password@example.com/owner/repo.git",
		"https://example.com/owner/repo.git?access_token=secret",
	} {
		if err := validateDeepSourceOrigin(origin); err == nil {
			t.Errorf("origin %q unexpectedly accepted", origin)
		}
	}
	for _, origin := range []string{
		"git@github.com:owner/repo.git",
		"ssh://git@github.com/owner/repo.git",
		"https://github.com/owner/repo.git",
	} {
		if err := validateDeepSourceOrigin(origin); err != nil {
			t.Errorf("origin %q rejected: %v", origin, err)
		}
	}
}

func TestDeepProductionLauncherFailureAndMissingHandshakeDoNotSaveBinding(t *testing.T) {
	fixture := newDeepProductionFixture(t)
	plan := prepareProductionFixture(t, fixture)
	for _, tt := range []struct {
		name   string
		output string
		runErr error
		want   string
	}{
		{name: "launcher error", output: "ONBOX_LAUNCH_FAIL bootstrap failed\n", runErr: errors.New("exit status 1"), want: "bootstrap failed"},
		{name: "exit zero without supervisor marker", output: `{"status":"RUNNING","session":"deep-1","deadline":"` + fixture.deadline.Format(time.RFC3339) + `"}` + "\n", want: "without the detached supervisor marker"},
		{name: "supervisor without durable record", output: "ONBOX_SUPERVISOR_RUNNING pid=9\n", want: "without the detached supervisor marker"},
		{name: "wrong deadline", output: "ONBOX_SUPERVISOR_RUNNING pid=9\n{" + `"status":"RUNNING","session":"deep-1","deadline":"2026-09-24T00:00:00Z"}` + "\n", want: "does not match READY Stint session deadline"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout := new(bytes.Buffer)
			runner := func(_ context.Context, _ string, _ []string, _ io.Writer, _ io.Writer) ([]byte, error) {
				return []byte(tt.output), tt.runErr
			}
			err := executeDeepProductionLaunch(context.Background(), plan, runner, stdout, new(bytes.Buffer))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("launch error = %v, want substring %q", err, tt.want)
			}
			if _, err := os.Stat(filepath.Join(fixture.paths.StateDir, deepProductionBinding)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("launch wrote a production binding without a valid RUNNING handshake: %v", err)
			}
		})
	}
}

func TestParseDeepProductionHandshakeRequiresBothMarkersAndMatchingDeadline(t *testing.T) {
	deadline := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	valid := []byte("ONBOX_SUPERVISOR_RUNNING pid=51\n{" + `"status":"RUNNING","session":"deep-51","deadline":"2026-09-24T12:00:00Z"}` + "\n")
	got, err := parseDeepProductionHandshake(valid, deadline)
	if err != nil || got.Session != "deep-51" {
		t.Fatalf("valid handshake = %+v, %v", got, err)
	}
	if _, err := parseDeepProductionHandshake([]byte("ONBOX_SUPERVISOR_STARTED pid=51\n"+string(valid[strings.Index(string(valid), "{"):])), deadline); err == nil {
		t.Fatal("started marker was accepted without the supervisor RUNNING marker")
	}
}

func envMap(env []string) map[string]string {
	result := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			result[key] = value
		}
	}
	return result
}

func TestDeepProductionDashboardBindingRoundTripsWithoutCredentials(t *testing.T) {
	fixture := newDeepProductionFixture(t)
	binding := deepProductionRunBinding{
		InstanceID: fixture.session.InstanceID, SSHHost: fixture.session.SSHHost, SSHPort: fixture.session.SSHPort,
		RemoteRoot: deepProductionRemoteRoot, DeepSessionID: "deep-abc", Deadline: fixture.deadline,
		SourceCommit: "abcdef", Mission: "Spark MCP graduation", LaunchedAt: time.Now().UTC(),
	}
	if err := saveDeepProductionBinding(fixture.paths, binding); err != nil {
		t.Fatal(err)
	}
	got, err := loadDeepProductionBinding(fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	if got != binding {
		t.Fatalf("binding = %+v, want %+v", got, binding)
	}
	data, err := os.ReadFile(filepath.Join(fixture.paths.StateDir, deepProductionBinding))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("github-secret-fixture")) || bytes.Contains(data, []byte("vast-secret-fixture")) {
		t.Fatal("dashboard binding persisted a credential")
	}
}

func TestDeepProductionDashboardCommandUsesSavedOnBoxStateAndSession(t *testing.T) {
	binding := deepProductionRunBinding{
		RemoteRoot:    "/var/lib/stint-onbox",
		DeepSessionID: "deep-20260924-120000",
	}
	command := deepProductionDashboardCommand(binding, "", true, true)
	for _, want := range []string{
		"XDG_STATE_HOME='/var/lib/stint-onbox/state'",
		"'/var/lib/stint-onbox/bin/stint' deep dash",
		"--session 'deep-20260924-120000'", "--no-color", "--refresh",
	} {
		if !strings.Contains(command, want) {
			t.Errorf("remote dashboard command %q missing %q", command, want)
		}
	}
	if strings.Contains(command, "github-token") || strings.Contains(command, "credentials.json") {
		t.Fatalf("remote dashboard command contains a credential path: %q", command)
	}
}

func TestDeepDashboardPrefersNewestProductionBindingAndRespectsExplicitSession(t *testing.T) {
	launchedAt := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	binding := deepProductionRunBinding{DeepSessionID: "production-session", LaunchedAt: launchedAt}
	controller := &deepDashboardController{snapshot: deepDashboardSnapshot{State: deep.DeepState{SessionID: "older-local", StartedAt: launchedAt.Add(-time.Hour)}}}
	if !shouldRouteDeepDashboardRemote(controller, binding, "") {
		t.Fatal("default dashboard should route to the newer production run")
	}
	if !shouldRouteDeepDashboardRemote(controller, binding, "production-session") {
		t.Fatal("explicit production session should route remotely")
	}
	if shouldRouteDeepDashboardRemote(controller, binding, "older-local") {
		t.Fatal("explicit local historical session should remain local")
	}
	controller.model.Error = "no local state"
	if !shouldRouteDeepDashboardRemote(controller, binding, "") {
		t.Fatal("dashboard should route remotely when local state is unavailable")
	}
}
