package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	dash "github.com/Marguelgtz/Stint/internal/dashboard"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const dashboardRefreshInterval = 10 * time.Second

const (
	dashboardKeyPrevious byte = 0x80
	dashboardKeyNext     byte = 0x81
)

type dashboardActionKind int

const (
	dashboardActionNone dashboardActionKind = iota
	dashboardActionBenchmark
	dashboardActionResume
	dashboardActionExtend
	dashboardActionShorten
	dashboardActionDown
)

type dashboardAction struct {
	Kind     dashboardActionKind
	Duration time.Duration
}

type dashboardModalMode int

const (
	dashboardModalNone dashboardModalMode = iota
	dashboardModalBenchmark
	dashboardModalResumeConfirm
	dashboardModalDeadlineChoice
	dashboardModalDeadlineCustom
	dashboardModalDeadlineConfirm
	dashboardModalDownConfirm
)

func init() {
	if len(os.Args) < 2 || os.Args[1] != "dashboard" {
		return
	}
	if wantsHelp(os.Args[2:]) {
		printCommandHelp("dashboard")
		os.Exit(0)
	}
	if err := runDashboard(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "stint:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

type dashboardLoadResult struct {
	Snapshot  sessionSnapshot
	NoSession bool
	Err       error
}

type dashboardController struct {
	paths             config.Paths
	model             dash.Model
	snapshot          sessionSnapshot
	refreshing        bool
	refreshCh         chan dashboardLoadResult
	benchmarking      bool
	benchmarkCh       chan dashboardBenchmarkResult
	logs              []string
	modalMode         dashboardModalMode
	deadlineDirection deadlineDirection
	deadlineDelta     time.Duration
	customDuration    string
	lastGoodInference dash.Inference
	lastGoodAt        time.Time
	lastGoodInstance  int64
}

func runDashboard(args []string) error {
	fs := flag.NewFlagSet("dashboard", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	noColor := fs.Bool("no-color", false, "disable ANSI colors")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("dashboard does not accept positional arguments")
	}
	if !dash.IsTTY(os.Stdin) || !dash.IsTTY(os.Stdout) {
		return runStatusTelemetry([]string{"--refresh"})
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	width, height := dash.Size()
	controller := &dashboardController{
		paths:       paths,
		model:       dash.Model{Width: width, Height: height, NoColor: *noColor || os.Getenv("NO_COLOR") != "", View: dash.Home},
		refreshCh:   make(chan dashboardLoadResult, 1),
		benchmarkCh: make(chan dashboardBenchmarkResult, 1),
	}
	controller.loadLocal()
	controller.startRefresh()

	terminal, err := dash.OpenTerminal(os.Stdin, os.Stdout)
	if err != nil {
		return err
	}
	defer func() {
		if terminal != nil {
			terminal.Restore()
		}
	}()
	if err := terminal.Draw(dash.Render(controller.model)); err != nil {
		return err
	}

	inputCh := make(chan byte, 16)
	inputErrCh := make(chan error, 1)
	go readDashboardInput(os.Stdin, inputCh, inputErrCh)
	localTick := time.NewTicker(time.Second)
	defer localTick.Stop()
	remoteTick := time.NewTicker(dashboardRefreshInterval)
	defer remoteTick.Stop()
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGWINCH)
	defer signal.Stop(sigCh)

	for {
		redraw := false
		select {
		case key := <-inputCh:
			quit, changed, action := controller.handleKey(key)
			if quit {
				return nil
			}
			if action.Kind != dashboardActionNone {
				if action.Kind == dashboardActionBenchmark {
					controller.startBenchmark()
				} else {
					terminal.Restore()
					err := controller.executeBlockingAction(action)
					terminal, _ = dash.OpenTerminal(os.Stdin, os.Stdout)
					if terminal == nil {
						return errors.New("failed to restore dashboard terminal after session action")
					}
					controller.loadLocal()
					controller.startRefresh()
					if err != nil {
						controller.model.Error = compactTelemetryError(err)
					} else {
						controller.model.Notice = dashboardActionSuccess(action)
					}
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
			controller.applyRefresh(result)
			redraw = true
		case result := <-controller.benchmarkCh:
			controller.applyBenchmark(result)
			redraw = true
		case <-localTick.C:
			controller.tick(time.Now().UTC())
			redraw = true
		case <-remoteTick.C:
			controller.startRefresh()
			redraw = true
		case sig := <-sigCh:
			if sig == syscall.SIGWINCH {
				controller.model.Width, controller.model.Height = dash.Size()
				redraw = true
			} else {
				return nil
			}
		}
		if redraw {
			if err := terminal.Draw(dash.Render(controller.model)); err != nil {
				return err
			}
		}
	}
}

func readDashboardInput(reader io.Reader, keys chan<- byte, errs chan<- error) {
	const (
		inputPlain = iota
		inputEscape
		inputCSI
	)

	state := inputPlain
	buf := make([]byte, 1)
	flushPending := func() {
		switch state {
		case inputEscape:
			keys <- 27
		case inputCSI:
			keys <- 27
			keys <- '['
		}
		state = inputPlain
	}

	for {
		n, err := reader.Read(buf)
		if n == 0 && err == nil {
			flushPending()
			continue
		}
		if n == 1 {
			key := buf[0]
			switch state {
			case inputPlain:
				if key == 27 {
					state = inputEscape
				} else {
					keys <- key
				}
			case inputEscape:
				if key == '[' {
					state = inputCSI
				} else {
					keys <- 27
					state = inputPlain
					if key == 27 {
						state = inputEscape
					} else {
						keys <- key
					}
				}
			case inputCSI:
				state = inputPlain
				switch key {
				case 'A', 'D':
					keys <- dashboardKeyPrevious
				case 'B', 'C':
					keys <- dashboardKeyNext
				default:
					keys <- 27
					keys <- '['
					if key == 27 {
						state = inputEscape
					} else {
						keys <- key
					}
				}
			}
		}
		if err != nil {
			flushPending()
			errs <- err
			return
		}
	}
}

func (c *dashboardController) handleKey(key byte) (quit, changed bool, action dashboardAction) {
	if key == 3 || key == 'q' || key == 'Q' {
		return true, false, dashboardAction{}
	}
	if c.modalMode != dashboardModalNone {
		return c.handleModalKey(key)
	}
	c.model.Notice = ""
	c.model.Error = ""
	switch key {
	case '1':
		c.model.View = dash.Home
	case '2':
		c.model.View = dash.Performance
	case '3':
		c.model.View = dash.Config
	case '4':
		c.model.View = dash.Logs
		c.reloadLogs()
	case dashboardKeyPrevious:
		c.navigateView(-1)
	case dashboardKeyNext, '\t':
		c.navigateView(1)
	case 'r', 'R':
		if dashboardSessionRecoverable(c.snapshot) {
			c.modalMode = dashboardModalResumeConfirm
			c.model.Modal = &dash.Modal{
				Title: "RESUME SESSION?",
				Lines: []string{
					fmt.Sprintf("Instance       %d", c.model.Session.InstanceID),
					fmt.Sprintf("Checkpoint     %s", valueOr(c.snapshot.Session.Checkpoint, "saved checkpoint")),
					fmt.Sprintf("Remaining      %s", formatSessionDuration(c.model.Session.Remaining)),
					"",
					"This reattaches the tunnel/runtime/model without renting new compute.",
				},
				Hint: "Enter resume · Esc cancel",
			}
			break
		}
		if c.model.View == dash.Logs {
			c.reloadLogs()
		}
		c.startRefresh()
	case 'b', 'B':
		if c.model.NoSession || c.benchmarking {
			return false, false, dashboardAction{}
		}
		if c.model.Session.Status != sessionstate.StatusReady {
			c.model.Notice = "Benchmark unavailable until the session is READY"
			return false, true, dashboardAction{}
		}
		c.modalMode = dashboardModalBenchmark
		c.model.Modal = &dash.Modal{Title: "RUN PERFORMANCE SAMPLE", Lines: []string{"Runs       1", "Tokens     128", "", "This sends one real generation request to the active model."}, Hint: "Enter run · Esc cancel"}
	case '+':
		c.openDeadlineChoice(deadlineExtend)
	case '-':
		c.openDeadlineChoice(deadlineShorten)
	case 'd':
		if !c.model.NoSession {
			c.modalMode = dashboardModalDownConfirm
			c.model.Modal = &dash.Modal{Title: "DESTROY SESSION?", Lines: []string{fmt.Sprintf("Instance       %d", c.model.Session.InstanceID), fmt.Sprintf("Remaining      %s", formatSessionDuration(c.model.Session.Remaining)), "", "This immediately destroys the Vast instance."}, Hint: "Press uppercase D to confirm · Esc cancel"}
		}
	default:
		return false, false, dashboardAction{}
	}
	return false, true, dashboardAction{}
}

func (c *dashboardController) navigateView(delta int) {
	count := int(dash.Logs) + 1
	next := (int(c.model.View) + delta) % count
	if next < 0 {
		next += count
	}
	c.model.View = dash.View(next)
	if c.model.View == dash.Logs {
		c.reloadLogs()
	}
}

func (c *dashboardController) handleModalKey(key byte) (bool, bool, dashboardAction) {
	if key == 27 {
		c.clearModal()
		return false, true, dashboardAction{}
	}
	switch c.modalMode {
	case dashboardModalBenchmark:
		if key == '\r' || key == '\n' {
			c.clearModal()
			return false, true, dashboardAction{Kind: dashboardActionBenchmark}
		}
	case dashboardModalResumeConfirm:
		if key == '\r' || key == '\n' {
			c.clearModal()
			return false, true, dashboardAction{Kind: dashboardActionResume}
		}
	case dashboardModalDeadlineChoice:
		var delta time.Duration
		switch key {
		case '1':
			delta = 15 * time.Minute
		case '2':
			delta = 30 * time.Minute
		case '3':
			delta = time.Hour
		case '4':
			c.modalMode = dashboardModalDeadlineCustom
			c.customDuration = ""
			c.renderCustomDurationModal()
			return false, true, dashboardAction{}
		default:
			return false, false, dashboardAction{}
		}
		c.prepareDeadlinePreview(delta)
		return false, true, dashboardAction{}
	case dashboardModalDeadlineCustom:
		if key == 127 || key == 8 {
			if len(c.customDuration) > 0 {
				c.customDuration = c.customDuration[:len(c.customDuration)-1]
			}
			c.renderCustomDurationModal()
			return false, true, dashboardAction{}
		}
		if key == '\r' || key == '\n' {
			delta, err := parseSessionDuration(c.customDuration)
			if err != nil {
				c.model.Error = err.Error()
				return false, true, dashboardAction{}
			}
			c.prepareDeadlinePreview(delta)
			return false, true, dashboardAction{}
		}
		if (key >= '0' && key <= '9') || key == 'h' || key == 'm' || key == 's' || key == '.' {
			c.customDuration += string(key)
			c.renderCustomDurationModal()
			return false, true, dashboardAction{}
		}
	case dashboardModalDeadlineConfirm:
		if key == '\r' || key == '\n' {
			kind := dashboardActionExtend
			if c.deadlineDirection == deadlineShorten {
				kind = dashboardActionShorten
			}
			action := dashboardAction{Kind: kind, Duration: c.deadlineDelta}
			c.clearModal()
			return false, true, action
		}
	case dashboardModalDownConfirm:
		if key == 'D' {
			c.clearModal()
			return false, true, dashboardAction{Kind: dashboardActionDown}
		}
	}
	return false, false, dashboardAction{}
}

func (c *dashboardController) openDeadlineChoice(direction deadlineDirection) {
	if c.model.NoSession {
		return
	}
	c.deadlineDirection = direction
	c.modalMode = dashboardModalDeadlineChoice
	title := "EXTEND SESSION"
	if direction == deadlineShorten {
		title = "SHORTEN SESSION"
	}
	c.model.Modal = &dash.Modal{Title: title, Lines: []string{"1   15m", "2   30m", "3   1h", "4   Custom"}, Hint: "Choose duration · Esc cancel"}
}

func (c *dashboardController) renderCustomDurationModal() {
	title := "CUSTOM EXTENSION"
	if c.deadlineDirection == deadlineShorten {
		title = "CUSTOM SHORTENING"
	}
	value := c.customDuration
	if value == "" {
		value = "_"
	}
	c.model.Modal = &dash.Modal{Title: title, Lines: []string{"Duration: " + value, "Examples: 20m, 45m, 1h30m"}, Hint: "Enter preview · Backspace edit · Esc cancel"}
}

func (c *dashboardController) prepareDeadlinePreview(delta time.Duration) {
	state, err := sessionstate.Load(c.paths)
	if err != nil {
		c.model.Error = err.Error()
		c.clearModal()
		return
	}
	preview, err := buildDeadlineMutationPreview(state, time.Now().UTC(), c.deadlineDirection, delta)
	if err != nil {
		c.model.Error = err.Error()
		c.clearModal()
		return
	}
	c.deadlineDelta = delta
	c.modalMode = dashboardModalDeadlineConfirm
	verb := "Extension"
	exposureLabel := "Additional exposure"
	exposure := preview.ExposureDeltaUSD
	if c.deadlineDirection == deadlineShorten {
		verb = "Reduction"
		exposureLabel = "Exposure reduction"
		exposure = -preview.ExposureDeltaUSD
	}
	c.model.Modal = &dash.Modal{Title: strings.ToUpper(string(c.deadlineDirection)) + " SESSION", Lines: []string{
		fmt.Sprintf("Current remaining     %s", formatSessionDuration(preview.CurrentRemaining)),
		fmt.Sprintf("Current deadline      %s", preview.PreviousDeadline.Local().Format("15:04:05")),
		fmt.Sprintf("%s             %s", verb, formatSessionDuration(delta)),
		fmt.Sprintf("New deadline          %s", preview.NewDeadline.Local().Format("15:04:05")),
		"",
		fmt.Sprintf("%s   $%.2f", exposureLabel, exposure),
		fmt.Sprintf("Projected session     $%.2f", preview.ProjectedUSD),
		fmt.Sprintf("Session ceiling       $%.2f", preview.SessionCeilingUSD),
	}, Hint: "Enter confirm · Esc cancel"}
}

func (c *dashboardController) clearModal() {
	c.modalMode = dashboardModalNone
	c.model.Modal = nil
	c.customDuration = ""
}

func (c *dashboardController) executeBlockingAction(action dashboardAction) error {
	switch action.Kind {
	case dashboardActionResume:
		return runResume(nil)
	case dashboardActionExtend:
		return runDeadlineMutation(deadlineExtend, []string{action.Duration.String(), "--yes"})
	case dashboardActionShorten:
		return runDeadlineMutation(deadlineShorten, []string{action.Duration.String(), "--yes"})
	case dashboardActionDown:
		return runDown(nil)
	default:
		return nil
	}
}

func dashboardActionSuccess(action dashboardAction) string {
	switch action.Kind {
	case dashboardActionResume:
		return "Session resumed"
	case dashboardActionExtend:
		return "Session extended by " + formatSessionDuration(action.Duration)
	case dashboardActionShorten:
		return "Session shortened by " + formatSessionDuration(action.Duration)
	case dashboardActionDown:
		return "Session destroyed"
	default:
		return ""
	}
}

func (c *dashboardController) startBenchmark() {
	if c.benchmarking || c.model.NoSession {
		return
	}
	c.benchmarking = true
	c.model.Perf.Benchmarking = true
	c.model.Notice = "Benchmarking active model…"
	paths := c.paths
	go func() {
		sample, err := runDashboardBenchmark(paths)
		c.benchmarkCh <- dashboardBenchmarkResult{Sample: sample, Err: err}
	}()
}

func (c *dashboardController) applyBenchmark(result dashboardBenchmarkResult) {
	c.benchmarking = false
	c.model.Perf.Benchmarking = false
	if result.Err != nil {
		c.model.Error = compactTelemetryError(result.Err)
		return
	}
	state, err := sessionstate.Load(c.paths)
	if err != nil {
		c.model.Error = err.Error()
		return
	}
	c.snapshot.Performance = loadPerformanceSnapshot(c.paths, state, time.Now().UTC())
	c.projectSnapshot()
	c.model.Notice = "Performance sample updated"
}

func (c *dashboardController) loadLocal() {
	state, err := sessionstate.Load(c.paths)
	if errors.Is(err, os.ErrNotExist) {
		c.snapshot = sessionSnapshot{}
		c.model.NoSession = true
		c.model.Session = dash.Session{}
		return
	}
	if err != nil {
		c.model.Error = err.Error()
		return
	}
	now := time.Now().UTC()
	c.snapshot = collectSessionSnapshot(context.Background(), c.paths, state, now, false, defaultSnapshotProbeDeps())
	c.projectSnapshot()
}

func (c *dashboardController) startRefresh() {
	if c.refreshing {
		return
	}
	c.refreshing = true
	c.model.Health.Refreshing = true
	paths := c.paths
	go func() {
		state, err := sessionstate.Load(paths)
		if errors.Is(err, os.ErrNotExist) {
			c.refreshCh <- dashboardLoadResult{NoSession: true}
			return
		}
		if err != nil {
			c.refreshCh <- dashboardLoadResult{Err: err}
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		snapshot := collectSessionSnapshot(ctx, paths, state, time.Now().UTC(), true, defaultSnapshotProbeDeps())
		c.refreshCh <- dashboardLoadResult{Snapshot: snapshot}
	}()
}

func (c *dashboardController) applyRefresh(result dashboardLoadResult) {
	c.refreshing = false
	c.model.Health.Refreshing = false
	if result.Err != nil {
		c.model.Error = compactTelemetryError(result.Err)
		return
	}
	if result.NoSession {
		c.snapshot = sessionSnapshot{}
		c.model.NoSession = true
		c.model.Session = dash.Session{}
		c.model.Health = dash.Health{}
		c.model.GPU = dash.GPU{}
		c.model.Perf = dash.Perf{}
		c.model.Inference = dash.Inference{}
		c.lastGoodInference = dash.Inference{}
		c.lastGoodAt = time.Time{}
		c.lastGoodInstance = 0
		c.model.Notice = "The recorded session has ended."
		return
	}
	c.snapshot = result.Snapshot
	c.projectSnapshot()
}

func (c *dashboardController) tick(now time.Time) {
	if c.model.NoSession {
		return
	}
	started := c.snapshot.Time.StartedAt
	deadline := c.snapshot.Time.Deadline
	elapsed := time.Duration(0)
	if !started.IsZero() && now.After(started) {
		elapsed = now.Sub(started)
	}
	remaining := time.Duration(0)
	if !deadline.IsZero() && deadline.After(now) {
		remaining = deadline.Sub(now)
	}
	c.snapshot.Time.Elapsed = elapsed
	c.snapshot.Time.Remaining = remaining
	c.snapshot.Time.Expired = !deadline.IsZero() && !now.Before(deadline)
	c.snapshot.Cost.EstimatedSpentUSD = scheduledCostUSD(c.snapshot.Cost.HourlyUSD, elapsed)
	if c.snapshot.Performance.Available {
		age := now.Sub(c.snapshot.Performance.SampledAt)
		if age < 0 {
			age = 0
		}
		c.snapshot.Performance.Age = age
	}
	c.projectSnapshot()
}

func (c *dashboardController) projectSnapshot() {
	s := c.snapshot
	c.model.NoSession = s.Session.InstanceID <= 0
	if c.model.NoSession {
		return
	}
	status := dashboardDisplayStatus(s)
	c.model.Session = dash.Session{InstanceID: s.Session.InstanceID, Status: status, Model: s.Session.Model, GPUModel: s.Session.GPUModel, Runtime: s.Session.Runtime, Context: s.Session.ContextTokens, Profile: s.Session.Profile, Rate: s.Cost.HourlyUSD, Spent: s.Cost.EstimatedSpentUSD, Exposure: s.Cost.ScheduledUSD, Started: s.Time.StartedAt, Deadline: s.Time.Deadline, Elapsed: s.Time.Elapsed, Remaining: s.Time.Remaining, Scheduled: s.Time.ScheduledDuration}
	c.model.Health = dash.Health{Endpoint: dashboardEndpointLabel(s.Health.Endpoint), Tunnel: dashboardRunningLabel(s.Health.Tunnel.Running), Runtime: dashboardRuntimeLabel(s.Health.Runtime), Watchdog: dashboardRunningLabel(s.Health.Watchdog.Running), SSH: dashboardSSHLabel(s.Health.Runtime), Refreshed: s.Health.Endpoint.Refreshed || s.Health.Runtime.Refreshed, Refreshing: c.refreshing}
	c.model.GPU = dashboardGPU(s.GPU)
	perf := dashboardPerf(s.Performance)
	perf.Benchmarking = c.benchmarking
	c.model.Perf = perf
	c.model.Inference = c.projectInference(s)
	c.model.Logs = c.logs
	if notice := dashboardRecoveryNotice(s); notice != "" && c.model.Modal == nil {
		c.model.Notice = notice
	}
}

func dashboardEndpointLabel(value endpointHealth) string {
	if !value.Refreshed {
		return "not refreshed"
	}
	if value.Healthy {
		return fmt.Sprintf("healthy · %.0fms", float64(value.Latency)/float64(time.Millisecond))
	}
	return "unavailable"
}
func dashboardRunningLabel(running bool) string {
	if running {
		return "running"
	}
	return "not running"
}
func dashboardRuntimeLabel(value runtimeHealth) string {
	if !value.Refreshed {
		return "not refreshed"
	}
	if value.Running {
		return "running"
	}
	return "not running"
}
func dashboardSSHLabel(value runtimeHealth) string {
	if !value.Refreshed {
		return "not refreshed"
	}
	if value.SSH {
		return "healthy"
	}
	return "unavailable"
}

func dashboardGPU(value gpuTelemetry) dash.GPU {
	result := dash.GPU{Available: value.Available, Error: value.Meta.Error}
	if value.UtilizationPercent != nil {
		result.Utilization = fmt.Sprintf("%.0f%% load", *value.UtilizationPercent)
	}
	if value.MemoryUsedMiB != nil && value.MemoryTotalMiB != nil {
		result.VRAM = fmt.Sprintf("%.1f / %.1f GB VRAM", *value.MemoryUsedMiB/1024, *value.MemoryTotalMiB/1024)
	}
	if value.PowerDrawW != nil {
		if value.PowerLimitW != nil {
			result.Power = fmt.Sprintf("%.0f / %.0f W", *value.PowerDrawW, *value.PowerLimitW)
		} else {
			result.Power = fmt.Sprintf("%.0f W", *value.PowerDrawW)
		}
	}
	if value.TemperatureC != nil {
		result.Temperature = fmt.Sprintf("%.0f C", *value.TemperatureC)
	}
	return result
}

func dashboardPerf(value performanceSnapshot) dash.Perf {
	result := dash.Perf{Available: value.Available, Error: value.UnavailableReason}
	if !value.Available {
		return result
	}
	result.TTFT = fmt.Sprintf("%.2fs", value.TTFT.Seconds())
	result.Total = fmt.Sprintf("%.2fs", value.TotalLatency.Seconds())
	result.Decode = fmt.Sprintf("%.1f tok/s", value.DecodeTokensSec)
	result.PromptTokens = value.PromptTokens
	result.CompletionTokens = value.CompletionTokens
	result.Age = formatSessionDuration(value.Age)
	return result
}

func dashboardInference(value inferenceTelemetry) dash.Inference {
	result := dash.Inference{Refreshed: value.Refreshed, Available: value.Available, Error: value.UnavailableReason}
	if value.Meta.Error != "" {
		result.Error = value.Meta.Error
	}
	if !value.Available {
		return result
	}
	result.Agents = value.Agents
	result.Depth = value.ResidentDepth
	result.ContextUsed, result.Clients = dashboardClientContexts(value.Lanes)
	if value.DecodeTokensSec != nil {
		result.Decode = fmt.Sprintf("%.1f tok/s", *value.DecodeTokensSec)
	}
	if value.PrefillTokensSec != nil {
		result.Prefill = fmt.Sprintf("%.1f tok/s", *value.PrefillTokensSec)
	}
	if value.Deferred > 0 {
		result.Queue = fmt.Sprintf("%d queued", value.Deferred)
	}
	if value.CacheReuseRatio != nil {
		result.CacheReuse = fmt.Sprintf("%.0f%%", *value.CacheReuseRatio*100)
	}
	if value.SpecAcceptRatio != nil {
		result.Speculative = fmt.Sprintf("%.0f%% accepted", *value.SpecAcceptRatio*100)
	}
	result.Lanes = inferenceLaneSummary(value.Lanes)
	return result
}

// projectInference displays the live inference domain. When a refresh probe
// fails after a good sample exists (slow tunnel, engine restart), it keeps
// the last good sample visible, marked stale with its age, instead of the
// panel flipping to "unavailable" for that cycle (observed flicker at peak
// two-client load, 2026-09-03). A successful probe replaces the retained
// sample, and a different session discards it.
func (c *dashboardController) projectInference(s sessionSnapshot) dash.Inference {
	sample := dashboardInference(s.Inference)
	if s.Session.InstanceID != c.lastGoodInstance {
		c.lastGoodInference = dash.Inference{}
		c.lastGoodAt = time.Time{}
		c.lastGoodInstance = s.Session.InstanceID
	}
	if s.Inference.Refreshed && s.Inference.Available {
		c.lastGoodInference = sample
		if !s.Inference.Meta.SampledAt.IsZero() {
			c.lastGoodAt = s.Inference.Meta.SampledAt
		} else {
			c.lastGoodAt = time.Now().UTC()
		}
		return sample
	}
	if c.lastGoodAt.IsZero() {
		return sample
	}
	retained := c.lastGoodInference
	retained.Stale = true
	retained.Age = formatSessionDuration(time.Since(c.lastGoodAt))
	return retained
}

func (c *dashboardController) reloadLogs() {
	c.logs = loadDashboardLogs(c.paths, 120)
	c.model.Logs = c.logs
}

func loadDashboardLogs(paths config.Paths, limit int) []string {
	if limit <= 0 {
		return nil
	}
	var all []string
	for _, name := range []string{"tunnel.log", "watchdog.log"} {
		path := filepath.Join(paths.StateDir, name)
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		var lines []string
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				lines = append(lines, name+": "+line)
			}
		}
		_ = file.Close()
		if len(lines) > limit/2 {
			lines = lines[len(lines)-limit/2:]
		}
		all = append(all, lines...)
	}
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all
}
