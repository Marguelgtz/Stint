package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func runResume(args []string) (retErr error) {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("resume does not take positional arguments")
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	releaseLifecycle, err := acquireLifecycleLock(paths)
	if err != nil {
		return err
	}
	defer releaseLifecycle()
	state, err := sessionstate.Load(paths)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("no resumable Stint session is recorded")
	}
	if err != nil {
		return err
	}
	if state.Profile != "interactive" {
		return fmt.Errorf("resume currently supports interactive sessions only, got %q", state.Profile)
	}

	credentials, err := config.LoadCredentials(paths)
	if err != nil {
		return errors.New("Vast credentials are not configured; run: stint auth vast")
	}
	client := vast.NewClient(credentials.Vast.APIKey)
	rootCtx, stop := signalContext()
	defer stop()

	preserve := true
	ready := false
	defer func() {
		if ready || !preserve {
			return
		}
		killPID(state.TunnelPID)
		state.TunnelPID = 0
		state.Status = sessionstate.StatusRecoverable
		if retErr != nil {
			state.LastError = retErr.Error()
		}
		_ = sessionstate.Save(paths, state)
		if retErr != nil {
			fmt.Fprintf(os.Stderr, "\nPaid instance %d remains resumable at %s. Run: stint resume\n", state.InstanceID, valueOr(state.Checkpoint, state.Status))
		}
	}()

	if !state.Deadline.IsZero() && !time.Now().Before(state.Deadline) {
		killPID(state.TunnelPID)
		killPID(state.WatchdogPID)
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		destroyErr := client.DestroyInstance(ctx, state.InstanceID)
		cancel()
		if destroyErr != nil {
			return fmt.Errorf("session deadline passed and cleanup failed: %w", destroyErr)
		}
		preserve = false
		if err := sessionstate.Clear(paths); err != nil {
			return err
		}
		return errors.New("session deadline has passed; compute was destroyed")
	}

	if localModelReady(rootCtx, 1500*time.Millisecond) {
		state.Status = sessionstate.StatusReady
		state.Checkpoint = sessionstate.CheckpointReady
		state.LastError = ""
		if err := sessionstate.Save(paths, state); err != nil {
			return err
		}
		ready = true
		printReadySession(state)
		return nil
	}

	if state.TunnelPID > 0 {
		killPID(state.TunnelPID)
		state.TunnelPID = 0
		fmt.Printf("Stopping stale local tunnel on port %d...\n", clinePort)
		if err := waitForPortAvailable(rootCtx, clinePort, resumePortReleaseTimeout); err != nil {
			return fmt.Errorf("%w; stop the process using it, then run: stint resume", err)
		}
	} else if err := waitForPortAvailable(rootCtx, clinePort, 250*time.Millisecond); err != nil {
		closed, closeErr := closeLegacyStintControlMasters(rootCtx)
		if closeErr != nil {
			return closeErr
		}
		if !closed {
			return fmt.Errorf("local port %d is already in use by another process; stop it, then run: stint resume", clinePort)
		}
		fmt.Printf("Stopping orphaned Stint SSH tunnel on port %d...\n", clinePort)
		if err := waitForPortAvailable(rootCtx, clinePort, resumePortReleaseTimeout); err != nil {
			return fmt.Errorf("%w; stop the process using it, then run: stint resume", err)
		}
	}

	if err := ensureWatchdogAlive(paths, &state); err != nil {
		return fmt.Errorf("ensure session watchdog: %w", err)
	}

	publicKey, _, err := ensureSSHKeyForResume(paths)
	if err != nil {
		return err
	}

	fmt.Printf("Resuming Vast instance %d from %s...\n", state.InstanceID, valueOr(state.Checkpoint, state.Status))
	instance, err := client.ShowInstance(rootCtx, state.InstanceID)
	if err != nil {
		var apiErr *vast.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			preserve = false
			_ = sessionstate.Clear(paths)
			return errors.New("the recorded Vast instance no longer exists; cleared local session state")
		}
		return err
	}

	if !strings.EqualFold(instance.ActualStatus, "running") || instance.SSHHost == "" || instance.SSHPort <= 0 {
		fmt.Println("Waiting for Vast SSH metadata...")
		instance, err = waitForSSHMetadata(rootCtx, client, state.InstanceID, providerStartupTimeout)
		if err != nil {
			return err
		}
	}

	if state.SSHHost != instance.SSHHost || state.SSHPort != instance.SSHPort {
		_ = os.Remove(filepath.Join(paths.StateDir, "known_hosts"))
	}
	state.SSHHost = instance.SSHHost
	state.SSHPort = instance.SSHPort
	state.Status = sessionstate.StatusSSHConnecting
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}
	if err := retryAttachSSHKey(rootCtx, client, state.InstanceID, publicKey, 90*time.Second); err != nil {
		return err
	}
	fmt.Println("SSH key         attached")
	if err := waitForSSHResponsive(rootCtx, paths, state, 4*time.Minute); err != nil {
		return err
	}
	state.Status = sessionstate.StatusSSHReady
	if state.Checkpoint == "" || state.Checkpoint == sessionstate.CheckpointInstanceCreated {
		state.Checkpoint = sessionstate.CheckpointSSHReady
	}
	state.LastError = ""
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}

	runtimeReady, err := remoteRuntimeReady(rootCtx, paths, state)
	if err != nil {
		return err
	}
	if !runtimeReady {
		state.Status = sessionstate.StatusRuntimeBootstrap
		if err := sessionstate.Save(paths, state); err != nil {
			return err
		}
		actualRuntime, err := bootstrapSelectedRuntime(rootCtx, paths, state)
		if err != nil {
			return err
		}
		if actualRuntime != state.Runtime {
			state.Runtime = actualRuntime
			state.ContextTokens = contextForRuntime(actualRuntime)
			fmt.Printf("Runtime        %s\n", state.Runtime)
			fmt.Printf("Context        %d tokens\n", state.ContextTokens)
		}
	}
	state.Status = sessionstate.StatusRuntimeReady
	state.Checkpoint = sessionstate.CheckpointRuntimeReady
	state.LastError = ""
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}

	pid, err := startTunnel(paths, state)
	if err != nil {
		return err
	}
	state.TunnelPID = pid
	state.Status = sessionstate.StatusModelLoading
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}

	if localModelReady(rootCtx, 2*time.Second) {
		state.Status = sessionstate.StatusReady
		state.Checkpoint = sessionstate.CheckpointReady
		state.LastError = ""
		if err := sessionstate.Save(paths, state); err != nil {
			return err
		}
		ready = true
		printReadySession(state)
		return nil
	}

	modelRunning, err := remoteModelRunning(rootCtx, paths, state)
	if err != nil {
		return err
	}
	if !modelRunning {
		state.Status = sessionstate.StatusModelStarting
		if err := sessionstate.Save(paths, state); err != nil {
			return err
		}
		if err := startRemoteModelSafe(rootCtx, paths, state); err != nil {
			return err
		}
		state.Status = sessionstate.StatusModelStarted
		state.Checkpoint = sessionstate.CheckpointModelStarted
		state.LastError = ""
		if err := sessionstate.Save(paths, state); err != nil {
			return err
		}
	} else {
		fmt.Printf("Remote %s model server is still running; continuing model load.\n", runtimeForState(state))
	}

	state.Status = sessionstate.StatusModelLoading
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}
	fmt.Println("Waiting for Qwen3.8-27B to become ready...")
	if err := waitForModel(rootCtx, paths, state, 20*time.Minute); err != nil {
		return err
	}

	state.Status = sessionstate.StatusReady
	state.Checkpoint = sessionstate.CheckpointReady
	state.LastError = ""
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}
	ready = true
	printReadySession(state)
	return nil
}

func ensureSSHKeyForResume(paths config.Paths) (string, bool, error) {
	return ensureLocalSSHKey(paths)
}

func remoteRuntimeReady(ctx context.Context, paths config.Paths, state sessionstate.State) (bool, error) {
	out, err := runSSH(ctx, paths, state, selectedRuntimeReadyCommand(state))
	if err != nil {
		return false, fmt.Errorf("check remote runtime: %w", err)
	}
	return strings.TrimSpace(out) == "ready", nil
}

func remoteModelRunning(ctx context.Context, paths config.Paths, state sessionstate.State) (bool, error) {
	processName := selectedModelProcessName(state)
	command := fmt.Sprintf(`pid="$(cat /workspace/stint/llama.pid 2>/dev/null || true)"; if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then echo running; elif command -v pgrep >/dev/null 2>&1 && pgrep -x %s >/dev/null 2>&1; then echo running; else echo stopped; fi`, processName)
	out, err := runSSH(ctx, paths, state, command)
	if err != nil {
		return false, fmt.Errorf("check remote model process: %w", err)
	}
	return strings.TrimSpace(out) == "running", nil
}

func localModelReady(ctx context.Context, timeout time.Duration) bool {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/v1/models", clinePort), nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func ensureWatchdogAlive(paths config.Paths, state *sessionstate.State) error {
	if processAlive(state.WatchdogPID) {
		return nil
	}
	pid, err := spawnWatchdog(paths)
	if err != nil {
		return err
	}
	state.WatchdogPID = pid
	return sessionstate.Save(paths, *state)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func printReadySession(state sessionstate.State) {
	fmt.Println()
	fmt.Println("READY")
	fmt.Printf("GPU             %s\n", state.GPUModel)
	fmt.Printf("Price           $%.3f/hr\n", state.HourlyUSD)
	fmt.Printf("Instance        %d\n", state.InstanceID)
	fmt.Printf("Endpoint        http://127.0.0.1:%d/v1\n", clinePort)
	fmt.Printf("Model           %s\n", interactiveModelAlias)
	fmt.Printf("Runtime         %s\n", runtimeForState(state))
	fmt.Printf("Context         %d tokens\n", contextForState(state))
	fmt.Printf("Auto-destroy    %s\n", state.Deadline.Local().Format(time.RFC1123))
	fmt.Println("\nCline can connect now. Run `stint down` when finished.")
}
