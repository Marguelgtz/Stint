package main

import (
	"strings"
	"testing"
)

func TestVastOnStartRepairsOpenSSHStrictModes(t *testing.T) {
	for _, runtime := range []string{runtimeLlamaCpp, runtimeNInfer} {
		command := vastOnStartForRuntime(runtime)
		for _, required := range []string{
			"/root/.ssh/authorized_keys",
			"chown root:root",
			"chmod 700 /root /root/.ssh",
			"chmod 600 /root/.ssh/authorized_keys",
			"sleep 2",
		} {
			if !strings.Contains(command, required) {
				t.Fatalf("%s onstart command missing %q: %s", runtime, required, command)
			}
		}
	}
}
