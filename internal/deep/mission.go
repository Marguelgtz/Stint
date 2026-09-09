package deep

import (
	"fmt"
	"regexp"
	"strings"
)

// Mission is the parsed form of a Deep Work mission file.
//
// A mission is a small Markdown document with a documented structure:
//
//	# <mission name>
//
//	## Objective
//	<one or more lines>
//
//	## Success
//	- <criterion>
//
//	## Constraints
//	- <constraint>
//
//	## Verification
//	<shell command the coordinator runs to verify the workspace>
//
//	## Tasks
//	- [ ] <ID>: <objective>
//	  - acceptance: <what must be true for the task to count as done>
//	  - verify: <shell command the coordinator runs to verify THIS task;
//	    overrides the mission-level ## Verification command for this task>
//
// Unknown sections are ignored so the format can grow. Objective and a
// non-empty task list are required; anything else is optional.
type Mission struct {
	Name        string
	Objective   string
	Success     []string
	Constraints []string
	Verify      string
	GitHub      GitHubPolicy
	Completion  CompletionPolicy
	Tasks       []Task
}

// ParseMission parses mission Markdown content into a Mission.
func ParseMission(content string) (Mission, error) {
	var m Mission
	var section string
	var taskIdx = -1
	var githubMode, githubRepository, githubBase, githubAuthors, githubApproval string
	var completionValue string

	taskIDRe := regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			continue
		}
		if strings.HasPrefix(trimmed, "# ") && section == "" && m.Name == "" {
			m.Name = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			continue
		}
		if trimmed == "" {
			continue
		}

		switch section {
		case "objective":
			m.Objective = appendLine(m.Objective, trimmed)
		case "success":
			if b := bullet(trimmed); b != "" {
				m.Success = append(m.Success, b)
			}
		case "constraints":
			if b := bullet(trimmed); b != "" {
				m.Constraints = append(m.Constraints, b)
			}
		case "verification":
			if v := stripCodeFence(trimmed); v != "" && m.Verify == "" {
				m.Verify = v
			}
		case "github":
			key, value, ok := policyField(trimmed)
			if !ok {
				continue
			}
			switch key {
			case "mode":
				githubMode = value
			case "repository":
				githubRepository = value
			case "base":
				githubBase = value
			case "allowed-authors", "allowed_authors":
				githubAuthors = value
			case "approval":
				githubApproval = value
			}
		case "completion":
			key, value, ok := policyField(trimmed)
			if ok && key == "policy" {
				completionValue = value
			}
		case "tasks":
			body := strings.TrimSpace(line)
			for _, p := range []string{"- [ ]", "- [x]", "- [X]", "- ", "* "} {
				if strings.HasPrefix(body, p) {
					body = strings.TrimSpace(strings.TrimPrefix(body, p))
					break
				}
			}
			if strings.HasPrefix(body, "acceptance:") && taskIdx >= 0 {
				m.Tasks[taskIdx].Acceptance = strings.TrimSpace(strings.TrimPrefix(body, "acceptance:"))
			} else if strings.HasPrefix(body, "verify:") && taskIdx >= 0 {
				m.Tasks[taskIdx].Verify = strings.TrimSpace(stripCodeFence(strings.TrimPrefix(body, "verify:")))
			} else if strings.HasPrefix(body, "reasoning:") && taskIdx >= 0 {
				level, err := NormalizeReasoning(strings.TrimSpace(strings.TrimPrefix(body, "reasoning:")))
				if err != nil {
					return m, fmt.Errorf("task %s: %w", m.Tasks[taskIdx].ID, err)
				}
				m.Tasks[taskIdx].Reasoning = level
			} else if strings.HasPrefix(body, "phase:") && taskIdx >= 0 {
				phase, err := NormalizeTaskPhase(strings.TrimSpace(strings.TrimPrefix(body, "phase:")))
				if err != nil {
					return m, fmt.Errorf("task %s: %w", m.Tasks[taskIdx].ID, err)
				}
				m.Tasks[taskIdx].Phase = phase
			} else if id, objective, ok := taskFields(body); ok {
				if !taskIDRe.MatchString(id) {
					return m, fmt.Errorf("task ID %q is invalid (use letters, digits, _ or -)", id)
				}
				for _, existing := range m.Tasks {
					if existing.ID == id {
						return m, fmt.Errorf("duplicate task ID %q", id)
					}
				}
				m.Tasks = append(m.Tasks, Task{ID: id, Objective: objective, Phase: PhaseWork, Status: StatusQueued, Source: "mission"})
				taskIdx = len(m.Tasks) - 1
			}
		}
	}

	if strings.TrimSpace(m.Objective) == "" {
		return m, fmt.Errorf("mission requires an ## Objective section")
	}
	if len(m.Tasks) == 0 {
		return m, fmt.Errorf("mission requires at least one task (## Tasks: '- [ ] ID: objective')")
	}
	mode, err := NormalizeGitHubMode(githubMode)
	if err != nil {
		return m, err
	}
	approval, err := NormalizeApprovalPolicy(githubApproval)
	if err != nil {
		return m, err
	}
	completion, err := NormalizeCompletionPolicy(completionValue)
	if err != nil {
		return m, err
	}
	m.GitHub = GitHubPolicy{Mode: mode, Repository: strings.TrimSpace(githubRepository), Base: strings.TrimSpace(githubBase), AllowedAuthors: splitPolicyList(githubAuthors), Approval: approval}
	m.Completion = completion
	if err := m.GitHub.Validate(); err != nil {
		return m, err
	}
	return m, nil
}

func policyField(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if b := bullet(line); b != "" {
		line = b
	}
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	key = strings.ToLower(strings.TrimSpace(line[:idx]))
	value = strings.TrimSpace(line[idx+1:])
	return key, value, value != ""
}

func splitPolicyList(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func ParseMissionFile(path string) (Mission, error) {
	content, err := readAll(path)
	if err != nil {
		return Mission{}, err
	}
	return ParseMission(string(content))
}

func appendLine(current, line string) string {
	if current == "" {
		return line
	}
	return current + "\n" + line
}

func bullet(line string) string {
	line = strings.TrimSpace(line)
	for _, p := range []string{"- ", "* ", "• "} {
		if strings.HasPrefix(line, p) {
			return strings.TrimSpace(strings.TrimPrefix(line, p))
		}
	}
	return ""
}

func stripCodeFence(line string) string {
	line = strings.TrimSpace(line)
	for _, f := range []string{"```", "~~~"} {
		if strings.HasPrefix(line, f) {
			line = strings.TrimSpace(strings.TrimPrefix(line, f))
		}
	}
	for _, f := range []string{"```", "~~~"} {
		if strings.HasSuffix(line, f) {
			line = strings.TrimSpace(strings.TrimSuffix(line, f))
		}
	}
	return line
}

// taskFields splits "ID: objective" from an already bullet-stripped line.
func taskFields(body string) (id, objective string, ok bool) {
	idx := strings.Index(body, ":")
	if idx <= 0 {
		return "", "", false
	}
	id = strings.TrimSpace(body[:idx])
	objective = strings.TrimSpace(body[idx+1:])
	if id == "" || objective == "" {
		return "", "", false
	}
	return id, objective, true
}
