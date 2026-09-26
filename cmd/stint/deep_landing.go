package main

import (
	"bytes"
	"context"
	"errors"
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

// commitAll records actual staged/worktree changes. Callers that need a
// checkpoint without a repository change must reuse the current HEAD instead
// of manufacturing an empty marker commit.
func (g *gitRunner) commitAll(dir, message string) (string, error) {
	if _, err := g.run(dir, "add", "-A"); err != nil {
		return "", err
	}
	out, err := g.run(dir, "commit", "-m", message,
		"--author", "Stint Deep Work <deep@stint.local>")
	if err != nil {
		return strings.TrimSpace(out), err
	}
	return strings.TrimSpace(out), nil
}

// runVerifyCmd runs the mission's raw shell command in the worktree with a
// bounded timeout and preserves the concrete verifier outcome.
func runVerifyCmd(ctx context.Context, command, workdir string) verificationResult {
	started := time.Now().UTC()
	if err := deep.ValidateVerifyCommand(command); err != nil {
		return verificationResult{Command: command, Outcome: verificationInvalid, StartedAt: started, CompletedAt: time.Now().UTC(), Error: err.Error()}
	}
	vctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(vctx, "sh", "-c", command)
	cmd.Dir = workdir
	output, err := newPrivateOutputFile("stint-verifier-output-*")
	if err != nil {
		return verificationResult{Command: command, Outcome: verificationExecutionErr, StartedAt: started, CompletedAt: time.Now().UTC(), Error: "create verifier output file: " + err.Error()}
	}
	cmd.Stdout, cmd.Stderr = output, output
	runErr, quiesceErr := runQuiescedProcessGroup(cmd)
	outBytes, outputErr := readAndRemoveOutputFile(output)
	if outputErr != nil {
		return verificationResult{Command: command, Outcome: verificationExecutionErr, StartedAt: started, CompletedAt: time.Now().UTC(), Error: "read verifier output: " + outputErr.Error()}
	}
	out := string(outBytes)
	if len(out) > 4000 {
		out = out[len(out)-4000:]
	}
	result := verificationResult{Command: command, StartedAt: started, CompletedAt: time.Now().UTC(), Output: out}
	if quiesceErr != nil {
		result.Outcome = verificationExecutionErr
		result.Error = "could not quiesce verifier process group: " + quiesceErr.Error()
		result.QuiescenceUnconfirmed = true
		return result
	}
	if runErr == nil {
		result.Outcome = verificationPassed
		result.ExitCode = 0
		result.HasExitCode = true
		return result
	}
	if errors.Is(vctx.Err(), context.DeadlineExceeded) {
		result.Outcome = verificationTimedOut
		result.Error = vctx.Err().Error()
		return result
	}
	if errors.Is(vctx.Err(), context.Canceled) {
		result.Outcome = verificationCanceled
		result.Error = vctx.Err().Error()
		return result
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		result.Outcome = verificationFailed
		result.ExitCode = exitErr.ExitCode()
		result.HasExitCode = true
		return result
	}
	result.Outcome = verificationExecutionErr
	result.Error = runErr.Error()
	return result
}

func landingVerificationBookkeeping(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	copy := make(map[string]string, len(metadata))
	for path, identity := range metadata {
		if path != deepWorktreeHandoff {
			copy[path] = identity
		}
	}
	if len(copy) == 0 {
		return nil
	}
	return copy
}

func landingEvidenceMatches(snapshot verificationSnapshot, subject *deep.VerificationSubject, bookkeeping map[string]string) bool {
	return subject != nil && *subject == snapshot.Subject && sameMetadata(bookkeeping, landingVerificationBookkeeping(snapshot.Bookkeeping))
}

func summarizeLandingVerification(result verificationResult) string {
	if result.Passed() {
		return "passed"
	}
	finalVerify := "FAILED (" + string(result.Outcome) + ")"
	if result.HasExitCode {
		finalVerify += fmt.Sprintf(" (exit %d)", result.ExitCode)
	}
	if strings.TrimSpace(result.Output) != "" {
		finalVerify += "\n" + strings.TrimSpace(result.Output)
	}
	if result.Error != "" {
		finalVerify += "\nerror: " + result.Error
	}
	return finalVerify
}

// land is a resumable transaction. It persists PhaseLanding before any
// handoff side effects, binds final verification to the product tree it
// exercised, and records the matching checkpoint tree before marking the
// session landed. Landing never touches compute: the existing watchdog owns
// the hard deadline.
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
	if c.state.ExecutionQuiescenceUnconfirmed {
		return fmt.Errorf("cannot verify or checkpoint while executor writers may still be active (task %s); quiescence must be confirmed first", c.state.ExecutionQuiescenceTaskID)
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
	activeTaskID := ""
	for i := range c.state.Tasks {
		if c.state.Tasks[i].Status == deep.StatusActive {
			if activeTaskID == "" {
				activeTaskID = c.state.Tasks[i].ID
			}
			c.state.Tasks[i].Status = deep.StatusIncomplete
			c.state.Tasks[i].Blocker = "stopped mid-task (" + reason + ")"
			changed = true
		}
	}
	if changed {
		c.state.ExecutionQuiescenceUnconfirmed = true
		c.state.ExecutionQuiescenceTaskID = activeTaskID
		if err := c.save(); err != nil {
			return fmt.Errorf("persist active tasks before landing: %w", err)
		}
		return fmt.Errorf("landing deferred until active task %s executor has returned and quiesced", activeTaskID)
	}

	bookkeepingPaths := c.verificationBookkeepingPaths()
	verificationSnapshot, err := c.git.verificationSubject(c.state.WorktreePath, bookkeepingPaths)
	if err != nil {
		return fmt.Errorf("capture repository state before final verification: %w", err)
	}
	verifyRequired := strings.TrimSpace(c.state.Verify) != ""
	if verifyRequired {
		evidenceReusable := landingEvidenceMatches(verificationSnapshot, c.state.LandingVerificationSubject, c.state.LandingVerificationBookkeeping) &&
			c.state.LandingVerificationOutcome.Recorded()
		if !evidenceReusable {
			// A legacy done bit has no subject provenance. Clear it durably before
			// rerunning so a crash cannot make the old result look reusable. A
			// subject-bearing A2 result without a typed A3 outcome is also rerun:
			// its prose summary is not canonical verifier state.
			if c.state.LandingVerifyDone || c.state.LandingVerificationOutcome != deep.VerificationNotRun || c.state.LandingVerificationSubject != nil || c.state.LandingVerificationBookkeeping != nil {
				c.state.LandingVerify = ""
				c.state.LandingVerifyDone = false
				c.state.LandingVerificationOutcome = deep.VerificationNotRun
				c.state.LandingVerificationSubject = nil
				c.state.LandingVerificationBookkeeping = nil
				if err := c.save(); err != nil {
					return fmt.Errorf("invalidate stale final verification evidence: %w", err)
				}
			}
			run := c.finalVerify
			if run == nil {
				run = func(ctx context.Context, command string) verificationResult {
					return runVerifyCmd(ctx, command, c.state.WorktreePath)
				}
			}
			result := run(ctx, c.state.Verify)
			if result.QuiescenceUnconfirmed {
				c.state.ExecutionQuiescenceUnconfirmed = true
				c.state.ExecutionQuiescenceTaskID = "mission-final-verifier"
				if err := c.save(); err != nil {
					return fmt.Errorf("final verifier quiescence is unconfirmed and the block could not be persisted: %w", err)
				}
				return fmt.Errorf("landing stopped because final verifier process quiescence is unconfirmed")
			}
			afterVerify, err := c.git.verificationSubject(c.state.WorktreePath, bookkeepingPaths)
			if err != nil {
				return fmt.Errorf("capture repository state after final verification: %w", err)
			}
			if afterVerify.Subject != verificationSnapshot.Subject || !sameMetadata(
				landingVerificationBookkeeping(verificationSnapshot.Bookkeeping),
				landingVerificationBookkeeping(afterVerify.Bookkeeping),
			) {
				return fmt.Errorf("repository changed during final verification; verified subject %s/%s no longer matches %s/%s",
					verificationSnapshot.Subject.HeadCommit, verificationSnapshot.Subject.TreeSHA,
					afterVerify.Subject.HeadCommit, afterVerify.Subject.TreeSHA)
			}
			c.state.LandingVerify = summarizeLandingVerification(result)
			c.state.LandingVerifyDone = true
			c.state.LandingVerificationOutcome = result.Outcome
			verifiedSubject := verificationSnapshot.Subject
			c.state.LandingVerificationSubject = &verifiedSubject
			c.state.LandingVerificationBookkeeping = landingVerificationBookkeeping(verificationSnapshot.Bookkeeping)
			if err := c.save(); err != nil {
				return fmt.Errorf("persist final verification result and subject: %w", err)
			}
		}
	} else if !c.state.LandingVerifyDone || c.state.LandingVerify != "" || c.state.LandingVerificationOutcome != deep.VerificationNotRun || c.state.LandingVerificationSubject != nil || c.state.LandingVerificationBookkeeping != nil {
		c.state.LandingVerify = ""
		c.state.LandingVerifyDone = true
		c.state.LandingVerificationOutcome = deep.VerificationNotRun
		c.state.LandingVerificationSubject = nil
		c.state.LandingVerificationBookkeeping = nil
		if err := c.save(); err != nil {
			return fmt.Errorf("persist absence of final verifier: %w", err)
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
	_, handoffIsBookkeeping := verificationSnapshot.Bookkeeping[deepWorktreeHandoff]
	// Mirror the generated summary into the worktree only while the path is
	// Stint-owned untracked bookkeeping. If it is already Git-visible, it is a
	// product-tree input and landing must leave it untouched after verification.
	if handoffIsBookkeeping {
		if err := worktreeWrite(filepath.Join(c.state.WorktreePath, deepWorktreeHandoff), handoff); err != nil {
			return fmt.Errorf("write worktree handoff: %w", err)
		}
	}

	commitSubject := fmt.Sprintf("deep: %s handoff", c.state.SessionID)
	checkpointSnapshot, err := c.git.verificationSubject(c.state.WorktreePath, bookkeepingPaths)
	if err != nil {
		return fmt.Errorf("capture repository state before landing checkpoint: %w", err)
	}
	if verifyRequired && !landingEvidenceMatches(checkpointSnapshot, c.state.LandingVerificationSubject, c.state.LandingVerificationBookkeeping) {
		return fmt.Errorf("repository changed after final verification; verified subject %s/%s no longer matches checkpoint subject %s/%s",
			c.state.LandingVerificationSubject.HeadCommit, c.state.LandingVerificationSubject.TreeSHA,
			checkpointSnapshot.Subject.HeadCommit, checkpointSnapshot.Subject.TreeSHA)
	}
	head, checkpointTree, err := c.git.checkpointSubject(c.state.WorktreePath, commitSubject, checkpointSnapshot)
	if err != nil {
		return fmt.Errorf("checkpoint landing repository state: %w", err)
	}
	if verifyRequired && checkpointTree != c.state.LandingVerificationSubject.TreeSHA {
		return fmt.Errorf("landing checkpoint tree %s does not match final verification tree %s", checkpointTree, c.state.LandingVerificationSubject.TreeSHA)
	}
	head = strings.TrimSpace(head)
	if head == "" {
		return fmt.Errorf("landing checkpoint SHA is empty")
	}

	priorLandingCommit := c.state.LandingCommit
	priorLandingTree := c.state.LandingCheckpointTreeSHA
	priorPhase := c.state.Phase
	priorLandedAt := c.state.LandedAt
	priorOutcome := c.state.MissionOutcome
	priorHandoff := c.state.LandingHandoff
	restoreLandingState := func() {
		c.state.LandingCommit = priorLandingCommit
		c.state.LandingCheckpointTreeSHA = priorLandingTree
		c.state.Phase = priorPhase
		c.state.LandedAt = priorLandedAt
		c.state.MissionOutcome = priorOutcome
		c.state.LandingHandoff = priorHandoff
	}
	c.state.LandingCommit = head
	c.state.LandingCheckpointTreeSHA = checkpointTree
	c.state.Phase = deep.PhaseLanded
	c.state.LandedAt = &now
	c.state.MissionOutcome = deep.DetermineMissionOutcome(*c.state)
	c.state.LandingHandoff = updateHandoffLandingResult(c.state.LandingHandoff, c.state.MissionOutcome, reason)
	if c.state.LandingHandoff != priorHandoff {
		handoff = []byte(c.state.LandingHandoff)
		if err := writeAtomicFile(handoffPath, handoff); err != nil {
			restoreLandingState()
			return fmt.Errorf("update durable handoff with mission outcome: %w", err)
		}
		if handoffIsBookkeeping {
			if err := worktreeWrite(filepath.Join(c.state.WorktreePath, deepWorktreeHandoff), handoff); err != nil {
				restoreLandingState()
				return fmt.Errorf("update worktree handoff with mission outcome: %w", err)
			}
		}
	}
	if err := c.save(); err != nil {
		restoreLandingState()
		return fmt.Errorf("persist landed state and checkpoint SHA: %w", err)
	}
	c.incident(deep.IncidentLanded, "", fmt.Sprintf("%s (checkpoint %s)", reason, head))
	c.logf("landed: %s", handoffPath)

	fmt.Fprintf(c.out, "\nDeep Work reached the landing boundary (%s).\n", reason)
	fmt.Fprintf(c.out, "  mission outcome: %s\n", deep.DisplayMissionOutcome(c.state.MissionOutcome, c.state.Phase))
	fmt.Fprintf(c.out, "  handoff:  %s\n", handoffPath)
	fmt.Fprintf(c.out, "  worktree: %s (branch %s)\n", c.state.WorktreePath, c.state.Branch)
	fmt.Fprintf(c.out, "  landing checkpoint: %s\n", head)
	fmt.Fprintf(c.out, "  review locally: branch %s — merge or discard when ready.\n", c.state.Branch)
	return nil
}
