package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/deep"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

// deepOnBoxFlags are deliberately independent of the operator-side compute
// session flags. The on-box supervisor already owns a live NInfer process. It
// still requires session.json so state cannot attach to a replacement instance
// that happens to have the same worktree path.
type deepOnBoxFlags struct {
	missionPath        string
	repoPath           string
	actionPlan         string
	actionPlanSeed     string
	provider           string
	providerSet        bool
	model              string
	modelSet           bool
	reasoning          string
	reasoningSet       bool
	actionPlanSet      bool
	hours              float64
	deadline           string
	taskTimeout        time.Duration
	taskTimeoutSet     bool
	maxAttempts        int
	maxAttemptsSet     bool
	allowCommands      stringSlice
	allowCommandsSet   bool
	clearAllowCommands bool
	readyFile          string
	resume             bool
	rebindCompute      bool
	rebindReason       string
}

// runDeepOnBox starts (or resumes) the coordinator in the same instance as
// Hermes and NInfer. It is intended to be launched by the detached supervisor
// after the instance runtime is ready. There is no session-state lookup, SSH
// client, or local model tunnel in this mode.
func runDeepOnBox(args []string) error {
	fs := flag.NewFlagSet("deep onbox", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	f := &deepOnBoxFlags{}
	fs.StringVar(&f.missionPath, "mission", "", "mission Markdown file (required for a new session)")
	fs.StringVar(&f.repoPath, "repo", "", "target git repository on this instance (required for a new session)")
	fs.StringVar(&f.actionPlan, "action-plan", "", "optional worktree-relative living action plan")
	fs.StringVar(&f.actionPlanSeed, "action-plan-seed", "", "optional on-box seed file copied to --action-plan before execution")
	fs.StringVar(&f.provider, "provider", "custom:qwen-stint-{reasoning}", "Hermes provider id or reasoning template")
	fs.StringVar(&f.model, "model", "", "model id (default: first model served by 127.0.0.1:8080)")
	fs.StringVar(&f.reasoning, "reasoning", "", "reasoning effort: none, low, medium, or xhigh")
	fs.Float64Var(&f.hours, "hours", 1, "on-box session duration when --deadline is omitted")
	fs.StringVar(&f.deadline, "deadline", "", "absolute RFC3339 deadline (overrides --hours)")
	fs.DurationVar(&f.taskTimeout, "task-timeout", 0, "maximum wall time per Hermes invocation")
	fs.IntVar(&f.maxAttempts, "max-attempts", 3, "executor attempts per task before parking")
	fs.Var(&f.allowCommands, "allow-command", "advisory command prefix included in the Hermes prompt (repeatable)")
	fs.BoolVar(&f.clearAllowCommands, "clear-allow-commands", false, "clear persisted advisory command guidance when resuming")
	fs.StringVar(&f.readyFile, "ready-file", "", "write RUNNING after durable state is persisted")
	fs.BoolVar(&f.resume, "resume", false, "resume the latest on-box session instead of creating one")
	fs.BoolVar(&f.rebindCompute, "rebind-compute", false, "explicitly bind the saved worktree to the current Vast instance")
	fs.StringVar(&f.rebindReason, "rebind-reason", "", "required audit reason for --rebind-compute")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fs.Visit(func(flg *flag.Flag) {
		if flg.Name == "reasoning" {
			f.reasoningSet = true
		}
		if flg.Name == "task-timeout" {
			f.taskTimeoutSet = true
		}
		if flg.Name == "provider" {
			f.providerSet = true
		}
		if flg.Name == "model" {
			f.modelSet = true
		}
		if flg.Name == "action-plan" {
			f.actionPlanSet = true
		}
		if flg.Name == "max-attempts" {
			f.maxAttemptsSet = true
		}
		if flg.Name == "allow-command" {
			f.allowCommandsSet = true
		}
	})
	if f.rebindCompute && !f.resume {
		return errors.New("--rebind-compute is only valid with --resume")
	}
	if f.clearAllowCommands && !f.resume {
		return errors.New("--clear-allow-commands is only valid with --resume")
	}
	if f.clearAllowCommands && f.allowCommandsSet {
		return errors.New("--clear-allow-commands cannot be combined with --allow-command")
	}
	if f.rebindCompute && strings.TrimSpace(f.rebindReason) == "" {
		return errors.New("--rebind-compute requires --rebind-reason")
	}
	if !f.rebindCompute && strings.TrimSpace(f.rebindReason) != "" {
		return errors.New("--rebind-reason requires --rebind-compute")
	}
	if f.resume && f.actionPlanSeed != "" {
		return errors.New("--action-plan-seed is only valid when creating a new Deep Work session")
	}
	if f.resume && f.actionPlanSet && strings.TrimSpace(f.actionPlan) == "" {
		return errors.New("--action-plan cannot be empty when overriding a resumed session")
	}
	if f.resume && f.providerSet && strings.TrimSpace(f.provider) == "" {
		return errors.New("--provider cannot be empty when overriding a resumed session")
	}
	if f.resume && f.modelSet && strings.TrimSpace(f.model) == "" {
		return errors.New("--model cannot be empty when overriding a resumed session")
	}
	if f.resume {
		if f.taskTimeoutSet && f.taskTimeout <= 0 {
			return errors.New("--task-timeout must be positive")
		}
	} else {
		if f.taskTimeout == 0 {
			f.taskTimeout = 10 * time.Minute
		}
		if f.reasoning == "" {
			f.reasoning = deep.ReasoningMedium
		}
		if f.taskTimeout <= 0 {
			return errors.New("--task-timeout must be positive")
		}
	}
	if f.maxAttempts < 1 {
		return errors.New("--max-attempts must be at least 1")
	}
	reasoning, err := deep.NormalizeReasoning(f.reasoning)
	if err != nil {
		return fmt.Errorf("--reasoning: %w", err)
	}
	if f.reasoningSet {
		if reasoning == "" {
			return errors.New("--reasoning cannot be empty when overriding a session")
		}
		f.reasoning = reasoning
	}
	if f.actionPlan != "" {
		f.actionPlan, err = actionPlanPath(f.actionPlan)
		if err != nil {
			return err
		}
	}
	if f.actionPlanSeed != "" && f.actionPlan == "" {
		return errors.New("--action-plan-seed requires --action-plan")
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}
	var resumeState deep.DeepState
	if f.resume {
		resumeState, err = loadValidatedDeepResumeState(paths.StateDir, "")
		if err != nil {
			return err
		}
	}
	compute, err := sessionstate.Load(paths)
	if err != nil {
		return fmt.Errorf("on-box compute identity is unavailable: %w", err)
	}
	if compute.Status != sessionstate.StatusReady || !compute.Deadline.After(time.Now().UTC()) {
		return errors.New("on-box compute session is not READY with a future deadline")
	}
	if _, err := lookPath("hermes"); err != nil {
		return errors.New("hermes was not found on the compute instance")
	}

	if f.resume {
		return resumeDeepOnBox(paths, f, resumeState)
	}
	if f.missionPath == "" || f.repoPath == "" {
		return errors.New("deep onbox requires --mission and --repo for a new session")
	}
	mission, err := deep.ParseMissionFile(f.missionPath)
	if err != nil {
		return err
	}
	if err := validateOnBoxGitHubPolicy(mission.GitHub, mission.GitHubConfigured); err != nil {
		return err
	}
	if err := preflightLocalVerifyTools(mission); err != nil {
		return err
	}
	if _, err := os.Stat(f.repoPath); err != nil {
		return fmt.Errorf("on-box repository: %w", err)
	}
	git := newGitRunner()
	if _, err := git.repoHead(f.repoPath); err != nil {
		return fmt.Errorf("%s is not a git repository: %v", f.repoPath, err)
	}
	if clean, detail := git.cleanTracked(f.repoPath); !clean {
		return fmt.Errorf("%s has uncommitted tracked changes; commit or stash them first:\n%s", f.repoPath, detail)
	}
	modelIDs, err := onBoxEndpointModelIDs()
	if err != nil {
		return fmt.Errorf("read on-box model endpoint: %w", err)
	}
	model := strings.TrimSpace(f.model)
	if model == "" {
		model = modelIDs[0]
	}
	if !containsString(modelIDs, model) {
		return fmt.Errorf("model %q is not served by the on-box endpoint (available: %s)", model, strings.Join(modelIDs, ", "))
	}
	now := time.Now().UTC()
	deadline, err := onBoxDeadline(f.deadline, f.hours, now)
	if err != nil {
		return err
	}
	if !deadline.After(now) {
		return errors.New("on-box deadline must be in the future")
	}
	landBefore := landingDeadline(deadline, now)
	sessionID := deep.NewSessionID(now)
	worktree := filepath.Join(f.repoPath, ".stint-deep", sessionID)
	if err := git.worktreeAdd(f.repoPath, worktree, deep.BranchName(sessionID)); err != nil {
		return fmt.Errorf("create on-box deep worktree: %w", err)
	}
	if f.actionPlanSeed != "" {
		if err := seedOnBoxActionPlan(f.actionPlanSeed, worktree, f.actionPlan); err != nil {
			return err
		}
	}
	baseCommit, _ := git.repoHead(worktree)
	state := deep.NewState(sessionID, mission, f.repoPath, worktree, deadline, landBefore, f.maxAttempts, now)
	state.BaseCommit = baseCommit
	if err := state.BindCompute("vast", compute.InstanceID, now); err != nil {
		return fmt.Errorf("bind Deep Work session to compute instance: %w", err)
	}
	if f.actionPlan != "" {
		state.Tasks = addActionPlanTask(state.Tasks, f.actionPlan)
	}
	state.Exec = &deep.ExecSettings{
		Worker:          workerHermesOnBox,
		Provider:        f.provider,
		Model:           model,
		Reasoning:       f.reasoning,
		ActionPlanPath:  f.actionPlan,
		TaskTimeoutSec:  int(f.taskTimeout.Seconds()),
		AllowedCommands: f.allowCommands,
	}
	if err := deep.SaveMissionCopy(paths.StateDir, sessionID, f.missionPath); err != nil {
		return err
	}
	if err := deep.BeginNewRun(paths.StateDir, &state, now); err != nil {
		return err
	}
	if err := writeOnBoxReady(f.readyFile, state); err != nil {
		return err
	}

	return deepRunSession(paths.StateDir, &state, &deepRunConfig{
		worker:          workerHermesOnBox,
		allowedCommands: f.allowCommands,
		provider:        f.provider,
		model:           model,
		reasoning:       f.reasoning,
		actionPlan:      f.actionPlan,
		taskTimeout:     f.taskTimeout,
		missionName:     mission.Name,
		taskCount:       len(state.Tasks),
		paths:           paths,
	}, git, false)
}

func seedOnBoxActionPlan(seedPath, worktree, relativePath string) error {
	clean, err := actionPlanPath(relativePath)
	if err != nil {
		return fmt.Errorf("on-box action-plan destination: %w", err)
	}
	data, err := os.ReadFile(seedPath)
	if err != nil {
		return fmt.Errorf("read on-box action-plan seed: %w", err)
	}
	destination := filepath.Join(worktree, filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create on-box action-plan directory: %w", err)
	}
	if err := os.WriteFile(destination, data, 0o644); err != nil {
		return fmt.Errorf("write on-box action-plan seed: %w", err)
	}
	return nil
}

func onBoxDeadline(raw string, hours float64, now time.Time) (time.Time, error) {
	if strings.TrimSpace(raw) != "" {
		deadline, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
		if err != nil {
			return time.Time{}, fmt.Errorf("parse --deadline: %w", err)
		}
		return deadline.UTC(), nil
	}
	if hours <= 0 {
		return time.Time{}, errors.New("--hours must be positive when --deadline is omitted")
	}
	return now.Add(time.Duration(hours * float64(time.Hour))), nil
}

func writeOnBoxReady(path string, state deep.DeepState) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		Status   string    `json:"status"`
		Session  string    `json:"session"`
		Deadline time.Time `json:"deadline"`
	}{Status: "RUNNING", Session: state.SessionID, Deadline: state.Deadline})
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o600)
}

func applyDeepOnBoxOverrides(state *deep.DeepState, f *deepOnBoxFlags) error {
	if state.Exec == nil {
		return errors.New("on-box resume has no persisted Hermes execution settings")
	}
	if f.providerSet {
		state.Exec.Provider = f.provider
	}
	if f.modelSet {
		state.Exec.Model = f.model
	}
	if f.reasoningSet {
		state.Exec.Reasoning = f.reasoning
	}
	if f.actionPlanSet {
		previous := state.Exec.ActionPlanPath
		if previous != f.actionPlan {
			for _, task := range state.Tasks {
				if task.Status == deep.StatusVerified {
					return errors.New("cannot change the action-plan destination after a task checkpoint has been published; start a fresh session to preserve publication identity")
				}
			}
			state.Tasks = retargetOnBoxActionPlanTask(state.Tasks, f.actionPlan)
		}
		state.Exec.ActionPlanPath = f.actionPlan
	}
	if f.taskTimeoutSet {
		state.Exec.TaskTimeoutSec = int(f.taskTimeout.Seconds())
	}
	if f.maxAttemptsSet {
		state.TaskAttemptCap = f.maxAttempts
	}
	if f.clearAllowCommands {
		state.Exec.AllowedCommands = nil
	} else if f.allowCommandsSet {
		state.Exec.AllowedCommands = append([]string(nil), f.allowCommands...)
	}
	return nil
}

func retargetOnBoxActionPlanTask(tasks []deep.Task, actionPlan string) []deep.Task {
	for i := range tasks {
		if !strings.HasPrefix(tasks[i].ID, "STINT-PLAN-") {
			continue
		}
		tasks[i].Objective = "Create or update the living action plan at " + actionPlan + " before execution begins"
		tasks[i].Acceptance = "the living action plan exists at the requested path and records decisions, risks, next steps, and evidence pointers consistent with the mission and repository state"
		tasks[i].Verify = "test -s " + shellQuote(actionPlan)
		tasks[i].Reasoning = deep.ReasoningXHigh
		// A changed destination is a new acceptance obligation. Do not retain a
		// verified marker or prior attempt result from the old file.
		tasks[i].Status = deep.StatusQueued
		tasks[i].Attempts = 0
		tasks[i].Blocker = ""
		tasks[i].LastResult = ""
		tasks[i].ExecutionError = ""
		tasks[i].VerificationCommand = ""
		tasks[i].VerificationResult = ""
		tasks[i].VerificationOutput = ""
		tasks[i].ConfiguredTimeoutSec = 0
		tasks[i].EffectiveTimeoutSec = 0
		tasks[i].TimeoutDecision = ""
		tasks[i].Findings = nil
		tasks[i].VerifiedAt = nil
		tasks[i].CheckpointCommit = ""
		return tasks
	}
	return addActionPlanTask(tasks, actionPlan)
}

func onBoxComputeRebindNeeded(state *deep.DeepState, instanceID int64) bool {
	return state.ComputeBinding == nil || state.ComputeBinding.Provider != "vast" || state.ComputeBinding.InstanceID != instanceID
}

func githubPolicyFromEnvironment() (deep.GitHubPolicy, error) {
	mode, err := deep.NormalizeGitHubMode(os.Getenv("STINT_GITHUB_MODE"))
	if err != nil {
		return deep.GitHubPolicy{}, err
	}
	approval, err := deep.NormalizeApprovalPolicy(os.Getenv("STINT_GITHUB_APPROVAL"))
	if err != nil {
		return deep.GitHubPolicy{}, err
	}
	var authors []string
	for _, author := range strings.Split(os.Getenv("STINT_GITHUB_ALLOWED_AUTHORS"), ",") {
		if author = strings.TrimSpace(author); author != "" {
			authors = append(authors, author)
		}
	}
	policy := deep.GitHubPolicy{
		Mode: mode, Repository: strings.TrimSpace(os.Getenv("STINT_GITHUB_REPOSITORY")),
		Base: strings.TrimSpace(os.Getenv("STINT_GITHUB_BASE")), AllowedAuthors: authors, Approval: approval,
	}
	if err := policy.Validate(); err != nil {
		return deep.GitHubPolicy{}, err
	}
	return policy, nil
}

func validateOnBoxGitHubPolicy(policy deep.GitHubPolicy, configured bool) error {
	if os.Getenv("STINT_ONBOX_SKIP_GITHUB") == "1" {
		mode, err := deep.NormalizeGitHubMode(string(policy.Mode))
		if err != nil || mode != deep.GitHubNone {
			return errors.New("GitHub publishing cannot be disabled for a session whose persisted policy enables it")
		}
		return nil
	}
	if !configured {
		return errors.New("production on-box missions must declare an explicit ## GitHub policy")
	}
	if err := policy.Validate(); err != nil {
		return fmt.Errorf("mission GitHub policy: %w", err)
	}
	if policy.Mode != deep.GitHubEngineering {
		return fmt.Errorf("on-box checkpoint publication currently requires GitHub mode engineering; got %q", policy.Mode)
	}
	candidate, err := githubPolicyFromEnvironment()
	if err != nil {
		return fmt.Errorf("publisher GitHub configuration: %w", err)
	}
	if !deep.SameGitHubPolicy(policy, candidate) {
		return errors.New("publisher GitHub configuration differs from the persisted mission policy (mode, repository, base, allowed authors, approval)")
	}
	return nil
}

func prepareDeepOnBoxResume(state *deep.DeepState, compute sessionstate.State, f *deepOnBoxFlags, git gitOps, now time.Time) (bool, error) {
	rebindNeeded := onBoxComputeRebindNeeded(state, compute.InstanceID)
	if rebindNeeded && !f.rebindCompute {
		bound := int64(0)
		if state.ComputeBinding != nil {
			bound = state.ComputeBinding.InstanceID
		}
		return false, fmt.Errorf("on-box session is bound to instance %d, current instance is %d; verify the saved repository and worktree are present, then resume with --rebind-compute --rebind-reason <reason>", bound, compute.InstanceID)
	}
	if !rebindNeeded && f.rebindCompute {
		return false, errors.New("--rebind-compute was supplied, but this Deep Work session is already bound to the current instance")
	}
	if _, err := git.repoHead(state.RepoPath); err != nil {
		return false, fmt.Errorf("on-box repository is unavailable: %w", err)
	}
	if err := ensureDeepWorktree(git, false, state); err != nil {
		return false, err
	}
	if rebindNeeded {
		if err := state.RebindCompute("vast", compute.InstanceID, f.rebindReason, now); err != nil {
			return false, fmt.Errorf("record explicit compute rebind: %w", err)
		}
	}
	deadlineReset, err := reanchorDeadline(state, compute.Deadline, now)
	if err != nil {
		return false, err
	}
	if err := applyDeepOnBoxOverrides(state, f); err != nil {
		return false, err
	}
	return deadlineReset, nil
}

func firstOnBoxEndpointModel() (string, error) {
	ids, err := onBoxEndpointModelIDs()
	if err != nil {
		return "", err
	}
	return ids[0], nil
}

func onBoxEndpointModelIDs() ([]string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://127.0.0.1:8080/v1/models")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("on-box model endpoint returned %s", resp.Status)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(body.Data))
	for _, model := range body.Data {
		if id := strings.TrimSpace(model.ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("the on-box model endpoint reported no models")
	}
	return ids, nil
}

func resumeDeepOnBox(paths config.Paths, f *deepOnBoxFlags, state deep.DeepState) error {
	var err error
	resumeFromPhase := state.Phase
	if state.Exec == nil || state.Exec.Worker != workerHermesOnBox {
		return errors.New("latest Deep Work session is not an on-box Hermes session")
	}
	if err := validateOnBoxGitHubPolicy(state.GitHub, true); err != nil {
		return fmt.Errorf("persisted GitHub policy: %w", err)
	}
	compute, err := sessionstate.Load(paths)
	if err != nil {
		return fmt.Errorf("on-box compute identity is unavailable: %w", err)
	}
	if err := preflightLocalVerifyTools(deep.Mission{Verify: state.Verify, Tasks: state.Tasks}); err != nil {
		return err
	}
	if _, err := assertNoLiveCoordinator(paths.StateDir, state.SessionID); err != nil {
		return err
	}
	git := newGitRunner()
	rebindNeeded := onBoxComputeRebindNeeded(&state, compute.InstanceID)
	now := time.Now().UTC()
	deadlineReset, err := prepareDeepOnBoxResume(&state, compute, f, git, now)
	if err != nil {
		return err
	}
	if resumeFromPhase == deep.PhaseLanding {
		deep.AppendLog(paths.StateDir, state, "resuming interrupted landing")
	}
	modelIDs, err := onBoxEndpointModelIDs()
	if err != nil {
		return fmt.Errorf("read on-box model endpoint: %w", err)
	}
	if err := applyDeepOnBoxOverrides(&state, f); err != nil {
		return err
	}
	model := state.Exec.Model
	if strings.TrimSpace(model) == "" {
		model = modelIDs[0]
	}
	if !containsString(modelIDs, model) {
		return fmt.Errorf("model %q is not served by the on-box endpoint (available: %s)", model, strings.Join(modelIDs, ", "))
	}
	state.Exec.Model = model
	if err := deep.BeginResumeEpoch(paths.StateDir, &state, resumeFromPhase, now); err != nil {
		return err
	}
	resumeNote := "deadline re-anchored to min(saved Deep Work, compute)"
	if deadlineReset {
		resumeNote = "expired Deep Work deadline reset to current compute deadline"
	}
	deep.AppendLog(paths.StateDir, state, "resumed on box: %s (deadline %s)", resumeNote, state.Deadline.Format(time.RFC3339))
	deep.AppendIncident(paths.StateDir, state, deep.IncidentResumed, "", resumeNote)
	if rebindNeeded {
		deep.AppendIncident(paths.StateDir, state, deep.IncidentComputeRebind, "", fmt.Sprintf("Vast instance %d: %s", compute.InstanceID, f.rebindReason))
	}
	if err := writeOnBoxReady(f.readyFile, state); err != nil {
		return err
	}
	return deepRunSession(paths.StateDir, &state, &deepRunConfig{
		worker:          workerHermesOnBox,
		allowedCommands: state.Exec.AllowedCommands,
		provider:        state.Exec.Provider,
		model:           model,
		reasoning:       state.Exec.Reasoning,
		actionPlan:      state.Exec.ActionPlanPath,
		taskTimeout:     time.Duration(state.Exec.TaskTimeoutSec) * time.Second,
		missionName:     state.MissionName,
		taskCount:       len(state.Tasks),
		paths:           paths,
	}, git, true)
}
