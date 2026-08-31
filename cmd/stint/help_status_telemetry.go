package main

func init() {
	updated := cmdStatus
	updated.summary = "show the active session snapshot and cached telemetry"
	updated.detail = "Prints the local session snapshot without contacting the remote host by default. --refresh adds passive endpoint, runtime, and GPU telemetry; --json exposes the same snapshot as a machine-readable contract. Status never sends an inference request."
	updated.usage = "stint status [flags]"
	updated.flags = []cliFlag{
		{name: "--refresh", defaultVal: "false", purpose: "collect passive endpoint, runtime and GPU telemetry (bounded read-only probes)"},
		{name: "--json", defaultVal: "false", purpose: "print the assembled snapshot as machine-readable JSON"},
	}
	updated.examples = []string{
		"stint status",
		"stint status --refresh",
		"stint status --json",
		"stint status --refresh --json",
	}
	updated.notes = []string{
		"Plain `stint status` is local/cached: it performs no SSH and no model generation.",
		"`--refresh` probes /v1/models and performs one read-only SSH round trip for runtime + nvidia-smi metrics; telemetry failures are reported inside the snapshot rather than treated as lifecycle failures.",
		"Performance values come from the most recent successful `stint perf` sample for the same instance/runtime/context; status never benchmarks automatically.",
	}
	cmdStatus = updated

	for i := range cliCommands {
		if cliCommands[i].name == "status" {
			cliCommands[i] = updated
		}
	}
	for i := range helpSections {
		for j := range helpSections[i].commands {
			if helpSections[i].commands[j].name == "status" {
				helpSections[i].commands[j] = updated
			}
		}
	}
}
