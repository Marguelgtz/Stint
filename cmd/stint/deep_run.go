package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/deep"
)

// worker ids for the supported Deep Work executor targets.
const (
	workerHermes      = "hermes"
	workerHermesOnBox = "hermes-onbox"
)

// deepRunConfig carries the per-session invocation settings from the CLI.
type deepRunConfig struct {
	worker          string // Hermes over SSH or co-located with the coordinator
	allowedCommands []string
	provider        string
	model           string
	reasoning       string
	actionPlan      string
	taskTimeout     time.Duration
	missionName     string
	taskCount       int
	// remote is the box SSH seam, set for the remote Hermes worker; nil for the
	// on-box Hermes worker.
	remote remoteCmd
	paths  config.Paths
}

// lookPath is a seam over exec.LookPath so preflight checks are testable.
func lookPath(name string) (string, error) { return exec.LookPath(name) }

// deepRunSession builds the coordinator and runs the loop until landing.
// resumed selects the banner verb: `start` reports "started", `resume`
// reports "resumed" for the same coordinator.
func deepRunSession(stateDir string, state *deep.DeepState, cfg *deepRunConfig, git gitOps, resumed bool) error {
	// Remote Hermes uses SSH; on-box Hermes uses local subprocesses on the
	// compute instance. Both share the same coordinator state machine.
	var exec executor
	verifyFn := func(ctx context.Context, command, workdir string) verificationResult {
		return runVerifyCmd(ctx, command, workdir)
	}
	switch cfg.worker {
	case workerHermes:
		exec = newHermesExecutor(cfg.remote)
		verifyFn = func(ctx context.Context, command, workdir string) verificationResult {
			return runVerifyCmdRemote(ctx, cfg.remote, command, workdir)
		}
	case workerHermesOnBox:
		exec = newLocalHermesExecutor("hermes")
	default:
		return fmt.Errorf("unsupported Deep Work worker %q; only Hermes is supported", cfg.worker)
	}
	coord := &deepCoordinator{
		stateDir: stateDir,
		state:    state,
		execCfg: execInput{
			allowedCommands: cfg.allowedCommands,
			provider:        cfg.provider,
			model:           cfg.model,
			reasoning:       cfg.reasoning,
			actionPlan:      cfg.actionPlan,
		},
		executor:    exec,
		now:         func() time.Time { return time.Now().UTC() },
		taskTimeout: cfg.taskTimeout,
		verify:      verifyFn,
		logf:        func(format string, args ...any) { deep.AppendLog(stateDir, *state, format, args...) },
		out:         os.Stdout,
		git:         git,
	}
	if cfg.worker == workerHermes {
		// The mission-level check at landing runs in the on-box worktree.
		coord.finalVerify = func(ctx context.Context, command string) verificationResult {
			return runVerifyCmdRemote(ctx, cfg.remote, command, state.WorktreePath)
		}
		// The worktree handoff file must be written on the box too. It is a
		// generated summary and stays outside the product checkpoint unless it
		// is already part of Git-visible repository state.
		coord.worktreeWrite = func(path string, data []byte) error {
			return writeRemoteFile(cfg.remote, path, data)
		}
	}

	// The pid file marks this process as the session's coordinator: `deep
	// resume` probes it and refuses to start a second coordinator. A crash
	// leaves a stale file, which resume tolerates via the liveness probe.
	if err := deep.WriteCoordinatorPid(stateDir, state.SessionID, os.Getpid()); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: write coordinator pid: %v\n", err)
	}
	defer deep.ClearCoordinatorPid(stateDir, state.SessionID)

	verb := "started"
	if resumed {
		verb = "resumed"
	}
	fmt.Printf("Deep Work session %s %s.\n", state.SessionID, verb)
	fmt.Printf("  mission:   %s (%d tasks)\n", cfg.missionName, cfg.taskCount)
	if cfg.worker == workerHermes {
		fmt.Printf("  worker:    hermes on the compute box (model %s via http://127.0.0.1:8080/v1 on the box)\n", cfg.model)
	} else if cfg.worker == workerHermesOnBox {
		fmt.Printf("  worker:    hermes local to the compute box (model %s via http://127.0.0.1:8080/v1)\n", cfg.model)
	} else {
		fmt.Printf("  model:     %s via http://127.0.0.1:8080/v1\n", cfg.model)
	}
	fmt.Printf("  worktree:  %s (branch %s)\n", state.WorktreePath, state.Branch)
	fmt.Printf("  deadline:  %s  (lands from %s)\n", state.Deadline.Format(time.RFC3339), state.LandBefore.Format(time.RFC3339))
	if cfg.worker == workerHermesOnBox {
		fmt.Println("  the on-box supervisor owns this process; the operator machine may disconnect.")
	} else {
		fmt.Println("  the coordinator runs in this process; keep this machine awake.")
	}

	// Record the execution policy this coordinator runs under: the incident
	// log starts with what the operator permitted, so the audit trail is
	// complete even if the process dies before any invocation.
	policy := fmt.Sprintf("worker=%s", cfg.worker)
	if len(cfg.allowedCommands) > 0 {
		policy += " advisoryCommands=[" + strings.Join(cfg.allowedCommands, ", ") + "]"
	} else {
		policy += " StintCommandRestrictions=none"
	}
	deep.AppendIncident(stateDir, *state, deep.IncidentPolicy, "", policy)

	if err := coord.run(context.Background()); err != nil {
		return err
	}
	fmt.Println("Deep Work session landed.")
	if state.HandoffPath != "" {
		fmt.Printf("  handoff:  %s\n", state.HandoffPath)
	}
	fmt.Println("  inspect:  stint deep status")
	return nil
}

// landingDeadline is where the coordinator must have landed: a fixed 10
// minutes before the deadline, or a quarter of shorter windows. These are
// first-draft constants to calibrate from live runs (DWX-010).
func landingDeadline(deadline, now time.Time) time.Time {
	dur := deadline.Sub(now)
	window := 10 * time.Minute
	if dur < 40*time.Minute {
		window = dur / 4
		if window < 2*time.Minute {
			window = 2 * time.Minute
		}
	}
	return deadline.Add(-window)
}
