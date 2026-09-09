package deep

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// GitHubMode controls the repository side effects a worker may request.
type GitHubMode string

const (
	GitHubNone        GitHubMode = "none"
	GitHubEngineering GitHubMode = "engineering"
	GitHubMaintenance GitHubMode = "maintenance"
)

type ApprovalPolicy string

const (
	ApprovalInternal ApprovalPolicy = "internal"
	ApprovalGitHub   ApprovalPolicy = "github"
	ApprovalBot      ApprovalPolicy = "bot"
)

type CompletionPolicy string

const (
	CompletionReportAndDestroy CompletionPolicy = "report-and-destroy"
	CompletionBoundedReplan    CompletionPolicy = "bounded-replan"
)

type TaskPhase string

const (
	PhasePlan   TaskPhase = "plan"
	PhaseWork   TaskPhase = "work"
	PhaseReview TaskPhase = "review"
	PhaseClose  TaskPhase = "close"
)

type GitHubPolicy struct {
	Mode           GitHubMode     `json:"mode,omitempty"`
	Repository     string         `json:"repository,omitempty"`
	Base           string         `json:"base,omitempty"`
	AllowedAuthors []string       `json:"allowedAuthors,omitempty"`
	Approval       ApprovalPolicy `json:"approval,omitempty"`
}

var githubRepositoryRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

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
	policy := ApprovalPolicy(strings.ToLower(strings.TrimSpace(value)))
	if policy == "" {
		return ApprovalInternal, nil
	}
	switch policy {
	case ApprovalInternal, ApprovalGitHub, ApprovalBot:
		return policy, nil
	default:
		return "", fmt.Errorf("invalid approval policy %q (want internal, github, or bot)", value)
	}
}

func NormalizeCompletionPolicy(value string) (CompletionPolicy, error) {
	policy := CompletionPolicy(strings.ToLower(strings.TrimSpace(value)))
	if policy == "" {
		return CompletionReportAndDestroy, nil
	}
	switch policy {
	case CompletionReportAndDestroy, CompletionBoundedReplan:
		return policy, nil
	default:
		return "", fmt.Errorf("invalid completion policy %q (want report-and-destroy or bounded-replan)", value)
	}
}

func NormalizeTaskPhase(value string) (TaskPhase, error) {
	phase := TaskPhase(strings.ToLower(strings.TrimSpace(value)))
	if phase == "" {
		return PhaseWork, nil
	}
	switch phase {
	case PhasePlan, PhaseWork, PhaseReview, PhaseClose:
		return phase, nil
	default:
		return "", fmt.Errorf("invalid task phase %q (want plan, work, review, or close)", value)
	}
}

func (p GitHubPolicy) Validate() error {
	mode, err := NormalizeGitHubMode(string(p.Mode))
	if err != nil {
		return err
	}
	if mode == GitHubNone {
		return nil
	}
	if !githubRepositoryRe.MatchString(strings.TrimSpace(p.Repository)) {
		return fmt.Errorf("GitHub repository must be owner/name for mode %s", mode)
	}
	if strings.TrimSpace(p.Base) == "" || strings.ContainsAny(p.Base, "\r\n\x00") {
		return fmt.Errorf("GitHub base branch is required for mode %s", mode)
	}
	if _, err := NormalizeApprovalPolicy(string(p.Approval)); err != nil {
		return err
	}
	if mode == GitHubMaintenance && len(p.AllowedAuthors) == 0 {
		return fmt.Errorf("maintenance mode requires at least one allowed author")
	}
	return nil
}

// ValidateResumePolicy ensures a resumed session cannot acquire broader
// repository authority than the policy persisted at its original start.
// Callers may pass a zero candidate to retain the previous policy.
func ValidateResumePolicy(previous, candidate GitHubPolicy) error {
	prevMode, err := NormalizeGitHubMode(string(previous.Mode))
	if err != nil {
		return err
	}
	candMode, err := NormalizeGitHubMode(string(candidate.Mode))
	if err != nil {
		return err
	}
	if candidate.Mode == "" {
		candidate = previous
		candMode = prevMode
	}
	if modeRank(candMode) > modeRank(prevMode) {
		return fmt.Errorf("resume GitHub policy broadens mode from %s to %s", prevMode, candMode)
	}
	if candMode != GitHubNone {
		if strings.TrimSpace(candidate.Repository) != strings.TrimSpace(previous.Repository) || strings.TrimSpace(candidate.Base) != strings.TrimSpace(previous.Base) {
			return errors.New("resume GitHub policy cannot change repository or base")
		}
		allowed := make(map[string]struct{}, len(previous.AllowedAuthors))
		for _, author := range previous.AllowedAuthors {
			allowed[author] = struct{}{}
		}
		for _, author := range candidate.AllowedAuthors {
			if _, ok := allowed[author]; !ok {
				return fmt.Errorf("resume GitHub policy adds author %q", author)
			}
		}
	}
	return candidate.Validate()
}

func modeRank(mode GitHubMode) int {
	switch mode {
	case GitHubMaintenance:
		return 2
	case GitHubEngineering:
		return 1
	default:
		return 0
	}
}

// ValidateResumeCompletionPolicy applies the same monotonic rule to the
// unattended completion policy. Bounded replan can only be tightened to
// report-and-destroy on resume.
func ValidateResumeCompletionPolicy(previous, candidate CompletionPolicy) error {
	prev, err := NormalizeCompletionPolicy(string(previous))
	if err != nil {
		return err
	}
	if candidate == "" {
		candidate = prev
	}
	cand, err := NormalizeCompletionPolicy(string(candidate))
	if err != nil {
		return err
	}
	if completionRank(cand) > completionRank(prev) {
		return fmt.Errorf("resume completion policy broadens from %s to %s", prev, cand)
	}
	return nil
}

func completionRank(policy CompletionPolicy) int {
	if policy == CompletionBoundedReplan {
		return 1
	}
	return 0
}

func (p CompletionPolicy) Validate() error {
	_, err := NormalizeCompletionPolicy(string(p))
	return err
}
