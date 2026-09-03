package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

// gitOps is the git surface the Deep Work coordinator drives. Both the local
// *gitRunner (Cline worker, worktree on the operator's machine) and the remote
// *remoteGit (Hermes worker, worktree on the compute box) implement it, so the
// coordinator's acceptance, checkpoint, summary, and recovery logic is
// worker-agnostic: the same code path runs whether the worktree is local or on
// the box.
type gitOps interface {
	repoHead(dir string) (string, error)
	cleanTracked(dir string) (bool, string)
	headCommit(dir string) (string, error)
	logOneline(dir string, n int) (string, error)
	statusShort(dir string) (string, error)
	diffStat(dir, base string) (string, error)
	worktreeAdd(repo, worktree, branch string) error
	branchExists(repo, branch string) bool
	worktreeUsable(worktree string) bool
	worktreeReattach(repo, worktree, branch string) error
	commitAll(dir, message string) (string, error)
}

// remoteCmd runs one shell line on the compute box over the Stint SSH channel
// and returns its combined output. It is the single seam the remote git,
// remote verify, and remote (Hermes) executor all share, so tests can
// substitute a fake without touching a live box.
type remoteCmd func(ctx context.Context, remoteCommand string) (string, error)

// newRemoteCmd binds runSSH to the session's box (Stint key, host, port,
// per-state known_hosts). The box is the compute session's GPU instance.
func newRemoteCmd(paths config.Paths, ssn sessionstate.State) remoteCmd {
	return func(ctx context.Context, remoteCommand string) (string, error) {
		return runSSH(ctx, paths, ssn, remoteCommand)
	}
}

// shellQuote renders s for use inside a POSIX sh command (single-quoted, with
// embedded single quotes escaped). Remote commands are assembled as sh lines,
// so every dynamic value is quoted this way.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// remoteGit implements gitOps by running git on the box over SSH. Every
// operation is local-to-the-box only (no push, no fetch), mirroring the local
// gitRunner's safety model: the coordinator checkpoints the on-box worktree on
// the session branch and never pushes.
type remoteGit struct {
	remote remoteCmd
}

// run executes one `git -C <dir> <args...>` command on the box.
func (g *remoteGit) run(dir string, args ...string) (string, error) {
	parts := append([]string{"git", "-C", dir}, args...)
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellQuote(p)
	}
	out, err := g.remote(context.Background(), strings.Join(quoted, " "))
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("git %s: %s", args[0], msg)
	}
	return out, nil
}

func (g *remoteGit) repoHead(dir string) (string, error) {
	out, err := g.run(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (g *remoteGit) cleanTracked(dir string) (bool, string) {
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

func (g *remoteGit) headCommit(dir string) (string, error) { return g.repoHead(dir) }

func (g *remoteGit) logOneline(dir string, n int) (string, error) {
	out, err := g.run(dir, "log", "--oneline", fmt.Sprintf("-%d", n))
	if err != nil {
		return "", nil // history is best effort in prompts
	}
	return strings.TrimSpace(out), nil
}

func (g *remoteGit) statusShort(dir string) (string, error) {
	out, err := g.run(dir, "status", "--porcelain")
	if err != nil {
		return "", nil // best effort in prompts
	}
	return strings.TrimSpace(out), nil
}

func (g *remoteGit) diffStat(dir, base string) (string, error) {
	out, err := g.run(dir, "diff", "--stat", base)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (g *remoteGit) worktreeAdd(repo, worktree, branch string) error {
	_, err := g.run(repo, "worktree", "add", worktree, "-b", branch, "HEAD")
	return err
}

func (g *remoteGit) branchExists(repo, branch string) bool {
	_, err := g.run(repo, "rev-parse", "--verify", "refs/heads/"+branch)
	return err == nil
}

func (g *remoteGit) worktreeUsable(worktree string) bool {
	_, err := g.run(worktree, "rev-parse", "--git-dir")
	return err == nil
}

func (g *remoteGit) worktreeReattach(repo, worktree, branch string) error {
	if _, err := g.run(repo, "worktree", "prune"); err != nil {
		return err
	}
	_, err := g.run(repo, "worktree", "add", worktree, branch)
	return err
}

// commitAll checkpoints the on-box worktree. "Nothing to commit" is not an
// error for Deep Work: evidence may already be committed by the worker.
func (g *remoteGit) commitAll(dir, message string) (string, error) {
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

// runVerifyCmdRemote runs the verification command on the box in the on-box
// worktree with the same 3-minute bound as the local variant and returns its
// combined output tail and pass/fail (command exit 0).
func runVerifyCmdRemote(ctx context.Context, remote remoteCmd, command, workdir string) (string, bool, error) {
	vctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	line := fmt.Sprintf("cd %s && sh -c %s", shellQuote(workdir), shellQuote(command))
	out, err := remote(vctx, line)
	if len(out) > 4000 {
		out = out[len(out)-4000:]
	}
	return out, err == nil, nil
}

// hermesPromptFile is the on-box staging path for the reconstructed prompt.
// It lives outside the worktree so the coordinator's `git add -A` checkpoint
// never captures it, and a fixed path is safe because a session runs one
// invocation at a time on one box.
const hermesPromptFile = "/tmp/stint-deep-prompt.md"

// hermesExitMarker delimits the Hermes invocation's exit code in the remote
// output: the box shell appends "<marker><code>" after the run so the exit
// code survives the SSH channel (runSSH reports a non-zero remote exit as an
// error, which would otherwise mask the real code).
const hermesExitMarker = "__STINT_EXIT__="

// writeRemoteFile writes data to a path on the box over the SSH seam,
// base64-encoded so arbitrary content (the handoff's UTF-8 markdown)
// survives the shell round-trip unchanged — the same mechanism the
// executor uses to stage prompts.
func writeRemoteFile(remote remoteCmd, path string, data []byte) error {
	b64 := base64.StdEncoding.EncodeToString(data)
	line := fmt.Sprintf("printf %%s %s | base64 -d > %s", shellQuote(b64), shellQuote(path))
	_, err := remote(context.Background(), line)
	return err
}

// hermesExecutor runs one bounded agent invocation ON THE COMPUTE BOX: it
// stages the reconstructed prompt, runs headless Hermes (pointed at the box's
// local model endpoint), and reports the invocation's exit code and output
// tail. The coordinator still decides acceptance from repository evidence
// (the remote verify command) — the exit code only feeds the blocker reason,
// mirroring the Cline executor's contract.
type hermesExecutor struct {
	remote remoteCmd
}

func newHermesExecutor(remote remoteCmd) *hermesExecutor {
	return &hermesExecutor{remote: remote}
}

func (e *hermesExecutor) run(ctx context.Context, in execInput) (execResult, error) {
	if in.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, in.timeout)
		defer cancel()
	}
	start := time.Now()

	b64 := base64.StdEncoding.EncodeToString([]byte(in.prompt))
	secs := int(in.timeout.Seconds())
	hermesArgs := "hermes chat --query-file " + shellQuote(hermesPromptFile) +
		" --oneshot -Q"
	if in.model != "" {
		hermesArgs = "hermes chat --query-file " + shellQuote(hermesPromptFile) +
			" --oneshot -Q --provider custom -m " + shellQuote(in.model)
	}
	line := fmt.Sprintf(
		"printf %%s %s | base64 -d > %s; cd %s; timeout %d %s 2>&1; ec=$?; echo %s$ec",
		shellQuote(b64), shellQuote(hermesPromptFile), shellQuote(in.workdir),
		secs, hermesArgs, hermesExitMarker)

	out, err := e.remote(ctx, line)
	res := execResult{duration: time.Since(start), stderrTail: tailLine(out, 5)}

	// Recover the invocation's exit code from the marker. A missing marker
	// (SSH failure, or the box line not reaching the marker) means the
	// invocation did not complete.
	if idx := strings.LastIndex(out, hermesExitMarker); idx >= 0 {
		rest := strings.TrimSpace(out[idx+len(hermesExitMarker):])
		if code, perr := strconv.Atoi(rest); perr == nil {
			res.exitCode = code
			res.completed = code == 0
			if res.completed {
				res.finishReason = "completed"
			}
		}
	}
	// The marker line is bookkeeping, not agent output: drop it from the
	// report so the handoff carries only the worker's text.
	if i := strings.LastIndex(out, hermesExitMarker); i >= 0 {
		res.outputText = strings.TrimSpace(out[:i])
	}
	if err != nil && !strings.Contains(out, hermesExitMarker) {
		res.exitCode = -1
		return res, fmt.Errorf("hermes invocation over SSH: %w", err)
	}
	return res, nil
}
