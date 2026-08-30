package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	localenv "github.com/Marguelgtz/Stint/internal/local"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const vastDisableAutoTmuxCommand = "touch ~/.no_auto_tmux"

func runSSHAccess(args []string) error {
	fs := flag.NewFlagSet("ssh", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("ssh does not accept positional arguments")
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	state, err := sessionstate.Load(paths)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("no active Stint session is recorded; run: stint start interactive")
	}
	if err != nil {
		return err
	}
	if err := validateSSHAccessState(state); err != nil {
		return err
	}
	if !localenv.SSHKeyExists(paths) {
		return errors.New("Stint SSH key is not configured; run: stint setup ssh")
	}

	// Vast images automatically attach interactive SSH logins to tmux unless
	// ~/.no_auto_tmux exists. In a forced PTY session that nested wrapper can
	// leave bracketed paste, Enter, and arrow-key escape sequences uninterpreted.
	// Disable it idempotently through the already-tested non-interactive SSH path
	// before opening the user's shell.
	prepareCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	_, prepareErr := runSSH(prepareCtx, paths, state, vastDisableAutoTmuxCommand)
	cancel()
	if prepareErr != nil {
		return fmt.Errorf("prepare interactive SSH shell on instance %d: %w", state.InstanceID, prepareErr)
	}

	ssh, err := localenv.SSHExecutable()
	if err != nil {
		return err
	}
	fmt.Printf("Connecting to Vast instance %d (%s:%d)...\n", state.InstanceID, state.SSHHost, state.SSHPort)
	cmd := exec.Command(ssh, interactiveSSHArgs(paths, state)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh to instance %d: %w", state.InstanceID, err)
	}
	return nil
}

func validateSSHAccessState(state sessionstate.State) error {
	if state.InstanceID <= 0 {
		return errors.New("recorded Stint session has no Vast instance ID")
	}
	if state.SSHHost == "" || state.SSHPort <= 0 || state.SSHPort > 65535 {
		return fmt.Errorf("instance %d does not have a usable SSH endpoint yet; run: stint resume", state.InstanceID)
	}
	return nil
}

func interactiveSSHArgs(paths config.Paths, state sessionstate.State) []string {
	knownHosts := filepath.Join(paths.StateDir, "known_hosts")
	return []string{
		"-tt",
		"-i", paths.SSHPrivateKey,
		"-p", strconv.Itoa(state.SSHPort),
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=" + knownHosts,
		"root@" + state.SSHHost,
	}
}
