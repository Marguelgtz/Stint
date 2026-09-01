package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	localenv "github.com/Marguelgtz/Stint/internal/local"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

// The current main command table dispatches status without its argument slice.
// Preserve the existing zero-argument path and intercept only flagged status
// invocations here. This keeps telemetry work isolated from a broader router
// refactor while still making `status --refresh/--json` first-class CLI forms.
func init() {
	if len(os.Args) < 3 || os.Args[1] != "status" || wantsHelp(os.Args[2:]) {
		return
	}
	if err := runStatusTelemetry(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "stint:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func runStatusTelemetry(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	refresh := fs.Bool("refresh", false, "collect endpoint, runtime, GPU and live-inference telemetry")
	jsonOutput := fs.Bool("json", false, "print the snapshot as machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("status does not accept positional arguments")
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	state, err := sessionstate.Load(paths)
	if errors.Is(err, os.ErrNotExist) {
		if *jsonOutput {
			payload := map[string]any{"collectedAt": time.Now().UTC(), "active": false}
			encoded, marshalErr := json.MarshalIndent(payload, "", "  ")
			if marshalErr != nil {
				return marshalErr
			}
			fmt.Println(string(encoded))
			return nil
		}
		printStatusPreamble(paths)
		fmt.Println("Active compute     none")
		return nil
	}
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	ctx := context.Background()
	if *refresh {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
	}
	snapshot := collectSessionSnapshot(ctx, paths, state, now, *refresh, defaultSnapshotProbeDeps())
	if *jsonOutput {
		encoded, err := json.MarshalIndent(snapshotJSON(snapshot), "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return nil
	}
	printStatusPreamble(paths)
	printSessionSnapshotHuman(snapshot, *refresh)
	return nil
}

func printStatusPreamble(paths config.Paths) {
	_, credentialsErr := config.LoadCredentials(paths)
	fmt.Println(ui.accent("Stint local status"))
	fmt.Printf("%s%s\n", ui.pad(ui.muted("Vast provider"), 19), yesNo(credentialsErr == nil))
	fmt.Printf("%s%s\n", ui.pad(ui.muted("Stint SSH key"), 19), yesNo(localenv.SSHKeyExists(paths)))
	fmt.Printf("%s%s\n", ui.pad(ui.muted("Cline endpoint"), 19), ui.accent(fmt.Sprintf("http://127.0.0.1:%d/v1", clinePort)))
	fmt.Printf("%s%s\n", ui.pad(ui.muted("Product identity"), 19), "GitHub (same model as Spark; hosted login not needed for local pre-v0)")
}

func printSessionSnapshotHuman(snapshot sessionSnapshot, refreshed bool) {
	fmt.Printf("%s%s\n", ui.pad(ui.muted("Active compute"), 19), fmt.Sprintf("instance %d (%s)", snapshot.Session.InstanceID, sessionStatusColor(snapshot.Session.Status)))
	fmt.Printf("%s%s\n", ui.pad(ui.muted("GPU"), 19), snapshot.Session.GPUModel)
	fmt.Printf("%s%s\n", ui.pad(ui.muted("Runtime"), 19), snapshot.Session.Runtime)
	if snapshot.Session.Runtime == runtimeNInfer {
		fmt.Printf("%s%s\n", ui.pad(ui.muted("NInfer config"), 19), ninferConfigForContext(snapshot.Session.ContextTokens).Name)
	}
	fmt.Printf("%s%s\n", ui.pad(ui.muted("Model"), 19), snapshot.Session.Model)
	fmt.Printf("%s%s\n", ui.pad(ui.muted("Context"), 19), fmt.Sprintf("%d tokens", snapshot.Session.ContextTokens))
	fmt.Printf("%s%s\n", ui.pad(ui.muted("Rate"), 19), ui.accent(fmt.Sprintf("$%.3f/hr", snapshot.Cost.HourlyUSD)))
	if !snapshot.Time.StartedAt.IsZero() {
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Started"), 19), snapshot.Time.StartedAt.Local().Format(time.RFC1123))
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Elapsed"), 19), formatSessionDuration(snapshot.Time.Elapsed))
	}
	if snapshot.Time.Expired {
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Remaining"), 19), ui.danger("expired"))
	} else if !snapshot.Time.Deadline.IsZero() {
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Remaining"), 19), formatSessionDuration(snapshot.Time.Remaining))
	}
	fmt.Printf("%s%s\n", ui.pad(ui.muted("Spent estimate"), 19), fmt.Sprintf("$%.2f", snapshot.Cost.EstimatedSpentUSD))
	fmt.Printf("%s%s\n", ui.pad(ui.muted("Session exposure"), 19), fmt.Sprintf("$%.2f scheduled", snapshot.Cost.ScheduledUSD))
	if !snapshot.Time.Deadline.IsZero() {
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Auto-destroy"), 19), snapshot.Time.Deadline.Local().Format(time.RFC1123))
	}

	fmt.Println("\n" + ui.accent("HEALTH"))
	fmt.Printf("%s%s", ui.pad(ui.muted("Tunnel"), 19), runningLabel(snapshot.Health.Tunnel.Running))
	if snapshot.Health.Tunnel.PID > 0 {
		fmt.Printf(" (pid %d)", snapshot.Health.Tunnel.PID)
	}
	fmt.Println()
	fmt.Printf("%s%s", ui.pad(ui.muted("Watchdog"), 19), runningLabel(snapshot.Health.Watchdog.Running))
	if snapshot.Health.Watchdog.PID > 0 {
		fmt.Printf(" (pid %d)", snapshot.Health.Watchdog.PID)
	}
	fmt.Println()
	if refreshed {
		if snapshot.Health.Endpoint.Healthy {
			fmt.Printf("%s%s\n", ui.pad(ui.muted("Endpoint"), 19), fmt.Sprintf("healthy · %.0fms", float64(snapshot.Health.Endpoint.Latency)/float64(time.Millisecond)))
		} else {
			fmt.Printf("%s%s\n", ui.pad(ui.muted("Endpoint"), 19), fmt.Sprintf("unavailable%s", telemetryErrorSuffix(snapshot.Health.Endpoint.Meta.Error)))
		}
		if snapshot.Health.Runtime.SSH {
			fmt.Printf("%s%s\n", ui.pad(ui.muted("SSH"), 19), ui.success("healthy"))
		} else {
			fmt.Printf("%s%s\n", ui.pad(ui.muted("SSH"), 19), fmt.Sprintf("unavailable%s", telemetryErrorSuffix(snapshot.Health.Runtime.Meta.Error)))
		}
		fmt.Printf("%s%s", ui.pad(ui.muted("Runtime process"), 19), runningLabel(snapshot.Health.Runtime.Running))
		if snapshot.Health.Runtime.Meta.Error != "" && snapshot.Health.Runtime.SSH {
			fmt.Printf(" · %s", snapshot.Health.Runtime.Meta.Error)
		}
		fmt.Println()
	} else {
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Endpoint"), 19), ui.muted("not refreshed (use --refresh)"))
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Remote runtime"), 19), ui.muted("not refreshed (use --refresh)"))
	}

	fmt.Println("\n" + ui.accent("GPU TELEMETRY"))
	if !refreshed {
		fmt.Printf("%s%s\n", ui.pad(ui.muted("GPU metrics"), 19), ui.muted("not refreshed (use --refresh)"))
	} else if !snapshot.GPU.Available {
		fmt.Printf("%s%s\n", ui.pad(ui.muted("GPU metrics"), 19), fmt.Sprintf("unavailable%s", telemetryErrorSuffix(snapshot.GPU.Meta.Error)))
	} else {
		if snapshot.GPU.UtilizationPercent != nil {
			fmt.Printf("%s%s\n", ui.pad(ui.muted("Utilization"), 19), fmt.Sprintf("%.0f%%", *snapshot.GPU.UtilizationPercent))
		}
		if snapshot.GPU.MemoryUsedMiB != nil && snapshot.GPU.MemoryTotalMiB != nil {
			fmt.Printf("%s%s\n", ui.pad(ui.muted("VRAM"), 19), fmt.Sprintf("%.1f / %.1f GB", *snapshot.GPU.MemoryUsedMiB/1024, *snapshot.GPU.MemoryTotalMiB/1024))
		}
		if snapshot.GPU.TemperatureC != nil {
			fmt.Printf("%s%s\n", ui.pad(ui.muted("Temperature"), 19), fmt.Sprintf("%.0f C", *snapshot.GPU.TemperatureC))
		}
		if snapshot.GPU.PowerDrawW != nil {
			if snapshot.GPU.PowerLimitW != nil {
				fmt.Printf("%s%s\n", ui.pad(ui.muted("Power"), 19), fmt.Sprintf("%.0f / %.0f W", *snapshot.GPU.PowerDrawW, *snapshot.GPU.PowerLimitW))
			} else {
				fmt.Printf("%s%s\n", ui.pad(ui.muted("Power"), 19), fmt.Sprintf("%.0f W", *snapshot.GPU.PowerDrawW))
			}
		}
	}

	fmt.Println("\n" + ui.accent("INFERENCE LIVE"))
	if !refreshed {
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Live inference"), 19), ui.muted("not refreshed (use --refresh)"))
	} else if !snapshot.Inference.Available {
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Live inference"), 19), fmt.Sprintf("unavailable%s", inferenceUnavailableSuffix(snapshot.Inference)))
	} else {
		agents := snapshot.Inference.Agents
		if agents == 0 {
			fmt.Printf("%s%s\n", ui.pad(ui.muted("Agents"), 19), "0 active (engine idle)")
		} else {
			fmt.Printf("%s%s\n", ui.pad(ui.muted("Agents"), 19), fmt.Sprintf("%d active", agents))
		}
		fmt.Printf("%s%s", ui.pad(ui.muted("Live prompt depth"), 19), fmt.Sprintf("%d tokens", snapshot.Inference.ResidentDepth))
		if snapshot.Inference.Deferred > 0 {
			fmt.Printf(" · %d queued", snapshot.Inference.Deferred)
		}
		fmt.Println()
		if snapshot.Inference.DecodeTokensSec != nil {
			fmt.Printf("%s%s\n", ui.pad(ui.muted("Decode"), 19), fmt.Sprintf("%.1f tok/s", *snapshot.Inference.DecodeTokensSec))
		}
		if snapshot.Inference.PrefillTokensSec != nil {
			fmt.Printf("%s%s\n", ui.pad(ui.muted("Prefill"), 19), fmt.Sprintf("%.1f tok/s", *snapshot.Inference.PrefillTokensSec))
		}
		if snapshot.Inference.CacheReuseRatio != nil {
			fmt.Printf("%s%s\n", ui.pad(ui.muted("Cache reuse"), 19), fmt.Sprintf("%.0f%% of prompt", *snapshot.Inference.CacheReuseRatio*100))
		}
		if snapshot.Inference.SpecAcceptRatio != nil {
			fmt.Printf("%s%s\n", ui.pad(ui.muted("Speculative"), 19), fmt.Sprintf("%.0f%% accepted", *snapshot.Inference.SpecAcceptRatio*100))
		}
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Lanes"), 19), inferenceLaneSummary(snapshot.Inference.Lanes))
	}

	fmt.Println("\n" + ui.accent("PERFORMANCE"))
	if snapshot.Performance.Available {
		fmt.Printf("%s%s\n", ui.pad(ui.muted("TTFT"), 19), fmt.Sprintf("%.2fs", snapshot.Performance.TTFT.Seconds()))
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Decode"), 19), fmt.Sprintf("%.1f tok/s", snapshot.Performance.DecodeTokensSec))
		if snapshot.Performance.PromptTokens > 0 {
			fmt.Printf("%s%s\n", ui.pad(ui.muted("Sample prompt"), 17), fmt.Sprintf("%d tokens", snapshot.Performance.PromptTokens))
		}
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Sample"), 19), formatSessionDuration(snapshot.Performance.Age)+" ago")
	} else {
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Sample"), 19), "unavailable · "+snapshot.Performance.UnavailableReason)
	}
	if snapshot.Session.Checkpoint != "" {
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Checkpoint"), 19), snapshot.Session.Checkpoint)
	}
	if snapshot.Session.LastError != "" {
		lastError := strings.ReplaceAll(snapshot.Session.LastError, "\n", " ")
		if len(lastError) > 140 {
			lastError = lastError[:137] + "..."
		}
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Last error"), 19), ui.danger(lastError))
	}
	switch snapshot.Session.Status {
	case sessionstate.StatusRecoverable:
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Next action"), 19), ui.accent("stint resume"))
	case sessionstate.StatusReady:
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Next action"), 19), "use Cline; "+ui.accent("stint down")+" when finished")
	default:
		fmt.Printf("%s%s\n", ui.pad(ui.muted("Next action"), 19), ui.muted("wait for stint start"))
	}
}

func runningLabel(running bool) string {
	if running {
		return ui.success("running")
	}
	return ui.muted("not running")
}

// sessionStatusColor maps a session status to a colored label mirroring the
// dashboard's status styling (READY green, RECOVERABLE amber, anything else
// muted). It is a no-op when color is disabled so status output stays stable.
func sessionStatusColor(status string) string {
	switch status {
	case string(sessionstate.StatusReady):
		return ui.success(status)
	case string(sessionstate.StatusRecoverable):
		return ui.warn(status)
	default:
		return ui.muted(status)
	}
}

func inferenceUnavailableSuffix(inf inferenceTelemetry) string {
	if reason := strings.TrimSpace(inf.UnavailableReason); reason != "" {
		return " · " + reason
	}
	return telemetryErrorSuffix(inf.Meta.Error)
}

func inferenceLaneSummary(lanes []inferenceLane) string {
	if len(lanes) == 0 {
		return "no lanes reported"
	}
	parts := make([]string, 0, len(lanes))
	for _, lane := range lanes {
		switch {
		case lane.Processing:
			parts = append(parts, fmt.Sprintf("%d: %d tok", lane.ID, lane.NPrompt))
		case lane.NPrompt > 0:
			parts = append(parts, fmt.Sprintf("%d: %d tok (resident)", lane.ID, lane.NPrompt))
		default:
			parts = append(parts, fmt.Sprintf("%d: idle", lane.ID))
		}
	}
	return strings.Join(parts, " · ")
}

func telemetryErrorSuffix(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return " · " + compactTelemetryError(errors.New(text))
}

func snapshotJSON(snapshot sessionSnapshot) map[string]any {
	return map[string]any{
		"collectedAt": snapshot.CollectedAt,
		"active":      true,
		"session":     snapshot.Session,
		"time": map[string]any{
			"startedAt":                snapshot.Time.StartedAt,
			"deadline":                 snapshot.Time.Deadline,
			"elapsedSeconds":           snapshot.Time.Elapsed.Seconds(),
			"remainingSeconds":         snapshot.Time.Remaining.Seconds(),
			"scheduledDurationSeconds": snapshot.Time.ScheduledDuration.Seconds(),
			"expired":                  snapshot.Time.Expired,
		},
		"cost": snapshot.Cost,
		"health": map[string]any{
			"tunnel":   snapshot.Health.Tunnel,
			"watchdog": snapshot.Health.Watchdog,
			"endpoint": map[string]any{
				"refreshed":           snapshot.Health.Endpoint.Refreshed,
				"healthy":             snapshot.Health.Endpoint.Healthy,
				"statusCode":          snapshot.Health.Endpoint.StatusCode,
				"latencyMilliseconds": float64(snapshot.Health.Endpoint.Latency) / float64(time.Millisecond),
				"modelVisible":        snapshot.Health.Endpoint.ModelVisible,
				"sampledAt":           snapshot.Health.Endpoint.Meta.SampledAt,
				"error":               snapshot.Health.Endpoint.Meta.Error,
			},
			"runtime": snapshot.Health.Runtime,
		},
		"gpu": snapshot.GPU,
		"inference": map[string]any{
			"refreshed":         snapshot.Inference.Refreshed,
			"available":         snapshot.Inference.Available,
			"processing":        snapshot.Inference.Processing,
			"deferred":          snapshot.Inference.Deferred,
			"agents":            snapshot.Inference.Agents,
			"residentDepth":     snapshot.Inference.ResidentDepth,
			"decodeTokensSec":   snapshot.Inference.DecodeTokensSec,
			"prefillTokensSec":  snapshot.Inference.PrefillTokensSec,
			"cacheReuseRatio":   snapshot.Inference.CacheReuseRatio,
			"specAcceptRatio":   snapshot.Inference.SpecAcceptRatio,
			"lanes":             snapshot.Inference.Lanes,
			"unavailableReason": snapshot.Inference.UnavailableReason,
			"sampledAt":         snapshot.Inference.Meta.SampledAt,
			"error":             snapshot.Inference.Meta.Error,
		},
		"performance": map[string]any{
			"available":         snapshot.Performance.Available,
			"ttftMilliseconds":  float64(snapshot.Performance.TTFT) / float64(time.Millisecond),
			"totalMilliseconds": float64(snapshot.Performance.TotalLatency) / float64(time.Millisecond),
			"promptTokens":      snapshot.Performance.PromptTokens,
			"completionTokens":  snapshot.Performance.CompletionTokens,
			"decodeTokensSec":   snapshot.Performance.DecodeTokensSec,
			"sampledAt":         snapshot.Performance.SampledAt,
			"ageSeconds":        snapshot.Performance.Age.Seconds(),
			"unavailableReason": snapshot.Performance.UnavailableReason,
		},
	}
}
