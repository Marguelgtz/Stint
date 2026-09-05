package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
	interactiveImage      = "vastai/base-image:@vastai-automatic-tag"
	interactiveModelRef   = "ggml-org/Qwen3.8-27B-GGUF:Q4_K_M"
	interactiveModelAlias = "qwen3.8-27b"
	interactiveContext    = 16384
	llamaCppRef           = "v0.3.0"
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
	location := fs.String("location", "", "prefer an offer whose location contains this text")
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
	if strings.TrimSpace(*location) != "" {
		offers, err = preferLocation(profile, offers, *location)
		if err != nil {
			return err
		}
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

	state.Status = "RUNTIME_BOOTSTRAP"
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}
	if err := bootstrapRemoteRuntime(rootCtx, paths, state); err != nil {
		return err
	}

	state.Status = "MODEL_STARTING"
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}
	if err := startRemoteModel(rootCtx, paths, state); err != nil {
		return err
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
	if err := waitForModel(rootCtx, paths, state, 20*time.Minute); err != nil {
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

func preferLocation(profile core.Profile, offers []core.Offer, needle string) ([]core.Offer, error) {
	needle = strings.ToLower(strings.TrimSpace(needle))
	matched := make([]core.Offer, 0)
	for _, offer := range offers {
		if strings.Contains(strings.ToLower(offer.Geolocation), needle) && core.Qualifies(profile, offer) {
			matched = append(matched, offer)
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("no qualifying interactive offers matched --location %q; rerun without --location or choose a current marketplace location", needle)
	}
	return matched, nil
}

func bootstrapRemoteRuntime(ctx context.Context, paths config.Paths, state sessionstate.State) error {
	fmt.Printf("Preparing llama.cpp %s CUDA runtime on the Vast base image...\n", llamaCppRef)
	fmt.Println("The first bootstrap compiles llama-server once on this disposable instance; build output follows.")
	command := fmt.Sprintf(`set -eu
mkdir -p /workspace/stint
if [ ! -x /workspace/stint/llama.cpp/build/bin/llama-server ]; then
  export DEBIAN_FRONTEND=noninteractive
  if ! command -v git >/dev/null 2>&1 || ! command -v cmake >/dev/null 2>&1 || ! command -v g++ >/dev/null 2>&1 || ! dpkg -s libcurl4-openssl-dev >/dev/null 2>&1; then
    apt-get update
    apt-get install -y --no-install-recommends git cmake build-essential libcurl4-openssl-dev ca-certificates
  fi
  rm -rf /workspace/stint/llama.cpp
  git clone --filter=blob:none --depth 1 --branch %s https://github.com/ggml-org/llama.cpp.git /workspace/stint/llama.cpp
  cmake -S /workspace/stint/llama.cpp -B /workspace/stint/llama.cpp/build \
    -DGGML_CUDA=ON \
    -DLLAMA_CURL=ON \
    -DBUILD_SHARED_LIBS=OFF \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_CUDA_ARCHITECTURES="86;89"
  cmake --build /workspace/stint/llama.cpp/build --config Release -j "$(nproc)" --target llama-server
fi
/workspace/stint/llama.cpp/build/bin/llama-server --version
`, llamaCppRef)
	if err := runSSHStreaming(ctx, paths, state, command); err != nil {
		return fmt.Errorf("bootstrap remote llama.cpp runtime: %w", err)
	}
	fmt.Println("llama.cpp runtime ready.")
	return nil
}

func startRemoteModel(ctx context.Context, paths config.Paths, state sessionstate.State) error {
	fmt.Println("Starting Qwen3.8-27B Q4_K_M on the remote GPU...")
	remoteCommand := fmt.Sprintf(
		"mkdir -p /workspace/stint; pkill -f '/workspace/stint/llama.cpp/build/bin/llama-server.*--port %d' >/dev/null 2>&1 || true; nohup /workspace/stint/llama.cpp/build/bin/llama-server -hf %s --no-mmproj --alias %s --host 127.0.0.1 --port %d -ngl all -c %d -ctk q8_0 -ctv q8_0 --flash-attn on > /workspace/stint/llama.log 2>&1 < /dev/null &",
		clineRemotePort, interactiveModelRef, interactiveModelAlias, clineRemotePort, interactiveContext,
	)
	if _, err := runSSH(ctx, paths, state, remoteCommand); err != nil {
		return fmt.Errorf("start remote llama server: %w", err)
	}
	return nil
}

func runDown(args []string) error {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	yes := fs.Bool("yes", false, "destroy without the interactive type-to-confirm")
	if err := fs.Parse(args); err != nil {
		return err
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
		fmt.Println("No active Stint session is recorded.")
		return nil
	}
	if err != nil {
		return err
	}
	if !*yes && !confirmDestroy(os.Stdin, os.Stdout, state) {
		return nil
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
	if err := waitForInstanceGone(ctx, instanceGoneProbe(client, state.InstanceID)); err != nil {
		// Vast accepted the destroy but the instance is still visible after
		// the verification window. Keep tracking the possibly-still-paid
		// instance instead of clearing state: `stint down` is idempotent, so
		// re-running it once the instance is gone is a safe no-op.
		fmt.Printf("Warning: destroy was accepted but instance %d is still visible on Vast.\n", state.InstanceID)
		fmt.Println("Local session state was kept. Check the Vast dashboard or run `stint down` again once the instance is gone.")
		state.LastError = fmt.Sprintf("destroy unverified: instance %d still visible on Vast", state.InstanceID)
		if err := sessionstate.Save(paths, state); err != nil {
			return err
		}
		return nil
	}
	if err := sessionstate.Clear(paths); err != nil {
		return err
	}
	fmt.Println("Compute destroyed. Cline endpoint is offline.")
	return nil
}

// destroyGonePollInterval is the gap between Vast show polls that verify a
// destroyed instance has actually disappeared before local state is cleared.
var destroyGonePollInterval = 2 * time.Second

// instanceGoneProbe adapts the Vast client to a probe that returns nil once
// the instance no longer exists (404/410) and an error while it is still
// visible or on transient API failures.
func instanceGoneProbe(client *vast.Client, instanceID int64) func(context.Context) error {
	return func(ctx context.Context) error {
		_, err := client.ShowInstance(ctx, instanceID)
		if err == nil {
			return errors.New("instance still visible")
		}
		var apiErr *vast.APIError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusGone) {
			return nil
		}
		return err
	}
}

// waitForInstanceGone polls show until it reports the instance as gone or
// the context expires. Transient API failures do not abort verification;
// an unconfirmed teardown returns an error so the caller keeps tracking
// the paid instance.
func waitForInstanceGone(ctx context.Context, show func(context.Context) error) error {
	for {
		if err := show(ctx); err == nil {
			return nil
		}
		timer := time.NewTimer(destroyGonePollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("instance not confirmed gone: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// confirmDestroy is the type-to-confirm gate before `stint down` destroys a
// paid instance: it summarizes what is about to be destroyed and requires
// the literal word "destroy" (the dashboard uses an equivalent modal). Any
// other input — including EOF on a non-interactive stdin — aborts with no
// side effects; unattended callers pass --yes to skip the gate.
func confirmDestroy(stdin io.Reader, out io.Writer, state sessionstate.State) bool {
	fmt.Fprintf(out, "Instance       %d\n", state.InstanceID)
	fmt.Fprintf(out, "Remaining      %s\n", formatSessionDuration(time.Until(state.Deadline)))
	fmt.Fprintln(out)
	fmt.Fprintln(out, "This immediately destroys the Vast instance, stops the tunnel and watchdog, and clears the local session state.")
	fmt.Fprint(out, "Type \"destroy\" to confirm: ")
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil {
		fmt.Fprintln(out, "\nConfirmation unavailable (no interactive input). Use --yes to destroy without confirmation.")
		return false
	}
	if strings.TrimSpace(line) != "destroy" {
		fmt.Fprintln(out, "Aborted: session left running.")
		return false
	}
	return true
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
	startedAt := time.Now()
	deadline := startedAt.Add(timeout)
	var lastErr error
	lastStatus := ""
	lastHeartbeat := time.Time{}
	for {
		instance, err := client.ShowInstance(ctx, id)
		if err == nil && strings.EqualFold(instance.ActualStatus, "running") && instance.SSHHost != "" && instance.SSHPort > 0 {
			fmt.Printf("  Vast %-12s SSH ready after %s\n", "running", formatWaitDuration(time.Since(startedAt)))
			return instance, nil
		}
		if err != nil {
			lastErr = err
		}

		status := "checking"
		if err != nil {
			status = "API retry"
		} else if strings.TrimSpace(instance.ActualStatus) != "" {
			status = strings.TrimSpace(instance.ActualStatus)
		}
		now := time.Now()
		if status != lastStatus || lastHeartbeat.IsZero() || now.Sub(lastHeartbeat) >= 10*time.Second {
			fmt.Printf("  Vast %-12s Waiting for SSH %s / %s\n", status, formatWaitDuration(now.Sub(startedAt)), formatWaitDuration(timeout))
			lastStatus = status
			lastHeartbeat = now
		}

		if now.After(deadline) {
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

func formatWaitDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int(d / time.Second)
	return fmt.Sprintf("%02d:%02d", totalSeconds/60, totalSeconds%60)
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
	args := sshArgs(paths, state, knownHosts, remoteCommand)
	cmd := exec.CommandContext(ctx, ssh, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("ssh: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func runSSHStreaming(ctx context.Context, paths config.Paths, state sessionstate.State, remoteCommand string) error {
	ssh, err := localenv.SSHExecutable()
	if err != nil {
		return err
	}
	knownHosts := filepath.Join(paths.StateDir, "known_hosts")
	args := sshArgs(paths, state, knownHosts, remoteCommand)
	cmd := exec.CommandContext(ctx, ssh, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh: %w", err)
	}
	return nil
}

func sshArgs(paths config.Paths, state sessionstate.State, knownHosts, remoteCommand string) []string {
	return []string{
		"-i", paths.SSHPrivateKey,
		"-p", strconv.Itoa(state.SSHPort),
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=" + knownHosts,
		"root@" + state.SSHHost,
		remoteCommand,
	}
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

func waitForModel(ctx context.Context, paths config.Paths, state sessionstate.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 4 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/models", clinePort)
	lastLog := ""
	lastLogCheck := time.Time{}
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
		if time.Since(lastLogCheck) >= 15*time.Second {
			lastLogCheck = time.Now()
			if tail, tailErr := runSSH(ctx, paths, state, remoteModelProgressCommandForState(state)); tailErr == nil {
				tail = strings.TrimSpace(tail)
				if tail != "" && tail != lastLog {
					if len(tail) > 300 {
						tail = tail[len(tail)-300:]
					}
					fmt.Printf("  model: %s\n", tail)
					lastLog = tail
				}
			}
		}
		if time.Now().After(deadline) {
			tail, _ := runSSH(context.Background(), paths, state, "tail -n 12 /workspace/stint/llama.log 2>/dev/null || true")
			if strings.TrimSpace(tail) != "" {
				return fmt.Errorf("timed out waiting for Qwen endpoint; remote llama log:\n%s", strings.TrimSpace(tail))
			}
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
