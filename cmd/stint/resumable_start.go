package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
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

const providerStartupTimeout = 6 * time.Minute

// candidateWithinSessionBudget repeats the requested-duration ceiling at the
// rental boundary. The initial plan does not cover later network-ranked
// fallback candidates.
func candidateWithinSessionBudget(profile core.Profile, candidate core.Offer, hours float64) bool {
	if profile.Session.MaxCostUSD <= 0 {
		return true
	}
	if candidate.HourlyUSD <= 0 || hours <= 0 {
		return false
	}
	return estimatedCandidateSessionCost(candidate, hours) <= profile.Session.MaxCostUSD
}

func estimatedCandidateSessionCost(candidate core.Offer, hours float64) float64 {
	return math.Round(candidate.HourlyUSD*hours*100) / 100
}

// applySessionCostCeiling lets a caller tighten an existing profile limit for
// one start invocation. A command-line value must never expand configured
// spending policy.
func applySessionCostCeiling(profile core.Profile, requested float64) (core.Profile, error) {
	if math.IsNaN(requested) || math.IsInf(requested, 0) || requested <= 0 {
		return profile, fmt.Errorf("invalid --max-cost-usd value %q: must be a finite positive amount", strconv.FormatFloat(requested, 'f', -1, 64))
	}
	if profile.Session.MaxCostUSD > 0 && requested > profile.Session.MaxCostUSD {
		return profile, fmt.Errorf("--max-cost-usd $%.2f exceeds the interactive profile ceiling of $%.2f", requested, profile.Session.MaxCostUSD)
	}
	profile.Session.MaxCostUSD = requested
	return profile, nil
}

// applySessionHourlyCeiling adjusts the per-run offer-price ceiling. Raising
// the profile's default hourly limit requires an explicit session-cost ceiling;
// every candidate still passes candidateWithinSessionBudget immediately before
// rental, so this cannot expand the requested total spend.
func applySessionHourlyCeiling(profile core.Profile, requested float64, explicitSessionCeiling bool) (core.Profile, error) {
	if math.IsNaN(requested) || math.IsInf(requested, 0) || requested <= 0 {
		return profile, fmt.Errorf("invalid --max-hourly-usd value %q: must be a finite positive amount", strconv.FormatFloat(requested, 'f', -1, 64))
	}
	if profile.GPU.MaxHourlyUSD > 0 && requested > profile.GPU.MaxHourlyUSD && !explicitSessionCeiling {
		return profile, errors.New("raising the hourly profile ceiling requires an explicit --max-cost-usd session cap")
	}
	profile.GPU.MaxHourlyUSD = requested
	return profile, nil
}

// runStartResumable is the paid interactive start path with explicit recovery
// checkpoints. Provider/SSH startup failures reject the host and move to the
// next distinct candidate. Once SSH is usable, later startup failures preserve
// the paid instance and leave the deadline watchdog running so `stint resume`
// can continue rather than forcing another rental.
func runStartResumable(args []string) (retErr error) {
	startupStartedAt := time.Now().UTC()
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
	clients := fs.Int("clients", defaultNInferClients, "NInfer client lanes (1 or 2; shared dynamic KV pool)")
	minNetworkMbps := fs.Float64("min-network-mbps", defaultMinAdvertisedNetworkMbps, "minimum Vast advertised download bandwidth in Mbps; 0 disables")
	minMeasuredDownloadMBps := fs.Float64("min-measured-download-mbps", defaultMinMeasuredDownloadMBps, "minimum measured post-SSH download throughput in MB/s; 0 disables")
	networkCandidateAttempts := fs.Int("network-candidate-attempts", defaultNetworkCandidateAttempts, "maximum distinct Vast machines to try during provider startup and measured-network qualification")
	maxCostUSD := fs.Float64("max-cost-usd", 0, "lower the profile's maximum requested-session cost (USD)")
	maxHourlyUSD := fs.Float64("max-hourly-usd", 0, "set the maximum hourly offer price (raising the profile limit requires --max-cost-usd)")
	validateOnly := fs.Bool("validate-only", false, "validate start options without reading credentials or contacting a provider")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	hours, err := strconv.ParseFloat(*hoursValue, 64)
	if err != nil || hours <= 0 {
		return fmt.Errorf("invalid --hours value %q", *hoursValue)
	}
	if err := validateNetworkMinimums(*minNetworkMbps, *minMeasuredDownloadMBps); err != nil {
		return err
	}
	if err := validateNetworkCandidateAttempts(*networkCandidateAttempts); err != nil {
		return err
	}
	if err := validateNInferClients(*clients); err != nil {
		return err
	}
	runtimeRequest, err := normalizeRuntime(*runtimeValue)
	if err != nil {
		return err
	}
	if runtimeRequest == runtimeLlamaCpp && *clients > defaultNInferClients {
		return validateClientsForRuntime(runtimeLlamaCpp, *clients)
	}
	requestedNInferConfig, err := resolveNInferConfig(*ninferConfigValue)
	if err != nil {
		return err
	}
	maxCostProvided := false
	maxHourlyProvided := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "max-cost-usd" {
			maxCostProvided = true
		}
		if f.Name == "max-hourly-usd" {
			maxHourlyProvided = true
		}
	})
	if maxCostProvided {
		profile, err = applySessionCostCeiling(profile, *maxCostUSD)
	}
	if err != nil {
		return err
	}
	if maxHourlyProvided {
		profile, err = applySessionHourlyCeiling(profile, *maxHourlyUSD, maxCostProvided)
	}
	if err != nil {
		return err
	}
	if *validateOnly {
		if strings.TrimSpace(*contextValue) != "" {
			if runtimeRequest == runtimeNInfer {
				return errors.New("--context is supported only with llama.cpp; use --ninfer-config for NInfer context profiles")
			}
			if _, err := resolveLlamaContext(*contextValue); err != nil {
				return err
			}
		}
		fmt.Println("start options are valid; no local credentials or provider were accessed")
		return nil
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
	offers = filterOffersByMinimumNetwork(offers, *minNetworkMbps)
	if len(offers) == 0 {
		return fmt.Errorf("no qualifying interactive offers meet the minimum advertised network %.0f Mbps; lower --min-network-mbps or retry the marketplace", *minNetworkMbps)
	}
	candidates := selectNetworkCandidates(profile, offers, *networkCandidateAttempts)
	if len(candidates) == 0 {
		return errors.New("no qualifying interactive offers remain after policy ranking")
	}
	plan, err := core.CreateSessionPlan(profile, hours, offers)
	if err != nil {
		return err
	}
	selected := candidates[0]
	selectedRuntime, err := selectInteractiveRuntime(runtimeRequest, selected.GPUModel)
	if err != nil {
		return err
	}
	if err := validateClientsForRuntime(selectedRuntime, *clients); err != nil {
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
	fmt.Printf("Network        %.0f Mbps advertised (min %.0f)\n", selected.InetDownMBps, *minNetworkMbps)
	if *minMeasuredDownloadMBps > 0 {
		fmt.Printf("Probe minimum  %.1f MB/s measured\n", *minMeasuredDownloadMBps)
		fmt.Printf("Host attempts  up to %d distinct machine(s)\n", len(candidates))
	}
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
		fmt.Printf("Clients        %d concurrent lane(s), shared KV\n", *clients)
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
	var state sessionstate.State
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

	qualified := false
	costRejected := 0
	rentalAttempts := 0
	for attempt, candidate := range candidates {
		candidateRuntime, candidateErr := selectInteractiveRuntime(runtimeRequest, candidate.GPUModel)
		if candidateErr != nil {
			return candidateErr
		}
		if err := validateClientsForRuntime(candidateRuntime, *clients); err != nil {
			return err
		}
		candidateContext := contextForRuntime(candidateRuntime)
		if strings.TrimSpace(*contextValue) != "" {
			if candidateRuntime != runtimeLlamaCpp {
				return errors.New("--context is supported only with llama.cpp; use --ninfer-config for NInfer context profiles")
			}
			candidateContext, candidateErr = resolveLlamaContext(*contextValue)
			if candidateErr != nil {
				return candidateErr
			}
		}
		if candidateRuntime == runtimeNInfer {
			candidateContext = requestedNInferConfig.ContextTokens
		}

		selected = candidate
		selectedRuntime = candidateRuntime
		selectedContext = candidateContext
		startedAt := time.Now().UTC()
		deadline := startedAt.Add(time.Duration(hours * float64(time.Hour)))
		state = sessionstate.State{
			OfferID: selected.ID, Profile: profileName, GPUModel: selected.GPUModel,
			RuntimeRequest: runtimeRequest, Runtime: selectedRuntime, ContextTokens: selectedContext, Clients: *clients,
			HourlyUSD: selected.HourlyUSD, Hours: hours, StartedAt: startedAt, Deadline: deadline,
			Status: sessionstate.StatusRenting,
		}

		// This check sits immediately before every provider rental mutation so
		// fallbacks cannot pass hourly policy while exceeding the session cap.
		if !candidateWithinSessionBudget(profile, selected, hours) {
			costRejected++
			fmt.Printf("Rejected        candidate %d/%d (estimated session cost $%.2f exceeds $%.2f ceiling)\n",
				attempt+1, len(candidates), estimatedCandidateSessionCost(selected, hours), profile.Session.MaxCostUSD)
			continue
		}
		if len(candidates) > 1 {
			fmt.Printf("Renting candidate %d/%d (%s, %s, %.0f Mbps advertised)...\n", attempt+1, len(candidates), selected.GPUModel, valueOr(selected.Geolocation, "unknown"), selected.InetDownMBps)
		} else {
			fmt.Println("Renting selected offer...")
		}
		rentalAttempts++
		instanceID, createErr := client.CreateInstance(rootCtx, selected.ID, vast.CreateInstanceOptions{
			Image:   vastImageForRuntime(runtimeRequest),
			DiskGB:  profile.Session.StorageGB,
			Label:   "stint-interactive",
			OnStart: vastOnStartForRuntime(selectedRuntime),
		})
		if createErr != nil {
			if rootCtx.Err() != nil || !vast.IsOfferUnavailableError(createErr) {
				return createErr
			}
			fmt.Printf("Rejected        candidate %d/%d (offer %s is no longer available)\n", attempt+1, len(candidates), selected.ID)
			if attempt+1 < len(candidates) {
				fmt.Println("Trying next network candidate...")
				continue
			}
			return fmt.Errorf("startup exhausted %d distinct candidate(s); final offer disappeared before rental: %w", len(candidates), createErr)
		}
		created = true
		state.InstanceID = instanceID
		state.Status = sessionstate.StatusBooting
		state.Checkpoint = sessionstate.CheckpointInstanceCreated
		if err := sessionstate.Save(paths, state); err != nil {
			return fmt.Errorf("instance %d was created but state persistence failed: %w", instanceID, err)
		}
		fmt.Printf("Instance       %d\n", instanceID)

		watchdogPID, watchdogErr := spawnWatchdog(paths)
		if watchdogErr != nil {
			return fmt.Errorf("start session watchdog: %w", watchdogErr)
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
		instance, metadataErr := waitForSSHMetadata(rootCtx, client, instanceID, providerStartupTimeout)
		if metadataErr != nil {
			if rootCtx.Err() != nil {
				return metadataErr
			}
			rejectedInstanceID := state.InstanceID
			if destroyErr := destroyRejectedInstance(client, paths, state); destroyErr != nil {
				return fmt.Errorf("provider startup failed (%v), and cleanup of instance %d failed: %w", metadataErr, rejectedInstanceID, destroyErr)
			}
			created = false
			fmt.Printf("Rejected        instance %d (provider startup failed: %v)\n", rejectedInstanceID, metadataErr)
			if attempt+1 < len(candidates) {
				fmt.Println("Trying next network candidate...")
				continue
			}
			return fmt.Errorf("startup exhausted %d distinct candidate(s); instance %d never became SSH-ready: %w", len(candidates), rejectedInstanceID, metadataErr)
		}
		state.SSHHost = instance.SSHHost
		state.SSHPort = instance.SSHPort
		state.Status = sessionstate.StatusSSHConnecting
		if err := sessionstate.Save(paths, state); err != nil {
			return err
		}
		if sshErr := waitForSSH(rootCtx, paths, state, 4*time.Minute); sshErr != nil {
			if rootCtx.Err() != nil {
				return sshErr
			}
			rejectedInstanceID := state.InstanceID
			if destroyErr := destroyRejectedInstance(client, paths, state); destroyErr != nil {
				return fmt.Errorf("SSH startup failed (%v), and cleanup of instance %d failed: %w", sshErr, rejectedInstanceID, destroyErr)
			}
			created = false
			fmt.Printf("Rejected        instance %d (SSH startup failed: %v)\n", rejectedInstanceID, sshErr)
			if attempt+1 < len(candidates) {
				fmt.Println("Trying next network candidate...")
				continue
			}
			return fmt.Errorf("startup exhausted %d distinct candidate(s); instance %d never accepted SSH: %w", len(candidates), rejectedInstanceID, sshErr)
		}
		state.Status = sessionstate.StatusSSHReady
		state.Checkpoint = sessionstate.CheckpointSSHReady
		state.LastError = ""
		if err := sessionstate.Save(paths, state); err != nil {
			return err
		}

		if *minMeasuredDownloadMBps <= 0 {
			qualified = true
			break
		}

		fmt.Println("Checking remote download throughput before model startup...")
		measured, probeErr := measureRemoteDownloadMBps(rootCtx, paths, state)
		if probeErr != nil {
			rejectedInstanceID := state.InstanceID
			if destroyErr := destroyRejectedInstance(client, paths, state); destroyErr != nil {
				return fmt.Errorf("network qualification probe failed (%v), and cleanup of instance %d failed: %w", probeErr, rejectedInstanceID, destroyErr)
			}
			created = false
			fmt.Printf("Rejected        instance %d (network probe failed: %v)\n", rejectedInstanceID, probeErr)
			if attempt+1 < len(candidates) {
				fmt.Println("Trying next network candidate...")
				continue
			}
			return fmt.Errorf("network qualification exhausted %d distinct candidate(s); last probe failed on instance %d: %w", len(candidates), rejectedInstanceID, probeErr)
		}

		fmt.Printf("Network probe   %.1f MB/s measured (min %.1f)\n", measured, *minMeasuredDownloadMBps)
		if measured < *minMeasuredDownloadMBps {
			rejectedInstanceID := state.InstanceID
			if destroyErr := destroyRejectedInstance(client, paths, state); destroyErr != nil {
				return fmt.Errorf("instance %d measured %.1f MB/s below the %.1f MB/s minimum, and cleanup failed: %w", rejectedInstanceID, measured, *minMeasuredDownloadMBps, destroyErr)
			}
			created = false
			fmt.Printf("Rejected        instance %d (%.1f MB/s below %.1f MB/s)\n", rejectedInstanceID, measured, *minMeasuredDownloadMBps)
			if attempt+1 < len(candidates) {
				fmt.Println("Trying next network candidate...")
				continue
			}
			return fmt.Errorf("network qualification exhausted %d distinct candidate(s); instance %d measured %.1f MB/s below the %.1f MB/s minimum", len(candidates), rejectedInstanceID, measured, *minMeasuredDownloadMBps)
		}

		qualified = true
		break
	}
	if !qualified {
		if rentalAttempts == 0 && costRejected > 0 {
			return fmt.Errorf("all %d candidate(s) exceeded the $%.2f requested-session cost ceiling", costRejected, profile.Session.MaxCostUSD)
		}
		return errors.New("network qualification did not select a candidate")
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
	modelServingAt := time.Now().UTC()
	state.Status = sessionstate.StatusReady
	state.Checkpoint = sessionstate.CheckpointReady
	state.LastError = ""
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}
	ready = true
	printReadySession(state)
	fmt.Printf("Startup         %s (stint start -> model serving)\n", formatStartupDuration(startupStartedAt, modelServingAt))
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
