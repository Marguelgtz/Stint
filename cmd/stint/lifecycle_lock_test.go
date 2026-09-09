package main

import (
	"strings"
	"testing"

	"github.com/Marguelgtz/Stint/internal/config"
)

func TestLifecycleLockRejectsConcurrentMutationAndReleases(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{
		ConfigDir: root + "/config",
		StateDir:  root + "/state",
		SSHDir:    root + "/ssh",
	}

	releaseFirst, err := acquireLifecycleLock(paths)
	if err != nil {
		t.Fatalf("acquire first lifecycle lock: %v", err)
	}

	if _, err := acquireLifecycleLock(paths); err == nil {
		releaseFirst()
		t.Fatal("concurrent lifecycle lock unexpectedly succeeded")
	} else if !strings.Contains(err.Error(), "already running") {
		releaseFirst()
		t.Fatalf("concurrent lifecycle lock error = %q", err)
	}

	releaseFirst()
	releaseSecond, err := acquireLifecycleLock(paths)
	if err != nil {
		t.Fatalf("reacquire released lifecycle lock: %v", err)
	}
	releaseSecond()
}
