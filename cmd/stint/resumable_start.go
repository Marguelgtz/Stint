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
	runtimeValue := fs.String("runtime", runtimeAuto, "inference runtime: auto, ninfer, or llama.cpp")
	contextValue := fs.String("context", "", "llama.cpp context tokens (1024-131072; default 16384)")
	ninferConfigValue := fs.String("ninfer-config", ninferConfigCoding, "NInfer config: coding, precision, or native")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	hours, err := strconv.ParseFloat(*hoursValue, 64)
	if err != nil || hours <= 0 {
		return fmt.Errorf("invalid --hours value %q", *hoursValue)
	}
	runtimeRequest, err := normalizeRuntime(*runtimeValue)
	if err != nil {
		return err
	}
	requestedNInferConfig, err := resolveNInferConfig(*ninferConfigValue)
	if err != nil {
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
	searchProfile, searchOptions := prepareVastSearchForRuntime(profile, vast.SearchOptions{
		Hours: hours, Limit: 250, StorageGB: profile.Session.StorageGB,
	}, runtimeRequest)
	profile = searchProfile
	searchCtx, searchCancel := context.WithTimeout(context.Background(), 35*time.Second)
	offers, err := client.SearchOffers(searchCtx, profile, searchOptions)
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
	selectedRuntime, err := selectInteractiveRuntime(runtimeRequest, selected.GPUModel)
	if err != nil {
		return err
	}
	selectedContext := contextForRuntime(selectedRuntime)
	if strings.TrimSpace(*contextValue) != "" {
		if selectedRuntime != runtimeLlamaCpp {
			return errors.New("--context is supported only with llama.cpp; use --ninfer-config for NInfer context profiles")
		}
		selectedContext, err = resolveLlamaContext(*contextValue)
		if err != nil {
			return err
		}
	}
	if selectedRuntime == runtimeNInfer {
		selectedContext = requestedNInferConfig.ContextTokens
	}

	fmt.Println()
	fmt.Println("READY TO RENT")
	fmt.Printf("GPU            %s\n", selected.GPUModel)
	fmt.Printf("Location       %s\n", valueOr(selected.Geolocation, "unknown"))
	fmt.Printf("Price          $%.3f/hr\n", selected.HourlyUSD)
	fmt.Printf("Duration cap   %.2fh\n", hours)
	fmt.Printf("Compute cap    $%.2f\n", plan.EstimatedTotalUSD)
	fmt.Printf("Model          %s\n", interactiveModelAlias)
	fmt.Printf("Runtime        %s", selectedRuntime)
	if runtimeRequest == runtimeAuto {
		fmt.Print(" (auto)")
	}
	fmt.Println()
	if selectedRuntime == runtimeNInfer {
		fmt.Printf("NInfer config  %s (%s)\n", requestedNInferConfig.Name, requestedNInferConfig.Description)
	}
	fmt.Printf("Context        %d tokens\n", selectedContext)
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
		RuntimeRequest: runtimeRequest, Runtime: selectedRuntime, ContextTokens: selectedContext,
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
		Image:  vastImageForRuntime(selectedRuntime),
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

	fmt.Println("Downloading/loading Qwen3.8-27B; waiting for the local OpenAI endpoint...")
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
	fmt.Printf("Starting Qwen3.8-27B with %s on the remote GPU...\n", runtimeForState(state))
	remoteCommand := remoteModelLaunchCommandForState(state)
	if _, err := runSSH(ctx, paths, state, remoteCommand); err != nil {
		return fmt.Errorf("start remote %s model server: %w", runtimeForState(state), err)
	}
	return nil
}

// remoteModelLaunchCommand is retained for the existing llama.cpp regression
// test while the active lifecycle uses remoteModelLaunchCommandForState.
func remoteModelLaunchCommand() string {
	return llamaModelLaunchCommand(interactiveContext)
}
