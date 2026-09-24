package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/deep"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const (
	deepProductionRemoteRoot = "/var/lib/stint-onbox"
	deepProductionBinding    = "deep/production-binding.json"
	deepProductionTimeout    = 15 * time.Minute
	deepProductionAttempts   = 2
	deepProductionModel      = "qwen3.8-27b"
)

type deepProductionStartFlags struct {
	missionPath   string
	repoPath      string
	taskTimeout   time.Duration
	maxAttempts   int
	allowCommands stringSlice
	provider      string
	model         string
	reasoning     string
	actionPlan    string
	githubToken   string
	r2Env         string
}

type deepProductionLaunchPlan struct {
	Paths         config.Paths
	Session       sessionstate.State
	Mission       deep.Mission
	MissionBytes  []byte
	MissionPath   string
	RepoPath      string
	SourceHead    string
	SourceOrigin  string
	Launcher      string
	StintBinary   string
	RemoteRoot    string
	TaskTimeout   time.Duration
	MaxAttempts   int
	Provider      string
	Model         string
	Reasoning     string
	ActionPlan    string
	AllowCommands []string
	GitHubToken   string
	R2Env         string
	Clients       int
	ClientsKnown  bool
	CreatedAt     time.Time
}

type deepProductionRunBinding struct {
	InstanceID    int64     `json:"instanceId"`
	SSHHost       string    `json:"sshHost"`
	SSHPort       int       `json:"sshPort"`
	RemoteRoot    string    `json:"remoteRoot"`
	DeepSessionID string    `json:"deepSessionId"`
	Deadline      time.Time `json:"deadline"`
	SourceCommit  string    `json:"sourceCommit"`
	Mission       string    `json:"mission"`
	LaunchedAt    time.Time `json:"launchedAt"`
}

type deepProductionLauncher func(context.Context, string, []string, io.Writer, io.Writer) ([]byte, error)

// runDeepStart is the public READY-session to detached production launch path.
func runDeepStart(args []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	launcher, err := findDeepProductionLauncher()
	if err != nil {
		return err
	}
	stintBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve running Stint binary: %w", err)
	}
	stintBinary, err = filepath.EvalSymlinks(stintBinary)
	if err != nil {
		return fmt.Errorf("resolve running Stint binary path: %w", err)
	}
	return runDeepStartWith(args, paths, launcher, stintBinary, runDeepProductionLauncher, os.Stdout, os.Stderr, time.Now().UTC())
}

func runDeepStartWith(args []string, paths config.Paths, launcher, stintBinary string, runner deepProductionLauncher, stdout, stderr io.Writer, now time.Time) error {
	fs := flag.NewFlagSet("deep start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	f := &deepProductionStartFlags{}
	fs.StringVar(&f.missionPath, "mission", "", "mission Markdown file (required)")
	fs.StringVar(&f.repoPath, "repo", "", "target git repository (required; must be clean)")
	fs.DurationVar(&f.taskTimeout, "task-timeout", 0, "maximum wall time per Hermes task (default: 15m)")
	fs.IntVar(&f.maxAttempts, "max-attempts", 0, "maximum attempts per task (default: 2)")
	fs.Var(&f.allowCommands, "allow-command", "advisory command prefix for Hermes (repeatable)")
	fs.StringVar(&f.provider, "provider", "custom:qwen-stint-{reasoning}", "configured Hermes provider id or reasoning template")
	fs.StringVar(&f.model, "model", "", "Hermes model id (default: qwen3.8-27b)")
	fs.StringVar(&f.reasoning, "reasoning", deep.ReasoningMedium, "Hermes reasoning effort: none, low, medium, or xhigh")
	fs.StringVar(&f.actionPlan, "action-plan", "", "optional local or repository-relative action-plan seed")
	fs.StringVar(&f.githubToken, "github-token-file", "", "GitHub token file (default: ~/.config/stint/github-token)")
	fs.StringVar(&f.r2Env, "r2-env-file", "", "optional R2 environment file (default: ~/.config/vanta-r2.env when present)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("deep start does not accept positional arguments")
	}
	if f.missionPath == "" || f.repoPath == "" {
		return errors.New("deep start requires --mission <file> and --repo <path>")
	}
	var taskTimeoutSet, maxAttemptsSet bool
	fs.Visit(func(item *flag.Flag) {
		if item.Name == "task-timeout" {
			taskTimeoutSet = true
		}
		if item.Name == "max-attempts" {
			maxAttemptsSet = true
		}
	})
	if taskTimeoutSet && f.taskTimeout <= 0 {
		return errors.New("--task-timeout must be positive")
	}
	if maxAttemptsSet && f.maxAttempts < 1 {
		return errors.New("--max-attempts must be at least 1")
	}
	if f.taskTimeout == 0 {
		f.taskTimeout = deepProductionTimeout
	}
	if f.maxAttempts == 0 {
		f.maxAttempts = deepProductionAttempts
	}
	var err error
	if f.reasoning, err = deep.NormalizeReasoning(f.reasoning); err != nil {
		return fmt.Errorf("--reasoning: %w", err)
	}
	if f.reasoning == "" {
		return errors.New("--reasoning cannot be empty")
	}
	plan, err := prepareDeepProductionLaunch(f, paths, launcher, stintBinary, now)
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}
	return executeDeepProductionLaunch(context.Background(), plan, runner, stdout, stderr)
}

func findDeepProductionLauncher() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve Stint executable: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	candidates := []string{
		filepath.Join(filepath.Dir(executable), "..", "scripts", "launch-onbox-deep.sh"),
	}
	if cwd, cwdErr := os.Getwd(); cwdErr == nil {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			candidates = append(candidates, filepath.Join(dir, "scripts", "launch-onbox-deep.sh"))
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	for _, candidate := range candidates {
		candidate, err = filepath.Abs(candidate)
		if err != nil {
			continue
		}
		info, statErr := os.Stat(candidate)
		if statErr == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", errors.New("production Deep Work launcher was not found beside this Stint binary or in the current project; build/use Stint from a checkout containing scripts/launch-onbox-deep.sh")
}

func prepareDeepProductionLaunch(f *deepProductionStartFlags, paths config.Paths, launcher, stintBinary string, now time.Time) (*deepProductionLaunchPlan, error) {
	if f == nil {
		return nil, errors.New("missing Deep Work launch options")
	}
	missionPath, err := filepath.Abs(f.missionPath)
	if err != nil {
		return nil, fmt.Errorf("resolve mission path: %w", err)
	}
	missionBytes, err := os.ReadFile(missionPath)
	if err != nil {
		return nil, fmt.Errorf("read mission %s: %w", missionPath, err)
	}
	mission, err := deep.ParseMission(string(missionBytes))
	if err != nil {
		return nil, fmt.Errorf("validate mission %s: %w", missionPath, err)
	}
	if !mission.GitHubConfigured {
		return nil, errors.New("production missions must declare an explicit ## GitHub policy (mode, repository, base, approval)")
	}
	if err := mission.GitHub.Validate(); err != nil {
		return nil, fmt.Errorf("mission GitHub policy: %w", err)
	}
	if mission.GitHub.Mode != deep.GitHubEngineering {
		return nil, errors.New("the production on-box publisher currently supports GitHub mode `engineering` only")
	}

	repoPath, err := filepath.Abs(f.repoPath)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	repoPath, err = filepath.EvalSymlinks(repoPath)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path %s: %w", f.repoPath, err)
	}
	inside, err := runLocalGit(repoPath, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		if err == nil {
			err = errors.New("not inside a git worktree")
		}
		return nil, fmt.Errorf("invalid repository %s: %w", repoPath, err)
	}
	status, err := runLocalGit(repoPath, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return nil, fmt.Errorf("inspect repository status: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return nil, fmt.Errorf("repository %s has uncommitted changes; commit or stash these before Deep Work stages HEAD:\n%s", repoPath, strings.TrimSpace(status))
	}
	head, err := runLocalGit(repoPath, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("read repository HEAD: %w", err)
	}
	head = strings.TrimSpace(head)
	if head == "" {
		return nil, errors.New("repository HEAD is empty")
	}
	origin, err := runLocalGit(repoPath, "remote", "get-url", "origin")
	if err != nil {
		return nil, fmt.Errorf("repository must have an origin remote for production staging: %w", err)
	}
	origin = strings.TrimSpace(origin)
	if err := validateDeepSourceOrigin(origin); err != nil {
		return nil, err
	}

	session, err := sessionstate.Load(paths)
	if err != nil {
		return nil, fmt.Errorf("no active Stint compute session; run `stint start interactive` first: %w", err)
	}
	if session.Status != sessionstate.StatusReady {
		return nil, fmt.Errorf("Stint compute session is %s, not READY; resume or start a session and wait for READY", session.Status)
	}
	if session.InstanceID <= 0 {
		return nil, errors.New("READY Stint session has no valid Vast instance identity")
	}
	if !session.Deadline.After(now) {
		return nil, errors.New("READY Stint compute session has expired")
	}
	if !validSSHHost(session.SSHHost) || session.SSHPort < 1 || session.SSHPort > 65535 {
		return nil, errors.New("READY Stint session is missing a valid SSH host or port; run `stint resume` to refresh its connection details")
	}
	keyInfo, err := os.Stat(paths.SSHPrivateKey)
	if err != nil || !keyInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("Stint SSH private key is missing or unreadable at %s", paths.SSHPrivateKey)
	}
	if keyInfo.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("Stint SSH private key at %s must not be accessible by group or others; run `chmod 600 %s`", paths.SSHPrivateKey, paths.SSHPrivateKey)
	}
	if _, err := config.LoadCredentials(paths); err != nil {
		return nil, fmt.Errorf("Vast credentials required by the production deadline watchdog are unavailable at %s: %w", paths.CredentialsFile, err)
	}

	githubToken := strings.TrimSpace(f.githubToken)
	if githubToken == "" {
		githubToken = filepath.Join(paths.ConfigDir, "github-token")
	}
	githubToken, err = filepath.Abs(githubToken)
	if err != nil {
		return nil, fmt.Errorf("resolve GitHub token path: %w", err)
	}
	if err := validateGitHubTokenFile(githubToken); err != nil {
		return nil, err
	}

	r2Env, err := resolveDeepR2Env(paths, f.r2Env)
	if err != nil {
		return nil, err
	}
	clients, clientsKnown := session.Clients, session.Clients > 0
	if clients < 1 {
		clients = 1 // the production launcher default for older session snapshots
	}

	actionPlan := strings.TrimSpace(f.actionPlan)
	if actionPlan != "" {
		if strings.IndexByte(actionPlan, 0) >= 0 {
			return nil, errors.New("--action-plan contains a NUL byte")
		}
		if filepath.IsAbs(actionPlan) {
			actionPlan, err = filepath.Abs(actionPlan)
		} else {
			actionPlan, err = actionPlanPath(actionPlan)
		}
		if err != nil {
			return nil, err
		}
		seedPath := actionPlan
		if !filepath.IsAbs(seedPath) {
			seedPath = filepath.Join(repoPath, filepath.FromSlash(seedPath))
		}
		seed, err := os.Open(seedPath)
		if err != nil {
			return nil, fmt.Errorf("action-plan seed %s is not readable: %w", seedPath, err)
		}
		seedInfo, statErr := seed.Stat()
		_ = seed.Close()
		if statErr != nil || !seedInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("action-plan seed %s must be a readable regular file", seedPath)
		}
	}

	model := strings.TrimSpace(f.model)
	if model == "" {
		model = deepProductionModel
	}
	provider := strings.TrimSpace(f.provider)
	if provider == "" {
		provider = "custom:qwen-stint-{reasoning}"
	}
	if f.taskTimeout <= 0 {
		f.taskTimeout = deepProductionTimeout
	}
	if f.maxAttempts < 1 {
		f.maxAttempts = deepProductionAttempts
	}
	if launcher == "" {
		return nil, errors.New("production Deep Work launcher path is empty")
	}
	if stintBinary == "" {
		return nil, errors.New("Stint binary path is empty")
	}

	return &deepProductionLaunchPlan{
		Paths: paths, Session: session, Mission: mission, MissionBytes: missionBytes,
		MissionPath: missionPath, RepoPath: repoPath, SourceHead: head, SourceOrigin: origin,
		Launcher: launcher, StintBinary: stintBinary, RemoteRoot: deepProductionRemoteRoot,
		TaskTimeout: f.taskTimeout, MaxAttempts: f.maxAttempts, Provider: provider,
		Model: model, Reasoning: f.reasoning, ActionPlan: actionPlan,
		AllowCommands: append([]string(nil), f.allowCommands...), GitHubToken: githubToken,
		R2Env: r2Env, Clients: clients, ClientsKnown: clientsKnown, CreatedAt: now,
	}, nil
}

func runLocalGit(repo string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func validateDeepSourceOrigin(origin string) error {
	if origin == "" || strings.ContainsAny(origin, "\r\n\x00") {
		return errors.New("repository origin is empty or contains a newline")
	}
	parsed, err := url.Parse(origin)
	if strings.Contains(origin, "://") && err != nil {
		return fmt.Errorf("repository origin is not a valid URL: %w", err)
	}
	if err == nil && parsed.User != nil {
		if parsed.Scheme != "ssh" {
			return errors.New("repository origin contains embedded user credentials; remove them before staging to the GPU")
		}
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return errors.New("repository origin contains an embedded SSH password; remove it before staging to the GPU")
		}
	}
	if err == nil && (parsed.RawQuery != "" || parsed.Fragment != "") && parsed.Scheme != "" {
		return errors.New("repository origin URLs with query strings or fragments are not safe to stage")
	}
	return nil
}

func validSSHHost(host string) bool {
	if strings.TrimSpace(host) != host || host == "" || strings.HasPrefix(host, "-") || strings.ContainsAny(host, " \t\r\n\x00/@") {
		return false
	}
	return true
}

func validateGitHubTokenFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("GitHub publisher token is unavailable at %s: %w", path, err)
	}
	text := strings.TrimSpace(string(data))
	var token string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		if key, value, ok := strings.Cut(line, "="); ok {
			switch strings.TrimSpace(key) {
			case "GITHUB_TOKEN", "GH_TOKEN", "STINT_GITHUB_TOKEN":
				token = strings.Trim(strings.TrimSpace(value), "\"'")
			}
		} else if len(strings.Split(text, "\n")) == 1 {
			token = line
		}
	}
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return fmt.Errorf("GitHub token file %s contains no usable token", path)
	}
	return nil
}

func resolveDeepR2Env(paths config.Paths, explicit string) (string, error) {
	candidate := strings.TrimSpace(explicit)
	if candidate == "" {
		candidate = filepath.Join(filepath.Dir(paths.ConfigDir), "vanta-r2.env")
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return "", nil
		} else if err != nil {
			return "", fmt.Errorf("inspect default R2 configuration %s: %w", candidate, err)
		}
	}
	candidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve R2 configuration path: %w", err)
	}
	file, err := os.Open(candidate)
	if err != nil {
		return "", fmt.Errorf("R2 configuration is unavailable at %s: %w", candidate, err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close R2 configuration %s: %w", candidate, err)
	}
	return candidate, nil
}

func executeDeepProductionLaunch(ctx context.Context, plan *deepProductionLaunchPlan, runner deepProductionLauncher, stdout, stderr io.Writer) error {
	if plan == nil {
		return errors.New("missing Deep Work launch plan")
	}
	missionSnapshot, cleanup, err := writeDeepMissionSnapshot(plan.MissionBytes)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := printDeepRunContract(stdout, plan); err != nil {
		return err
	}
	output, err := runner(ctx, plan.Launcher, deepProductionEnvironment(plan, missionSnapshot), stdout, stderr)
	if err != nil {
		if detail := launcherFailureDetail(output); detail != "" {
			return fmt.Errorf("production Deep Work launcher failed before RUNNING: %s", detail)
		}
		return fmt.Errorf("production Deep Work launcher failed before RUNNING: %w", err)
	}
	handshake, err := parseDeepProductionHandshake(output, plan.Session.Deadline)
	if err != nil {
		return err
	}
	binding := deepProductionRunBinding{
		InstanceID: plan.Session.InstanceID, SSHHost: plan.Session.SSHHost, SSHPort: plan.Session.SSHPort,
		RemoteRoot: plan.RemoteRoot, DeepSessionID: handshake.Session, Deadline: handshake.Deadline,
		SourceCommit: plan.SourceHead, Mission: plan.Mission.Name, LaunchedAt: time.Now().UTC(),
	}
	if err := saveDeepProductionBinding(plan.Paths, binding); err != nil {
		return fmt.Errorf("Deep Work reached RUNNING (session %s), but local dashboard connection state could not be saved: %w", handshake.Session, err)
	}
	fmt.Fprintf(stdout, "Deep Work %s is detached and RUNNING; compute instance %d remains owned by the Stint deadline watchdog.\n", handshake.Session, binding.InstanceID)
	return nil
}

func launcherFailureDetail(output []byte) string {
	for _, raw := range strings.Split(string(output), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "ONBOX_LAUNCH_FAIL ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "ONBOX_LAUNCH_FAIL "))
		}
	}
	return ""
}

func writeDeepMissionSnapshot(content []byte) (string, func(), error) {
	file, err := os.CreateTemp("", "stint-deep-mission-*.md")
	if err != nil {
		return "", nil, fmt.Errorf("create immutable mission snapshot: %w", err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("write immutable mission snapshot: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close immutable mission snapshot: %w", err)
	}
	return path, cleanup, nil
}

func deepProductionEnvironment(plan *deepProductionLaunchPlan, missionSnapshot string) []string {
	values := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok && !strings.HasPrefix(key, "STINT_") {
			values[key] = value
		}
	}
	values["STINT_BOX_HOST"] = plan.Session.SSHHost
	values["STINT_BOX_PORT"] = fmt.Sprint(plan.Session.SSHPort)
	values["STINT_BOX_KEY"] = plan.Paths.SSHPrivateKey
	values["STINT_SESSION_JSON"] = sessionstate.Path(plan.Paths)
	values["STINT_INSTANCE_ID"] = fmt.Sprint(plan.Session.InstanceID)
	values["STINT_MISSION"] = missionSnapshot
	values["STINT_REPO"] = plan.RepoPath
	values["STINT_SOURCE_HEAD"] = plan.SourceHead
	values["STINT_SOURCE_ORIGIN"] = plan.SourceOrigin
	values["STINT_BIN"] = plan.StintBinary
	values["STINT_REMOTE_ROOT"] = plan.RemoteRoot
	values["STINT_VAST_CREDENTIALS"] = plan.Paths.CredentialsFile
	values["STINT_GITHUB_TOKEN_FILE"] = plan.GitHubToken
	values["STINT_GITHUB_REPOSITORY"] = plan.Mission.GitHub.Repository
	values["STINT_GITHUB_BASE"] = plan.Mission.GitHub.Base
	values["STINT_GITHUB_MODE"] = string(plan.Mission.GitHub.Mode)
	values["STINT_GITHUB_APPROVAL"] = string(plan.Mission.GitHub.Approval)
	values["STINT_GITHUB_ALLOWED_AUTHORS"] = strings.Join(plan.Mission.GitHub.AllowedAuthors, ",")
	values["STINT_GITHUB_PR_DRAFT"] = "1"
	values["STINT_ONBOX_CLIENTS"] = fmt.Sprint(plan.Clients)
	values["STINT_ONBOX_PROVIDER"] = plan.Provider
	values["STINT_ONBOX_MODEL"] = plan.Model
	values["STINT_ONBOX_REASONING"] = plan.Reasoning
	values["STINT_ONBOX_TASK_TIMEOUT"] = plan.TaskTimeout.String()
	values["STINT_ONBOX_MAX_ATTEMPTS"] = fmt.Sprint(plan.MaxAttempts)
	values["STINT_ONBOX_SKIP_GITHUB"] = "0"
	values["STINT_ONBOX_SKIP_WATCHDOG"] = "0"
	if plan.ActionPlan != "" {
		values["STINT_ONBOX_ACTION_PLAN"] = plan.ActionPlan
	}
	if len(plan.AllowCommands) > 0 {
		values["STINT_ONBOX_ALLOW_COMMANDS"] = strings.Join(plan.AllowCommands, "\n")
	}
	if plan.R2Env != "" {
		values["STINT_R2_ENV_FILE"] = plan.R2Env
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(values))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

func runDeepProductionLauncher(ctx context.Context, launcher string, env []string, stdout, stderr io.Writer) ([]byte, error) {
	var capturedStdout, capturedStderr bytes.Buffer
	cmd := exec.CommandContext(ctx, launcher)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.MultiWriter(stdout, &capturedStdout)
	cmd.Stderr = io.MultiWriter(stderr, &capturedStderr)
	err := cmd.Run()
	return append(capturedStdout.Bytes(), capturedStderr.Bytes()...), err
}

type deepProductionHandshake struct {
	Session  string    `json:"session"`
	Status   string    `json:"status"`
	Deadline time.Time `json:"deadline"`
}

func parseDeepProductionHandshake(output []byte, expectedDeadline time.Time) (deepProductionHandshake, error) {
	var result deepProductionHandshake
	runningMarker := false
	for _, raw := range strings.Split(string(output), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "ONBOX_SUPERVISOR_RUNNING") {
			runningMarker = true
		}
		var candidate deepProductionHandshake
		if json.Unmarshal([]byte(line), &candidate) == nil && candidate.Status == "RUNNING" {
			result = candidate
		}
	}
	if !runningMarker || result.Status != "RUNNING" || strings.TrimSpace(result.Session) == "" {
		return deepProductionHandshake{}, errors.New("production launcher exited without the detached supervisor marker and durable RUNNING record; Deep Work was not confirmed started")
	}
	if result.Deadline.IsZero() || !result.Deadline.Equal(expectedDeadline) {
		return deepProductionHandshake{}, fmt.Errorf("production RUNNING record has deadline %s, which does not match READY Stint session deadline %s", result.Deadline.Format(time.RFC3339), expectedDeadline.Format(time.RFC3339))
	}
	return result, nil
}

func printDeepRunContract(out io.Writer, plan *deepProductionLaunchPlan) error {
	remaining := time.Until(plan.Session.Deadline).Round(time.Minute)
	clients := fmt.Sprintf("%d", plan.Clients)
	if !plan.ClientsKnown {
		clients += " (launcher default; not recorded in session)"
	}
	fmt.Fprintf(out, "Deep Work production run contract\n")
	fmt.Fprintf(out, "  Repository       %s\n", plan.RepoPath)
	fmt.Fprintf(out, "  Source commit    %s\n", plan.SourceHead)
	fmt.Fprintf(out, "  Origin           %s\n", plan.SourceOrigin)
	fmt.Fprintf(out, "  Working tree     clean; staging committed HEAD only\n")
	fmt.Fprintf(out, "  Mission          %s (%s)\n", plan.Mission.Name, plan.MissionPath)
	fmt.Fprintf(out, "  Compute          Vast instance %d\n", plan.Session.InstanceID)
	fmt.Fprintf(out, "  GPU / runtime    %s / %s\n", valueOr(plan.Session.GPUModel, "unknown"), deepRuntimeDescription(plan.Session))
	fmt.Fprintf(out, "  Remaining        %s\n", remaining)
	fmt.Fprintf(out, "  Deadline         %s\n", plan.Session.Deadline.Format(time.RFC3339))
	fmt.Fprintf(out, "  Clients          %s\n", clients)
	fmt.Fprintf(out, "  Model            %s\n", plan.Model)
	fmt.Fprintf(out, "  Task timeout     %s\n", plan.TaskTimeout)
	fmt.Fprintf(out, "  Max attempts     %d\n", plan.MaxAttempts)
	fmt.Fprintf(out, "  GitHub           %s (base %s, mode %s)\n", plan.Mission.GitHub.Repository, plan.Mission.GitHub.Base, plan.Mission.GitHub.Mode)
	if plan.R2Env == "" {
		fmt.Fprintf(out, "  R2 evidence      disabled\n")
	} else {
		fmt.Fprintf(out, "  R2 evidence      enabled (%s)\n", plan.R2Env)
	}
	if plan.Session.HourlyUSD > 0 {
		started := plan.Session.RentalStartedAt
		if started.IsZero() {
			started = plan.Session.StartedAt
		}
		spentHours := time.Since(started).Hours()
		if spentHours < 0 {
			spentHours = 0
		}
		remainingHours := time.Until(plan.Session.Deadline).Hours()
		if remainingHours < 0 {
			remainingHours = 0
		}
		fmt.Fprintf(out, "  Hourly rate      $%.3f/hour\n", plan.Session.HourlyUSD)
		fmt.Fprintf(out, "  Spent estimate   $%.2f\n", spentHours*plan.Session.HourlyUSD)
		fmt.Fprintf(out, "  Remaining cost   $%.2f scheduled exposure\n", remainingHours*plan.Session.HourlyUSD)
	} else {
		fmt.Fprintf(out, "  Cost             unknown (session rate is not recorded)\n")
	}
	_, err := fmt.Fprintln(out, "  Launch           production detached supervisor; returns only after RUNNING")
	return err
}

func deepRuntimeDescription(session sessionstate.State) string {
	if session.Runtime == "" {
		return "unknown"
	}
	if session.RuntimeDeployment == "" {
		return session.Runtime
	}
	return session.Runtime + " (" + session.RuntimeDeployment + ")"
}

func saveDeepProductionBinding(paths config.Paths, binding deepProductionRunBinding) error {
	path := filepath.Join(paths.StateDir, deepProductionBinding)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(binding, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".production-binding-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func loadDeepProductionBinding(paths config.Paths) (deepProductionRunBinding, error) {
	data, err := os.ReadFile(filepath.Join(paths.StateDir, deepProductionBinding))
	if err != nil {
		return deepProductionRunBinding{}, err
	}
	var binding deepProductionRunBinding
	if err := json.Unmarshal(data, &binding); err != nil {
		return deepProductionRunBinding{}, fmt.Errorf("parse Deep Work dashboard connection: %w", err)
	}
	if binding.InstanceID <= 0 || !validSSHHost(binding.SSHHost) || binding.SSHPort < 1 || binding.SSHPort > 65535 || binding.RemoteRoot == "" || binding.DeepSessionID == "" {
		return deepProductionRunBinding{}, errors.New("Deep Work dashboard connection is incomplete")
	}
	return binding, nil
}
