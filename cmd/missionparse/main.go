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
	fmt.Printf("github:    mode=%s repository=%s base=%s approval=%s\n", m.GitHub.Mode, m.GitHub.Repository, m.GitHub.Base, m.GitHub.Approval)
	fmt.Printf("completion: %s\n", m.Completion)
	fmt.Printf("tasks:     %d\n", len(m.Tasks))
	for _, t := range m.Tasks {
		hasVerify := "verify:"
		if t.Verify == "" {
			hasVerify = "NO-VERIFY (falls back to mission-level!)"
		}
		fmt.Printf("  [%s] phase=%s %s\n    verify(%s): %s\n", t.ID, t.Phase, t.Objective, hasVerify, t.Verify)
	}
}
