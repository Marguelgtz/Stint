package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/deep"
	localenv "github.com/Marguelgtz/Stint/internal/local"
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
	case "preview":
		return runDeepPreview(args[1:])
	case "list":
		return runDeepList(args[1:])
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

// runDeepPreview validates a mission through the production parser and emits
// its parsed form for the GPUI launch review.
func runDeepPreview(args []string) error {
	fs := flag.NewFlagSet("deep preview", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	missionPath := fs.String("mission", "", "mission Markdown file")
	repoPath := fs.String("repo", "", "target git repository")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *missionPath == "" || *repoPath == "" {
		return errors.New("deep preview requires --mission and --repo")
	}
	mission, err := deep.ParseMissionFile(*missionPath)
	if err != nil {
		return err
	}
	if err := validateDeepRepository(*repoPath); err != nil {
		return err
	}
	content, err := os.ReadFile(*missionPath)
	if err != nil { return err }
	return json.NewEncoder(os.Stdout).Encode(struct {
		Name        string      `json:"name"`
		Objective   string      `json:"objective"`
		Success     []string    `json:"success,omitempty"`
		Constraints []string    `json:"constraints,omitempty"`
		Tasks       []deep.Task `json:"tasks"`
		Content     string      `json:"content"`
	}{mission.Name, mission.Objective, mission.Success, mission.Constraints, mission.Tasks, string(content)})
}

// runDeepList prints durable sessions for the GPUI history selector.
func runDeepList(args []string) error {
	fs := flag.NewFlagSet("deep list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*jsonOut {
		return errors.New("deep list currently requires --json")
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(paths.StateDir, "deep"))
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		entries = nil
	}
	if binding, bindErr := loadDeepProductionBinding(paths); bindErr == nil {
		data, remoteErr := loadRemoteDeepHistory(paths, binding)
		if remoteErr != nil { return fmt.Errorf("read Deep Work history from compute instance %d: %w", binding.InstanceID, remoteErr) }
		_, err := os.Stdout.Write(append(data, '\n'))
		return err
	}
	latestBytes, _ := os.ReadFile(deep.LatestFile(paths.StateDir))
	latest := strings.TrimSpace(string(latestBytes))
	type item struct {
		State            deep.DeepState  `json:"state"`
		Latest           bool            `json:"latest"`
		CoordinatorAlive bool            `json:"coordinatorAlive"`
		Incidents        []deep.Incident `json:"incidents"`
	}
	items := make([]item, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		state, err := deep.LoadState(paths.StateDir, entry.Name())
		if err != nil {
			continue
		}
		incs, _ := deep.ReadIncidents(paths.StateDir, state.SessionID)
		if len(incs) > 8 {
			incs = incs[len(incs)-8:]
		}
		alive, _ := deep.CoordinatorAlive(paths.StateDir, state.SessionID)
		items = append(items, item{state, state.SessionID == latest, alive, incs})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].State.StartedAt.After(items[j].State.StartedAt) })
	return json.NewEncoder(os.Stdout).Encode(items)
}

func loadRemoteDeepHistory(paths config.Paths, binding deepProductionRunBinding) ([]byte, error) {
	return runRemoteDeepCommand(paths, binding, "deep", "list", "--json")
}

func runRemoteDeepCommand(paths config.Paths, binding deepProductionRunBinding, commandArgs ...string) ([]byte, error) {
	ssh, err := localenv.SSHExecutable()
	if err != nil { return nil, err }
	state := sessionstate.State{InstanceID: binding.InstanceID, SSHHost: binding.SSHHost, SSHPort: binding.SSHPort}
	parts := []string{"env", "XDG_STATE_HOME=" + shellQuote(filepath.Join(binding.RemoteRoot, "state")), shellQuote(filepath.Join(binding.RemoteRoot, "bin", "stint"))}
	for _, arg := range commandArgs { parts = append(parts, shellQuote(arg)) }
	remoteCommand := strings.Join(parts, " ")
	args := sshArgs(paths, state, filepath.Join(paths.StateDir, "known_hosts"), remoteCommand)
	command := exec.Command(ssh, args...)
	data, err := command.Output()
	if err != nil {
		stderr := ""
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		if stderr != "" { return nil, fmt.Errorf("SSH Deep Work query failed: %s", stderr) }
		return nil, fmt.Errorf("SSH Deep Work query failed: %w", err)
	}
	return data, nil
}

func validateDeepRepository(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("target repository: %w", err)
	}
	if !info.IsDir() {
		return errors.New("target repository must be a directory")
	}
	status, err := runLocalGit(path, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect target repository: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("target repository must be clean before Deep Work starts")
	}
	return nil
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
