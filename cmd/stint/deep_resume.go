package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/deep"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

type deepResumeFlags struct {
	sessionID     string
	taskTimeout   time.Duration
	provider      string
	model         string
	reasoning     string
	reasoningSet  bool
	rebindCompute bool
	rebindReason  string
}

// runDeepResume continues a Deep Work session from durable state (DWX-008).
// It is the recovery path for a crashed or stopped coordinator, a lapsed
// machine, and a deadline landing: compute is re-established with the usual
// `stint resume` / `stint start interactive` first — Deep Work never rents,
// extends, or destroys compute. Everything else is reconstructed: the
// deadline is re-anchored to the current compute deadline, the worktree is
// re-attached if a crash lost its directory, executor settings come from the
// persisted state (flag-overridable), and the same coordinator loop runs on
// the same branch, so verified work is never redone.
func runDeepResume(args []string) error {
	fs := flag.NewFlagSet("deep resume", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	f := &deepResumeFlags{}
	fs.StringVar(&f.sessionID, "session", "", "session id to resume (default: latest)")
	fs.DurationVar(&f.taskTimeout, "task-timeout", 0, "override the session's per-invocation timeout")
	fs.StringVar(&f.provider, "provider", "", "override the session's Hermes provider id")
	fs.StringVar(&f.model, "model", "", "override the session's model id")
	fs.StringVar(&f.reasoning, "reasoning", "", "override the session's reasoning effort: none, low, medium, or xhigh")
	fs.BoolVar(&f.rebindCompute, "rebind-compute", false, "explicitly bind this worktree to the current compute instance")
	fs.StringVar(&f.rebindReason, "rebind-reason", "", "required audit reason for --rebind-compute")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Visit reports whether --reasoning was explicitly supplied. An unset
	// value keeps the session's persisted effort.
	fs.Visit(func(flg *flag.Flag) {
		if flg.Name == "reasoning" {
			f.reasoningSet = true
		}
	})
	if f.reasoningSet {
		var err error
		if f.reasoning, err = deep.NormalizeReasoning(f.reasoning); err != nil {
			return fmt.Errorf("--reasoning: %w", err)
		}
	}
	if f.rebindCompute && strings.TrimSpace(f.rebindReason) == "" {
		return errors.New("--rebind-compute requires --rebind-reason")
	}
	if !f.rebindCompute && strings.TrimSpace(f.rebindReason) != "" {
		return errors.New("--rebind-reason requires --rebind-compute")
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}

	// 1. Load durable state and validate its executable command fields before
	//    checking or contacting compute/runtime services.
	state, err := loadValidatedDeepResumeState(paths.StateDir, f.sessionID)
	if err != nil {
		return err
	}
	if state.Exec == nil || state.Exec.Worker == "" {
		return errors.New("this Deep Work session has no persisted Hermes worker identity; it cannot be resumed safely")
	}

	// 2. Never run two coordinators for one session.
	if _, err := assertNoLiveCoordinator(paths.StateDir, state.SessionID); err != nil {
		return err
	}

	// 3. Compute: a READY session is a precondition (Deep Work rides it).
	session, err := sessionstate.Load(paths)
	if err != nil {
		return fmt.Errorf("no active compute session (%v); run `stint resume` or `stint start interactive` first", err)
	}
	now := time.Now().UTC()
	if session.Status != sessionstate.StatusReady {
		return fmt.Errorf("compute session is %s, not READY; run `stint resume` or `stint start interactive` first", session.Status)
	}
	if !session.Deadline.After(now) {
		return fmt.Errorf("compute session deadline has passed; run `stint resume` or `stint start interactive` first")
	}
	rebindNeeded := state.ComputeBinding == nil || state.ComputeBinding.Provider != "vast" || state.ComputeBinding.InstanceID != session.InstanceID
	if rebindNeeded && !f.rebindCompute {
		boundID := int64(0)
		if state.ComputeBinding != nil {
			boundID = state.ComputeBinding.InstanceID
		}
		return fmt.Errorf("Deep Work session is bound to Vast instance %d, current compute is %d; verify the worktree is present, then resume with --rebind-compute --rebind-reason <reason>", boundID, session.InstanceID)
	}
	if !rebindNeeded && f.rebindCompute {
		return errors.New("--rebind-compute was supplied, but this Deep Work session is already bound to the current instance")
	}

	// 4. Hermes runs on the compute box. Reconstruct the persisted worker
	//    policy and verify the box runtime before resuming.
	exec := resolveExecSettings(&state, execOverrides{
		provider:     f.provider,
		taskTimeout:  f.taskTimeout,
		reasoning:    f.reasoning,
		reasoningSet: f.reasoningSet,
	})
	workerID := exec.Worker
	if workerID == "cline" {
		return errors.New("this session was created for the retired Cline worker; create a Hermes Deep Work session to continue")
	}
	if workerID == workerHermesOnBox {
		return errors.New("this session runs on the compute box; resume it there with `stint deep onbox --resume`")
	}
	if workerID != workerHermes {
		return fmt.Errorf("unsupported Deep Work worker %q; expected Hermes", workerID)
	}
	remoteFn := newRemoteCmd(paths, session)
	if _, err := remoteFn(context.Background(), "true"); err != nil {
		return fmt.Errorf("cannot reach the compute box over SSH (%v); check the session (`stint status`)", err)
	}
	if out, err := remoteFn(context.Background(), "command -v hermes"); err != nil || strings.TrimSpace(out) == "" {
		return errors.New("hermes was not found on the compute box")
	}
	modelsJSON, err := remoteFn(context.Background(), "curl -fsS -m 5 http://127.0.0.1:8080/v1/models")
	if err != nil {
		return fmt.Errorf("the compute-box model endpoint is not answering: %w", err)
	}
	if err := preflightRemoteVerifyTools(deep.Mission{Verify: state.Verify, Tasks: state.Tasks}, remoteFn); err != nil {
		return err
	}

	// 5. Repository: it must still be a git repository. The clean-tree check
	//    is skipped on purpose: the session's worktree already holds the
	//    session's state; the developer's checkout is untouched either way.
	//    For the remote worker the repo lives on the box.
	var git gitOps = &remoteGit{remote: remoteFn}
	if _, err := git.repoHead(state.RepoPath); err != nil {
		return fmt.Errorf("%s is no longer a git repository: %v", state.RepoPath, err)
	}

	// 6. Deadline: re-anchor against the current compute deadline.
	reset, err := reanchorDeadline(&state, session.Deadline, now)
	if err != nil {
		return err
	}

	// 7. Workspace: restore the worktree if a crash or cleanup lost it.
	if err := ensureDeepWorktree(git, true, &state); err != nil {
		return err
	}
	if rebindNeeded {
		if err := state.RebindCompute("vast", session.InstanceID, f.rebindReason, now); err != nil {
			return fmt.Errorf("record explicit compute rebind: %w", err)
		}
	}

	// 8. Executor settings: `exec` was resolved in step 4; apply the model
	//    override (persisted value, else the first model the endpoint serves).
	modelID := f.model
	if modelID == "" {
		modelID = exec.Model
	}
	if modelID == "" {
		modelID, err = firstEndpointModelFromJSON(modelsJSON)
		if err != nil {
			return fmt.Errorf("resolve model from the compute-box endpoint: %w (or pass --model)", err)
		}
	}
	modelIDs, err := endpointModelIDsFromJSON(modelsJSON)
	if err != nil {
		return fmt.Errorf("parse compute-box model list: %w", err)
	}
	if !containsString(modelIDs, modelID) {
		return fmt.Errorf("model %q is not served by the compute-box endpoint (available: %s)", modelID, strings.Join(modelIDs, ", "))
	}
	exec.Model = modelID

	// 9. Revive: the coordinator loop only runs executing sessions. A
	//    landed or stopped session is continued in the same worktree and
	//    branch — a deadline landing is a pause, not a verdict.
	if state.Phase == deep.PhaseLanding {
		deep.AppendLog(paths.StateDir, state, "resuming interrupted landing")
	} else if state.Phase == deep.PhaseLanded {
		state.ReopenAfterLanding(now)
		deep.AppendLog(paths.StateDir, state, "resuming after a recorded landing outcome")
	} else if state.Phase != deep.PhaseExecuting {
		deep.AppendLog(paths.StateDir, state, "resuming from %s", state.Phase)
		state.Phase = deep.PhaseExecuting
		state.LandedAt = nil
	}
	state.Exec = exec
	stateNote := "deadline re-anchored to the compute session"
	if !reset {
		stateNote = "deadline re-anchored to min(session, compute)"
	}
	if err := state.SaveDir(paths.StateDir); err != nil {
		return err
	}
	deep.AppendLog(paths.StateDir, state, "resumed: %s (deadline %s, lands from %s)",
		stateNote, state.Deadline.Format(time.RFC3339), state.LandBefore.Format(time.RFC3339))
	deep.AppendIncident(paths.StateDir, state, deep.IncidentResumed, "", stateNote)
	if rebindNeeded {
		deep.AppendIncident(paths.StateDir, state, deep.IncidentComputeRebind, "", fmt.Sprintf("Vast instance %d: %s", session.InstanceID, f.rebindReason))
	}

	return deepRunSession(paths.StateDir, &state, &deepRunConfig{
		worker:          workerID,
		allowedCommands: exec.AllowedCommands,
		provider:        exec.Provider,
		model:           modelID,
		reasoning:       exec.Reasoning,
		actionPlan:      exec.ActionPlanPath,
		taskTimeout:     time.Duration(exec.TaskTimeoutSec) * time.Second,
		missionName:     state.MissionName,
		taskCount:       len(state.Tasks),
		remote:          remoteFn,
		paths:           paths,
	}, git, true)
}

func loadValidatedDeepResumeState(stateDir, sessionID string) (deep.DeepState, error) {
	var state deep.DeepState
	var err error
	if sessionID != "" {
		state, err = deep.LoadState(stateDir, sessionID)
	} else {
		state, err = deep.LoadLatestState(stateDir)
	}
	if err != nil {
		return deep.DeepState{}, err
	}
	if err := validateMissionVerifyCommands(deep.Mission{Verify: state.Verify, Tasks: state.Tasks}); err != nil {
		return deep.DeepState{}, fmt.Errorf("persisted verification command data is invalid: %w", err)
	}
	return state, nil
}

// assertNoLiveCoordinator refuses to start a second coordinator for a
// session whose coordinator process is still alive. A stale pid file (crash,
// power loss) is cleared and tolerated: liveness is a signal-0 probe.
func assertNoLiveCoordinator(stateDir, sessionID string) (int, error) {
	alive, pid := deep.CoordinatorAlive(stateDir, sessionID)
	if !alive {
		_ = deep.ClearCoordinatorPid(stateDir, sessionID) // clear stale/absent file
		return 0, nil
	}
	return pid, fmt.Errorf("a coordinator for %s is already running (pid %d); wait for it to land, or run `stint deep stop` first", sessionID, pid)
}

// reanchorDeadline restates the session deadline against the current compute
// deadline. While the original Deep Work deadline is still in the future it
// can only be tightened (a crash or a lapsed machine never extends the
// budget); once it has lapsed, the fresh compute deadline becomes the budget
// — the operator deliberately re-provisioned compute to continue. Returns
// whether the deadline was reset.
func reanchorDeadline(state *deep.DeepState, sessionDeadline, now time.Time) (bool, error) {
	deadline := state.Deadline
	if !deadline.After(now) {
		if !sessionDeadline.After(now) {
			return false, errors.New("both the Deep Work deadline and the compute session deadline have passed; start a fresh session with `stint deep start`")
		}
		deadline = sessionDeadline
		state.Deadline = deadline
		state.LandBefore = landingDeadline(deadline, now)
		return true, nil
	}
	if sessionDeadline.Before(deadline) {
		deadline = sessionDeadline
	}
	state.Deadline = deadline
	state.LandBefore = landingDeadline(deadline, now)
	return false, nil
}

// execOverrides are the resume-time overrides on top of the session's
// persisted executor settings.
type execOverrides struct {
	provider     string
	taskTimeout  time.Duration // zero = keep the session's value
	reasoning    string
	reasoningSet bool
}

// resolveExecSettings merges the session's persisted Hermes settings with
// resume-time overrides. Sessions started before settings were persisted
// (Exec == nil) fall back to current Hermes defaults. Advisory command
// guidance always comes from persisted session policy.
func resolveExecSettings(st *deep.DeepState, o execOverrides) *deep.ExecSettings {
	const defaultProvider = "custom:qwen-stint-{reasoning}"
	const defaultTaskTimeout = 10 * time.Minute
	es := &deep.ExecSettings{Provider: defaultProvider, Reasoning: deep.ReasoningMedium, TaskTimeoutSec: int(defaultTaskTimeout.Seconds())}
	if st.Exec != nil {
		*es = *st.Exec
	}
	if o.provider != "" {
		es.Provider = o.provider
	}
	if o.taskTimeout > 0 {
		es.TaskTimeoutSec = int(o.taskTimeout.Seconds())
	}
	if o.reasoningSet {
		es.Reasoning = o.reasoning
	}
	if es.Provider == "" {
		es.Provider = defaultProvider
	}
	if es.TaskTimeoutSec <= 0 {
		es.TaskTimeoutSec = int(defaultTaskTimeout.Seconds())
	}
	if es.Reasoning == "" {
		es.Reasoning = deep.ReasoningMedium
	}
	return es
}

// ensureDeepWorktree restores the session workspace. A crash or a
// `git worktree remove` may have lost the directory; the branch still holds
// every checkpoint commit, so re-attaching recovers the work. A missing
// branch is unrecoverable: that is a fresh `stint deep start`, not a resume.
// When remote is true the worktree lives on the compute box, so its
// presence is probed through the (remote) git seam rather than the local
// filesystem.
func ensureDeepWorktree(g gitOps, remote bool, state *deep.DeepState) error {
	if remote {
		if g.worktreeUsable(state.WorktreePath) {
			return nil
		}
	} else {
		if info, err := os.Stat(state.WorktreePath); err == nil && info.IsDir() {
			if g.worktreeUsable(state.WorktreePath) {
				return nil
			}
			return fmt.Errorf("worktree %s is not a usable git worktree (the repository may have moved); run `git -C %s worktree repair` and retry",
				state.WorktreePath, state.RepoPath)
		}
	}
	if !g.branchExists(state.RepoPath, state.Branch) {
		return fmt.Errorf("the worktree (%s) and its branch (%s) are gone; start a fresh session with `stint deep start`",
			state.WorktreePath, state.Branch)
	}
	return g.worktreeReattach(state.RepoPath, state.WorktreePath, state.Branch)
}
