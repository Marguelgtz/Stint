package deep

import (
	"errors"
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
//	<raw trusted shell command the coordinator runs to verify the workspace>
//
//	## Acceptance Contract
//	version: 2
//
//	## Semantic Review Contract
//	version: 1
//
//	## Tasks
//	- [ ] <ID>: <objective>
//	  - acceptance: <narrative objective intent included in the executor prompt>
//	  - verify: <raw trusted shell command producing evidence for THIS work unit;
//	    overrides the mission-level ## Verification command for this work unit>
//	  - repository-change: required | optional | forbidden
//	  - acceptance-check: <raw trusted shell command proving the specific outcome>
//	  - depends-on: IMPLEMENT-001
//
// Unknown sections are ignored so the format can grow. Objective and a
// non-empty task list are required; anything else is optional.
type Mission struct {
	Name        string
	Objective   string
	Success     []string
	Constraints []string
	Verify      string
	// AcceptanceContractVersion is zero for missions authored before the
	// deterministic acceptance contract was explicitly opted into.
	AcceptanceContractVersion int
	AcceptanceContractSHA256  string
	// SemanticReviewContractVersion is zero unless a mission explicitly opts
	// into the fresh-context, checkpoint-bound reviewer gate.
	SemanticReviewContractVersion int
	SemanticReviewContractSHA256  string
	GitHub                        GitHubPolicy
	GitHubConfigured              bool
	Tasks                         []Task
}

// ParseMission parses mission Markdown content into a Mission.
func ParseMission(content string) (Mission, error) {
	var m Mission
	var section string
	var taskIdx = -1
	var verifyFence string
	var verifyFenceBody []string
	acceptanceContractDeclared := false
	acceptanceContractFields := make(map[string]bool)
	semanticReviewContractDeclared := false
	semanticReviewContractFields := make(map[string]bool)
	taskAcceptanceFields := make(map[string]map[string]int)
	var githubMode, githubRepository, githubBase, githubAuthors, githubApproval string
	githubFields := make(map[string]bool)

	taskIDRe := regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)

		if verifyFence != "" {
			if trimmed == verifyFence {
				command := strings.TrimSpace(strings.Join(verifyFenceBody, "\n"))
				if err := ValidateVerifyCommand(command); err != nil {
					return m, fmt.Errorf("mission verification command: %w", err)
				}
				m.Verify = command
				verifyFence = ""
				verifyFenceBody = nil
			} else {
				verifyFenceBody = append(verifyFenceBody, line)
			}
			continue
		}

		if strings.HasPrefix(trimmed, "## ") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			if section == "acceptance contract" {
				acceptanceContractDeclared = true
			}
			if section == "semantic review contract" {
				semanticReviewContractDeclared = true
			}
			if section == "github" {
				m.GitHubConfigured = true
			}
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
			if trimmed != "" && m.Verify == "" {
				command, fence, fenced, err := parseVerificationFenceLine(trimmed)
				if err != nil {
					return m, fmt.Errorf("mission verification command: %w", err)
				}
				if fenced && fence != "" {
					verifyFence = fence
					verifyFenceBody = nil
					continue
				}
				if err := ValidateVerifyCommand(command); err != nil {
					return m, fmt.Errorf("mission verification command: %w", err)
				}
				m.Verify = command
			}
		case "acceptance contract":
			key, value, ok := policyField(trimmed)
			if !ok {
				return m, errors.New("acceptance contract requires a version field")
			}
			if key != "version" {
				return m, fmt.Errorf("unknown acceptance contract field %q", key)
			}
			if acceptanceContractFields[key] {
				return m, errors.New("acceptance contract version is declared more than once")
			}
			acceptanceContractFields[key] = true
			if value != "2" {
				return m, fmt.Errorf("unsupported mission acceptance contract version %q", value)
			}
			m.AcceptanceContractVersion = DeterministicAcceptanceContractVersion
		case "semantic review contract":
			key, value, ok := policyField(trimmed)
			if !ok {
				return m, errors.New("semantic review contract requires a version field")
			}
			if key != "version" {
				return m, fmt.Errorf("unknown semantic review contract field %q", key)
			}
			if semanticReviewContractFields[key] {
				return m, errors.New("semantic review contract version is declared more than once")
			}
			semanticReviewContractFields[key] = true
			if value != "1" && value != "2" {
				return m, fmt.Errorf("unsupported semantic review contract version %q", value)
			}
			if value == "1" {
				m.SemanticReviewContractVersion = SemanticReviewContractVersion
			} else {
				m.SemanticReviewContractVersion = SemanticReviewMissionContractVersion
			}
		case "github":
			key, value, ok := policyField(trimmed)
			if !ok {
				continue
			}
			if key == "allowed_authors" {
				key = "allowed-authors"
			}
			if key == "mode" || key == "repository" || key == "base" || key == "allowed-authors" || key == "approval" {
				if githubFields[key] {
					return m, fmt.Errorf("GitHub policy field %q is declared more than once", key)
				}
				githubFields[key] = true
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
		case "tasks":
			body := strings.TrimSpace(line)
			for _, p := range []string{"- [ ]", "- [x]", "- [X]", "- ", "* "} {
				if strings.HasPrefix(body, p) {
					body = strings.TrimSpace(strings.TrimPrefix(body, p))
					break
				}
			}
			if strings.HasPrefix(body, "depends-on:") && taskIdx >= 0 {
				value := strings.TrimSpace(strings.TrimPrefix(body, "depends-on:"))
				for _, id := range strings.Split(value, ",") {
					id = strings.TrimSpace(id)
					if id == "" || !taskIDRe.MatchString(id) {
						return m, fmt.Errorf("task %s: depends-on must name one or more valid task IDs", m.Tasks[taskIdx].ID)
					}
					for _, existing := range m.Tasks[taskIdx].DependsOn {
						if existing == id {
							return m, fmt.Errorf("task %s: duplicate prerequisite %q", m.Tasks[taskIdx].ID, id)
						}
					}
					m.Tasks[taskIdx].DependsOn = append(m.Tasks[taskIdx].DependsOn, id)
				}
			} else if strings.HasPrefix(body, "repository-change:") && taskIdx >= 0 {
				markTaskAcceptanceField(taskAcceptanceFields, m.Tasks[taskIdx].ID, "repository-change")
				value := RepositoryChangeExpectation(strings.ToLower(strings.TrimSpace(strings.TrimPrefix(body, "repository-change:"))))
				m.Tasks[taskIdx].RepositoryChange = value
			} else if strings.HasPrefix(body, "acceptance-check:") && taskIdx >= 0 {
				markTaskAcceptanceField(taskAcceptanceFields, m.Tasks[taskIdx].ID, "acceptance-check")
				m.Tasks[taskIdx].AcceptanceCheck = strings.TrimSpace(strings.TrimPrefix(body, "acceptance-check:"))
			} else if strings.HasPrefix(body, "acceptance:") && taskIdx >= 0 {
				m.Tasks[taskIdx].Acceptance = strings.TrimSpace(strings.TrimPrefix(body, "acceptance:"))
			} else if strings.HasPrefix(body, "verify:") && taskIdx >= 0 {
				command, err := parseTaskVerifyCommand(strings.TrimSpace(strings.TrimPrefix(body, "verify:")))
				if err != nil {
					return m, fmt.Errorf("task %s verify command: %w", m.Tasks[taskIdx].ID, err)
				}
				m.Tasks[taskIdx].Verify = command
			} else if strings.HasPrefix(body, "reasoning:") && taskIdx >= 0 {
				level, err := NormalizeReasoning(strings.TrimSpace(strings.TrimPrefix(body, "reasoning:")))
				if err != nil {
					return m, fmt.Errorf("task %s: %w", m.Tasks[taskIdx].ID, err)
				}
				m.Tasks[taskIdx].Reasoning = level
			} else if id, objective, ok := taskFields(body); ok {
				if !taskIDRe.MatchString(id) {
					return m, fmt.Errorf("task ID %q is invalid (use letters, digits, _ or -)", id)
				}
				if isReservedTaskID(id) {
					return m, fmt.Errorf("task ID %q uses the reserved STINT coordinator namespace", id)
				}
				for _, existing := range m.Tasks {
					if existing.ID == id {
						return m, fmt.Errorf("duplicate task ID %q", id)
					}
				}
				m.Tasks = append(m.Tasks, Task{ID: id, Objective: objective, Status: StatusQueued, Source: "mission"})
				taskIdx = len(m.Tasks) - 1
			}
		}
	}
	if verifyFence != "" {
		return m, fmt.Errorf("mission verification command: unterminated %s fence", verifyFence)
	}

	if strings.TrimSpace(m.Objective) == "" {
		return m, fmt.Errorf("mission requires an ## Objective section")
	}
	if len(m.Tasks) == 0 {
		return m, fmt.Errorf("mission requires at least one task (## Tasks: '- [ ] ID: objective')")
	}
	if acceptanceContractDeclared && m.AcceptanceContractVersion == 0 {
		return m, errors.New("acceptance contract section requires an explicit supported version")
	}
	if semanticReviewContractDeclared && m.SemanticReviewContractVersion == 0 {
		return m, errors.New("semantic review contract section requires an explicit supported version")
	}
	if m.AcceptanceContractVersion == 0 {
		// These names were previously unknown task annotations. Keep a legacy
		// mission on its original behavior, including when it contains
		// malformed values that were never executable under that contract.
		for i := range m.Tasks {
			m.Tasks[i].RepositoryChange = ""
			m.Tasks[i].AcceptanceCheck = ""
		}
	} else {
		for i := range m.Tasks {
			if duplicates := taskAcceptanceFields[m.Tasks[i].ID]; duplicates != nil {
				for field, count := range duplicates {
					if count > 1 {
						return m, fmt.Errorf("task %s: %s is declared more than once", m.Tasks[i].ID, field)
					}
				}
			}
			if strings.TrimSpace(m.Tasks[i].AcceptanceCheck) != "" {
				command, err := parseTaskCommand(m.Tasks[i].AcceptanceCheck, "acceptance-check")
				if err != nil {
					return m, fmt.Errorf("task %s acceptance-check command: %w", m.Tasks[i].ID, err)
				}
				m.Tasks[i].AcceptanceCheck = command
			}
		}
	}
	seenTasks := make(map[string]bool, len(m.Tasks))
	for _, task := range m.Tasks {
		for _, dependency := range task.DependsOn {
			if !seenTasks[dependency] {
				return m, fmt.Errorf("task %s: prerequisite %q must be a task declared earlier in the mission", task.ID, dependency)
			}
		}
		seenTasks[task.ID] = true
	}
	mode, err := NormalizeGitHubMode(githubMode)
	if err != nil {
		return m, err
	}
	approval, err := NormalizeApprovalPolicy(githubApproval)
	if err != nil {
		return m, err
	}
	m.GitHub = GitHubPolicy{
		Mode:           mode,
		Repository:     strings.TrimSpace(githubRepository),
		Base:           strings.TrimSpace(githubBase),
		AllowedAuthors: splitPolicyList(githubAuthors),
		Approval:       approval,
	}
	if err := m.GitHub.Validate(); err != nil {
		return m, err
	}
	if err := ValidateAcceptanceContract(m.AcceptanceContractVersion, m.Tasks); err != nil {
		return m, err
	}
	if err := ValidateSemanticReviewContract(m.SemanticReviewContractVersion, m.AcceptanceContractVersion, m.Tasks); err != nil {
		return m, err
	}
	if m.AcceptanceContractVersion != 0 {
		m.AcceptanceContractSHA256, err = AcceptanceContractIdentity(m)
		if err != nil {
			return m, err
		}
	}
	if m.SemanticReviewContractVersion != 0 {
		m.SemanticReviewContractSHA256, err = SemanticReviewContractIdentity(m)
		if err != nil {
			return m, err
		}
	}
	return m, nil
}

func markTaskAcceptanceField(fields map[string]map[string]int, taskID, field string) {
	taskFields := fields[taskID]
	if taskFields == nil {
		taskFields = make(map[string]int)
		fields[taskID] = taskFields
	}
	taskFields[field]++
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

func isReservedTaskID(id string) bool {
	return strings.HasPrefix(id, "STINT-PLAN-") || strings.HasPrefix(id, "STINT-CLOSE-")
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
