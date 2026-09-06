package main

func init() {
	cmdDoctor.summary = "check prerequisites or diagnose the active paid session"
	cmdDoctor.detail = "With no active session, verifies the local/Vast prerequisites needed for paid start. With a recorded session, Doctor switches to active-session mode and checks provider state, lifecycle ownership, SSH/runtime health, tunnel, local endpoint, and deadline watchdog, then emits one root-cause diagnosis and recovery action."
	cmdDoctor.usage = "stint doctor [--json] [--last]"
	cmdDoctor.flags = []cliFlag{
		{name: "--json", defaultVal: "false", purpose: "print the diagnostic report as machine-readable JSON"},
		{name: "--last", defaultVal: "false", purpose: "inspect the most recently archived session instead of the active session"},
	}
	cmdDoctor.examples = []string{"stint doctor", "stint doctor --json", "stint doctor --last"}
	cmdDoctor.notes = []string{
		"Doctor is read-only: it does not rent, resume, destroy, or mutate provider compute.",
		"When a paid session is active, Doctor reports session reality instead of preflight readiness.",
	}
}
