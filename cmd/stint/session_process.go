package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/Marguelgtz/Stint/internal/config"
	localenv "github.com/Marguelgtz/Stint/internal/local"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func sessionTunnelArgs(paths config.Paths, state sessionstate.State) []string {
	knownHosts := filepath.Join(paths.StateDir, "known_hosts")
	forward := fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", clinePort, clineRemotePort)
	return []string{
		"-N",
		"-i", paths.SSHPrivateKey,
		"-p", strconv.Itoa(state.SSHPort),
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=" + knownHosts,
		"-L", forward,
		"root@" + state.SSHHost,
	}
}

// processCommandMatches is deliberately Linux-only. If /proc cannot establish
// the executable and full argv, the caller must leave the process alone.
func processCommandMatches(pid int, executable string, args []string) bool {
	if pid <= 0 || strings.TrimSpace(executable) == "" {
		return false
	}
	if !processExecutableMatches(pid, executable) {
		return false
	}
	procExe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil || cleanExecutablePath(procExe) != cleanExecutablePath(executable) {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(data) == 0 {
		return false
	}
	argv := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	if len(argv) != len(args)+1 || cleanExecutablePath(argv[0]) != cleanExecutablePath(executable) {
		return false
	}
	for i, want := range args {
		if argv[i+1] != want {
			return false
		}
	}
	return true
}

func processExecutableMatches(pid int, executable string) bool {
	if pid <= 0 || strings.TrimSpace(executable) == "" {
		return false
	}
	runningPath, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil || cleanExecutablePath(runningPath) != cleanExecutablePath(executable) {
		return false
	}
	runningInfo, err := os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return false
	}
	executableInfo, err := os.Stat(executable)
	return err == nil && os.SameFile(runningInfo, executableInfo)
}

func sessionWatchdogRunning(state sessionstate.State) bool {
	if state.WatchdogPID <= 0 {
		return false
	}
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	return processCommandMatches(state.WatchdogPID, executable, []string{"_watchdog", strconv.FormatInt(state.InstanceID, 10)})
}

func sessionTunnelRunning(paths config.Paths, state sessionstate.State) bool {
	if state.TunnelPID <= 0 || state.SSHHost == "" || state.SSHPort <= 0 {
		return false
	}
	ssh, err := localenv.SSHExecutable()
	if err != nil {
		return false
	}
	return processCommandMatches(state.TunnelPID, ssh, sessionTunnelArgs(paths, state))
}

func stopSessionTunnel(paths config.Paths, state sessionstate.State) (bool, error) {
	if state.TunnelPID <= 0 || state.SSHHost == "" || state.SSHPort <= 0 {
		return false, nil
	}
	ssh, err := localenv.SSHExecutable()
	if err != nil || !processCommandMatches(state.TunnelPID, ssh, sessionTunnelArgs(paths, state)) {
		return false, nil
	}
	// Recheck identity immediately before signaling so stale state never
	// authorizes signaling a reused PID.
	if !processCommandMatches(state.TunnelPID, ssh, sessionTunnelArgs(paths, state)) {
		return false, nil
	}
	process, err := os.FindProcess(state.TunnelPID)
	if err != nil {
		return false, fmt.Errorf("find verified Stint tunnel pid %d: %w", state.TunnelPID, err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return false, fmt.Errorf("stop verified Stint tunnel pid %d: %w", state.TunnelPID, err)
	}
	return true, nil
}

func startTunnelProcess(paths config.Paths, state sessionstate.State, log *os.File) (*exec.Cmd, error) {
	ssh, err := localenv.SSHExecutable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(ssh, sessionTunnelArgs(paths, state)...)
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start SSH tunnel: %w", err)
	}
	return cmd, nil
}
