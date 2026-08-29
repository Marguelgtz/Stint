package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// closeLegacyStintControlMasters removes orphaned multiplexed SSH masters left
// by older Stint tunnel launches. Only Stint-owned control sockets are targeted.
// This is attempted only when the Cline endpoint is not responding but port 8409
// remains occupied, so a healthy existing tunnel is adopted before cleanup.
func closeLegacyStintControlMasters(ctx context.Context) (bool, error) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		return false, fmt.Errorf("OpenSSH client not found in PATH")
	}

	sockets, err := filepath.Glob(filepath.Join(os.TempDir(), "stint-ssh-*"))
	if err != nil {
		return false, fmt.Errorf("find Stint SSH control sockets: %w", err)
	}
	closed := false
	for _, socket := range sockets {
		cmd := exec.CommandContext(ctx, ssh, "-S", socket, "-O", "exit", "localhost")
		if err := cmd.Run(); err == nil {
			closed = true
		}
	}
	return closed, nil
}
