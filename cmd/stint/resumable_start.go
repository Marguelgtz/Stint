package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/core"
	localenv "github.com/Marguelgtz/Stint/internal/local"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
	"github.com/Marguelgtz/Stint/internal/router"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const providerStartupTimeout = 12 * time.Minute

// runStartResumable is the paid interactive start path with explicit recovery
// checkpoints. Once Vast has created the paid instance, later startup failures
// preserve it and leave the deadline watchdog running so `stint resume` can
// continue the same session rather than silently discarding already-provisioned
// work or forcing another rental.
func runStartResumable(args []string) (retErr error) {
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
	contextTokens := fs.Int("context", interactiveContext, "model context window in tokens")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	hours, err := strconv.ParseFloat(*hoursValue, 64)
	if err != nil || hours <= 0 {
		return fmt.Errorf("invalid --hours value %q", *hoursValue)
	}
	if err := validateInteractiveContext(*contextTokens); err != nil {
		return err
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if existing, loadErr := sessionstate.Load(paths); loadErr == nil {
		next := "run: stint status or stint down"
		if existing.Status == sessionstate.StatusRecoverable || checkpointIsRecoverable(existing.Checkpoint) {
			next = "run: stint resume or stint down"
		}
		return fmt.Errorf("session %d is already recorded (%s); %s", existing.InstanceID, existing.Status, next)
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
	fmt.Printf("Context        %d tokens\n", *contextTokens)
	fmt.Printf("Max output     %d tokens\n", interactiveMaxOutput)
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
		RuntimeContext: *contextTokens,
		HourlyUSD: selected.HourlyUSD, Hours: hours, StartedAt: startedAt, Deadline: deadline,
		Status: sessionstate.StatusRenting,
	}
	created := false
	ready := false
	defer func() {
		if !created || ready {
			return
		}

		killPID(state.TunnelPID)
		state.TunnelPID = 0
		if checkpointIsRecoverable(state.Checkpoint) {
			state.Status = sessionstate.StatusRecoverable
			if retErr != nil {
				state.LastError = retErr.Error()
			}
			if saveErr := sessionstate.Save(paths, state); saveErr != nil {
				fmt.Fprintf(os.Stderr, "stint: preserve session state: %v\n", saveErr)
			}
			fmt.Fprintf(os.Stderr, "\nPaid instance %d preserved at %s. Run: stint resume\n", state.InstanceID, state.Checkpoint)
			return
		}

		killPID(state.WatchdogPID)
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		if destroyErr := client.DestroyInstance(cleanupCtx, state.InstanceID); destroyErr != nil {
			fmt.Fprintf(os.Stderr, "stint: cleanup instance %d: %v\n", state.InstanceID, destroyErr)
		}
		cancel()
		_ = sessionstate.Clear(paths)
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
	state.Status = sessionstate.StatusBooting
	state.Checkpoint = sessionstate.CheckpointInstanceCreated
	if err := sessionstate.Save(paths, state); err != nil {
		return fmt.Errorf("instance %d was created but state persistence failed: %w", instanceID, err)
	}
	fmt.Printf("Instance       %d\n", instanceID)

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
	_ = os.Remove(filepath.Join(paths.StateDir, "known_hosts"))

	fmt.Println("Waiting for Vast SSH...")
	instance, err := waitForSSHMetadata(rootCtx, client, instanceID, providerStartupTimeout)
	if err != nil {
		return err
	}
	state.SSHHost = instance.SSHHost
	state.SSHPort = instance.SSHPort
	state.Status = sessionstate.StatusSSHConnecting
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}
	if err := waitForSSH(rootCtx, paths, state, 4*time.Minute); err != nil {
		return err
	}
	state.Status = sessionstate.StatusSSHReady
	state.Checkpoint = sessionstate.CheckpointSSHReady
	state.LastError = ""
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}

	state.Status = sessionstate.StatusRuntimeBootstrap
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}
	if err := bootstrapRemoteRuntime(rootCtx, paths, state); err != nil {
		return err
	}
	state.Status = sessionstate.StatusRuntimeReady
	state.Checkpoint = sessionstate.CheckpointRuntimeReady
	state.LastError = ""
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}

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

	pid, err := startTunnel(paths, state)
	if err != nil {
		return err
	}
	state.TunnelPID = pid
	state.Status = sessionstate.StatusModelLoading
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}

	fmt.Println("Downloading/loading ~19 GB model; waiting for the local OpenAI endpoint...")
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

func checkpointIsRecoverable(checkpoint string) bool {
	switch checkpoint {
	case sessionstate.CheckpointInstanceCreated,
		sessionstate.CheckpointSSHReady,
		sessionstate.CheckpointRuntimeReady,
		sessionstate.CheckpointModelStarted,
		sessionstate.CheckpointReady:
		return true
	default:
		return false
	}
}

func startRemoteModelSafe(ctx context.Context, paths config.Paths, state sessionstate.State) error {
	fmt.Println("Starting Qwen3.8-27B Q4_K_M on the remote GPU...")
	remoteCommand := remoteModelLaunchCommand(effectiveInteractiveContext(state.RuntimeContext))
	if _, err := runSSH(ctx, paths, state, remoteCommand); err != nil {
		return fmt.Errorf("start remote llama server: %w", err)
	}
	return nil
}

func remoteModelLaunchCommand(contextTokens int) string {
	return fmt.Sprintf(`set -eu
mkdir -p /workspace/stint
pid_file=/workspace/stint/llama.pid
log_file=/workspace/stint/llama.log

# This instance is dedicated to Stint. Stop an older llama-server by its exact
# process name rather than pkill -f; the latter can match and kill this SSH shell.
if command -v pgrep >/dev/null 2>&1; then
  for old_pid in $(pgrep -x llama-server 2>/dev/null || true); do
    kill "$old_pid" 2>/dev/null || true
  done
fi

if [ -r "$pid_file" ]; then
  old_pid="$(cat "$pid_file" 2>/dev/null || true)"
  if [ -n "$old_pid" ] && kill -0 "$old_pid" 2>/dev/null; then
    kill "$old_pid" 2>/dev/null || true
  fi
fi
rm -f "$pid_file"
: > "$log_file"

nohup /workspace/stint/llama.cpp/build/bin/llama-server \
  -hf %s \
  --no-mmproj \
  --alias %s \
  --host 127.0.0.1 \
  --port %d \
  -ngl all \
  -c %d \
  -ctk q8_0 \
  -ctv q8_0 \
  --flash-attn on \
  > "$log_file" 2>&1 < /dev/null &
new_pid=$!
printf '%%s\n' "$new_pid" > "$pid_file"
sleep 1
if ! kill -0 "$new_pid" 2>/dev/null; then
  tail -n 20 "$log_file" >&2 || true
  exit 1
fi
`, interactiveModelRef, interactiveModelAlias, clineRemotePort, contextTokens)
}
