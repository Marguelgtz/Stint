package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func bookkeepingPathsFromSnapshot(snapshot verificationSnapshot) []string {
	paths := make([]string, 0, len(snapshot.Bookkeeping))
	for path := range snapshot.Bookkeeping {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func taskCheckpointMessage(task deep.Task) string {
	objective := strings.Join(strings.Fields(task.Objective), " ")
	if objective == "" {
		objective = "verified task state"
	}
	const maxSubjectRunes = 64
	runes := []rune(objective)
	if len(runes) > maxSubjectRunes {
		objective = string(runes[:maxSubjectRunes-1]) + "…"
	}
	return fmt.Sprintf("deep(%s): %s", task.ID, objective)
}

func (g *gitRunner) checkpointSubject(dir, message string, snapshot verificationSnapshot) (string, string, error) {
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

func sameMetadata(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for path, hash := range a {
		if b[path] != hash {
			return false
		}
	}
	return true
}
