package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestInteractiveSSHArgsUsesStintKeyRecordedEndpointAndForcesPTY(t *testing.T) {
	paths := config.Paths{
		StateDir:      "/tmp/stint-state",
		SSHPrivateKey: "/tmp/stint-key",
	}
	state := sessionstate.State{
		SSHHost: "189.79.25.23",
		SSHPort: 42831,
	}

	got := interactiveSSHArgs(paths, state)
	want := []string{
		"-tt",
		"-i", "/tmp/stint-key",
		"-p", "42831",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=/tmp/stint-state/known_hosts",
		"root@189.79.25.23",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("interactiveSSHArgs() = %#v, want %#v", got, want)
	}
}

func TestValidateSSHAccessStateRequiresEndpoint(t *testing.T) {
	err := validateSSHAccessState(sessionstate.State{InstanceID: 49206927})
	if err == nil {
		t.Fatal("validateSSHAccessState() error = nil, want missing endpoint error")
	}
	if !strings.Contains(err.Error(), "stint resume") {
		t.Fatalf("validateSSHAccessState() error = %q, want resume guidance", err)
	}
}

func TestValidateSSHAccessStateAcceptsRecordedEndpoint(t *testing.T) {
	err := validateSSHAccessState(sessionstate.State{
		InstanceID: 49206927,
		SSHHost:    "189.79.25.23",
		SSHPort:    42831,
	})
	if err != nil {
		t.Fatalf("validateSSHAccessState() error = %v, want nil", err)
	}
}
