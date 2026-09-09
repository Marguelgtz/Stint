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
	return BuildTaskPromptWithActionPlan(m, t, attempt, repo, "")
}

// BuildTaskPromptWithActionPlan adds the path to an optional durable plan.
// The plan is guidance and an audit trail; repository evidence and the task's
// verify command remain authoritative.
func BuildTaskPromptWithActionPlan(m Mission, t Task, attempt int, repo RepoSummary, actionPlanPath string) string {
	var b strings.Builder
	b.WriteString("You are resuming a bounded Deep Work mission. Work only inside your working directory. ")
	b.WriteString("Do not bypass the repository policy or run destructive commands. ")
	b.WriteString("When you finish, state exactly which acceptance criteria you met, with the evidence you checked.\n\n")
	if section := GitHubPolicySection(m.GitHub); section != "" {
		b.WriteString(section)
	}

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
	phase := t.Phase
	if phase == "" {
		phase = PhaseWork
	}
	fmt.Fprintf(&b, "TASK PHASE: %s\n", phase)
	fmt.Fprintf(&b, "TASK OBJECTIVE: %s\n", t.Objective)
	if t.Reasoning != "" {
		fmt.Fprintf(&b, "TASK REASONING EFFORT: %s\n", t.Reasoning)
	}
	if strings.TrimSpace(actionPlanPath) != "" {
		fmt.Fprintf(&b, "LIVING ACTION PLAN: %s\n", actionPlanPath)
		b.WriteString("Read the living action plan before acting. Keep it current with decisions, risks, next steps, and evidence pointers; do not treat it as proof when repository or verification evidence is available.\n")
	}
	if t.Acceptance != "" {
		fmt.Fprintf(&b, "ACCEPTANCE: %s\n", t.Acceptance)
	}
	if t.Verify != "" {
		fmt.Fprintf(&b, "VERIFY COMMAND (the coordinator runs this in the worktree to check your work; make it pass): %s\n", t.Verify)
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

// GitHubPolicySection describes the repository capabilities granted to the
// worker. It is deliberately explicit because the executor is a fresh
// process and must not infer authority from a prior conversation.
func GitHubPolicySection(policy GitHubPolicy) string {
	mode, _ := NormalizeGitHubMode(string(policy.Mode))
	if mode == GitHubNone {
		return "GITHUB POLICY (mode: none)\nGitHub side effects are disabled. You may inspect local repository state, but do not push, open or update pull requests, reply to comments, or merge.\n\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "GITHUB POLICY (mode: %s)\n", mode)
	fmt.Fprintf(&b, "repository: %s\nbase: %s\napproval: %s\n", policy.Repository, policy.Base, policy.Approval)
	if len(policy.AllowedAuthors) > 0 {
		fmt.Fprintf(&b, "allowed PR authors: %s\n", strings.Join(policy.AllowedAuthors, ", "))
	}
	b.WriteString("You may inspect pull requests and review evidence. ")
	b.WriteString("Use only the configured repository and preserve its declared base branch.\n")
	b.WriteString("GPU-side GitHub operations are available through `$STINT_ONBOX_GITHUB_PUBLISH`; credentials are already provided through its protected environment and must never be printed or copied.\n")
	b.WriteString("If STINT_OPERATOR_BOUNDARY_FILE is set, read it to preserve the operator checkout's dirty/untracked boundary in the action plan and handoff.\n")
	b.WriteString("The detached supervisor stores the compact one-time PR inventory at the session state directory as `pr-inventory.json`; fetch detailed context only for the PR under review.\n")
	b.WriteString("You may commit changes in the session worktree, push session-owned branches, open or update the session's pull requests, and reply to review comments.\n")
	b.WriteString("Never force-push, delete branches, retarget or close pull requests, use administrator overrides, or expose credentials.\n")
	if mode == GitHubMaintenance {
		b.WriteString("Maintenance mode also permits narrowly scoped repairs on explicitly selected existing PR heads and submitting merge requests.\n")
		b.WriteString("You propose repairs and merge decisions; a deterministic GPU-side gatekeeper performs the actual merge only after every merge gate passes.\n")
		b.WriteString("Use the on-box publisher commands `inventory`, `context <pr>`, `reply <pr> <body>`, `push <state-dir> <branch> <commit>`, `update-pr <state-dir> <pr>`, `record <state-dir> <operation> <result> <reason>`, `gate <pr> --approval <file>`, and `request-merge <state-dir> <pr> --approval <file>`; the supervisor gatekeeper performs the actual merge after approval evidence is recorded.\n")
	}
	b.WriteString("\n")
	return b.String()
}
