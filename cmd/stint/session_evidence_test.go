package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Marguelgtz/Stint/internal/config"
)

func TestSessionEvidencePathsAreInstanceScoped(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{
		ConfigDir: filepath.Join(root, "config"),
		StateDir:  filepath.Join(root, "state"),
		SSHDir:    filepath.Join(root, "ssh"),
	}
	first, err := sessionEvidenceLogPath(paths, 101, "tunnel.log")
	if err != nil {
		t.Fatalf("first log path: %v", err)
	}
	second, err := sessionEvidenceLogPath(paths, 202, "tunnel.log")
	if err != nil {
		t.Fatalf("second log path: %v", err)
	}
	if first == second {
		t.Fatalf("instance logs collided at %s", first)
	}
	if want := filepath.Join(paths.StateDir, "sessions", "101", "tunnel.log"); first != want {
		t.Fatalf("first = %q, want %q", first, want)
	}
	info, err := os.Stat(filepath.Dir(first))
	if err != nil {
		t.Fatalf("stat evidence dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("evidence dir mode = %o, want 700", info.Mode().Perm())
	}
}

func TestSessionEvidenceRejectsInvalidInputs(t *testing.T) {
	paths := config.Paths{StateDir: filepath.Join(t.TempDir(), "state")}
	if _, err := sessionEvidenceLogPath(paths, 0, "tunnel.log"); err == nil {
		t.Fatal("expected zero instance id to fail")
	}
	if _, err := sessionEvidenceLogPath(paths, 42, "../tunnel.log"); err == nil {
		t.Fatal("expected path traversal name to fail")
	}
}
