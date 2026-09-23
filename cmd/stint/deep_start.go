package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/deep"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

// runDeep dispatches the Deep Work command group.
func runDeep(args []string) error {
	if len(args) == 0 {
		return errors.New("deep requires a subcommand: start, status, dash, stop, resume, or onbox")
	}
	switch args[0] {
	case "start":
		return runDeepStart(args[1:])
	case "status":
		return runDeepStatus(args[1:])
	case "dash", "dashboard":
		return runDeepDashboard(args[1:])
	case "stop":
		return runDeepStop(args[1:])
	case "resume":
		return runDeepResume(args[1:])
	case "onbox":
		return runDeepOnBox(args[1:])
	default:
		return fmt.Errorf("unknown deep subcommand %q (stint deep <start|status|dash|stop|resume|onbox>)", args[0])
	}
}

type deepStartFlags struct {
	missionPath   string
	repoPath      string
	hours         float64
	taskTimeout   time.Duration
	maxAttempts   int
	allowCommands stringSlice
	provider      string
	model         string
	reasoning     string
	actionPlan    string
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
	fs.StringVar(&f.missionPath, "mission", "", "mission Markdown file (required)")
	fs.StringVar(&f.repoPath, "repo", "", "target git repository path (required)")
	fs.Float64Var(&f.hours, "hours", 0, "optional Deep Work duration cap in hours (default: the compute session deadline)")
	fs.DurationVar(&f.taskTimeout, "task-timeout", 10*time.Minute, "maximum wall time per coding-agent invocation")
	fs.IntVar(&f.maxAttempts, "max-attempts", 3, "maximum executor attempts per task before parking")
	fs.Var(&f.allowCommands, "allow-command", "advisory command prefix included in the Hermes prompt (repeatable)")
	fs.StringVar(&f.provider, "provider", "custom:qwen-stint-{reasoning}", "configured Hermes provider id or reasoning template")
	fs.StringVar(&f.model, "model", "", "Hermes model id (default: first model served on the compute box)")
	fs.StringVar(&f.reasoning, "reasoning", deep.ReasoningMedium, "request reasoning effort: none, low, medium, or xhigh (task-level metadata may override it)")
	fs.StringVar(&f.actionPlan, "action-plan", "", "optional path inside the worktree for a living action plan; creates a first xhigh planning task")
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
	var err error
	if f.reasoning, err = deep.NormalizeReasoning(f.reasoning); err != nil {
		return fmt.Errorf("--reasoning: %w", err)
	}
	if f.reasoning == "" {
		return errors.New("--reasoning cannot be empty")
	}
	if f.actionPlan != "" {
		f.actionPlan, err = actionPlanPath(f.actionPlan)
		if err != nil {
			return err
		}
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

	remoteFn := newRemoteCmd(paths, session)

	// 3. Executor preflight: SSH, Hermes, and the model endpoint must all be
	// reachable on the compute box before a worktree or session is created.
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

	// 4. Model: from the flags, or the first model the endpoint serves.
	modelIDs, err := endpointModelIDsFromJSON(modelsJSON)
	if err != nil {
		return fmt.Errorf("parse compute-box model list: %w", err)
	}
	modelID := f.model
	if modelID == "" {
		modelID, err = firstEndpointModelFromJSON(modelsJSON)
		if err != nil {
			return fmt.Errorf("resolve model from the compute-box endpoint: %w (or pass --model)", err)
		}
	}
	if !containsString(modelIDs, modelID) {
		return fmt.Errorf("model %q is not served by the compute-box endpoint (available: %s)", modelID, strings.Join(modelIDs, ", "))
	}

	// 5. Repository: the repo (and its clean tree) is checked on the box.
	var git gitOps = &remoteGit{remote: remoteFn}
	if _, err := git.repoHead(f.repoPath); err != nil {
		return fmt.Errorf("%s is not a git repository: %v", f.repoPath, err)
	}
	if clean, detail := git.cleanTracked(f.repoPath); !clean {
		return fmt.Errorf("%s has uncommitted tracked changes; commit or stash them first:\n%s", f.repoPath, detail)
	}
	if err := preflightRemoteVerifyTools(mission, remoteFn); err != nil {
		return err
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
	baseCommit, err := git.repoHead(worktree)
	if err != nil {
		return fmt.Errorf("read new worktree HEAD: %w", err)
	}
	if strings.TrimSpace(baseCommit) == "" {
		return errors.New("new worktree returned an empty HEAD")
	}

	state := deep.NewState(sessionID, mission, f.repoPath, worktree, deadline, landBefore, f.maxAttempts, now)
	state.BaseCommit = baseCommit
	if err := state.BindCompute("vast", session.InstanceID, now); err != nil {
		return fmt.Errorf("bind Deep Work session to compute instance: %w", err)
	}
	if f.actionPlan != "" {
		state.Tasks = addActionPlanTask(state.Tasks, f.actionPlan)
	}
	// Persist the executor settings (and command policy) so `stint deep resume`
	// can reconstruct identical invocations without a live endpoint or
	// operator memory.
	state.Exec = &deep.ExecSettings{
		Worker:          workerHermes,
		Provider:        f.provider,
		Model:           modelID,
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

	return deepRunSession(paths.StateDir, &state, &deepRunConfig{
		worker:          workerHermes,
		allowedCommands: f.allowCommands,
		provider:        f.provider,
		model:           modelID,
		reasoning:       f.reasoning,
		actionPlan:      f.actionPlan,
		taskTimeout:     f.taskTimeout,
		missionName:     mission.Name,
		taskCount:       len(state.Tasks),
		remote:          remoteFn,
		paths:           paths,
	}, git, false)
}

// actionPlanPath accepts a worktree-relative path so the same persisted value
// works for Hermes workers. Keeping it below the
// worktree prevents a planning task from writing outside the session branch.
func actionPlanPath(raw string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(raw)))
	if clean == "." || clean == "" || filepath.IsAbs(raw) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("--action-plan must be a non-empty path inside the worktree")
	}
	if strings.IndexByte(clean, 0) >= 0 {
		return "", errors.New("--action-plan contains a NUL byte")
	}
	return clean, nil
}

func addActionPlanTask(tasks []deep.Task, actionPlan string) []deep.Task {
	used := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		used[task.ID] = true
	}
	for n := 1; ; n++ {
		id := fmt.Sprintf("STINT-PLAN-%03d", n)
		if used[id] {
			continue
		}
		planTask := deep.Task{
			ID:         id,
			Objective:  "Create or update the living action plan at " + actionPlan + " before execution begins",
			Acceptance: "the living action plan exists at the requested path and records decisions, risks, next steps, and evidence pointers consistent with the mission and repository state",
			Verify:     "test -s " + shellQuote(actionPlan),
			Reasoning:  deep.ReasoningXHigh,
			Status:     deep.StatusQueued,
			Source:     "coordinator",
		}
		return append([]deep.Task{planTask}, tasks...)
	}
}

func firstEndpointModelFromJSON(raw string) (string, error) {
	ids, err := endpointModelIDsFromJSON(raw)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", errors.New("the model endpoint reported no models")
	}
	return ids[0], nil
}

func endpointModelIDsFromJSON(raw string) ([]string, error) {
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(body.Data))
	for _, item := range body.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func verificationToolNames(commands []string) []string {
	builtins := map[string]bool{
		".": true, "[": true, "alias": true, "break": true, "cd": true, "command": true,
		"continue": true, "echo": true, "eval": true, "exec": true, "exit": true, "export": true,
		"false": true, "local": true, "printf": true, "pwd": true, "read": true, "return": true,
		"set": true, "shift": true, "source": true, "test": true, "true": true, "trap": true,
		"type": true, "ulimit": true, "umask": true, "unset": true, "wait": true,
	}
	controlWords := map[string]bool{
		"!": true, "do": true, "done": true, "elif": true, "else": true, "esac": true,
		"fi": true, "for": true, "if": true, "in": true, "then": true, "time": true,
		"until": true, "while": true,
	}
	seen := map[string]bool{}
	for _, command := range commands {
		for _, segment := range splitShellCommands(command) {
			fields := strings.Fields(segment)
			for len(fields) > 0 && strings.Contains(fields[0], "=") && !strings.HasPrefix(fields[0], "=") {
				fields = fields[1:]
			}
			for len(fields) > 0 && controlWords[fields[0]] {
				fields = fields[1:]
			}
			if len(fields) == 0 {
				continue
			}
			name := strings.Trim(fields[0], "'\"` ")
			if name == "" || strings.HasPrefix(name, "$") || builtins[name] {
				continue
			}
			seen[name] = true
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func splitShellCommands(command string) []string {
	var out []string
	var b strings.Builder
	var quote rune
	escaped := false
	for _, r := range command {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			b.WriteRune(r)
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			b.WriteRune(r)
			continue
		}
		if r == '\'' || r == '"' || r == '`' {
			quote = r
			b.WriteRune(r)
			continue
		}
		if r == ';' || r == '|' || r == '&' || r == '\n' {
			if segment := strings.TrimSpace(b.String()); segment != "" {
				out = append(out, segment)
			}
			b.Reset()
			continue
		}
		b.WriteRune(r)
	}
	if segment := strings.TrimSpace(b.String()); segment != "" {
		out = append(out, segment)
	}
	return out
}

func preflightRemoteVerifyTools(mission deep.Mission, remote remoteCmd) error {
	commands := make([]string, 0, len(mission.Tasks)+1)
	commands = append(commands, mission.Verify)
	for _, task := range mission.Tasks {
		commands = append(commands, task.Verify)
	}
	for _, tool := range verificationToolNames(commands) {
		if _, err := remote(context.Background(), "command -v "+shellQuote(tool)+" >/dev/null 2>&1"); err != nil {
			return fmt.Errorf("mission verification requires %q, which is unavailable on the compute box", tool)
		}
	}
	return nil
}

func preflightLocalVerifyTools(mission deep.Mission) error {
	commands := make([]string, 0, len(mission.Tasks)+1)
	commands = append(commands, mission.Verify)
	for _, task := range mission.Tasks {
		commands = append(commands, task.Verify)
	}
	for _, tool := range verificationToolNames(commands) {
		if _, err := lookPath(tool); err != nil {
			return fmt.Errorf("mission verification requires %q, which is unavailable on the compute box", tool)
		}
	}
	return nil
}
