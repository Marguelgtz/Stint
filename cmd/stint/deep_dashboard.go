package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	dash "github.com/Marguelgtz/Stint/internal/dashboard"
	"github.com/Marguelgtz/Stint/internal/deep"
	deepdash "github.com/Marguelgtz/Stint/internal/deepdashboard"
	localenv "github.com/Marguelgtz/Stint/internal/local"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const (
	deepDashboardLocalRefreshInterval  = 5 * time.Second
	deepDashboardRemoteRefreshInterval = 10 * time.Second
)

type deepDashboardSnapshot struct {
	State       deep.DeepState
	Latest      bool
	Coordinator string
	ActiveSince *time.Time
	Events      []deepdash.Event
}

func recordedTunnelPort(state sessionstate.State) int {
	if state.TunnelPID <= 0 {
		return 0
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", state.TunnelPID))
	if err != nil {
		return 0
	}
	args := strings.Split(string(data), "\x00")
	for i := 0; i+1 < len(args); i++ {
		if args[i] != "-L" {
			continue
		}
		forward := args[i+1]
		parts := strings.SplitN(forward, ":", 3)
		if len(parts) != 3 {
			continue
		}
		port, err := strconv.Atoi(parts[1])
		if err == nil && port > 0 && port <= 65535 {
			return port
		}
	}
	return 0
}

type deepDashboardRemoteResult struct {
	Compute          sessionSnapshot
	ComputeAvailable bool
	ComputeErr       error
	Worker           deepdash.Worker
}

type deepDashboardController struct {
	paths       config.Paths
	sessionID   string
	model       deepdash.Model
	snapshot    deepDashboardSnapshot
	compute     sessionSnapshot
	computeLive bool
	worker      deepdash.Worker
	refreshing  bool
	refreshCh   chan deepDashboardRemoteResult
	modal       bool
}

// runDeepDashboard presents durable Deep Work progress even when no compute
// session remains. Remote observations are passive and are a separate domain
// from the coordinator's durable state.
func runDeepDashboard(args []string) error {
	fs := flag.NewFlagSet("deep dash", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	noColor := fs.Bool("no-color", false, "disable ANSI colors")
	refresh := fs.Bool("refresh", false, "add one passive compute and GPU-worker observation in non-interactive mode")
	sessionID := fs.String("session", "", "Deep Work session id (default: latest)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("deep dash does not accept positional arguments")
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	width, height := dash.Size()
	controller := &deepDashboardController{
		paths:     paths,
		sessionID: *sessionID,
		model:     deepdash.Model{Width: width, Height: height, NoColor: deepDashboardNoColor(*noColor, os.Getenv("NO_COLOR")), View: deepdash.Run},
		refreshCh: make(chan deepDashboardRemoteResult, 1),
	}
	controller.loadLocal()
	if binding, bindErr := loadDeepProductionBinding(paths); bindErr == nil && shouldRouteDeepDashboardRemote(controller, binding, *sessionID) {
		return runRemoteDeepDashboard(paths, binding, controller.model.NoColor, *refresh)
	}
	if !dash.IsTTY(os.Stdin) || !dash.IsTTY(os.Stdout) {
		if *refresh && controller.model.Error == "" {
			controller.refreshBlocking()
		}
		fmt.Println(deepdash.Render(controller.model))
		return nil
	}
	controller.startRefresh()
	terminal, err := dash.OpenTerminal(os.Stdin, os.Stdout)
	if err != nil {
		return err
	}
	defer terminal.Restore()
	if err := terminal.Draw(deepdash.Render(controller.model)); err != nil {
		return err
	}

	inputCh := make(chan byte, 16)
	inputErrCh := make(chan error, 1)
	go readDashboardInput(os.Stdin, inputCh, inputErrCh)
	localTick := time.NewTicker(time.Second)
	defer localTick.Stop()
	stateTick := time.NewTicker(deepDashboardLocalRefreshInterval)
	defer stateTick.Stop()
	remoteTick := time.NewTicker(deepDashboardRemoteRefreshInterval)
	defer remoteTick.Stop()
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGWINCH)
	defer signal.Stop(sigCh)

	for {
		redraw := false
		select {
		case key := <-inputCh:
			quit, changed, land := controller.handleKey(key)
			if quit {
				return nil
			}
			if land {
				terminal.Restore()
				err := runDeepStop(nil)
				terminal, _ = dash.OpenTerminal(os.Stdin, os.Stdout)
				if terminal == nil {
					return errors.New("failed to restore terminal after Deep Work landing")
				}
				controller.loadLocal()
				controller.startRefresh()
				if err != nil {
					controller.model.Error = err.Error()
				} else {
					controller.model.Notice = "Deep Work landed; compute remains active."
				}
				changed = true
			}
			redraw = changed
		case err := <-inputErrCh:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case result := <-controller.refreshCh:
			controller.applyRemote(result)
			redraw = true
		case <-localTick.C:
			controller.model.Now = time.Now().UTC()
			controller.project()
			redraw = true
		case <-stateTick.C:
			controller.loadLocal()
			redraw = true
		case <-remoteTick.C:
			controller.startRefresh()
		case sig := <-sigCh:
			if sig == syscall.SIGWINCH {
				controller.model.Width, controller.model.Height = dash.Size()
				redraw = true
			} else {
				return nil
			}
		}
		if redraw {
			if err := terminal.Draw(deepdash.Render(controller.model)); err != nil {
				return err
			}
		}
	}
}

func deepDashboardNoColor(flagValue bool, envValue string) bool {
	return flagValue || envValue != ""
}

func shouldRouteDeepDashboardRemote(controller *deepDashboardController, binding deepProductionRunBinding, requestedSession string) bool {
	if requestedSession != "" {
		return requestedSession == binding.DeepSessionID &&
			(controller.model.Error != "" || controller.snapshot.State.SessionID != requestedSession)
	}
	if controller.model.Error != "" {
		return true
	}
	if controller.snapshot.State.SessionID == binding.DeepSessionID {
		return false
	}
	return binding.LaunchedAt.After(controller.snapshot.State.StartedAt)
}

func deepProductionDashboardCommand(binding deepProductionRunBinding, sessionID string, noColor, refresh bool) string {
	if sessionID == "" {
		sessionID = binding.DeepSessionID
	}
	bin := filepath.Join(binding.RemoteRoot, "bin", "stint")
	stateHome := filepath.Join(binding.RemoteRoot, "state")
	parts := []string{
		"env",
		"XDG_STATE_HOME=" + shellQuote(stateHome),
		shellQuote(bin), "deep", "dash", "--session", shellQuote(sessionID),
	}
	if noColor {
		parts = append(parts, "--no-color")
	}
	if refresh {
		parts = append(parts, "--refresh")
	}
	return strings.Join(parts, " ")
}

func runRemoteDeepDashboard(paths config.Paths, binding deepProductionRunBinding, noColor, refresh bool) error {
	ssh, err := localenv.SSHExecutable()
	if err != nil {
		return err
	}
	state := sessionstate.State{InstanceID: binding.InstanceID, SSHHost: binding.SSHHost, SSHPort: binding.SSHPort}
	remoteCommand := deepProductionDashboardCommand(binding, binding.DeepSessionID, noColor, refresh)
	args := sshArgs(paths, state, filepath.Join(paths.StateDir, "known_hosts"), remoteCommand)
	if dash.IsTTY(os.Stdin) && dash.IsTTY(os.Stdout) {
		args = append([]string{"-t", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3"}, args...)
	}
	command := exec.Command(ssh, args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("connect to the on-box Deep Dashboard over the READY session SSH identity: %w", err)
	}
	return nil
}

func (c *deepDashboardController) handleKey(key byte) (quit, changed, land bool) {
	if key == 3 || key == 'q' || key == 'Q' {
		return true, false, false
	}
	if c.modal {
		if key == 27 {
			c.modal = false
			c.model.Modal = nil
			return false, true, false
		}
		if key == 'S' {
			c.modal = false
			c.model.Modal = nil
			return false, true, true
		}
		return false, false, false
	}
	c.model.Error, c.model.Notice = "", ""
	switch key {
	case '1':
		c.model.View = deepdash.Run
	case '2':
		c.model.View = deepdash.Tasks
	case '3':
		c.model.View = deepdash.Activity
	case '4':
		c.model.View = deepdash.WorkerView
	case '5':
		c.model.View = deepdash.PhaseDetails
	case dashboardKeyPrevious:
		c.model.View = deepdash.View((int(c.model.View) + 4) % 5)
	case dashboardKeyNext, '\t':
		c.model.View = deepdash.View((int(c.model.View) + 1) % 5)
	case 'r', 'R':
		c.loadLocal()
		c.startRefresh()
	case 's':
		if !c.isLatest() {
			c.model.Error = "Only the latest Deep Work session can be landed from the dashboard."
			return false, true, false
		}
		if c.snapshot.State.Phase == deep.PhaseLanded || c.snapshot.State.Phase == deep.PhaseStopped {
			c.model.Notice = "This Deep Work session is already landed."
			return false, true, false
		}
		c.modal = true
		c.model.Modal = &deepdash.Modal{Title: "LAND DEEP WORK?", Lines: []string{
			"Session       " + c.snapshot.State.SessionID,
			"Phase         " + string(c.snapshot.State.Phase),
			"Compute stays active.",
			"The coordinator will write a truthful handoff.",
		}, Hint: "Press uppercase S to land · Esc cancel"}
	default:
		return false, false, false
	}
	return false, true, false
}

func (c *deepDashboardController) isLatest() bool {
	if !c.snapshot.Latest {
		return false
	}
	latest, err := deep.LoadLatestState(c.paths.StateDir)
	if err != nil || latest.SessionID != c.snapshot.State.SessionID {
		return false
	}
	compute, err := sessionstate.Load(c.paths)
	return err == nil && compute.Status == sessionstate.StatusReady && deepStateMatchesCompute(latest, compute)
}

func deepStateMatchesCompute(state deep.DeepState, compute sessionstate.State) bool {
	return state.ComputeBinding != nil && state.ComputeBinding.Provider == "vast" &&
		state.ComputeBinding.InstanceID > 0 && state.ComputeBinding.InstanceID == compute.InstanceID
}

func (c *deepDashboardController) loadLocal() {
	snapshot, err := loadDeepDashboardSnapshot(c.paths.StateDir, c.sessionID)
	if err != nil {
		c.model.Error = err.Error()
		return
	}
	c.snapshot = snapshot
	c.sessionID = snapshot.State.SessionID
	c.model.Error = ""
	c.project()
}

func (c *deepDashboardController) project() {
	s := c.snapshot.State
	m := &c.model
	m.SessionID = s.SessionID
	m.Mission = s.MissionName
	m.Phase = string(s.Phase)
	m.BaseCommit = s.BaseCommit
	m.Branch = s.Branch
	m.Verify = s.Verify
	m.LandingReason = s.LandingReason
	m.LandingCommit = s.LandingCommit
	m.LandingVerify = s.LandingVerify
	m.LandingVerifyDone = s.LandingVerifyDone
	m.LandingHandoff = s.LandingHandoff
	m.LandedAt = s.LandedAt
	m.Latest = c.snapshot.Latest
	m.CanLand = c.isLatest() && s.Phase != deep.PhaseLanded && s.Phase != deep.PhaseStopped
	if s.ComputeBinding != nil {
		m.ComputeBinding = fmt.Sprintf("%s instance %d", s.ComputeBinding.Provider, s.ComputeBinding.InstanceID)
	} else {
		m.ComputeBinding = "unbound"
	}
	m.WorkerName = "Hermes on compute box"
	if s.Exec != nil && s.Exec.Worker != "" {
		m.WorkerName = "Hermes on compute box"
	}
	m.Coordinator = c.snapshot.Coordinator
	m.StartedAt, m.Deadline, m.LandBefore = s.StartedAt, s.Deadline, s.LandBefore
	m.Now = time.Now().UTC()
	m.ActiveSince = c.snapshot.ActiveSince
	m.Events = c.snapshot.Events
	m.Tasks = make([]deepdash.Task, 0, len(s.Tasks))
	for _, task := range s.Tasks {
		verifiedAt := ""
		if task.VerifiedAt != nil {
			verifiedAt = task.VerifiedAt.Local().Format(time.RFC3339)
		}
		m.Tasks = append(m.Tasks, deepdash.Task{
			ID: task.ID, Objective: task.Objective, Status: string(task.Status), Attempts: task.Attempts, Reasoning: task.Reasoning,
			Blocker: task.Blocker, LastResult: task.LastResult, Verify: task.Verify,
			CheckpointCommit: task.CheckpointCommit, VerifiedAt: verifiedAt,
			ExecutionError: task.ExecutionError, VerificationCommand: task.VerificationCommand,
			VerificationResult: task.VerificationResult, TimeoutDecision: task.TimeoutDecision,
			ConfiguredTimeoutSec: task.ConfiguredTimeoutSec, EffectiveTimeoutSec: task.EffectiveTimeoutSec,
			DependsOn: append([]string(nil), task.DependsOn...),
		})
	}
	m.Compute = projectDeepDashboardCompute(c.compute, c.computeLive)
	m.Worker = c.worker
}

func (c *deepDashboardController) startRefresh() {
	if c.refreshing || c.model.Error != "" {
		return
	}
	c.refreshing = true
	state := c.snapshot.State
	paths := c.paths
	go func() {
		result := deepDashboardRemoteResult{}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if deepDashboardUsesLocalWorker(state) {
			result.Worker = collectOnboxDeepDashboardWorker(ctx, state, runLocalDashboardCommand)
			c.refreshCh <- result
			return
		}
		ssn, err := deepDashboardSSHState(paths, state)
		if err != nil {
			result.ComputeErr = err
			result.Worker.HermesLogError = compactTelemetryError(err)
			c.refreshCh <- result
			return
		}
		workerCh := make(chan deepdash.Worker, 1)
		observeHermes := state.Exec != nil && state.Exec.Worker == workerHermes
		if observeHermes {
			go func() {
				remote := newRemoteCmd(paths, ssn)
				workerCh <- collectDeepDashboardWorkerEvidence(ctx, remote, state.StartedAt)
			}()
		}
		if ssn.Status != sessionstate.StatusReady {
			result.ComputeErr = fmt.Errorf("bound compute session is %s; live compute telemetry requires READY", ssn.Status)
		} else if tunnelPort := recordedTunnelPort(ssn); tunnelPort <= 0 {
			result.ComputeErr = errors.New("bound compute tunnel identity is unavailable")
		} else {
			result.ComputeAvailable = true
			deps := defaultSnapshotProbeDeps()
			deps.endpoint = func(probeCtx context.Context) endpointHealth {
				return probeEndpointHealthAtPort(probeCtx, tunnelPort)
			}
			deps.inference = func(probeCtx context.Context) inferenceTelemetry {
				return probeInferenceAtPort(probeCtx, tunnelPort)
			}
			result.Compute = collectSessionSnapshot(ctx, paths, ssn, time.Now().UTC(), true, deps)
		}
		if observeHermes {
			select {
			case result.Worker = <-workerCh:
			case <-ctx.Done():
				result.Worker.HermesLogError = "remote worker observation timed out"
			}
		}
		c.refreshCh <- result
	}()
}

func deepDashboardUsesLocalWorker(state deep.DeepState) bool {
	return state.Exec != nil && state.Exec.Worker == workerHermesOnBox
}

func collectOnboxDeepDashboardWorker(ctx context.Context, state deep.DeepState, local remoteCmd) deepdash.Worker {
	if !deepDashboardUsesLocalWorker(state) {
		return deepdash.Worker{Error: "Deep Work worker is not co-located with the dashboard"}
	}
	return collectDeepDashboardWorkerEvidence(ctx, local, state.StartedAt)
}

func collectDeepDashboardWorkerEvidence(ctx context.Context, run remoteCmd, startedAt time.Time) deepdash.Worker {
	type observationResult struct {
		worker deepdash.Worker
		err    error
	}
	type logResult struct {
		text string
		err  error
	}
	observationCh := make(chan observationResult, 1)
	logCh := make(chan logResult, 1)
	go func() {
		observed, err := collectDeepWorkerObservation(ctx, run, startedAt)
		observationCh <- observationResult{worker: observed, err: err}
	}()
	go func() {
		log, err := collectHermesAgentLog(ctx, run)
		logCh <- logResult{text: log, err: err}
	}()
	observed := <-observationCh
	log := <-logCh
	worker := observed.worker
	if observed.err != nil {
		worker.Error = compactTelemetryError(observed.err)
	}
	worker.HermesLogAt = time.Now().Local().Format("15:04:05")
	if log.err != nil {
		worker.HermesLogError = compactTelemetryError(log.err)
	} else {
		worker.HermesLog = log.text
	}
	return worker
}

func runLocalDashboardCommand(ctx context.Context, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("run co-located worker observation: %w", err)
	}
	return string(output), nil
}

func (c *deepDashboardController) refreshBlocking() {
	c.startRefresh()
	result := <-c.refreshCh
	c.applyRemote(result)
}

func (c *deepDashboardController) applyRemote(result deepDashboardRemoteResult) {
	c.refreshing = false
	c.compute, c.computeLive = result.Compute, result.ComputeAvailable
	if result.ComputeErr != nil {
		c.computeLive = false
		if result.Worker.HermesLogError == "" {
			result.Worker.HermesLogError = compactTelemetryError(result.ComputeErr)
		}
		c.worker = result.Worker
	} else if result.Worker.Observed || result.Worker.Error != "" || result.Worker.HermesLogAt != "" || result.Worker.HermesLogError != "" {
		c.worker = result.Worker
	}
	c.project()
}

func deepDashboardSSHState(paths config.Paths, state deep.DeepState) (sessionstate.State, error) {
	if state.ComputeBinding == nil || state.ComputeBinding.Provider != "vast" || state.ComputeBinding.InstanceID <= 0 {
		return sessionstate.State{}, errors.New("Deep Work has no persisted Vast instance binding for remote observation")
	}
	id := state.ComputeBinding.InstanceID
	usable := func(compute sessionstate.State) bool {
		return compute.InstanceID == id && strings.TrimSpace(compute.SSHHost) != "" && compute.SSHPort > 0 && compute.SSHPort <= 65535
	}
	if current, err := sessionstate.Load(paths); err == nil && usable(current) {
		return current, nil
	}
	entries, err := os.ReadDir(sessionArchiveDir(paths))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return sessionstate.State{}, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	prefix := fmt.Sprintf("%d.", id)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(sessionArchiveDir(paths), entry.Name()))
		if readErr != nil {
			continue
		}
		var compute sessionstate.State
		if json.Unmarshal(data, &compute) == nil && usable(compute) {
			return compute, nil
		}
	}
	return sessionstate.State{}, fmt.Errorf("no persisted SSH endpoint is available for bound Vast instance %d", id)
}

func loadDeepDashboardSnapshot(stateDir, sessionID string) (deepDashboardSnapshot, error) {
	var state deep.DeepState
	var err error
	if sessionID == "" {
		state, err = deep.LoadLatestState(stateDir)
	} else {
		state, err = deep.LoadState(stateDir, sessionID)
	}
	if err != nil {
		return deepDashboardSnapshot{}, err
	}
	latest, latestErr := deep.LoadLatestState(stateDir)
	isLatest := latestErr == nil && latest.SessionID == state.SessionID
	coordinator := "not running"
	if alive, pid := deep.CoordinatorAlive(stateDir, state.SessionID); alive {
		coordinator = fmt.Sprintf("running (pid %d)", pid)
	}
	incidents, err := deep.ReadIncidents(stateDir, state.SessionID)
	if err != nil {
		return deepDashboardSnapshot{}, err
	}
	if len(incidents) > 120 {
		incidents = incidents[len(incidents)-120:]
	}
	var activeSince *time.Time
	for _, incident := range incidents {
		if incident.Kind == deep.IncidentExecutorInvoke && activeTaskID(state.Tasks) == incident.Task {
			started := incident.Time
			activeSince = &started
		}
	}
	events := make([]deepdash.Event, 0, len(incidents))
	for _, incident := range incidents {
		events = append(events, deepdash.Event{Time: incident.Time.Local().Format("15:04:05"), Kind: incident.Kind, Task: incident.Task, Detail: incident.Detail})
	}
	return deepDashboardSnapshot{State: state, Latest: isLatest, Coordinator: coordinator, ActiveSince: activeSince, Events: events}, nil
}

func activeTaskID(tasks []deep.Task) string {
	for _, task := range tasks {
		if task.Status == deep.StatusActive {
			return task.ID
		}
	}
	return ""
}

func projectDeepDashboardCompute(snapshot sessionSnapshot, available bool) deepdash.Compute {
	if !available {
		return deepdash.Compute{}
	}
	result := deepdash.Compute{Available: true, Status: dashboardDisplayStatus(snapshot), Endpoint: dashboardEndpointLabel(snapshot.Health.Endpoint), Runtime: dashboardRuntimeLabel(snapshot.Health.Runtime), GPU: snapshot.Session.GPUModel}
	if snapshot.GPU.UtilizationPercent != nil {
		result.Utilization = fmt.Sprintf("%.0f%% GPU", *snapshot.GPU.UtilizationPercent)
	}
	if snapshot.GPU.MemoryUsedMiB != nil && snapshot.GPU.MemoryTotalMiB != nil {
		result.VRAM = fmt.Sprintf("%.1f / %.1f GB", *snapshot.GPU.MemoryUsedMiB/1024, *snapshot.GPU.MemoryTotalMiB/1024)
	}
	if snapshot.Inference.Available {
		result.Lane = inferenceLaneSummary(snapshot.Inference.Lanes)
	}
	return result
}

type deepWorkerWire struct {
	CollectedAt string `json:"collectedAt"`
	NInfer      struct {
		Running          bool `json:"running"`
		MaxContext       int  `json:"maxContext"`
		KVCapacity       int  `json:"kvCapacity"`
		DefaultMaxTokens int  `json:"defaultMaxTokens"`
	} `json:"ninfer"`
	PhaseRoutes struct {
		XHighRequests  int    `json:"xhighRequests"`
		MediumRequests int    `json:"mediumRequests"`
		LatestPhase    string `json:"latestPhase"`
		LatestAt       string `json:"latestAt"`
	} `json:"phaseRoutes"`
	Compression struct {
		State     string `json:"state"`
		Completed int    `json:"completed"`
		Failed    int    `json:"failed"`
		Truncated int    `json:"truncated"`
		LastAt    string `json:"lastAt"`
	} `json:"compression"`
}

func collectDeepWorkerObservation(ctx context.Context, remote remoteCmd, startedAt time.Time) (deepdash.Worker, error) {
	line := "STINT_DEEP_STARTED_AT=" + shellQuote(startedAt.UTC().Format(time.RFC3339)) + " /root/stint-phasing/deep-observe"
	out, err := remote(ctx, line)
	if err != nil {
		return deepdash.Worker{}, err
	}
	return parseDeepWorkerObservation(out)
}

func collectHermesAgentLog(ctx context.Context, remote remoteCmd) (string, error) {
	out, err := remote(ctx, "stint-deep-agent-log-tail")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func parseDeepWorkerObservation(raw string) (deepdash.Worker, error) {
	var wire deepWorkerWire
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &wire); err != nil {
		return deepdash.Worker{}, fmt.Errorf("parse worker observation: %w", err)
	}
	if !wire.NInfer.Running {
		return deepdash.Worker{}, errors.New("NInfer was not running on the GPU box")
	}
	worker := deepdash.Worker{Observed: true, Reachable: true, Scope: "Shared host logs since session start; counts are not attributed to this session", NInferContext: wire.NInfer.MaxContext, NInferKV: wire.NInfer.KVCapacity, NInferDefaultMaxTokens: wire.NInfer.DefaultMaxTokens, XHighRequests: wire.PhaseRoutes.XHighRequests, MediumRequests: wire.PhaseRoutes.MediumRequests, LatestPhase: wire.PhaseRoutes.LatestPhase, LatestAt: wire.PhaseRoutes.LatestAt, Compression: wire.Compression.State, CompressionAt: wire.Compression.LastAt, CompressionCompleted: wire.Compression.Completed, CompressionFailed: wire.Compression.Failed, Truncated: wire.Compression.Truncated}
	if worker.Compression == "" {
		worker.Compression = "not_observed"
	}
	return worker, nil
}
