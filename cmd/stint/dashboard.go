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
	paths      config.Paths
	model      dash.Model
	snapshot   sessionSnapshot
	refreshing bool
	refreshCh  chan dashboardLoadResult
	logs       []string
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
		// Piped dashboard output should stay useful and contain no terminal control
		// sequences. Reuse the status snapshot instead of emitting an ANSI TUI.
		return runStatusTelemetry([]string{"--refresh"})
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	width, height := dash.Size()
	controller := &dashboardController{
		paths: paths,
		model: dash.Model{
			Width:   width,
			Height:  height,
			NoColor: *noColor || os.Getenv("NO_COLOR") != "",
			View:    dash.Home,
		},
		refreshCh: make(chan dashboardLoadResult, 1),
	}
	controller.loadLocal()
	controller.startRefresh()

	terminal, err := dash.OpenTerminal(os.Stdin, os.Stdout)
	if err != nil {
		return err
	}
	defer terminal.Restore()
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
			quit, changed := controller.handleKey(key)
			if quit {
				return nil
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
	buf := make([]byte, 1)
	for {
		n, err := reader.Read(buf)
		if n == 1 {
			keys <- buf[0]
		}
		if err != nil {
			errs <- err
			return
		}
	}
}

func (c *dashboardController) handleKey(key byte) (quit, changed bool) {
	if key == 3 || key == 'q' || key == 'Q' {
		return true, false
	}
	c.model.Notice = ""
	c.model.Error = ""
	switch key {
	case '1':
		c.model.View = dash.Home
		return false, true
	case '2':
		c.model.View = dash.Performance
		return false, true
	case '3':
		c.model.View = dash.Config
		return false, true
	case '4':
		c.model.View = dash.Logs
		c.reloadLogs()
		return false, true
	case '\t':
		c.model.View = (c.model.View + 1) % 4
		if c.model.View == dash.Logs {
			c.reloadLogs()
		}
		return false, true
	case 'r', 'R':
		if c.model.View == dash.Logs {
			c.reloadLogs()
		}
		c.startRefresh()
		return false, true
	case 'b', 'B':
		c.model.Notice = "Benchmark action is disabled until the read-only dashboard slice is validated."
		return false, true
	case '+', '-':
		c.model.Notice = "Deadline actions are disabled until the read-only dashboard slice is validated."
		return false, true
	case 'd', 'D':
		c.model.Notice = "Down is disabled until the read-only dashboard slice is validated."
		return false, true
	default:
		return false, false
	}
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
	status := s.Session.Status
	if s.Time.Expired {
		status = "EXPIRED"
	}
	c.model.Session = dash.Session{
		InstanceID: s.Session.InstanceID,
		Status:     status,
		Model:      s.Session.Model,
		GPUModel:   s.Session.GPUModel,
		Runtime:    s.Session.Runtime,
		Context:    s.Session.ContextTokens,
		Profile:    s.Session.Profile,
		Rate:       s.Cost.HourlyUSD,
		Spent:      s.Cost.EstimatedSpentUSD,
		Exposure:   s.Cost.ScheduledUSD,
		Started:    s.Time.StartedAt,
		Deadline:   s.Time.Deadline,
		Elapsed:    s.Time.Elapsed,
		Remaining:  s.Time.Remaining,
		Scheduled:  s.Time.ScheduledDuration,
	}
	c.model.Health = dash.Health{
		Endpoint:   dashboardEndpointLabel(s.Health.Endpoint),
		Tunnel:     dashboardRunningLabel(s.Health.Tunnel.Running),
		Runtime:    dashboardRuntimeLabel(s.Health.Runtime),
		Watchdog:   dashboardRunningLabel(s.Health.Watchdog.Running),
		SSH:        dashboardSSHLabel(s.Health.Runtime),
		Refreshed:  s.Health.Endpoint.Refreshed || s.Health.Runtime.Refreshed,
		Refreshing: c.refreshing,
	}
	c.model.GPU = dashboardGPU(s.GPU)
	c.model.Perf = dashboardPerf(s.Performance)
	c.model.Logs = c.logs
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
