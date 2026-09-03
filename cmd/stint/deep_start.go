package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/deep"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

// runDeep dispatches the Deep Work command group: start | status | stop | resume.
func runDeep(args []string) error {
	if len(args) == 0 {
		return errors.New("deep requires a subcommand: start, status, stop, or resume")
	}
	switch args[0] {
	case "start":
		return runDeepStart(args[1:])
	case "status":
		return runDeepStatus(args[1:])
	case "stop":
		return runDeepStop(args[1:])
	case "resume":
		return runDeepResume(args[1:])
	default:
		return fmt.Errorf("unknown deep subcommand %q (stint deep <start|status|stop|resume>)", args[0])
	}
}

type deepStartFlags struct {
	worker        string
	missionPath   string
	repoPath      string
	hours         float64
	taskTimeout   time.Duration
	maxAttempts   int
	autoApprove   bool
	allowCommands stringSlice
	provider      string
	model         string
	apiKey        string
	clineConfig   string
}

// stringSlice collects a repeatable --flag value into a slice.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }

func (s *stringSlice) Set(v string) error {
	if v == "" {
		return errors.New("empty command")
	}
	*s = append(*s, v)
	return nil
}

// runDeepStart launches a Slice-1 Deep Work session: it rides an existing
// READY compute session (1:1 mapping) and runs the coordinator in the
// foreground until the session lands. It never rents, destroys, or extends
// compute: the existing start/resume/watchdog machinery owns that.
func runDeepStart(args []string) error {
	fs := flag.NewFlagSet("deep start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	f := &deepStartFlags{}
	fs.StringVar(&f.worker, "worker", workerCline, "worker: cline (runs on this machine, the default) or "+
		"hermes (Hermes agent plus all file/shell work on the compute box, model via the box's local endpoint; "+
		"--repo is then the repo path ON THE BOX)")
	fs.StringVar(&f.missionPath, "mission", "", "mission Markdown file (required)")
	fs.StringVar(&f.repoPath, "repo", "", "target git repository path (required)")
	fs.Float64Var(&f.hours, "hours", 0, "optional Deep Work duration cap in hours (default: the compute session deadline)")
	fs.DurationVar(&f.taskTimeout, "task-timeout", 10*time.Minute, "maximum wall time per coding-agent invocation")
	fs.IntVar(&f.maxAttempts, "max-attempts", 3, "maximum executor attempts per task before parking")
	fs.BoolVar(&f.autoApprove, "auto-approve", false, "auto-approve ALL Cline tool calls (default: off — deny-by-default; "+
		"with it off, commands outside --allow-command are denied by the CLI)")
	fs.Var(&f.allowCommands, "allow-command", "command prefix the worker may run (repeatable; named in the prompt and denied otherwise while auto-approve is off)")
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
	if f.worker != workerCline && f.worker != workerHermes {
		return fmt.Errorf("--worker must be %s or %s", workerCline, workerHermes)
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

	remote := f.worker == workerHermes
	var remoteFn remoteCmd
	if remote {
		remoteFn = newRemoteCmd(paths, session)
	}

	// 3. Executor preflight: Cline on PATH (local worker), or SSH + Hermes
	//    + the model endpoint reachable on the box (remote worker).
	if remote {
		if _, err := remoteFn(context.Background(), "true"); err != nil {
			return fmt.Errorf("cannot reach the compute box over SSH (%v); the hermes worker runs on the box — "+
				"check the session (`stint status`) or use --worker cline", err)
		}
		if out, err := remoteFn(context.Background(), "command -v hermes"); err != nil || strings.TrimSpace(out) == "" {
			return errors.New("hermes was not found on the compute box (install it there first, or use --worker cline)")
		}
		if out, err := remoteFn(context.Background(), "curl -s -m 5 http://127.0.0.1:8080/v1/models"); err != nil || strings.TrimSpace(out) == "" {
			return fmt.Errorf("the model endpoint is not answering on the box: %v", err)
		}
	} else {
		if _, err := lookPath("cline"); err != nil {
			return errors.New("the cline CLI was not found on PATH (install with: npm i -g cline)")
		}
	}

	// 4. Model: from the flags, or the first model the endpoint serves.
	modelID := f.model
	if modelID == "" {
		modelID, err = firstEndpointModel()
		if err != nil {
			return fmt.Errorf("resolve model from the Stint endpoint: %w (or pass --model)", err)
		}
	}

	// 5. Repository: must be a git repo with no tracked modifications. For
	//    the remote worker the repo (and its clean tree) is checked on the box.
	var git gitOps
	if remote {
		git = &remoteGit{remote: remoteFn}
	} else {
		git = newGitRunner()
	}
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
	// Persist the executor settings (and command policy) so `stint deep resume`
	// can reconstruct identical invocations without a live endpoint or
	// operator memory.
	state.Exec = &deep.ExecSettings{
		Worker:          f.worker,
		AutoApprove:     f.autoApprove,
		Provider:        f.provider,
		Model:           modelID,
		ClineConfig:     f.clineConfig,
		TaskTimeoutSec:  int(f.taskTimeout.Seconds()),
		AllowedCommands: f.allowCommands,
	}
	if err := state.SaveDir(paths.StateDir); err != nil {
		return err
	}
	if err := deep.SaveMissionCopy(paths.StateDir, sessionID, f.missionPath); err != nil {
		return err
	}

	return deepRunSession(paths.StateDir, &state, &deepRunConfig{
		worker:          f.worker,
		autoApprove:     f.autoApprove,
		allowedCommands: f.allowCommands,
		provider:        f.provider,
		model:           modelID,
		apiKey:          f.apiKey,
		clineConfig:     f.clineConfig,
		taskTimeout:     f.taskTimeout,
		missionName:     mission.Name,
		taskCount:       len(mission.Tasks),
		remote:          remoteFn,
		paths:           paths,
	}, git, false)
}
