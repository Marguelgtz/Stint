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
		return "", nil // best effort in prompts
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

// commitAll checkpoints the worktree. "Nothing to commit" is not an error
// for Deep Work: evidence may already be committed by the worker.
func (g *gitRunner) commitAll(dir, message string) (string, error) {
	if _, err := g.run(dir, "add", "-A"); err != nil {
		return "", err
	}
	out, err := g.run(dir, "commit", "-m", message,
		"--author", "Stint Deep Work <deep@stint.local>")
	if err != nil && strings.Contains(err.Error(), "nothing to commit") {
		return "nothing to commit", nil
	}
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

// land finishes the session: park anything active, run final verification,
// write the truthful handoff (state dir + worktree), checkpoint the
// worktree, and persist the landed state. Landing never touches compute:
// the existing watchdog owns the hard deadline.
func (c *deepCoordinator) land(ctx context.Context, reason string) error {
	now := c.now()
	if c.state.Phase == deep.PhaseLanded || c.state.Phase == deep.PhaseStopped {
		return nil
	}
	c.state.Phase = deep.PhaseLanded
	c.save()
	c.logf("landing: %s", reason)

	for i := range c.state.Tasks {
		if c.state.Tasks[i].Status == deep.StatusActive {
			c.state.Tasks[i].Status = deep.StatusIncomplete
			c.state.Tasks[i].Blocker = "stopped mid-task (" + reason + ")"
		}
	}
	c.save()

	finalVerify := ""
	if c.state.Verify != "" {
		out, ok, _ := runVerifyCmd(ctx, c.state.Verify, c.state.WorktreePath)
		// A silent command (test/grep -q) can pass without printing anything,
		// so pass/fail is reported separately from the captured output.
		if ok {
			finalVerify = "passed"
		} else {
			finalVerify = "FAILED"
		}
		if strings.TrimSpace(out) != "" {
			finalVerify = out
			if ok {
				finalVerify = "passed\n" + out
			} else {
				finalVerify = "FAILED\n" + out
			}
		}
	}

	handoff := buildHandoff(*c.state, reason, now, finalVerify, c.repoSummary())
	handoffPath := filepath.Join(deep.DeepDir(c.stateDir, c.state.SessionID), "handoff.md")
	if err := writeAtomicFile(handoffPath, []byte(handoff)); err != nil {
		c.logf("WARNING: write handoff: %v", err)
	}
	_ = os.WriteFile(filepath.Join(c.state.WorktreePath, "DEEP_WORK_HANDOFF.md"), []byte(handoff), 0o644)

	if _, err := c.git.commitAll(c.state.WorktreePath, fmt.Sprintf("deep: %s handoff", c.state.SessionID)); err != nil {
		c.logf("handoff commit: %v", err)
	}

	c.state.Phase = deep.PhaseLanded
	c.state.LandedAt = &now
	c.state.HandoffPath = handoffPath
	c.save()
	c.logf("landed: %s", handoffPath)

	fmt.Fprintf(c.out, "\nDeep Work landed (%s).\n", reason)
	fmt.Fprintf(c.out, "  handoff:  %s\n", handoffPath)
	fmt.Fprintf(c.out, "  worktree: %s (branch %s)\n", c.state.WorktreePath, c.state.Branch)
	fmt.Fprintf(c.out, "  review locally: branch %s — merge or discard when ready.\n", c.state.Branch)
	return nil
}
