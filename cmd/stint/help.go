package main

import (
	"errors"
	"fmt"
	"strings"
)

type cliFlag struct {
	name       string
	argument   string
	defaultVal string
	purpose    string
}

func (f cliFlag) nameColumn() string {
	if f.argument == "" {
		return f.name
	}
	return f.name + " " + f.argument
}

func (f cliFlag) line(width int) string {
	line := fmt.Sprintf("  %-*s  %s", width, f.nameColumn(), f.purpose)
	if f.defaultVal != "" {
		line += fmt.Sprintf(" (default: %s)", f.defaultVal)
	}
	return line
}

type cliArg struct {
	name    string
	purpose string
}

type cliCommand struct {
	name     string
	aliases  []string
	section  string
	summary  string
	detail   string
	usage    string
	args     []cliArg
	flags    []cliFlag
	examples []string
	notes    []string
	internal bool
}

type helpSection struct {
	title    string
	commands []cliCommand
}

var (
	cmdAuth = cliCommand{
		name:     "auth",
		section:  "setup",
		summary:  "verify and store the Vast API key",
		detail:   "Authenticates a Vast API key by checking both instance-read and marketplace-search access, then stores it locally with owner-only permissions. The key stays on this machine; it is a compute credential, not a Stint identity credential.",
		usage:    "stint auth vast [flags]",
		args:     []cliArg{{name: "<provider>", purpose: "vast (only supported provider)"}},
		flags:    []cliFlag{{name: "--from-env", defaultVal: "false", purpose: "read the API key from VAST_API_KEY instead of a hidden prompt"}},
		examples: []string{"stint auth vast", "stint auth vast --from-env"},
		notes: []string{
			"Credentials are stored at ~/.config/stint/credentials.json (mode 0600).",
			"Vast API keys are not GitHub credentials; Stint identity remains GitHub via Spark.",
		},
	}

	cmdSetup = cliCommand{
		name:     "setup",
		section:  "setup",
		summary:  "create the dedicated Stint SSH keypair",
		detail:   "Creates (or reuses) a dedicated ed25519 SSH keypair for Stint under ~/.config/stint/ssh/. `stint start` attaches the key to rented instances automatically; you add the public key to Vast once.",
		usage:    "stint setup ssh",
		examples: []string{"stint setup ssh"},
		notes: []string{
			"Add the printed public key in Vast: Account -> Keys -> SSH Keys.",
			"The private key never leaves ~/.config/stint/ssh/.",
		},
	}

	cmdDoctor = cliCommand{
		name:     "doctor",
		section:  "setup",
		summary:  "check local prerequisites and Vast access",
		detail:   "Verifies everything needed for live planning and paid start: Vast credentials with instance-read and search access, OpenSSH, the dedicated Stint SSH key, and local port 8409 for the Cline tunnel.",
		usage:    "stint doctor",
		examples: []string{"stint doctor"},
		notes: []string{
			"Exit status is non-zero when any check fails.",
			"Run `stint doctor` after `stint auth vast` and `stint setup ssh`.",
		},
	}

	cmdStatus = cliCommand{
		name:     "status",
		section:  "setup",
		summary:  "show local status and the active session",
		detail:   "Prints local Stint state (Vast provider, SSH key, Cline endpoint) and, when a session is recorded, the active instance: GPU, rate, checkpoint, last error, auto-destroy deadline, and the suggested next action.",
		usage:    "stint status",
		examples: []string{"stint status"},
		notes: []string{
			"When the next action is `stint resume`, the paid instance is preserved and resumable.",
		},
	}

	cmdOnboard = cliCommand{
		name:     "onboard",
		section:  "setup",
		summary:  "print the Spark onboarding plan",
		detail:   "Shows the Spark onboarding plan for this repository: profile path, dashboard URL, expected GitHub evidence names, and the onboarding steps. Nothing is created or sent; this is a read-only plan.",
		usage:    "stint onboard spark [flags]",
		args:     []cliArg{{name: "<target>", purpose: "spark (only supported onboarding target)"}},
		flags:    []cliFlag{{name: "--dashboard", argument: "<url>", defaultVal: "hosted Spark dashboard", purpose: "Spark dashboard URL"}},
		examples: []string{"stint onboard spark"},
		notes: []string{
			"GitHub Actions emits the evidence jobs Spark observes: spark-profile, go-vet, unit-tests.",
		},
	}

	cmdPlan = cliCommand{
		name:    "plan",
		section: "planning",
		summary: "rank marketplace offers under hard policy (read-only)",
		detail:  "Queries the live Vast marketplace (or deterministic local fixtures), evaluates every candidate against Stint's hard policy, ranks the qualifiers, and prints the selected offer, alternatives, and session cost. plan never rents or mutates anything on the provider side.",
		usage:   "stint plan <profile> [flags]",
		args:    []cliArg{{name: "<profile>", purpose: "interactive (live or fixture) or deep (fixture only)"}},
		flags: []cliFlag{
			{name: "--hours", argument: "<float>", defaultVal: "profile default (interactive 5, deep 8)", purpose: "session duration in hours"},
			{name: "--fixture", defaultVal: "false", purpose: "use deterministic local fixture offers instead of the Vast API"},
			{name: "--json", defaultVal: "false", purpose: "print machine-readable JSON (plan, alternatives, diagnostics)"},
		},
		examples: []string{
			"stint plan interactive --hours 5",
			"stint plan interactive --hours 5 --json",
			"stint plan deep --hours 8 --fixture",
		},
		notes: []string{
			"Hard interactive policy: 1x RTX 4090, <= $0.40/hour, >= 98.5% reliability, >= 24 GB VRAM, >= 1 direct port, verified + rentable + not rented, 50 GB storage, on-demand only, session ceiling $2.50.",
			"Discovery is intentionally broader (capped at $0.60/hour) so rejections are explainable; a failed plan prints marketplace diagnostics, including the Vast discovery bisect when the API returns zero candidates.",
		},
	}

	cmdStart = cliCommand{
		name:    "start",
		section: "compute",
		summary: "rent a Vast GPU, boot the model, tunnel to 127.0.0.1:8409",
		detail:  "Rents a Vast instance for the selected offer, qualifies the host by sampling the real model-transfer throughput, boots the inference runtime (NInfer on RTX 4090 hosts, llama.cpp otherwise), and serves Qwen3.8-27B at http://127.0.0.1:8409/v1 through a supervised SSH tunnel. A detached watchdog destroys the instance at the paid deadline even if this process exits.",
		usage:   "stint start interactive [flags]",
		args:    []cliArg{{name: "<profile>", purpose: "interactive (only live profile)"}},
		flags: []cliFlag{
			{name: "--hours", argument: "<float>", defaultVal: "1", purpose: "maximum paid session duration in hours"},
			{name: "--yes", defaultVal: "false", purpose: "confirm the selected rental without prompting"},
			{name: "--location", argument: "<text>", defaultVal: "", purpose: "prefer an offer whose location contains this text"},
			{name: "--runtime", argument: "<name>", defaultVal: "auto", purpose: "inference runtime: auto, ninfer, or llama.cpp"},
			{name: "--context", argument: "<int>", defaultVal: "16384", purpose: "llama.cpp context tokens (1024-131072)"},
			{name: "--ninfer-config", argument: "<name>", defaultVal: "coding", purpose: "NInfer config: coding, precision, or native"},
			{name: "--clients", argument: "<int>", defaultVal: "1", purpose: "NInfer client lanes: 1 or 2; lanes share the configured KV/context pool dynamically"},
			{name: "--min-network-mbps", argument: "<float>", defaultVal: "500", purpose: "minimum Vast advertised download bandwidth in Mbps; 0 disables the prefilter"},
			{name: "--min-measured-download-mbps", argument: "<float>", defaultVal: "40", purpose: "minimum measured post-SSH model-transfer throughput in MB/s; 0 disables"},
			{name: "--network-candidate-attempts", argument: "<int>", defaultVal: "3", purpose: "maximum distinct Vast machines to try during provider startup and network qualification"},
			{name: "--max-hourly-usd", argument: "<float>", defaultVal: "profile ceiling", purpose: "set per-run offer price ceiling; raising it requires --max-cost-usd"},
			{name: "--max-cost-usd", argument: "<float>", defaultVal: "profile ceiling", purpose: "lower, but never raise, the requested-session cost ceiling"},
			{name: "--validate-only", defaultVal: "false", purpose: "validate start options without reading credentials or contacting a provider"},
		},
		examples: []string{
			"stint start interactive --hours 2",
			"stint start interactive --yes --runtime ninfer --ninfer-config coding",
			"stint start interactive --runtime ninfer --ninfer-config native --clients 2",
			"stint start interactive --location germany --min-measured-download-mbps 50",
			"stint start interactive --runtime llama.cpp --context 32768",
			"stint start interactive --hours 1.5 --max-hourly-usd 1.33 --max-cost-usd 2 --validate-only",
		},
		notes: []string{
			"Requires `stint auth vast`, a free local port 8409, and the Stint SSH key (`stint setup ssh`).",
			"NInfer is qualified for RTX 4090 hosts with CUDA >= 12.8 only; with --runtime auto, a 4090 uses NInfer and any other qualifying GPU falls back to llama.cpp (auto also falls back if the NInfer bootstrap is unavailable).",
			"--clients 2 is NInfer-only. It maps to two generation lanes over one shared dynamic KV pool; Stint does not split the configured context in half. Auto mode will not silently fall back to llama.cpp when two clients were requested.",
			"--max-cost-usd can only lower the interactive profile's $2.50 requested-session ceiling; candidate rentals are checked against the resulting limit.",
			"--max-hourly-usd can raise the profile's $0.40 hourly limit only when an explicit --max-cost-usd session cap is also supplied; every candidate must still fit that total cap.",
			"--validate-only parses and validates start settings, then exits before reading credentials, changing local session state, or contacting Vast.",
			"Provider or SSH startup failures reject the host and try the next candidate. Failures after SSH is ready preserve the paid instance: run `stint resume` to continue.",
			"The endpoint is OpenAI-compatible: base URL http://127.0.0.1:8409/v1, model qwen3.8-27b.",
		},
	}

	cmdResume = cliCommand{
		name:     "resume",
		section:  "compute",
		summary:  "reattach to a saved session after an interruption",
		detail:   "Continues a recorded session after an interruption: re-establishes the SSH tunnel (releasing stale ports first), verifies or restarts the remote runtime, waits for the model, and reports READY. If the session deadline has already passed, resume destroys the compute and clears the local state.",
		usage:    "stint resume",
		examples: []string{"stint resume"},
		notes: []string{
			"Supports interactive sessions only.",
			"If resume fails again, the paid instance stays resumable and the deadline watchdog keeps running.",
		},
	}

	cmdDown = cliCommand{
		name:     "down",
		section:  "compute",
		summary:  "destroy the instance, tunnel, and session state",
		detail:   "Stops the local tunnel and watchdog, destroys the Vast instance, and clears the local session state. Safe to run when no session is recorded.",
		usage:    "stint down",
		examples: []string{"stint down"},
		notes: []string{
			"Compute is also destroyed automatically at the session deadline by the watchdog.",
		},
	}

	cmdPerf = cliCommand{
		name:    "perf",
		section: "diagnostics",
		summary: "benchmark the local endpoint at a chosen prompt depth",
		detail:  "Benchmarks the local OpenAI-compatible endpoint of the active session: time to first token, total latency, decode speed, and post-run VRAM per run, plus averages. Both NInfer and llama.cpp are measured through the same localhost path, and transient endpoint EOFs are retried. The benchmark prompt is built to the requested depth so TTFT and VRAM reflect real prompt encoding, not just decode.",
		usage:   "stint perf [flags]",
		flags: []cliFlag{
			{name: "--prompt-tokens", argument: "<int>", defaultVal: "8192", purpose: "target prompt depth in tokens (32-200000); the exact depth is reported from the endpoint"},
			{name: "--runs", argument: "<int>", defaultVal: "3", purpose: "number of benchmark requests (1-10)"},
			{name: "--tokens", argument: "<int>", defaultVal: "256", purpose: "maximum completion tokens per request (32-2048)"},
		},
		examples: []string{
			"stint perf",
			"stint perf --runs 5 --tokens 512",
			"stint perf --prompt-tokens 32768",
			"stint perf --prompt-tokens 131072 --runs 1",
		},
		notes: []string{
			"Requires an active session: run `stint start` or `stint resume` first.",
			"Configured context is a ceiling, not the measured depth: prompt plus completion tokens must fit inside the active context.",
			"Use deeper prompts to measure long-context agent behavior: 8192 is a typical mid-session agent turn; 32768-131072 approaches long-context agent workloads.",
			"VRAM is sampled on the remote GPU immediately after the run, so it reflects memory pressure at the measured depth.",
		},
	}

	cmdDeep = cliCommand{
		name:    "deep",
		section: "deepwork",
		summary: "run a bounded Hermes engineering mission",
		detail: `Deep Work runs bounded repository tasks through Hermes. The production path is ` + "`scripts/launch-onbox-deep.sh`" + `: Stint rents and qualifies compute, bootstraps the box, starts a detached on-box supervisor, and returns after the durable RUNNING handshake. Hermes, verification, checkpoints, and landing then continue on the box.

Use ` + "`stint deep status`" + ` to inspect durable state and ` + "`stint deep resume`" + ` after an interruption. ` + "`stint deep onbox`" + ` is the box-side supervisor entry point; it is not a replacement for the launcher bootstrap.

Subcommands:
  start   local development/fixture coordinator
  status  show durable session phase and task evidence
  dash    open the Deep Work execution cockpit
  stop    request a graceful landing
  resume  resume a recoverable coordinator
  onbox   run or resume the on-box Hermes coordinator`,
		usage: "stint deep <start|status|dash|stop|resume|onbox> [flags]",
		args:  []cliArg{{name: "<subcommand>", purpose: "start, status, dash, stop, resume, or onbox"}},
		flags: []cliFlag{
			{name: "--mission", argument: "<file>", purpose: "mission Markdown file"},
			{name: "--repo", argument: "<path>", purpose: "target git repository"},
			{name: "--hours", argument: "<float>", defaultVal: "session deadline", purpose: "bounded session duration"},
			{name: "--task-timeout", argument: "<dur>", defaultVal: "10m", purpose: "maximum wall time per Hermes invocation"},
			{name: "--max-attempts", argument: "<int>", defaultVal: "3", purpose: "maximum attempts per task"},
			{name: "--reasoning", argument: "<none|low|medium|xhigh>", defaultVal: "medium", purpose: "Hermes reasoning effort; task metadata may override it"},
			{name: "--action-plan", argument: "<path>", purpose: "worktree-relative living action plan"},
			{name: "--provider", argument: "<id>", defaultVal: "custom:qwen-stint-{reasoning}", purpose: "configured Hermes provider or reasoning template"},
			{name: "--model", argument: "<id>", purpose: "Hermes model id"},
			{name: "--allow-command", argument: "<prefix>", purpose: "advisory command guidance; Hermes does not enforce it"},
			{name: "--json", defaultVal: "false", purpose: "status: print machine-readable state"},
			{name: "--session", argument: "<id>", defaultVal: "latest", purpose: "status/dashboard: session to inspect"},
			{name: "--ready-file", argument: "<path>", purpose: "onbox: write RUNNING only after preflight and durable state"},
			{name: "--resume", defaultVal: "false", purpose: "onbox: resume persisted execution settings"},
		},
		examples: []string{
			"scripts/launch-onbox-deep.sh --mission mission.md --repo /path/to/repo",
			"stint deep status --json",
			"stint deep dash",
		},
		notes: []string{
			"The on-box coordinator uses a Stint-owned worktree and persists session state under the configured Stint state directory.",
			"Independent verification and checkpoint persistence are coordinator decisions; a Hermes completion response alone is not VERIFIED.",
			"Command prefixes are advisory prompt guidance; Stint does not enforce Hermes tool execution.",
		},
	}

	cmdVersion = cliCommand{
		name:     "version",
		aliases:  []string{"--version", "-v"},
		section:  "reference",
		summary:  "print the Stint version",
		detail:   "Prints the Stint CLI version.",
		usage:    "stint version",
		examples: []string{"stint version"},
	}

	cmdHelp = cliCommand{
		name:     "help",
		aliases:  []string{"--help", "-h"},
		section:  "reference",
		summary:  "show this overview or per-command help",
		detail:   "With no argument, prints the command overview. With a command name, prints its flags, examples, and notes. Every command also accepts --help.",
		usage:    "stint help [command]",
		examples: []string{"stint help", "stint help start", "stint start --help"},
	}
)

var cliCommands = []cliCommand{cmdAuth, cmdSetup, cmdDoctor, cmdStatus, cmdOnboard, cmdPlan, cmdStart, cmdResume, cmdDown, cmdPerf, cmdDeep, cmdVersion, cmdHelp}

var helpSections = []helpSection{
	{title: "Setup & checks", commands: []cliCommand{cmdAuth, cmdSetup, cmdDoctor, cmdStatus, cmdOnboard}},
	{title: "Planning (read-only)", commands: []cliCommand{cmdPlan}},
	{title: "Compute (paid)", commands: []cliCommand{cmdStart, cmdResume, cmdDown}},
	{title: "Diagnostics", commands: []cliCommand{cmdPerf}},
	{title: "Deep Work", commands: []cliCommand{cmdDeep}},
	{title: "Reference", commands: []cliCommand{cmdVersion, cmdHelp}},
}

func findCommand(name string) (cliCommand, bool) {
	for _, cmd := range cliCommands {
		if cmd.name == name {
			return cmd, true
		}
		for _, alias := range cmd.aliases {
			if alias == name {
				return cmd, true
			}
		}
	}
	return cliCommand{}, false
}

func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func runHelp(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}
	if len(args) > 1 {
		return errors.New("usage: stint help <command>")
	}
	cmd, ok := findCommand(args[0])
	if !ok {
		return fmt.Errorf("unknown command %q (run 'stint help' to list commands)", args[0])
	}
	printCommandHelp(cmd.name)
	return nil
}

func printUsage() {
	var b strings.Builder
	b.WriteString("STINT — elastic compute for coding agents\n\n")
	b.WriteString("Stint rents a remote GPU, boots a model runtime, and tunnels it to a\n")
	b.WriteString("stable local OpenAI-compatible endpoint so coding agents keep working:\n\n")
	b.WriteString("    http://127.0.0.1:8409/v1   (model: qwen3.8-27b)\n\n")
	b.WriteString("QUICK START\n")
	b.WriteString("  stint auth vast                   store + verify your Vast API key\n")
	b.WriteString("  stint setup ssh                   create the dedicated Stint SSH keypair\n")
	b.WriteString("  stint doctor                      verify credentials, SSH key, OpenSSH, port 8409\n")
	b.WriteString("  stint plan interactive --hours 5  read-only plan; never rents\n")
	b.WriteString("  stint start interactive           rent, boot, tunnel; READY when live\n")
	b.WriteString("  stint down                        destroy compute and clear the session\n\n")
	for _, section := range helpSections {
		b.WriteString(section.title + "\n")
		for _, cmd := range section.commands {
			fmt.Fprintf(&b, "  %-8s  %s\n", cmd.name, cmd.summary)
		}
		b.WriteString("\n")
	}
	b.WriteString("EXAMPLES\n")
	b.WriteString("  stint start interactive --hours 2 --runtime ninfer\n")
	b.WriteString("  stint start interactive --runtime ninfer --ninfer-config native --clients 2\n")
	b.WriteString("  stint start interactive --yes --min-measured-download-mbps 50\n")
	b.WriteString("  stint plan interactive --hours 5 --json\n")
	b.WriteString("  stint status && stint perf --runs 5\n\n")
	b.WriteString("Run `stint help <command>` or `stint <command> --help` for full flags.\n")
	b.WriteString("Safety: plan never rents; start confirms cost before paying; compute is\n")
	b.WriteString("destroyed automatically at the session deadline; credentials stay local.\n")
	fmt.Print(b.String())
}

func printCommandHelp(name string) {
	cmd, ok := findCommand(name)
	if !ok {
		fmt.Printf("Unknown command %q\n", name)
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "STINT %s\n\n", strings.ToUpper(cmd.name))
	b.WriteString(cmd.detail + "\n\n")
	b.WriteString("USAGE\n  " + cmd.usage + "\n\n")
	if len(cmd.args) > 0 {
		b.WriteString("ARGS\n")
		width := 0
		for _, arg := range cmd.args {
			if len(arg.name) > width {
				width = len(arg.name)
			}
		}
		for _, arg := range cmd.args {
			fmt.Fprintf(&b, "  %-*s  %s\n", width, arg.name, arg.purpose)
		}
		b.WriteString("\n")
	}
	if len(cmd.flags) > 0 {
		b.WriteString("FLAGS\n")
		width := 0
		for _, f := range cmd.flags {
			if len(f.nameColumn()) > width {
				width = len(f.nameColumn())
			}
		}
		for _, f := range cmd.flags {
			b.WriteString(f.line(width) + "\n")
		}
		b.WriteString("\n")
	}
	if len(cmd.examples) > 0 {
		b.WriteString("EXAMPLES\n")
		for _, example := range cmd.examples {
			b.WriteString("  " + example + "\n")
		}
		b.WriteString("\n")
	}
	if len(cmd.notes) > 0 {
		b.WriteString("NOTES\n")
		for _, note := range cmd.notes {
			b.WriteString("  - " + note + "\n")
		}
	}
	b.WriteString("\nRun `stint help` for the full command overview.\n")
	fmt.Print(b.String())
}
