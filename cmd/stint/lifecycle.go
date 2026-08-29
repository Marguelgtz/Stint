package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/core"
	localenv "github.com/Marguelgtz/Stint/internal/local"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
	"github.com/Marguelgtz/Stint/internal/router"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const (
	clineRemotePort       = 8080
	interactiveImage      = "ghcr.io/ggml-org/llama.cpp:server-cuda"
	interactiveModelRef   = "ggml-org/Qwen3.8-27B-GGUF:Q4_K_M"
	interactiveModelAlias = "qwen3.8-27b"
	interactiveContext    = 16384
)

func runStart(args []string) error {
	if len(args) == 0 {
		return errors.New("start requires a profile: interactive")
	}
	profileName := args[0]
	if profileName != "interactive" {
		return errors.New("first live lifecycle supports only: stint start interactive")
	}
	profile, err := router.ResolveProfile(profileName)
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("start interactive", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	hoursValue := fs.String("hours", "1", "maximum paid session duration in hours")
	yes := fs.Bool("yes", false, "confirm the selected rental without prompting")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	hours, err := strconv.ParseFloat(*hoursValue, 64)
	if err != nil || hours <= 0 {
		return fmt.Errorf("invalid --hours value %q", *hoursValue)
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if existing, loadErr := sessionstate.Load(paths); loadErr == nil {
		return fmt.Errorf("session %d is already recorded (%s); run: stint status or stint down", existing.InstanceID, existing.Status)
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return loadErr
	}
	if !localenv.PortAvailable(clinePort) {
		return fmt.Errorf("local port %d is already in use", clinePort)
	}
	publicKey, _, err := localenv.EnsureSSHKey(paths)
	if err != nil {
		return err
	}
	credentials, err := config.LoadCredentials(paths)
	if err != nil {
		return errors.New("Vast credentials are not configured; run: stint auth vast")
	}
	client := vast.NewClient(credentials.Vast.APIKey)

	fmt.Println("Searching Vast for interactive compute...")
	searchCtx, searchCancel := context.WithTimeout(context.Background(), 35*time.Second)
	offers, err := client.SearchOffers(searchCtx, profile, vast.SearchOptions{
		Hours: hours, Limit: 250, StorageGB: profile.Session.StorageGB,
	})
	searchCancel()
	if err != nil {
		return err
	}
	plan, err := core.CreateSessionPlan(profile, hours, offers)
	if err != nil {
		return err
	}
	selected := plan.Workers[0].Offer

	fmt.Println()
	fmt.Println("READY TO RENT")
	fmt.Printf("GPU            %s\n", selected.GPUModel)
	fmt.Printf("Location       %s\n", valueOr(selected.Geolocation, "unknown"))
	fmt.Printf("Price          $%.3f/hr\n", selected.HourlyUSD)
	fmt.Printf("Duration cap   %.2fh\n", hours)
	fmt.Printf("Compute cap    $%.2f\n", plan.EstimatedTotalUSD)
	fmt.Printf("Model          %s\n", interactiveModelAlias)
	fmt.Printf("Context        %d tokens\n", interactiveContext)
	fmt.Printf("Cline endpoint http://127.0.0.1:%d/v1\n", clinePort)
	fmt.Println()
	if !*yes {
		confirmed, err := confirmRental()
		if err != nil {
			return err
		}
		if !confirmed {
			return errors.New("rental cancelled; no compute was rented")
		}
	}

	rootCtx, stop := signalContext()
	defer stop()
	startedAt := time.Now().UTC()
	deadline := startedAt.Add(time.Duration(hours * float64(time.Hour)))
	state := sessionstate.State{
		OfferID: selected.ID, Profile: profileName, GPUModel: selected.GPUModel,
		HourlyUSD: selected.HourlyUSD, Hours: hours, StartedAt: startedAt, Deadline: deadline,
		Status: "RENTING",
	}
	created := false
	ready := false
	defer func() {
		if created && !ready {
			killPID(state.TunnelPID)
			killPID(state.WatchdogPID)
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			_ = client.DestroyInstance(cleanupCtx, state.InstanceID)
			cancel()
			_ = sessionstate.Clear(paths)
		}
	}()

	fmt.Println("Renting selected offer...")
	instanceID, err := client.CreateInstance(rootCtx, selected.ID, vast.CreateInstanceOptions{
		Image:  interactiveImage,
		DiskGB: profile.Session.StorageGB,
		Label:  "stint-interactive",
	})
	if err != nil {
		return err
	}
	created = true
	state.InstanceID = instanceID
	state.Status = "BOOTING"
	if err := sessionstate.Save(paths, state); err != nil {
		return fmt.Errorf("instance %d was created but state persistence failed: %w", instanceID, err)
	}
	fmt.Printf("Instance       %d\n", instanceID)

	// Start the deadline watchdog immediately after persisting the paid resource.
	// If the foreground CLI or terminal dies later, the instance still has a local
	// best-effort auto-destroy process waiting on the session deadline.
	watchdogPID, err := spawnWatchdog(paths)
	if err != nil {
		return fmt.Errorf("start session watchdog: %w", err)
	}
	state.WatchdogPID = watchdogPID
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}

	if err := retryAttachSSHKey(rootCtx, client, instanceID, publicKey, 90*time.Second); err != nil {
		return err
	}
	// Vast SSH endpoints are ephemeral. Keep trust-on-first-use scoped to this paid
	// session so a later host reuse cannot collide with a stale host key.
	_ = os.Remove(filepath.Join(paths.StateDir, "known_hosts"))

	fmt.Println("Waiting for Vast SSH...")
	instance, err := waitForSSHMetadata(rootCtx, client, instanceID, 6*time.Minute)
	if err != nil {
		return err
	}
	state.SSHHost = instance.SSHHost
	state.SSHPort = instance.SSHPort
	state.Status = "SSH_CONNECTING"
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}
	if err := waitForSSH(rootCtx, paths, state, 4*time.Minute); err != nil {
		return err
	}
	state.Status = "MODEL_STARTING"
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}

	fmt.Println("Starting Qwen3.8-27B Q4_K_M on the remote GPU...")
	remoteCommand := fmt.Sprintf(
		"mkdir -p /workspace/stint; nohup /app/llama-server -hf %s --no-mmproj --alias %s --host 127.0.0.1 --port %d -ngl all -c %d -ctk q8_0 -ctv q8_0 --flash-attn on > /workspace/stint/llama.log 2>&1 < /dev/null &",
		interactiveModelRef, interactiveModelAlias, clineRemotePort, interactiveContext,
	)
	if _, err := runSSH(rootCtx, paths, state, remoteCommand); err != nil {
		return fmt.Errorf("start remote llama server: %w", err)
	}

	pid, err := startTunnel(paths, state)
	if err != nil {
		return err
	}
	state.TunnelPID = pid
	state.Status = "MODEL_LOADING"
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}

	fmt.Println("Downloading/loading ~19 GB model; waiting for the local OpenAI endpoint...")
	if err := waitForModel(rootCtx, 20*time.Minute); err != nil {
		return err
	}
	state.Status = "READY"
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}
	ready = true

	fmt.Println()
	fmt.Println("READY")
	fmt.Printf("GPU             %s\n", state.GPUModel)
	fmt.Printf("Price           $%.3f/hr\n", state.HourlyUSD)
	fmt.Printf("Instance        %d\n", state.InstanceID)
	fmt.Printf("Endpoint        http://127.0.0.1:%d/v1\n", clinePort)
	fmt.Printf("Model           %s\n", interactiveModelAlias)
	fmt.Printf("Auto-destroy    %s\n", state.Deadline.Local().Format(time.RFC1123))
	fmt.Println("\nCline can connect now. Run `stint down` when finished.")
	return nil
}

func runDown(args []string) error {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	state, err := sessionstate.Load(paths)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("No active Stint session is recorded.")
		return nil
	}
	if err != nil {
		return err
	}
	credentials, err := config.LoadCredentials(paths)
	if err != nil {
		return err
	}
	client := vast.NewClient(credentials.Vast.APIKey)
	killPID(state.TunnelPID)
	killPID(state.WatchdogPID)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	fmt.Printf("Destroying Vast instance %d...\n", state.InstanceID)
	if err := client.DestroyInstance(ctx, state.InstanceID); err != nil {
		return err
	}
	if err := sessionstate.Clear(paths); err != nil {
		return err
	}
	fmt.Println("Compute destroyed. Cline endpoint is offline.")
	return nil
}

func runWatchdog(args []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	state, err := sessionstate.Load(paths)
	if err != nil {
		return nil
	}
	if wait := time.Until(state.Deadline); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		<-timer.C
	}
	credentials, err := config.LoadCredentials(paths)
	if err != nil {
		return err
	}
	killPID(state.TunnelPID)
	client := vast.NewClient(credentials.Vast.APIKey)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := client.DestroyInstance(ctx, state.InstanceID); err != nil {
		return err
	}
	return sessionstate.Clear(paths)
}

func confirmRental() (bool, error) {
	fmt.Print("Rent this compute now? [y/N] ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
}

func retryAttachSSHKey(ctx context.Context, client *vast.Client, instanceID int64, publicKey string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if err := client.AttachSSHKey(ctx, instanceID, publicKey); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("attach Stint SSH key: %w", lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func waitForSSHMetadata(ctx context.Context, client *vast.Client, id int64, timeout time.Duration) (vast.Instance, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		instance, err := client.ShowInstance(ctx, id)
		if err == nil && strings.EqualFold(instance.ActualStatus, "running") && instance.SSHHost != "" && instance.SSHPort > 0 {
			return instance, nil
		}
		if err != nil {
			lastErr = err
		}
		if err == nil && instance.StatusMsg != "" && !strings.EqualFold(instance.ActualStatus, "running") {
			fmt.Printf("  Vast status: %s (%s)\n", instance.ActualStatus, instance.StatusMsg)
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return vast.Instance{}, fmt.Errorf("timed out waiting for SSH metadata: %w", lastErr)
			}
			return vast.Instance{}, errors.New("timed out waiting for Vast instance to become SSH-ready")
		}
		select {
		case <-ctx.Done():
			return vast.Instance{}, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func waitForSSH(ctx context.Context, paths config.Paths, state sessionstate.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := runSSH(ctx, paths, state, "echo stint-ssh-ready"); err == nil {
			fmt.Println("SSH             ready")
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for SSH connection")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func runSSH(ctx context.Context, paths config.Paths, state sessionstate.State, remoteCommand string) (string, error) {
	ssh, err := localenv.SSHExecutable()
	if err != nil {
		return "", err
	}
	knownHosts := filepath.Join(paths.StateDir, "known_hosts")
	args := []string{
		"-i", paths.SSHPrivateKey,
		"-p", strconv.Itoa(state.SSHPort),
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=" + knownHosts,
		"root@" + state.SSHHost,
		remoteCommand,
	}
	cmd := exec.CommandContext(ctx, ssh, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("ssh: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func startTunnel(paths config.Paths, state sessionstate.State) (int, error) {
	ssh, err := localenv.SSHExecutable()
	if err != nil {
		return 0, err
	}
	if err := paths.Ensure(); err != nil {
		return 0, err
	}
	logPath := filepath.Join(paths.StateDir, "tunnel.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	knownHosts := filepath.Join(paths.StateDir, "known_hosts")
	forward := fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", clinePort, clineRemotePort)
	args := []string{
		"-N",
		"-i", paths.SSHPrivateKey,
		"-p", strconv.Itoa(state.SSHPort),
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=" + knownHosts,
		"-L", forward,
		"root@" + state.SSHHost,
	}
	cmd := exec.Command(ssh, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return 0, fmt.Errorf("start SSH tunnel: %w", err)
	}
	pid := cmd.Process.Pid
	time.Sleep(1200 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		_ = cmd.Wait()
		_ = logFile.Close()
		return 0, fmt.Errorf("SSH tunnel exited during startup; inspect %s", logPath)
	}
	_ = cmd.Process.Release()
	_ = logFile.Close()
	return pid, nil
}

func waitForModel(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 4 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/models", clinePort)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for Qwen endpoint; startup cleanup will destroy the instance")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func spawnWatchdog(paths config.Paths) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	if err := paths.Ensure(); err != nil {
		return 0, err
	}
	logFile, err := os.OpenFile(filepath.Join(paths.StateDir, "watchdog.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(exe, "_watchdog")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	_ = logFile.Close()
	return pid, nil
}

func killPID(pid int) {
	if pid <= 0 {
		return
	}
	process, err := os.FindProcess(pid)
	if err == nil {
		_ = process.Signal(syscall.SIGTERM)
	}
}
