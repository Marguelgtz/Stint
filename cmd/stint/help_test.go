package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestFindCommand(t *testing.T) {
	for _, name := range []string{"auth", "setup", "doctor", "status", "onboard", "plan", "start", "resume", "down", "perf", "version", "help"} {
		cmd, ok := findCommand(name)
		if !ok {
			t.Fatalf("findCommand(%q) not found", name)
		}
		if cmd.name != name {
			t.Fatalf("findCommand(%q) = %q", name, cmd.name)
		}
	}
	for alias, want := range map[string]string{"--version": "version", "-v": "version", "--help": "help", "-h": "help"} {
		cmd, ok := findCommand(alias)
		if !ok || cmd.name != want {
			t.Fatalf("findCommand(%q) = %q, %v; want %q", alias, cmd.name, ok, want)
		}
	}
	if _, ok := findCommand("bogus"); ok {
		t.Fatal("findCommand(bogus) should not be found")
	}
}

func TestCommandContent(t *testing.T) {
	for _, cmd := range cliCommands {
		if cmd.name == "" || cmd.summary == "" || cmd.detail == "" || cmd.usage == "" {
			t.Errorf("command %q has missing name/summary/detail/usage", cmd.name)
		}
		if !strings.HasPrefix(cmd.usage, "stint ") {
			t.Errorf("command %q usage must start with 'stint ', got %q", cmd.name, cmd.usage)
		}
		if cmd.section == "" {
			t.Errorf("command %q has no section", cmd.name)
		}
	}
}

func TestSectionsCoverAllCommandsOnce(t *testing.T) {
	seen := map[string]int{}
	for _, section := range helpSections {
		for _, cmd := range section.commands {
			seen[cmd.name]++
		}
	}
	for _, cmd := range cliCommands {
		if seen[cmd.name] != 1 {
			t.Errorf("command %q appears %d times in help sections; want 1", cmd.name, seen[cmd.name])
		}
	}
}

func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(data)
}

func TestUsageRenders(t *testing.T) {
	out := captureOutput(t, printUsage)
	for _, want := range []string{"QUICK START", "stint auth vast", "stint start interactive", "Setup & checks", "Compute (paid)", "stint help"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage output missing %q", want)
		}
	}
}

func TestCommandHelpRenders(t *testing.T) {
	out := captureOutput(t, func() { printCommandHelp("start") })
	for _, want := range []string{"STINT START", "USAGE", "FLAGS", "--hours", "EXAMPLES", "NOTES", "stint start interactive --hours 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("start help output missing %q", want)
		}
	}
}

func TestRunHelp(t *testing.T) {
	if err := runHelp(nil); err != nil {
		t.Fatalf("runHelp(nil): %v", err)
	}
	if err := runHelp([]string{"plan"}); err != nil {
		t.Fatalf("runHelp(plan): %v", err)
	}
	if err := runHelp([]string{"bogus"}); err == nil {
		t.Fatal("runHelp(bogus) should fail")
	}
	if err := runHelp([]string{"a", "b"}); err == nil {
		t.Fatal("runHelp with two args should fail")
	}
}
