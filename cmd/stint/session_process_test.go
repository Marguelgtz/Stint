package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestTunnelStartsInIndependentSession(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("session ownership is verified on Linux")
	}
	dir := t.TempDir()
	ssh := filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\nexec sleep 60\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	log, err := os.Create(filepath.Join(dir, "tunnel.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd, err := startTunnelProcess(config.Paths{StateDir: dir, SSHPrivateKey: filepath.Join(dir, "key")}, sessionstate.State{SSHHost: "example.invalid", SSHPort: 22}, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	group, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if group != cmd.Process.Pid {
		t.Fatalf("tunnel process group %d, want independent group %d", group, cmd.Process.Pid)
	}
}

func TestSessionProcessIdentityHelper(t *testing.T) {
	if os.Getenv("STINT_PROCESS_IDENTITY_HELPER") != "1" {
		return
	}
	time.Sleep(time.Hour)
}

func TestProcessCommandMatchesChecksExecutableAndFullArguments(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process identity verification uses Linux /proc")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	args := []string{"-test.run=^TestSessionProcessIdentityHelper$"}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "STINT_PROCESS_IDENTITY_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	deadline := time.Now().Add(time.Second)
	for !processCommandMatches(cmd.Process.Pid, exe, args) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !processCommandMatches(cmd.Process.Pid, exe, args) {
		t.Fatal("helper process did not match its exact executable and argv")
	}
	if processCommandMatches(cmd.Process.Pid, exe, []string{"-test.run=other"}) {
		t.Fatal("process identity accepted a mismatched argument list")
	}
	if processCommandMatches(cmd.Process.Pid, "/not/the/stint/executable", args) {
		t.Fatal("process identity accepted a mismatched executable")
	}
}
