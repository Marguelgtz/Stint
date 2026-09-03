package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/deep"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

type deepResumeFlags struct {
	sessionID      string
	taskTimeout    time.Duration
	autoApprove    bool
	autoApproveSet bool
	provider       string
	model          string
	apiKey         string
	clineConfig    string
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
	fs.BoolVar(&f.autoApprove, "auto-approve", true, "override the session's auto-approve setting")
	fs.StringVar(&f.provider, "provider", "", "override the session's Cline provider id")
	fs.StringVar(&f.model, "model", "", "override the session's model id")
	fs.StringVar(&f.apiKey, "api-key", "", "Cline API key override (never persisted)")
	fs.StringVar(&f.clineConfig, "cline-config", "", "override the session's Cline config directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Visit reports the flags the operator actually set, so an unset
	// --auto-approve keeps the session's persisted value instead of
	// clobbering it with the flag's default.
	fs.Visit(func(flg *flag.Flag) {
		if flg.Name == "auto-approve" {
			f.autoApproveSet = true
		}
	})

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}

	// 1. Load the session's durable state.
	var state deep.DeepState
	if f.sessionID != "" {
		state, err = deep.LoadState(paths.StateDir, f.sessionID)
	} else {
		state, err = deep.LoadLatestState(paths.StateDir)
	}
	if err != nil {
		return err
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

	// 4. Executor: Cline on PATH (local worker) or Hermes on the box
	//    (remote worker) must be reachable. The worker is session-level
	//    policy, reconstructed from the persisted settings.
	exec := resolveExecSettings(&state, execOverrides{
		autoApprove: f.autoApproveValue(),
		provider:    f.provider,
		clineConfig: f.clineConfig,
		taskTimeout: f.taskTimeout,
	})
	workerID := exec.Worker
	if workerID == "" {
		workerID = workerCline
	}
	remote := workerID == workerHermes
	var remoteFn remoteCmd
	if remote {
		remoteFn = newRemoteCmd(paths, session)
	}
	if remote {
		if _, err := remoteFn(context.Background(), "true"); err != nil {
			return fmt.Errorf("cannot reach the compute box over SSH (%v); check the session (`stint status`)", err)
		}
	} else {
		if _, err := lookPath("cline"); err != nil {
			return errors.New("the cline CLI was not found on PATH (install with: npm i -g cline)")
		}
	}

	// 5. Repository: it must still be a git repository. The clean-tree check
	//    is skipped on purpose: the session's worktree already holds the
	//    session's state; the developer's checkout is untouched either way.
	//    For the remote worker the repo lives on the box.
	var git gitOps
	if remote {
		git = &remoteGit{remote: remoteFn}
	} else {
		git = newGitRunner()
	}
	if _, err := git.repoHead(state.RepoPath); err != nil {
		return fmt.Errorf("%s is no longer a git repository: %v", state.RepoPath, err)
	}

	// 6. Deadline: re-anchor against the current compute deadline.
	reset, err := reanchorDeadline(&state, session.Deadline, now)
	if err != nil {
		return err
	}

	// 7. Workspace: restore the worktree if a crash or cleanup lost it.
	if err := ensureDeepWorktree(git, remote, &state); err != nil {
		return err
	}

	// 8. Executor settings: `exec` was resolved in step 4; apply the model
	//    override (persisted value, else the first model the endpoint serves).
	modelID := f.model
	if modelID == "" {
		modelID = exec.Model
	}
	if modelID == "" {
		modelID, err = firstEndpointModel()
		if err != nil {
			return fmt.Errorf("resolve model from the Stint endpoint: %w (or pass --model)", err)
		}
	}
	exec.Model = modelID

	// 9. Revive: the coordinator loop only runs executing sessions. A
	//    landed or stopped session is continued in the same worktree and
	//    branch — a deadline landing is a pause, not a verdict.
	if state.Phase != deep.PhaseExecuting {
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

	return deepRunSession(paths.StateDir, &state, &deepRunConfig{
		worker:          workerID,
		autoApprove:     exec.AutoApprove,
		allowedCommands: exec.AllowedCommands,
		provider:        exec.Provider,
		model:           modelID,
		apiKey:          f.apiKey,
		clineConfig:     exec.ClineConfig,
		taskTimeout:     time.Duration(exec.TaskTimeoutSec) * time.Second,
		missionName:     state.MissionName,
		taskCount:       len(state.Tasks),
		remote:          remoteFn,
		paths:           paths,
	}, git, true)
}

func (f *deepResumeFlags) autoApproveValue() *bool {
	if !f.autoApproveSet {
		return nil
	}
	return &f.autoApprove
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
	autoApprove *bool // nil = keep the session's value
	provider    string
	clineConfig string
	taskTimeout time.Duration // zero = keep the session's value
}

// resolveExecSettings merges the session's persisted executor settings with
// resume-time overrides. Sessions started before settings were persisted
// (Exec == nil) fall back to the start-time defaults, which are
// deny-by-default (auto-approval off). The command allow-list always comes
// from the persisted policy: it is session-level state, not an override.
func resolveExecSettings(st *deep.DeepState, o execOverrides) *deep.ExecSettings {
	const defaultProvider = "openai-compatible"
	const defaultTaskTimeout = 10 * time.Minute
	es := &deep.ExecSettings{AutoApprove: false, Provider: defaultProvider, TaskTimeoutSec: int(defaultTaskTimeout.Seconds())}
	if st.Exec != nil {
		*es = *st.Exec
	}
	if o.autoApprove != nil {
		es.AutoApprove = *o.autoApprove
	}
	if o.provider != "" {
		es.Provider = o.provider
	}
	if o.clineConfig != "" {
		es.ClineConfig = o.clineConfig
	}
	if o.taskTimeout > 0 {
		es.TaskTimeoutSec = int(o.taskTimeout.Seconds())
	}
	if es.Provider == "" {
		es.Provider = defaultProvider
	}
	if es.TaskTimeoutSec <= 0 {
		es.TaskTimeoutSec = int(defaultTaskTimeout.Seconds())
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
