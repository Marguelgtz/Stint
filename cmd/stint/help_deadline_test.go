package main

import (
	"strings"
	"testing"
)

func TestDeadlineCommandsRegisteredInHelp(t *testing.T) {
	for _, name := range []string{"extend", "shorten"} {
		cmd, ok := findCommand(name)
		if !ok {
			t.Fatalf("deadline command %q not registered", name)
		}
		if cmd.section != "compute" {
			t.Fatalf("deadline command %q section = %q, want compute", name, cmd.section)
		}
		out := captureOutput(t, func() { printCommandHelp(name) })
		for _, want := range []string{"USAGE", "<duration>", "--yes", "EXAMPLES", "NOTES"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%s help missing %q", name, want)
			}
		}
	}
}
