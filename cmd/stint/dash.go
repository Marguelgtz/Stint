package main

import (
	"fmt"
	"os"
)

// dash is the primary short-form entrypoint for the live session cockpit.
// `stint dashboard` remains a compatibility alias through dashboard.go.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "dash" {
		return
	}
	if wantsHelp(os.Args[2:]) {
		printCommandHelp("dash")
		os.Exit(0)
	}
	if err := runDashboard(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "stint:", err)
		os.Exit(1)
	}
	os.Exit(0)
}
