package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Marguelgtz/Stint/internal/deep"
)

const deepWorktreeHandoff = "DEEP_WORK_HANDOFF.md"

// verificationSnapshot keeps product Git identity and Stint bookkeeping
// identity separate while allowing both to be checked for mutation around
// verification and checkpoint creation.
type verificationSnapshot struct {
	Subject     deep.VerificationSubject
	Bookkeeping map[string]string
}

// verificationBookkeepingPaths names files Stint may write into the product
// worktree for run coordination. When such a path is not already represented
// by Git (in HEAD or the index), its content is identified separately and is
// excluded from the product tree. Staging one deliberately makes it part of
// the Git-visible product state.
func (c *deepCoordinator) verificationBookkeepingPaths() []string {
	paths := []string{deepWorktreeHandoff}
	if c.execCfg.actionPlan != "" {
		paths = append(paths, c.execCfg.actionPlan)
	}
	return paths
}

func cleanBookkeepingPaths(paths []string) ([]string, error) {
	seen := make(map[string]bool, len(paths))
	clean := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if strings.ContainsAny(path, "\x00\r\n\t") || len(path) > 4096 {
			return nil, fmt.Errorf("invalid Stint bookkeeping path")
		}
		normalized := filepath.ToSlash(filepath.Clean(path))
		if filepath.IsAbs(path) || normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") || normalized != path {
			return nil, fmt.Errorf("Stint bookkeeping path %q must be a normalized worktree-relative path", path)
		}
		if !seen[path] {
			seen[path] = true
			clean = append(clean, path)
		}
	}
	return clean, nil
}

func sameVerificationSnapshot(a, b verificationSnapshot) bool {
	if a.Subject != b.Subject || len(a.Bookkeeping) != len(b.Bookkeeping) {
		return false
	}
	for path, hash := range a.Bookkeeping {
		if b.Bookkeeping[path] != hash {
			return false
		}
	}
	return true
}

func bookkeepingMetadataValue(g *gitRunner, dir, path string) (string, error) {
	info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(path)))
	if os.IsNotExist(err) {
		return "absent", nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect Stint bookkeeping file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("Stint bookkeeping path %q is not a regular file", path)
	}
	out, err := g.run(dir, "hash-object", "--no-filters", "--", path)
	if err != nil {
		return "", fmt.Errorf("identify Stint bookkeeping file %q: %w", path, err)
	}
	oid := strings.TrimSpace(out)
	if oid == "" {
		return "", fmt.Errorf("Git returned an empty identity for Stint bookkeeping file %q", path)
	}
	return "git-blob:" + oid, nil
}

func (g *gitRunner) verificationSubject(dir string, bookkeepingPaths []string) (verificationSnapshot, error) {
	paths, err := cleanBookkeepingPaths(bookkeepingPaths)
	if err != nil {
		return verificationSnapshot{}, err
	}
	if err := g.ensureSubmodulesRepresentable(dir); err != nil {
		return verificationSnapshot{}, err
	}
	head, err := g.repoHead(dir)
	if err != nil {
		return verificationSnapshot{}, err
	}
	snapshot := verificationSnapshot{Subject: deep.VerificationSubject{HeadCommit: strings.TrimSpace(head)}, Bookkeeping: map[string]string{}}
	excluded := make([]string, 0, len(paths))
	for _, path := range paths {
		visible, err := g.pathGitVisible(dir, path)
		if err != nil {
			return verificationSnapshot{}, err
		}
		if visible {
			continue
		}
		identity, err := bookkeepingMetadataValue(g, dir, path)
		if err != nil {
			return verificationSnapshot{}, err
		}
		snapshot.Bookkeeping[path] = identity
		excluded = append(excluded, path)
	}
	tree, err := g.worktreeTree(dir, snapshot.Subject.HeadCommit, excluded)
	if err != nil {
		return verificationSnapshot{}, err
	}
	if after, err := g.repoHead(dir); err != nil {
		return verificationSnapshot{}, err
	} else if strings.TrimSpace(after) != snapshot.Subject.HeadCommit {
		return verificationSnapshot{}, fmt.Errorf("repository HEAD changed while capturing verification subject (%s -> %s)", snapshot.Subject.HeadCommit, strings.TrimSpace(after))
	}
	snapshot.Subject.TreeSHA = tree
	if len(snapshot.Bookkeeping) == 0 {
		snapshot.Bookkeeping = nil
	}
	return snapshot, nil
}

func (g *gitRunner) pathGitVisible(dir, path string) (bool, error) {
	pathspec := ":(literal)" + path
	indexed, err := g.run(dir, "ls-files", "-z", "--", pathspec)
	if err != nil {
		return false, fmt.Errorf("check Git index for bookkeeping path %q: %w", path, err)
	}
	if indexed != "" {
		return true, nil
	}
	committed, err := g.run(dir, "ls-tree", "-r", "-z", "--name-only", "HEAD", "--", pathspec)
	if err != nil {
		return false, fmt.Errorf("check HEAD for bookkeeping path %q: %w", path, err)
	}
	return committed != "", nil
}

func (g *gitRunner) ensureSubmodulesRepresentable(dir string) error {
	status, err := g.run(dir, "submodule", "status", "--recursive")
	if err != nil {
		return fmt.Errorf("inspect submodules for verification subject: %w", err)
	}
	for _, line := range strings.Split(status, "\n") {
		if strings.HasPrefix(line, "-") {
			return fmt.Errorf("verification subject cannot include an uninitialized submodule: %s", strings.TrimSpace(line))
		}
	}
	_, err = g.run(dir, "submodule", "foreach", "--quiet", "--recursive", `test -z "$(git status --porcelain --untracked-files=all)"`)
	if err != nil {
		return fmt.Errorf("verification subject cannot include a dirty submodule worktree: %w", err)
	}
	return nil
}

func (g *gitRunner) worktreeTree(dir, head string, excluded []string) (string, error) {
	tempDir, err := os.MkdirTemp("", "stint-verification-index-*")
	if err != nil {
		return "", fmt.Errorf("create temporary verification index directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	indexPath := filepath.Join(tempDir, "index")
	if _, err := runGitWithIndex(dir, indexPath, "read-tree", head); err != nil {
		return "", fmt.Errorf("initialize temporary verification index: %w", err)
	}
	args := []string{"add", "-A", "--", "."}
	for _, path := range excluded {
		args = append(args, ":(top,exclude,literal)"+path)
	}
	if _, err := runGitWithIndex(dir, indexPath, args...); err != nil {
		return "", fmt.Errorf("capture worktree in temporary verification index: %w", err)
	}
	tree, err := runGitWithIndex(dir, indexPath, "write-tree")
	if err != nil {
		return "", fmt.Errorf("write verification tree: %w", err)
	}
	tree = strings.TrimSpace(tree)
	if tree == "" {
		return "", fmt.Errorf("Git returned an empty verification tree")
	}
	return tree, nil
}

func runGitWithIndex(dir, indexPath string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = envWithGitIndex(indexPath)
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(errb.String())
		if message == "" {
			message = err.Error()
		}
		return out.String(), fmt.Errorf("git %s: %s", args[0], message)
	}
	return out.String(), nil
}

func envWithGitIndex(path string) []string {
	env := os.Environ()
	filtered := env[:0]
	for _, entry := range env {
		if strings.HasPrefix(entry, "GIT_INDEX_FILE=") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, "GIT_INDEX_FILE="+path)
}
