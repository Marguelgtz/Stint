package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/deep"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

// runDeep dispatches the Deep Work command group: start | status | stop.
func runDeep(args []string) error {
	if len(args) == 0 {
		return errors.New("deep requires a subcommand: start, status, or stop")
	}
	switch args[0] {
	case "start":
		return runDeepStart(args[1:])
	case "status":
		return runDeepStatus(args[1:])
	case "stop":
		return runDeepStop(args[1:])
	default:
		return fmt.Errorf("unknown deep subcommand %q (stint deep <start|status|stop>)", args[0])
	}
}

type deepStartFlags struct {
	missionPath string
	repoPath    string
	hours       float64
	taskTimeout time.Duration
	maxAttempts int
	autoApprove bool
	provider    string
	model       string
	apiKey      string
	clineConfig string
}

// runDeepStart launches a Slice-1 Deep Work session: it rides an existing
// READY compute session (1:1 mapping) and runs the coordinator in the
// foreground until the session lands. It never rents, destroys, or extends
// compute: the existing start/resume/watchdog machinery owns that.
func runDeepStart(args []string) error {
	fs := flag.NewFlagSet("deep start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	f := &deepStartFlags{}
	fs.StringVar(&f.missionPath, "mission", "", "mission Markdown file (required)")
	fs.StringVar(&f.repoPath, "repo", "", "target git repository path (required)")
	fs.Float64Var(&f.hours, "hours", 0, "optional Deep Work duration cap in hours (default: the compute session deadline)")
	fs.DurationVar(&f.taskTimeout, "task-timeout", 10*time.Minute, "maximum wall time per coding-agent invocation")
	fs.IntVar(&f.maxAttempts, "max-attempts", 3, "maximum executor attempts per task before parking")
	fs.BoolVar(&f.autoApprove, "auto-approve", true, "auto-approve Cline tool calls inside the isolated worktree")
	fs.StringVar(&f.provider, "provider", "openai-compatible", "Cline provider id")
	fs.StringVar(&f.model, "model", "", "model id (default: first model served by the Stint endpoint)")
	fs.StringVar(&f.apiKey, "api-key", "", "Cline API key override")
	fs.StringVar(&f.clineConfig, "cline-config", "", "Cline config directory (default: ~/.cline)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if f.missionPath == "" || f.repoPath == "" {
		return errors.New("deep start requires --mission <file> and --repo <path>")
	}
	if f.taskTimeout <= 0 {
		return errors.New("--task-timeout must be positive")
	}
	if f.maxAttempts < 1 {
		return errors.New("--max-attempts must be at least 1")
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}

	// 1. Mission: parse and validate before any side effects.
	mission, err := deep.ParseMissionFile(f.missionPath)
	if err != nil {
		return err
	}

	// 2. Compute: a READY session is a precondition (Slice-1 rides it).
	session, err := sessionstate.Load(paths)
	if err != nil {
		return fmt.Errorf("no active compute session (%v); run `stint start interactive` first — "+
			"Slice-1 Deep Work rides an existing session", err)
	}
	now := time.Now().UTC()
	if session.Status != sessionstate.StatusReady {
		return fmt.Errorf("compute session is %s, not READY; run `stint resume` or `stint start interactive` first", session.Status)
	}
	if !session.Deadline.After(now) {
		return fmt.Errorf("compute session deadline has passed; run `stint resume` or `stint start interactive` first")
	}

	// 3. Executor: the Cline CLI must be reachable.
	if _, err := lookPath("cline"); err != nil {
		return errors.New("the cline CLI was not found on PATH (install with: npm i -g cline)")
	}

	// 4. Model: from the flags, or the first model the endpoint serves.
	modelID := f.model
	if modelID == "" {
		modelID, err = firstEndpointModel()
		if err != nil {
			return fmt.Errorf("resolve model from the Stint endpoint: %w (or pass --model)", err)
		}
	}

	// 5. Repository: must be a git repo with no tracked modifications.
	git := newGitRunner()
	if _, err := git.repoHead(f.repoPath); err != nil {
		return fmt.Errorf("%s is not a git repository: %v", f.repoPath, err)
	}
	if clean, detail := git.cleanTracked(f.repoPath); !clean {
		return fmt.Errorf("%s has uncommitted tracked changes; commit or stash them first:\n%s", f.repoPath, detail)
	}

	// 6. Deep deadline: the compute deadline is the hard bound; --hours
	//    may only tighten it. The coordinator lands before either.
	deadline := session.Deadline
	if f.hours > 0 {
		if cap := now.Add(time.Duration(f.hours * float64(time.Hour))); cap.Before(deadline) {
			deadline = cap
		}
	}
	landBefore := landingDeadline(deadline, now)

	// 7. Workspace: Stint-owned worktree cut from the repo's current HEAD.
	sessionID := deep.NewSessionID(now)
	worktree := filepath.Join(f.repoPath, ".stint-deep", sessionID)
	if err := git.worktreeAdd(f.repoPath, worktree, deep.BranchName(sessionID)); err != nil {
		return fmt.Errorf("create deep worktree: %w", err)
	}
	baseCommit, _ := git.repoHead(worktree)

	state := deep.NewState(sessionID, mission, f.repoPath, worktree, deadline, landBefore, f.maxAttempts, now)
	state.BaseCommit = baseCommit
	if err := state.SaveDir(paths.StateDir); err != nil {
		return err
	}
	if err := deep.SaveMissionCopy(paths.StateDir, sessionID, f.missionPath); err != nil {
		return err
	}

	return deepRunSession(paths.StateDir, &state, &deepRunConfig{
		autoApprove: f.autoApprove,
		provider:    f.provider,
		model:       modelID,
		apiKey:      f.apiKey,
		clineConfig: f.clineConfig,
		taskTimeout: f.taskTimeout,
		missionName: mission.Name,
		taskCount:   len(mission.Tasks),
	}, git)
}
