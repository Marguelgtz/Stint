package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/deep"
)

// runDeepStatus prints the latest (or given) Deep Work session: phase,
// deadline, and task table. --json prints the durable state verbatim.
func runDeepStatus(args []string) error {
	fs := flag.NewFlagSet("deep status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	sessionID := fs.String("session", "", "session id (default: latest)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	var state deep.DeepState
	if *sessionID != "" {
		state, err = deep.LoadState(paths.StateDir, *sessionID)
	} else {
		state, err = deep.LoadLatestState(paths.StateDir)
	}
	if err != nil {
		return err
	}

	if *jsonOut {
		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	remaining := time.Until(state.Deadline)
	if remaining < 0 {
		remaining = 0
	}
	fmt.Printf("Deep Work %s — %s (%s)\n", state.SessionID, state.MissionName, state.Phase)
	fmt.Printf("  deadline: %s (%s remaining)\n", state.Deadline.Format(time.RFC3339), remaining.Round(time.Minute))
	fmt.Printf("  worktree: %s (branch %s)\n", state.WorktreePath, state.Branch)
	if alive, pid := deep.CoordinatorAlive(paths.StateDir, state.SessionID); alive {
		fmt.Printf("  coordinator: running (pid %d)\n", pid)
	} else {
		fmt.Printf("  coordinator: not running (continue with `stint deep resume`)\n")
	}
	if state.HandoffPath != "" {
		fmt.Printf("  handoff:  %s\n", state.HandoffPath)
	}
	fmt.Println()
	fmt.Printf("  %-10s %-10s %-4s %s\n", "TASK", "STATUS", "ATT", "OBJECTIVE")
	for _, t := range state.Tasks {
		obj := t.Objective
		if len(obj) > 52 {
			obj = obj[:49] + "..."
		}
		fmt.Printf("  %-10s %-10s %-4d %s\n", t.ID, t.Status, t.Attempts, obj)
		if t.Blocker != "" {
			fmt.Printf("  %-10s %-10s %-4s  blocker: %s\n", t.ID, "", "", t.Blocker)
		}
	}
	return nil
}

// runDeepStop lands the latest session from durable state. It works whether
// or not the start process is alive: if the coordinator is still running it
// observes the phase change on its next loop iteration; if the coordinator
// was lost, this recovers by landing from state (the handoff is truthful
// about what persisted). Stopping never touches compute.
func runDeepStop(args []string) error {
	fs := flag.NewFlagSet("deep stop", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	state, err := deep.LoadLatestState(paths.StateDir)
	if err != nil {
		return err
	}
	switch state.Phase {
	case deep.PhaseLanded, deep.PhaseStopped:
		fmt.Printf("Deep Work %s already %s.\n", state.SessionID, state.Phase)
		if state.HandoffPath != "" {
			fmt.Printf("  handoff: %s\n", state.HandoffPath)
		}
		return nil
	}

	coord := &deepCoordinator{
		stateDir:    paths.StateDir,
		state:       &state,
		taskTimeout: time.Minute,
		verify: func(ctx context.Context, workdir string) (string, bool, error) {
			return runVerifyCmd(ctx, state.Verify, workdir)
		},
		logf: func(format string, args ...any) { deep.AppendLog(paths.StateDir, state, format, args...) },
		out:  os.Stdout,
		git:  newGitRunner(),
	}
	return coord.land(context.Background(), "stopped by user")
}
