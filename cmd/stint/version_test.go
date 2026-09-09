package main

import (
	"runtime/debug"
	"testing"
)

func TestFormatBuildVersion(t *testing.T) {
	build := func(module, revision, modified string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Version: module}, Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: revision}, {Key: "vcs.modified", Value: modified},
		}}
	}
	for _, tc := range []struct {
		name, label string
		info        *debug.BuildInfo
		want        string
	}{
		{"missing", "dev", nil, "dev (commit unknown, tree unknown)"},
		{"unstamped", "dev", &debug.BuildInfo{}, "dev (commit unknown, tree unknown)"},
		{"clean", "dev", build("(devel)", "abc123", "false"), "dev (commit abc123, tree clean)"},
		{"dirty", "dev", build("(devel)", "abc123", "true"), "dev (commit abc123, tree dirty)"},
		{"module release", "dev", build("v0.1.0", "", ""), "v0.1.0 (commit unknown, tree unknown)"},
		{"module development", "dev", build("v0.0.0-20260906010840-e78ceef308d8+dirty", "e78ceef", "true"), "v0.0.0-20260906010840-e78ceef308d8+dirty (commit e78ceef, tree dirty)"},
		{"release override", "0.1.0", build("v0.0.0-test", "abc123", "true"), "0.1.0 (commit abc123, tree dirty)"},
		{"next designated release", "0.0.1", build("(devel)", "abc123", "false"), "0.0.1 (commit abc123, tree clean)"},
		{"unknown modified", "dev", build("(devel)", "abc123", "unexpected"), "dev (commit abc123, tree unknown)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatBuildVersion(tc.label, tc.info); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestVersionAliases(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		out := captureOutput(t, func() {
			if err := run([]string{arg}); err != nil {
				t.Fatal(err)
			}
		})
		if out != buildVersion()+"\n" {
			t.Fatalf("%s: %q", arg, out)
		}
	}
}
