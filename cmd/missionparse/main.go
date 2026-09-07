package main

import (
	"fmt"
	"os"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: missionparse <mission.md>")
		os.Exit(2)
	}
	m, err := deep.ParseMissionFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "PARSE ERROR:", err)
		os.Exit(1)
	}
	fmt.Printf("name:      %s\n", m.Name)
	fmt.Printf("objective: %d chars\n", len(m.Objective))
	fmt.Printf("success:   %d bullets\n", len(m.Success))
	fmt.Printf("constraints: %d bullets\n", len(m.Constraints))
	fmt.Printf("verify:    %q\n", m.Verify)
	fmt.Printf("tasks:     %d\n", len(m.Tasks))
	for _, t := range m.Tasks {
		hasVerify := "verify:"
		if t.Verify == "" {
			hasVerify = "NO-VERIFY (falls back to mission-level!)"
		}
		fmt.Printf("  [%s] %s\n    verify(%s): %s\n", t.ID, t.Objective, hasVerify, t.Verify)
	}
}