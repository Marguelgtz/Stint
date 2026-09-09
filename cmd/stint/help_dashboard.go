package main

var cmdDash = cliCommand{
	name:    "dash",
	aliases: []string{"dashboard"},
	section: "diagnostics",
	summary: "open the live session cockpit",
	detail:  "Opens an interactive terminal dashboard over the existing session snapshot and telemetry layers. Local timing updates every second without I/O; endpoint/runtime/GPU/live-inference telemetry refreshes passively about every 10 seconds. Recoverable paid sessions can be resumed through the existing lifecycle path. Exiting the dashboard never destroys compute.",
	usage:   "stint dash [flags]",
	flags: []cliFlag{
		{name: "--no-color", defaultVal: "false", purpose: "disable ANSI colors"},
	},
	examples: []string{"stint dash", "NO_COLOR=1 stint dash"},
	notes: []string{
		"`stint dashboard` remains a compatibility alias for `stint dash`.",
		"Interactive mode requires a TTY; when stdout/stdin are piped, dash falls back to a static refreshed status snapshot.",
		"Keys: 1 Home, 2 Performance, 3 Config, 4 Logs, arrows navigate, r refresh (resume when RECOVERABLE), b benchmark, + extend, - shorten, d down, q exit.",
		"RECOVERABLE is authoritative persisted session state; DEGRADED is an observational dashboard state derived from refreshed endpoint/runtime health and never mutates session.json.",
		"Home shows a LIVE strip and Performance shows a LIVE TRAFFIC section: observed inference activity (agents, resident prompt depth, rates, lanes) polled from /metrics and /slots — observation, never benchmarked; the benchmarked sample still comes only from `b`.",
		"The dashboard never benchmarks automatically and never owns lifecycle authority.",
		"q and Ctrl+C close only the dashboard; paid compute remains active until its deadline or an explicit down action.",
	},
}

var dashboardHelpRegistered = registerDashboardHelp()

func registerDashboardHelp() bool {
	cliCommands = append(cliCommands, cmdDash)
	for i := range helpSections {
		if helpSections[i].title == "Diagnostics" {
			helpSections[i].commands = append([]cliCommand{cmdDash}, helpSections[i].commands...)
			break
		}
	}
	return true
}
