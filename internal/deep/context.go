package deep

import (
	"fmt"
	"strings"
)

// RepoSummary is the durable git truth included in reconstructed task
// context: the fresh invocation must be able to continue from this text
// alone, without any memory of the previous conversation.
type RepoSummary struct {
	Branch     string
	HeadCommit string
	RecentLog  string // last few commits, oneline
	Changed    string // git status --short summary
	DiffStat   string // diff stat vs base commit (empty when no base)
}

// BuildTaskPrompt reconstructs the full context for one executor invocation:
// mission, current task, attempt number, previous attempt result, and the
// current repository state. A fresh coding-agent process reading only this
// prompt can continue the work.
func BuildTaskPrompt(m Mission, t Task, attempt int, repo RepoSummary) string {
	var b strings.Builder
	b.WriteString("You are resuming a bounded Deep Work mission. Work only inside your working directory. ")
	b.WriteString("Never push, open pull requests, or run destructive commands. ")
	b.WriteString("When you finish, state exactly which acceptance criteria you met, with the evidence you checked.\n\n")

	fmt.Fprintf(&b, "MISSION: %s\n", m.Name)
	fmt.Fprintf(&b, "OBJECTIVE: %s\n", strings.TrimSpace(m.Objective))
	if len(m.Success) > 0 {
		b.WriteString("SUCCESS CRITERIA:\n")
		for _, s := range m.Success {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	if len(m.Constraints) > 0 {
		b.WriteString("CONSTRAINTS:\n")
		for _, c := range m.Constraints {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}

	fmt.Fprintf(&b, "\nCURRENT TASK: %s (attempt %d)\n", t.ID, attempt)
	fmt.Fprintf(&b, "TASK OBJECTIVE: %s\n", t.Objective)
	if t.Acceptance != "" {
		fmt.Fprintf(&b, "ACCEPTANCE: %s\n", t.Acceptance)
	}
	if t.LastResult != "" {
		fmt.Fprintf(&b, "PREVIOUS ATTEMPT RESULT (attempt %d):\n%s\n", attempt-1, t.LastResult)
	}
	if len(t.Findings) > 0 {
		b.WriteString("FINDINGS SO FAR:\n")
		for _, f := range t.Findings {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}

	b.WriteString("\nREPOSITORY STATE:\n")
	fmt.Fprintf(&b, "branch: %s\n", repo.Branch)
	if repo.HeadCommit != "" {
		fmt.Fprintf(&b, "head: %s\n", repo.HeadCommit)
	}
	if strings.TrimSpace(repo.RecentLog) != "" {
		b.WriteString("recent commits:\n" + repo.RecentLog + "\n")
	}
	if strings.TrimSpace(repo.DiffStat) != "" {
		b.WriteString("diff vs session base:\n" + repo.DiffStat + "\n")
	}
	if strings.TrimSpace(repo.Changed) != "" {
		b.WriteString("uncommitted changes:\n" + repo.Changed + "\n")
	}

	b.WriteString("\nINSTRUCTIONS:\n")
	b.WriteString("1. Continue the task from the repository state above.\n")
	b.WriteString("2. Keep changes minimal and confined to this working directory.\n")
	b.WriteString("3. Verify the acceptance criteria yourself before claiming completion.\n")
	b.WriteString("4. If you are blocked, state precisely what blocks you and what you already tried.\n")
	return b.String()
}
