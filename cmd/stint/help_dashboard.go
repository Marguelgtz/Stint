package main

var cmdDashboard = cliCommand{
	name:     "dashboard",
	section:  "diagnostics",
	summary:  "open the live session cockpit",
	detail:   "Opens an interactive terminal dashboard over the existing session snapshot and telemetry layers. Local timing updates every second without I/O; endpoint/runtime/GPU telemetry refreshes passively about every 10 seconds. Exiting the dashboard never destroys compute.",
	usage:    "stint dashboard [flags]",
	flags: []cliFlag{
		{name: "--no-color", defaultVal: "false", purpose: "disable ANSI colors"},
	},
	examples: []string{"stint dashboard", "NO_COLOR=1 stint dashboard"},
	notes: []string{
		"Interactive mode requires a TTY; when stdout/stdin are piped, dashboard falls back to a static refreshed status snapshot.",
		"Keys: 1 Home, 2 Performance, 3 Config, 4 Logs, r refresh, q exit.",
		"The dashboard never benchmarks automatically and never owns lifecycle authority.",
	},
}

func init() {
	cliCommands = append(cliCommands, cmdDashboard)
	for i := range helpSections {
		if helpSections[i].title == "Diagnostics" {
			helpSections[i].commands = append([]cliCommand{cmdDashboard}, helpSections[i].commands...)
			return
		}
	}
}
