package main

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

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
