package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/deep"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

// gitOps is the git surface the Deep Work coordinator drives. *gitRunner
// handles a co-located on-box run, while *remoteGit runs the same operations
// over SSH for the operator-side coordinator.
type gitOps interface {
	repoHead(dir string) (string, error)
	cleanTracked(dir string) (bool, string)
	headCommit(dir string) (string, error)
	headSubject(dir string) (string, error)
	logOneline(dir string, n int) (string, error)
	statusShort(dir string) (string, error)
	diffStat(dir, base string) (string, error)
	verificationSubject(dir string, bookkeepingPaths []string) (verificationSnapshot, error)
	checkpointSubject(dir, message string, snapshot verificationSnapshot) (string, string, error)
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

func (g *remoteGit) headSubject(dir string) (string, error) {
	out, err := g.run(dir, "show", "-s", "--format=%s", "HEAD")
	return strings.TrimSpace(out), err
}

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
		return "", err
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

// verificationSubject captures the worktree's Git-visible product tree using
// a temporary index on the box. Stint-owned, untracked bookkeeping files are
// hashed separately and excluded from that tree; tracked or staged versions
// remain ordinary product inputs.
func (g *remoteGit) verificationSubject(dir string, bookkeepingPaths []string) (verificationSnapshot, error) {
	paths, err := cleanBookkeepingPaths(bookkeepingPaths)
	if err != nil {
		return verificationSnapshot{}, err
	}
	var script strings.Builder
	script.WriteString("set -e\n")
	script.WriteString("d=" + shellQuote(dir) + "\n")
	script.WriteString("head=$(git -C \"$d\" rev-parse HEAD)\n")
	script.WriteString("sub_status=$(git -C \"$d\" submodule status --recursive)\n")
	script.WriteString("while IFS= read -r line; do case \"$line\" in -*) printf '%s\\n' 'verification subject contains an uninitialized submodule' >&2; exit 1;; esac; done <<EOF\n$sub_status\nEOF\n")
	script.WriteString("git -C \"$d\" submodule foreach --quiet --recursive 'test -z \"$(git status --porcelain --untracked-files=all)\"' >/dev/null\n")
	script.WriteString("set --\n")
	for i, path := range paths {
		pathspec := ":(literal)" + path
		target := filepath.ToSlash(filepath.Join(dir, filepath.FromSlash(path)))
		script.WriteString("idx=$(git -C \"$d\" ls-files -- " + shellQuote(pathspec) + ")\n")
		script.WriteString("committed=$(git -C \"$d\" ls-tree -r --name-only HEAD -- " + shellQuote(pathspec) + ")\n")
		script.WriteString("if [ -z \"$idx\" ] && [ -z \"$committed\" ]; then\n")
		script.WriteString("  if [ -L " + shellQuote(target) + " ]; then printf '%s\\n' 'bookkeeping path is not a regular file' >&2; exit 1; else if [ -f " + shellQuote(target) + " ]; then oid=$(git -C \"$d\" hash-object --no-filters -- " + shellQuote(path) + "); else if [ -e " + shellQuote(target) + " ]; then printf '%s\\n' 'bookkeeping path is not a regular file' >&2; exit 1; else oid=absent; fi; fi; fi\n")
		script.WriteString("  printf '%s=%s\\n' " + shellQuote(fmt.Sprintf("__STINT_META_%d__", i)) + " \"$oid\"\n")
		script.WriteString("  set -- \"$@\" " + shellQuote(":(top,exclude,literal)"+path) + "\n")
		script.WriteString("fi\n")
	}
	script.WriteString("tmp=$(mktemp -d \"${TMPDIR:-/tmp}/stint-deep-index.XXXXXX\")\n")
	script.WriteString("trap 'rm -rf \"$tmp\"' EXIT HUP INT TERM\n")
	script.WriteString("export GIT_INDEX_FILE=\"$tmp/index\"\n")
	script.WriteString("git -C \"$d\" read-tree \"$head\"\n")
	script.WriteString("git -C \"$d\" add -A -- . \"$@\"\n")
	script.WriteString("tree=$(git -C \"$d\" write-tree)\n")
	script.WriteString("head_after=$(git -C \"$d\" rev-parse HEAD)\n")
	script.WriteString("[ \"$head\" = \"$head_after\" ] || { printf '%s\\n' 'repository HEAD changed while capturing verification subject' >&2; exit 1; }\n")
	script.WriteString("printf '__STINT_SUBJECT__=%s %s\\n' \"$head_after\" \"$tree\"\n")
	out, err := g.remote(context.Background(), script.String())
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return verificationSnapshot{}, fmt.Errorf("capture remote verification subject: %s", msg)
	}
	var snapshot verificationSnapshot
	metadata := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		for i, path := range paths {
			marker := fmt.Sprintf("__STINT_META_%d__=", i)
			if strings.HasPrefix(line, marker) {
				identity := strings.TrimPrefix(line, marker)
				if identity != "absent" {
					identity = "git-blob:" + identity
				}
				metadata[path] = identity
			}
		}
		if strings.HasPrefix(line, "__STINT_SUBJECT__=") {
			fields := strings.Fields(strings.TrimPrefix(line, "__STINT_SUBJECT__="))
			if len(fields) == 2 {
				snapshot.Subject.HeadCommit, snapshot.Subject.TreeSHA = fields[0], fields[1]
			}
		}
	}
	if snapshot.Subject.HeadCommit == "" || snapshot.Subject.TreeSHA == "" {
		return verificationSnapshot{}, fmt.Errorf("remote verification subject returned no valid tree identity")
	}
	if len(metadata) > 0 {
		snapshot.Bookkeeping = metadata
	}
	return snapshot, nil
}

func (g *remoteGit) checkpointSubject(dir, message string, snapshot verificationSnapshot) (string, string, error) {
	subject := snapshot.Subject
	if subject.HeadCommit == "" || subject.TreeSHA == "" {
		return "", "", fmt.Errorf("verification subject is incomplete")
	}
	excluded := bookkeepingPathsFromSnapshot(snapshot)
	current, err := g.verificationSubject(dir, excluded)
	if err != nil {
		return "", "", err
	}
	if !sameVerificationSnapshot(snapshot, current) {
		return "", "", fmt.Errorf("repository changed after verification; verified subject %s/%s no longer matches %s/%s", subject.HeadCommit, subject.TreeSHA, current.Subject.HeadCommit, current.Subject.TreeSHA)
	}
	args := []string{"add", "-A", "--", "."}
	for _, path := range excluded {
		args = append(args, ":(top,exclude,literal)"+path)
	}
	if _, err := g.run(dir, args...); err != nil {
		return "", "", err
	}
	tree, err := g.run(dir, "write-tree")
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(tree) != subject.TreeSHA {
		return "", "", fmt.Errorf("repository changed while preparing checkpoint tree; verified %s, staged %s", subject.TreeSHA, strings.TrimSpace(tree))
	}
	head, err := g.repoHead(dir)
	if err != nil {
		return "", "", err
	}
	head = strings.TrimSpace(head)
	if head != subject.HeadCommit {
		return "", "", fmt.Errorf("repository HEAD changed after verification; verified %s, found %s", subject.HeadCommit, head)
	}
	headTree, err := g.run(dir, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", "", err
	}
	checkpoint := head
	if strings.TrimSpace(headTree) != subject.TreeSHA {
		if _, err := g.run(dir, "commit", "-m", message, "--author", "Stint Deep Work <deep@stint.local>"); err != nil {
			return "", "", err
		}
		checkpoint, err = g.repoHead(dir)
		if err != nil {
			return "", "", err
		}
		checkpoint = strings.TrimSpace(checkpoint)
		parent, err := g.run(dir, "rev-parse", "HEAD^")
		if err != nil {
			return "", "", fmt.Errorf("read semantic checkpoint parent: %w", err)
		}
		if strings.TrimSpace(parent) != subject.HeadCommit {
			return "", "", fmt.Errorf("checkpoint parent changed after verification; verified HEAD %s, found parent %s", subject.HeadCommit, strings.TrimSpace(parent))
		}
	}
	checkpointTree, err := g.run(dir, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", "", err
	}
	checkpointTree = strings.TrimSpace(checkpointTree)
	if checkpointTree != subject.TreeSHA {
		return "", "", fmt.Errorf("checkpoint tree does not match verified tree: verified %s, checkpoint %s", subject.TreeSHA, checkpointTree)
	}
	after, err := g.verificationSubject(dir, excluded)
	if err != nil {
		return "", "", err
	}
	if after.Subject.HeadCommit != checkpoint || after.Subject.TreeSHA != subject.TreeSHA || !sameMetadata(snapshot.Bookkeeping, after.Bookkeeping) {
		return "", "", fmt.Errorf("repository changed before checkpoint acceptance; verified tree %s, current HEAD/tree %s/%s", subject.TreeSHA, after.Subject.HeadCommit, after.Subject.TreeSHA)
	}
	return checkpoint, checkpointTree, nil
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

// commitAll checkpoints the on-box worktree with a distinct coordinator
// marker, including when the worker already committed its changes.
func (g *remoteGit) commitAll(dir, message string) (string, error) {
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

const (
	verifyExitMarker  = "__STINT_VERIFY_EXIT__="
	verifySetupMarker = "__STINT_VERIFY_SETUP__="
)

// runVerifyCmdRemote executes the same validated raw shell command as the
// local verifier and transports its exit status separately from SSH status.
func runVerifyCmdRemote(ctx context.Context, remote remoteCmd, command, workdir string) verificationResult {
	started := time.Now().UTC()
	if err := deep.ValidateVerifyCommand(command); err != nil {
		return verificationResult{Command: command, Outcome: verificationInvalid, StartedAt: started, CompletedAt: time.Now().UTC(), Error: err.Error()}
	}
	vctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	line := fmt.Sprintf("cd %s || { status=$?; printf '\\n%s%%s\\n' \"$status\"; exit 0; }; if sh -c %s; then status=0; else status=$?; fi; printf '\\n%s%%s\\n' \"$status\"; exit 0",
		shellQuote(workdir), verifySetupMarker, shellQuote(command), verifyExitMarker)
	out, err := remote(vctx, line)
	result := verificationResult{Command: command, StartedAt: started, CompletedAt: time.Now().UTC()}
	if err != nil {
		result.Output = truncateVerifierOutput(out)
		if errors.Is(vctx.Err(), context.DeadlineExceeded) {
			result.Outcome = verificationTimedOut
			result.Error = vctx.Err().Error()
		} else if errors.Is(vctx.Err(), context.Canceled) {
			result.Outcome = verificationCanceled
			result.Error = vctx.Err().Error()
		} else {
			result.Outcome = verificationExecutionErr
			result.Error = "remote verification transport failed: " + err.Error()
		}
		return result
	}
	if exitCode, body, ok := takeTrailingVerifierMarker(out, verifyExitMarker); ok {
		result.Output = truncateVerifierOutput(body)
		result.ExitCode = exitCode
		result.HasExitCode = true
		if result.ExitCode == 0 {
			result.Outcome = verificationPassed
		} else {
			result.Outcome = verificationFailed
		}
		return result
	}
	if setupCode, body, ok := takeTrailingVerifierMarker(out, verifySetupMarker); ok {
		result.Outcome = verificationExecutionErr
		result.Error = fmt.Sprintf("could not enter verifier worktree (cd exit %d)", setupCode)
		result.Output = truncateVerifierOutput(body)
		return result
	}
	result.Output = truncateVerifierOutput(out)
	if errors.Is(vctx.Err(), context.DeadlineExceeded) {
		result.Outcome = verificationTimedOut
		result.Error = vctx.Err().Error()
	} else if errors.Is(vctx.Err(), context.Canceled) {
		result.Outcome = verificationCanceled
		result.Error = vctx.Err().Error()
	} else {
		result.Outcome = verificationExecutionErr
		result.Error = "remote verification returned no trailing exit marker"
	}
	return result
}

// takeTrailingVerifierMarker accepts only the wrapper's final complete output
// line. A verifier can print marker-like text earlier in its output without
// overriding the wrapper's appended status; trailing background output fails
// closed because it obscures the status boundary.
func takeTrailingVerifierMarker(output, marker string) (code int, body string, found bool) {
	trimmed := strings.TrimSuffix(output, "\n")
	trimmed = strings.TrimSuffix(trimmed, "\r")
	lineStart := strings.LastIndexByte(trimmed, '\n') + 1
	line := strings.TrimSuffix(trimmed[lineStart:], "\r")
	if !strings.HasPrefix(line, marker) {
		return 0, output, false
	}
	rawCode := strings.TrimPrefix(line, marker)
	if rawCode == "" {
		return 0, output, false
	}
	for _, digit := range rawCode {
		if digit < '0' || digit > '9' {
			return 0, output, false
		}
	}
	parsed, err := strconv.Atoi(rawCode)
	if err != nil || parsed > 255 {
		return 0, output, false
	}
	body = strings.TrimRight(output[:lineStart], "\r\n")
	return parsed, body, true
}

func truncateVerifierOutput(out string) string {
	if len(out) > 4000 {
		return out[len(out)-4000:]
	}
	return out
}

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
// tail. The coordinator still decides acceptance from repository evidence;
// the exit code only feeds the blocker reason.
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
	hermesArgs := "hermes chat --query-file \"$stint_prompt_file\" --oneshot"
	if in.model != "" {
		provider := in.provider
		if provider == "" {
			provider = "custom"
		}
		// A provider template lets a box provision static request overrides
		// for endpoints such as NInfer that do not advertise dynamic reasoning
		// fields. For example custom:qwen-stint-{reasoning} resolves to the
		// medium or xhigh custom-provider entry before Hermes starts.
		provider = strings.ReplaceAll(provider, "{reasoning}", in.reasoning)
		providerArg := shellQuote(provider)
		if provider == "custom" {
			// Preserve the established smoke/diagnostic command shape.
			providerArg = provider
		}
		hermesArgs += " --provider " + providerArg + " -m " + shellQuote(in.model)
		if in.reasoning != "" {
			hermesArgs += " --reasoning " + shellQuote(in.reasoning)
		}
	}
	line := fmt.Sprintf(
		"umask 077; stint_prompt_file=$(mktemp /tmp/stint-deep-prompt.XXXXXX) || exit $?; "+
			"trap 'rm -f \"$stint_prompt_file\"' EXIT; printf %%s %s | base64 -d > \"$stint_prompt_file\" && "+
			"cd %s && timeout %d %s 2>&1; ec=$?; echo %s$ec",
		shellQuote(b64), shellQuote(in.workdir), secs, hermesArgs, hermesExitMarker)

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

// localHermesExecutor runs Hermes in the same instance as the coordinator. It
// is the production on-box path: the coordinator, worker, verification, and
// git all share the box's filesystem and localhost NInfer endpoint. Keeping a
// separate executor makes it impossible for this mode to accidentally create
// an SSH dependency back to the same machine.
type localHermesExecutor struct {
	binary string
}

func newLocalHermesExecutor(binary string) *localHermesExecutor {
	if strings.TrimSpace(binary) == "" {
		binary = "hermes"
	}
	return &localHermesExecutor{binary: binary}
}

func (e *localHermesExecutor) run(ctx context.Context, in execInput) (execResult, error) {
	if in.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, in.timeout)
		defer cancel()
	}
	start := time.Now()
	promptFile, err := os.CreateTemp("", "stint-hermes-prompt-*")
	if err != nil {
		return execResult{exitCode: -1, duration: time.Since(start)}, err
	}
	promptPath := promptFile.Name()
	defer os.Remove(promptPath)
	if err := promptFile.Chmod(0o600); err != nil {
		_ = promptFile.Close()
		return execResult{exitCode: -1, duration: time.Since(start)}, err
	}
	if _, err := promptFile.WriteString(in.prompt); err != nil {
		_ = promptFile.Close()
		return execResult{exitCode: -1, duration: time.Since(start)}, err
	}
	if err := promptFile.Close(); err != nil {
		return execResult{exitCode: -1, duration: time.Since(start)}, err
	}

	provider := in.provider
	if provider == "" {
		provider = "custom"
	}
	provider = strings.ReplaceAll(provider, "{reasoning}", in.reasoning)
	argv := []string{"chat", "--query-file", promptPath, "--oneshot", "--provider", provider}
	if in.model != "" {
		argv = append(argv, "-m", in.model)
	}
	if in.reasoning != "" {
		argv = append(argv, "--reasoning", in.reasoning)
	}

	cmd := exec.CommandContext(ctx, e.binary, argv...)
	cmd.Dir = in.workdir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	res := execResult{
		duration:   time.Since(start),
		exitCode:   processExitCode(cmd),
		outputText: strings.TrimSpace(out.String()),
		stderrTail: tailLine(errb.String(), 5),
	}
	if err == nil {
		res.completed = true
		res.finishReason = "completed"
		return res, nil
	}
	if ctx.Err() != nil {
		res.finishReason = ctx.Err().Error()
	}
	return res, fmt.Errorf("local hermes invocation: %w", err)
}
