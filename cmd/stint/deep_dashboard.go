package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	dash "github.com/Marguelgtz/Stint/internal/dashboard"
	"github.com/Marguelgtz/Stint/internal/deep"
	deepdash "github.com/Marguelgtz/Stint/internal/deepdashboard"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const (
	deepDashboardLocalRefreshInterval  = 5 * time.Second
	deepDashboardRemoteRefreshInterval = 10 * time.Second
)

type deepDashboardSnapshot struct {
	State       deep.DeepState
	Coordinator string
	ActiveSince *time.Time
	Events      []deepdash.Event
	GitHub      deepDashboardGitHub
}

type deepDashboardGitHub struct {
	Reviewed, Repaired, Merged, Skipped int
	LastAction, LastError               string
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
		model:     deepdash.Model{Width: width, Height: height, NoColor: *noColor || os.Getenv("NO_COLOR") != "", View: deepdash.Run},
		refreshCh: make(chan deepDashboardRemoteResult, 1),
	}
	controller.loadLocal()
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
	case dashboardKeyPrevious:
		c.model.View = deepdash.View((int(c.model.View) + 3) % 4)
	case dashboardKeyNext, '\t':
		c.model.View = deepdash.View((int(c.model.View) + 1) % 4)
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
	latest, err := deep.LoadLatestState(c.paths.StateDir)
	return err == nil && latest.SessionID == c.snapshot.State.SessionID
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
	m.GitHubMode = string(s.GitHub.Mode)
	m.GitHubHead = s.HeadCommit
	m.GitHubBase = s.GitHub.Base
	m.GitHubReviewed = c.snapshot.GitHub.Reviewed
	m.GitHubRepaired = c.snapshot.GitHub.Repaired
	m.GitHubMerged = c.snapshot.GitHub.Merged
	m.GitHubSkipped = c.snapshot.GitHub.Skipped
	m.GitHubLastAction = c.snapshot.GitHub.LastAction
	m.GitHubLastError = c.snapshot.GitHub.LastError
	m.WorkerName = "Cline on operator machine"
	if s.Exec != nil && s.Exec.Worker != "" {
		if s.Exec.Worker == workerHermes || s.Exec.Worker == workerHermesOnBox {
			m.WorkerName = "Hermes on GPU"
		} else {
			m.WorkerName = s.Exec.Worker
		}
	}
	m.Coordinator = c.snapshot.Coordinator
	m.StartedAt, m.Deadline, m.LandBefore = s.StartedAt, s.Deadline, s.LandBefore
	m.Now = time.Now().UTC()
	m.ActiveSince = c.snapshot.ActiveSince
	m.Events = c.snapshot.Events
	m.Tasks = make([]deepdash.Task, 0, len(s.Tasks))
	for _, task := range s.Tasks {
		m.Tasks = append(m.Tasks, deepdash.Task{ID: task.ID, Objective: task.Objective, Phase: string(task.Phase), Status: string(task.Status), Attempts: task.Attempts, Blocker: task.Blocker, LastResult: task.LastResult})
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
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		ssn, err := sessionstate.Load(paths)
		if errors.Is(err, os.ErrNotExist) {
			c.refreshCh <- result
			return
		}
		if err != nil {
			result.ComputeErr = err
			c.refreshCh <- result
			return
		}
		result.ComputeAvailable = true
		// Dashboard is a separate process from `start`, so it cannot rely on
		// the package default (8409) when a session uses an isolated tunnel.
		// The lifecycle records the tunnel PID; recover its loopback port before
		// probing the endpoint.
		if tunnelPort := recordedTunnelPort(ssn); tunnelPort > 0 {
			clinePort = tunnelPort
		}
		workerCh := make(chan deepdash.Worker, 1)
		if state.Exec != nil && state.Exec.Worker == workerHermes {
			go func() {
				worker, workerErr := collectDeepWorkerObservation(ctx, newRemoteCmd(paths, ssn), state.StartedAt)
				if workerErr != nil {
					worker.Error = compactTelemetryError(workerErr)
				}
				workerCh <- worker
			}()
		}
		result.Compute = collectSessionSnapshot(ctx, paths, ssn, time.Now().UTC(), true, defaultSnapshotProbeDeps())
		if state.Exec != nil && state.Exec.Worker == workerHermes {
			select {
			case result.Worker = <-workerCh:
			case <-ctx.Done():
				result.Worker.Error = "worker observation timed out"
			}
		}
		c.refreshCh <- result
	}()
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
		c.worker.Error = compactTelemetryError(result.ComputeErr)
	} else if result.Worker.Observed || result.Worker.Error != "" {
		c.worker = result.Worker
	}
	c.project()
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
	github := deepDashboardGitHub{}
	if state.GitHubLedger != "" {
		if data, readErr := os.ReadFile(state.GitHubLedger); readErr == nil {
			for _, line := range strings.Split(string(data), "\n") {
				var entry struct {
					Operation string `json:"operation"`
					Result    string `json:"result"`
					Reason    string `json:"reason"`
				}
				if json.Unmarshal([]byte(line), &entry) != nil {
					continue
				}
				switch entry.Operation {
				case "merge-gate":
					github.Reviewed++
				case "repair":
					github.Repaired++
				case "merge":
					if entry.Result == "merged" || entry.Result == "already-merged" {
						github.Merged++
					}
				case "skip":
					github.Skipped++
				}
				if entry.Operation != "" {
					github.LastAction = entry.Operation + ": " + entry.Result
					github.LastError = ""
					if entry.Result == "failed" || entry.Result == "rejected" {
						github.LastError = entry.Reason
					}
				}
			}
		}
	}
	return deepDashboardSnapshot{State: state, Coordinator: coordinator, ActiveSince: activeSince, Events: events, GitHub: github}, nil
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

func parseDeepWorkerObservation(raw string) (deepdash.Worker, error) {
	var wire deepWorkerWire
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &wire); err != nil {
		return deepdash.Worker{}, fmt.Errorf("parse worker observation: %w", err)
	}
	if !wire.NInfer.Running {
		return deepdash.Worker{}, errors.New("NInfer was not running on the GPU box")
	}
	worker := deepdash.Worker{Observed: true, Reachable: true, NInferContext: wire.NInfer.MaxContext, NInferKV: wire.NInfer.KVCapacity, NInferDefaultMaxTokens: wire.NInfer.DefaultMaxTokens, XHighRequests: wire.PhaseRoutes.XHighRequests, MediumRequests: wire.PhaseRoutes.MediumRequests, LatestPhase: wire.PhaseRoutes.LatestPhase, LatestAt: wire.PhaseRoutes.LatestAt, Compression: wire.Compression.State, CompressionAt: wire.Compression.LastAt, CompressionCompleted: wire.Compression.Completed, CompressionFailed: wire.Compression.Failed, Truncated: wire.Compression.Truncated}
	if worker.Compression == "" {
		worker.Compression = "not_observed"
	}
	return worker, nil
}
