package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyControlSocketPatternTargetsOnlyStintSockets(t *testing.T) {
	pattern := filepath.Join(os.TempDir(), "stint-ssh-*")
	if !strings.Contains(pattern, "stint-ssh-") {
		t.Fatalf("pattern %q does not target Stint SSH control sockets", pattern)
	}
}
