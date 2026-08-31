package main

var (
	cmdExtend = cliCommand{
		name:    "extend",
		section: "compute",
		summary: "extend the active session deadline",
		detail:  "Moves the active session's Stint-managed auto-destroy deadline later without rerenting, restarting the model, or replacing a healthy watchdog. The command previews the new deadline and additional maximum cost exposure before applying it. Extensions remain bounded by the active profile's session-cost ceiling.",
		usage:   "stint extend <duration> [flags]",
		args:    []cliArg{{name: "<duration>", purpose: "positive Go-style duration such as 15m, 30m, 1h, or 1h30m"}},
		flags: []cliFlag{
			{name: "--yes", defaultVal: "false", purpose: "apply the reviewed extension without prompting"},
		},
		examples: []string{"stint extend 30m", "stint extend 1h --yes"},
		notes: []string{
			"Extension is relative to the current deadline, not the current clock time.",
			"The existing Vast instance, runtime, model process, and SSH tunnel remain unchanged.",
			"The deadline watchdog re-reads session state and observes the new deadline without process replacement.",
		},
	}

	cmdShorten = cliCommand{
		name:    "shorten",
		section: "compute",
		summary: "move the active session deadline earlier",
		detail:  "Moves the active session's Stint-managed auto-destroy deadline earlier without interrupting the running model. The command previews the change before applying it. A shortening that would make the session immediately expired is rejected; use stint down for immediate teardown.",
		usage:   "stint shorten <duration> [flags]",
		args:    []cliArg{{name: "<duration>", purpose: "positive Go-style duration such as 15m, 30m, 1h, or 1h30m"}},
		flags: []cliFlag{
			{name: "--yes", defaultVal: "false", purpose: "apply the reviewed shortening without prompting"},
		},
		examples: []string{"stint shorten 15m", "stint shorten 30m --yes"},
		notes: []string{
			"Shortening is relative to the current deadline, not the current clock time.",
			"Use `stint down` instead when you want to destroy the instance immediately.",
			"The running model and tunnel remain available until the new deadline.",
		},
	}
)

func init() {
	cliCommands = append(cliCommands, cmdExtend, cmdShorten)
	for i := range helpSections {
		if helpSections[i].title == "Compute (paid)" {
			// Keep teardown last while placing the deadline controls beside the
			// other active-session lifecycle commands.
			commands := helpSections[i].commands
			insertAt := len(commands)
			for j, command := range commands {
				if command.name == "down" {
					insertAt = j
					break
				}
			}
			updated := make([]cliCommand, 0, len(commands)+2)
			updated = append(updated, commands[:insertAt]...)
			updated = append(updated, cmdExtend, cmdShorten)
			updated = append(updated, commands[insertAt:]...)
			helpSections[i].commands = updated
			break
		}
	}
}
