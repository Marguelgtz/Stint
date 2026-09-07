package main

import (
	"runtime/debug"
)

func buildVersion() string {
	info, _ := debug.ReadBuildInfo()
	return formatBuildVersion(version, info)
}

// Read only embedded metadata: querying a binary must not depend on the
// current directory, an installed Git executable, or the checkout's HEAD.
func formatBuildVersion(label string, info *debug.BuildInfo) string {
	revision, modified := "unknown", "unknown"
	if info != nil {
		if label == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			label = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if setting.Value != "" {
					revision = setting.Value
				}
			case "vcs.modified":
				switch setting.Value {
				case "true":
					modified = "dirty"
				case "false":
					modified = "clean"
				}
			}
		}
	}
	return label + " (commit " + revision + ", tree " + modified + ")"
}
