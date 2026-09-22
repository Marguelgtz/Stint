package deep

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// GitHubMode selects the repository side effects permitted for a mission.
type GitHubMode string

const (
	GitHubNone        GitHubMode = "none"
	GitHubEngineering GitHubMode = "engineering"
	GitHubMaintenance GitHubMode = "maintenance"
)

// ApprovalPolicy records the approval evidence required by GitHub policy.
type ApprovalPolicy string

const (
	ApprovalInternal ApprovalPolicy = "internal"
	ApprovalGitHub   ApprovalPolicy = "github"
	ApprovalBot      ApprovalPolicy = "bot"
)

// GitHubPolicy is persisted with the mission state and is the authority for
// all publisher configuration that can change repository or merge behavior.
type GitHubPolicy struct {
	Mode           GitHubMode     `json:"mode"`
	Repository     string         `json:"repository,omitempty"`
	Base           string         `json:"base,omitempty"`
	AllowedAuthors []string       `json:"allowedAuthors,omitempty"`
	Approval       ApprovalPolicy `json:"approval"`
}

var (
	githubRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	githubAuthorPattern     = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
)

func NormalizeGitHubMode(value string) (GitHubMode, error) {
	mode := GitHubMode(strings.ToLower(strings.TrimSpace(value)))
	if mode == "" {
		return GitHubNone, nil
	}
	switch mode {
	case GitHubNone, GitHubEngineering, GitHubMaintenance:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid GitHub mode %q (want none, engineering, or maintenance)", value)
	}
}

func NormalizeApprovalPolicy(value string) (ApprovalPolicy, error) {
	approval := ApprovalPolicy(strings.ToLower(strings.TrimSpace(value)))
	if approval == "" {
		return ApprovalInternal, nil
	}
	switch approval {
	case ApprovalInternal, ApprovalGitHub, ApprovalBot:
		return approval, nil
	default:
		return "", fmt.Errorf("invalid approval policy %q (want internal, github, or bot)", value)
	}
}

func (p GitHubPolicy) Validate() error {
	mode, err := NormalizeGitHubMode(string(p.Mode))
	if err != nil {
		return err
	}
	if _, err := NormalizeApprovalPolicy(string(p.Approval)); err != nil {
		return err
	}
	if mode == GitHubNone {
		if strings.TrimSpace(p.Repository) != "" || strings.TrimSpace(p.Base) != "" || len(p.AllowedAuthors) != 0 {
			return fmt.Errorf("GitHub mode none cannot carry repository, base, or author policy")
		}
		return nil
	}
	if !githubRepositoryPattern.MatchString(strings.TrimSpace(p.Repository)) {
		return fmt.Errorf("GitHub repository must be owner/name for mode %s", mode)
	}
	if strings.TrimSpace(p.Base) == "" || strings.ContainsAny(p.Base, "\r\n\x00") {
		return fmt.Errorf("GitHub base branch is required for mode %s", mode)
	}
	seen := make(map[string]bool, len(p.AllowedAuthors))
	for _, author := range p.AllowedAuthors {
		author = strings.TrimSpace(author)
		if !githubAuthorPattern.MatchString(author) {
			return fmt.Errorf("invalid GitHub allowed author %q", author)
		}
		if seen[strings.ToLower(author)] {
			return fmt.Errorf("duplicate GitHub allowed author %q", author)
		}
		seen[strings.ToLower(author)] = true
	}
	if mode == GitHubMaintenance && len(seen) == 0 {
		return fmt.Errorf("maintenance mode requires at least one allowed author")
	}
	return nil
}

// SameGitHubPolicy compares all policy-bearing fields, treating authors as an
// unordered set because environment and Markdown representations may order them
// differently.
func SameGitHubPolicy(a, b GitHubPolicy) bool {
	authors := func(values []string) []string {
		out := make([]string, 0, len(values))
		for _, value := range values {
			out = append(out, strings.ToLower(strings.TrimSpace(value)))
		}
		sort.Strings(out)
		return out
	}
	modeA, errA := NormalizeGitHubMode(string(a.Mode))
	modeB, errB := NormalizeGitHubMode(string(b.Mode))
	approvalA, approvalErrA := NormalizeApprovalPolicy(string(a.Approval))
	approvalB, approvalErrB := NormalizeApprovalPolicy(string(b.Approval))
	return errA == nil && errB == nil && approvalErrA == nil && approvalErrB == nil &&
		modeA == modeB && approvalA == approvalB &&
		strings.TrimSpace(a.Repository) == strings.TrimSpace(b.Repository) &&
		strings.TrimSpace(a.Base) == strings.TrimSpace(b.Base) &&
		strings.Join(authors(a.AllowedAuthors), "\x00") == strings.Join(authors(b.AllowedAuthors), "\x00")
}
