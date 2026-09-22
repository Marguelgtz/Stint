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
	missionPath    string
	repoPath       string
	actionPlan     string
	actionPlanSeed string
	provider       string
	model          string
	reasoning      string
	reasoningSet   bool
	hours          float64
	deadline       string
	taskTimeout    time.Duration
	taskTimeoutSet bool
	maxAttempts    int
	allowCommands  stringSlice
	readyFile      string
	resume         bool
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
	fs.StringVar(&f.readyFile, "ready-file", "", "write RUNNING after durable state is persisted")
	fs.BoolVar(&f.resume, "resume", false, "resume the latest on-box session instead of creating one")
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
	})
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
		return resumeDeepOnBox(paths, f)
	}
	if f.missionPath == "" || f.repoPath == "" {
		return errors.New("deep onbox requires --mission and --repo for a new session")
	}
	mission, err := deep.ParseMissionFile(f.missionPath)
	if err != nil {
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
	if err := state.SaveDir(paths.StateDir); err != nil {
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

func resumeDeepOnBox(paths config.Paths, f *deepOnBoxFlags) error {
	state, err := deep.LoadLatestState(paths.StateDir)
	if err != nil {
		return err
	}
	if state.Exec == nil || state.Exec.Worker != workerHermesOnBox {
		return errors.New("latest Deep Work session is not an on-box Hermes session")
	}
	compute, err := sessionstate.Load(paths)
	if err != nil {
		return fmt.Errorf("on-box compute identity is unavailable: %w", err)
	}
	if state.ComputeBinding == nil || state.ComputeBinding.InstanceID != compute.InstanceID {
		bound := int64(0)
		if state.ComputeBinding != nil {
			bound = state.ComputeBinding.InstanceID
		}
		return fmt.Errorf("on-box session is bound to instance %d, current instance is %d", bound, compute.InstanceID)
	}
	if err := preflightLocalVerifyTools(deep.Mission{Verify: state.Verify, Tasks: state.Tasks}); err != nil {
		return err
	}
	if _, err := assertNoLiveCoordinator(paths.StateDir, state.SessionID); err != nil {
		return err
	}
	git := newGitRunner()
	if _, err := git.repoHead(state.RepoPath); err != nil {
		return fmt.Errorf("on-box repository is unavailable: %w", err)
	}
	if !state.Deadline.After(time.Now().UTC()) {
		return errors.New("on-box Deep Work deadline has passed")
	}
	if state.Phase == deep.PhaseLanding {
		deep.AppendLog(paths.StateDir, state, "resuming interrupted landing")
	} else if state.Phase != deep.PhaseExecuting {
		state.Phase = deep.PhaseExecuting
		state.LandedAt = nil
	}
	modelIDs, err := onBoxEndpointModelIDs()
	if err != nil {
		return fmt.Errorf("read on-box model endpoint: %w", err)
	}
	model := state.Exec.Model
	if strings.TrimSpace(f.model) != "" {
		model = f.model
	}
	if strings.TrimSpace(model) == "" {
		model = modelIDs[0]
	}
	if !containsString(modelIDs, model) {
		return fmt.Errorf("model %q is not served by the on-box endpoint (available: %s)", model, strings.Join(modelIDs, ", "))
	}
	state.Exec.Model = model
	if f.reasoningSet {
		state.Exec.Reasoning = f.reasoning
	}
	if f.taskTimeoutSet {
		state.Exec.TaskTimeoutSec = int(f.taskTimeout.Seconds())
	}
	if err := state.SaveDir(paths.StateDir); err != nil {
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
