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
		"The JSON field and unit contract is documented in docs/TELEMETRY.md.",
	}
	cmdStatus = updated

	perfUpdated := cmdPerf
	perfUpdated.detail = "Benchmarks the local OpenAI-compatible endpoint of the active session: time to first token, total latency, and decode speed per run, plus averages. Both NInfer and llama.cpp are measured through the same localhost path, transient endpoint EOFs are retried, and the latest successful aggregate is cached for status/dashboard telemetry."
	perfUpdated.notes = append([]string{}, cmdPerf.notes...)
	perfUpdated.notes = append(perfUpdated.notes,
		"A successful aggregate is atomically cached in the Stint state directory and is reused only for the same instance, runtime, and context size.",
		"`stint status` reads the cached sample; it never starts a benchmark automatically.",
	)
	cmdPerf = perfUpdated

	for i := range cliCommands {
		switch cliCommands[i].name {
		case "status":
			cliCommands[i] = updated
		case "perf":
			cliCommands[i] = perfUpdated
		}
	}
	for i := range helpSections {
		for j := range helpSections[i].commands {
			switch helpSections[i].commands[j].name {
			case "status":
				helpSections[i].commands[j] = updated
			case "perf":
				helpSections[i].commands[j] = perfUpdated
			}
		}
	}
}
