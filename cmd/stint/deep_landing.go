package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

// gitRunner is the git seam for Deep Work workspace operations: worktree
// creation, checkpoint commits, and the state summaries folded into task
// context. All operations are local-only (no push, no fetch).
type gitRunner struct {
	run func(dir string, args ...string) (string, error)
}

func newGitRunner() *gitRunner {
	g := &gitRunner{}
	g.run = func(dir string, args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		if err != nil {
			msg := strings.TrimSpace(errb.String())
			if msg == "" {
				msg = err.Error()
			}
			return out.String(), fmt.Errorf("git %s: %s", args[0], msg)
		}
		return out.String(), nil
	}
	return g
}

func (g *gitRunner) repoHead(dir string) (string, error) {
	out, err := g.run(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// cleanTracked reports whether the repository has no tracked modifications
// (untracked files are tolerated and never enter the worktree).
func (g *gitRunner) cleanTracked(dir string) (bool, string) {
	out, err := g.run(dir, "status", "--porcelain")
	if err != nil {
		return false, err.Error()
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if len(line) < 4 || strings.HasPrefix(line, "??") {
			continue
		}
		return false, strings.TrimSpace(out)
	}
	return true, ""
}

func (g *gitRunner) headCommit(dir string) (string, error) { return g.repoHead(dir) }

func (g *gitRunner) headSubject(dir string) (string, error) {
	out, err := g.run(dir, "show", "-s", "--format=%s", "HEAD")
	return strings.TrimSpace(out), err
}

func (g *gitRunner) logOneline(dir string, n int) (string, error) {
	out, err := g.run(dir, "log", "--oneline", fmt.Sprintf("-%d", n))
	if err != nil {
		return "", nil // history is best effort in prompts
	}
	return strings.TrimSpace(out), nil
}

func (g *gitRunner) statusShort(dir string) (string, error) {
	out, err := g.run(dir, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (g *gitRunner) diffStat(dir, base string) (string, error) {
	out, err := g.run(dir, "diff", "--stat", base)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// worktreeAdd creates the Deep Work workspace: a branch cut from the
// repository's current HEAD, checked out in an isolated directory. The
// developer's active checkout is never touched.
func (g *gitRunner) worktreeAdd(repo, worktree, branch string) error {
	_, err := g.run(repo, "worktree", "add", worktree, "-b", branch, "HEAD")
	return err
}

// branchExists reports whether the repository has the local branch (the
// branch holds every Deep Work checkpoint commit, even if the worktree
// directory is lost).
func (g *gitRunner) branchExists(repo, branch string) bool {
	_, err := g.run(repo, "rev-parse", "--verify", "refs/heads/"+branch)
	return err == nil
}

// worktreeUsable reports whether the directory is a functioning worktree of
// its repository (a moved repository leaves a directory whose links are
// stale; `git worktree repair` fixes that).
func (g *gitRunner) worktreeUsable(worktree string) bool {
	_, err := g.run(worktree, "rev-parse", "--git-dir")
	return err == nil
}

// worktreeReattach recreates a lost worktree directory over the existing
// branch: prune drops the stale metadata entry (its directory is missing),
// then a plain add re-checks the branch out with all its commits.
func (g *gitRunner) worktreeReattach(repo, worktree, branch string) error {
	if _, err := g.run(repo, "worktree", "prune"); err != nil {
		return err
	}
	_, err := g.run(repo, "worktree", "add", worktree, branch)
	return err
}

// commitAll checkpoints the worktree with an explicit coordinator marker.
// --allow-empty keeps the accepted revision unambiguous when a worker already
// committed its changes or verification legitimately accepts a no-op task.
func (g *gitRunner) commitAll(dir, message string) (string, error) {
	if _, err := g.run(dir, "add", "-A"); err != nil {
		return "", err
	}
	out, err := g.run(dir, "commit", "--allow-empty", "-m", message,
		"--author", "Stint Deep Work <deep@stint.local>")
	if err != nil {
		return strings.TrimSpace(out), err
	}
	return strings.TrimSpace(out), nil
}

// runVerifyCmd runs the mission's verification command in the worktree with
// a bounded timeout and returns its combined output tail and pass/fail.
func runVerifyCmd(ctx context.Context, command, workdir string) (string, bool, error) {
	vctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(vctx, "sh", "-c", command)
	cmd.Dir = workdir
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	out := buf.String()
	if len(out) > 4000 {
		out = out[len(out)-4000:]
	}
	return out, err == nil, nil
}

// land is a resumable transaction. It persists PhaseLanding before any
// handoff side effects, then records verification, the handoff, and its exact
// commit SHA before marking the session landed. Landing never touches compute:
// the existing watchdog owns the hard deadline.
func (c *deepCoordinator) land(ctx context.Context, reason string) error {
	if c.state.Phase == deep.PhaseLanded || c.state.Phase == deep.PhaseStopped {
		return nil
	}
	fresh, err := deep.LoadState(c.stateDir, c.state.SessionID)
	if err != nil {
		return fmt.Errorf("read durable state before landing: %w", err)
	}
	if fresh.Phase == deep.PhaseLanded || fresh.Phase == deep.PhaseStopped {
		*c.state = fresh
		return nil
	}
	*c.state = fresh
	if c.state.Phase != deep.PhaseExecuting && c.state.Phase != deep.PhaseLanding {
		return fmt.Errorf("cannot land Deep Work session from phase %q", c.state.Phase)
	}
	if c.state.LandingReason == "" {
		c.state.LandingReason = reason
	}
	if c.state.Phase == deep.PhaseExecuting {
		c.state.Phase = deep.PhaseLanding
		if err := c.save(); err != nil {
			return fmt.Errorf("persist landing transition: %w", err)
		}
	}
	reason = c.state.LandingReason
	now := c.now()
	c.logf("landing: %s", reason)

	changed := false
	for i := range c.state.Tasks {
		if c.state.Tasks[i].Status == deep.StatusActive {
			c.state.Tasks[i].Status = deep.StatusIncomplete
			c.state.Tasks[i].Blocker = "stopped mid-task (" + reason + ")"
			changed = true
		}
	}
	if changed {
		if err := c.save(); err != nil {
			return fmt.Errorf("persist active tasks before landing: %w", err)
		}
	}

	if !c.state.LandingVerifyDone {
		finalVerify := ""
		if c.state.Verify != "" {
			run := c.finalVerify
			if run == nil {
				run = func(ctx context.Context, command string) (string, bool, error) {
					return runVerifyCmd(ctx, command, c.state.WorktreePath)
				}
			}
			out, ok, verifyErr := run(ctx, c.state.Verify)
			label := "FAILED"
			if ok && verifyErr == nil {
				label = "passed"
			}
			finalVerify = label
			if strings.TrimSpace(out) != "" {
				finalVerify += "\n" + strings.TrimSpace(out)
			}
			if verifyErr != nil {
				finalVerify += "\nerror: " + verifyErr.Error()
			}
		}
		c.state.LandingVerify = finalVerify
		c.state.LandingVerifyDone = true
		if err := c.save(); err != nil {
			return fmt.Errorf("persist final verification result: %w", err)
		}
	}

	handoffPath := filepath.Join(deep.DeepDir(c.stateDir, c.state.SessionID), "handoff.md")
	if c.state.HandoffPath != handoffPath {
		c.state.HandoffPath = handoffPath
		if err := c.save(); err != nil {
			return fmt.Errorf("persist handoff path: %w", err)
		}
	}
	if c.state.LandingHandoff == "" {
		c.state.LandingHandoff = buildHandoff(*c.state, reason, now, c.state.LandingVerify, c.repoSummary())
		if err := c.save(); err != nil {
			return fmt.Errorf("persist landing handoff contents: %w", err)
		}
	}
	handoff := []byte(c.state.LandingHandoff)
	if err := writeAtomicFile(handoffPath, handoff); err != nil {
		return fmt.Errorf("write durable handoff: %w", err)
	}
	worktreeWrite := c.worktreeWrite
	if worktreeWrite == nil {
		worktreeWrite = func(path string, data []byte) error {
			return os.WriteFile(path, data, 0o644)
		}
	}
	if err := worktreeWrite(filepath.Join(c.state.WorktreePath, "DEEP_WORK_HANDOFF.md"), handoff); err != nil {
		return fmt.Errorf("write worktree handoff: %w", err)
	}

	commitSubject := fmt.Sprintf("deep: %s handoff", c.state.SessionID)
	headSubject, err := c.git.headSubject(c.state.WorktreePath)
	if err != nil {
		return fmt.Errorf("read current worktree commit subject: %w", err)
	}
	status, err := c.git.statusShort(c.state.WorktreePath)
	if err != nil {
		return fmt.Errorf("read worktree changes before handoff checkpoint: %w", err)
	}
	if strings.TrimSpace(headSubject) != commitSubject || strings.TrimSpace(status) != "" {
		if _, err := c.git.commitAll(c.state.WorktreePath, commitSubject); err != nil {
			return fmt.Errorf("checkpoint handoff: %w", err)
		}
	}
	head, err := c.git.headCommit(c.state.WorktreePath)
	if err != nil {
		return fmt.Errorf("read landing checkpoint SHA: %w", err)
	}
	head = strings.TrimSpace(head)
	if head == "" {
		return fmt.Errorf("landing checkpoint SHA is empty")
	}

	c.state.LandingCommit = head
	c.state.Phase = deep.PhaseLanded
	c.state.LandedAt = &now
	if err := c.save(); err != nil {
		c.state.Phase = deep.PhaseLanding
		c.state.LandedAt = nil
		return fmt.Errorf("persist landed state and checkpoint SHA: %w", err)
	}
	c.incident(deep.IncidentLanded, "", fmt.Sprintf("%s (checkpoint %s)", reason, head))
	c.logf("landed: %s", handoffPath)

	fmt.Fprintf(c.out, "\nDeep Work landed (%s).\n", reason)
	fmt.Fprintf(c.out, "  handoff:  %s\n", handoffPath)
	fmt.Fprintf(c.out, "  worktree: %s (branch %s)\n", c.state.WorktreePath, c.state.Branch)
	fmt.Fprintf(c.out, "  landing checkpoint: %s\n", head)
	fmt.Fprintf(c.out, "  review locally: branch %s — merge or discard when ready.\n", c.state.Branch)
	return nil
}
