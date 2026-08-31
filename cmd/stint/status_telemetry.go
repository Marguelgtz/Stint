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
	refresh := fs.Bool("refresh", false, "collect endpoint, runtime and GPU telemetry")
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
	fmt.Println("Stint local status")
	fmt.Printf("Vast provider      %s\n", yesNo(credentialsErr == nil))
	fmt.Printf("Stint SSH key      %s\n", yesNo(localenv.SSHKeyExists(paths)))
	fmt.Printf("Cline endpoint     http://127.0.0.1:%d/v1\n", clinePort)
	fmt.Println("Product identity   GitHub (same model as Spark; hosted login not needed for local pre-v0)")
}

func printSessionSnapshotHuman(snapshot sessionSnapshot, refreshed bool) {
	fmt.Printf("Active compute     instance %d (%s)\n", snapshot.Session.InstanceID, snapshot.Session.Status)
	fmt.Printf("GPU                %s\n", snapshot.Session.GPUModel)
	fmt.Printf("Runtime            %s\n", snapshot.Session.Runtime)
	fmt.Printf("Model              %s\n", snapshot.Session.Model)
	fmt.Printf("Context            %d tokens\n", snapshot.Session.ContextTokens)
	fmt.Printf("Rate               $%.3f/hr\n", snapshot.Cost.HourlyUSD)
	if !snapshot.Time.StartedAt.IsZero() {
		fmt.Printf("Started            %s\n", snapshot.Time.StartedAt.Local().Format(time.RFC1123))
		fmt.Printf("Elapsed            %s\n", formatSessionDuration(snapshot.Time.Elapsed))
	}
	if snapshot.Time.Expired {
		fmt.Println("Remaining          expired")
	} else if !snapshot.Time.Deadline.IsZero() {
		fmt.Printf("Remaining          %s\n", formatSessionDuration(snapshot.Time.Remaining))
	}
	fmt.Printf("Spent estimate     $%.2f\n", snapshot.Cost.EstimatedSpentUSD)
	fmt.Printf("Session exposure   $%.2f scheduled\n", snapshot.Cost.ScheduledUSD)
	if !snapshot.Time.Deadline.IsZero() {
		fmt.Printf("Auto-destroy       %s\n", snapshot.Time.Deadline.Local().Format(time.RFC1123))
	}

	fmt.Println("\nHEALTH")
	fmt.Printf("Tunnel             %s", runningLabel(snapshot.Health.Tunnel.Running))
	if snapshot.Health.Tunnel.PID > 0 {
		fmt.Printf(" (pid %d)", snapshot.Health.Tunnel.PID)
	}
	fmt.Println()
	fmt.Printf("Watchdog           %s", runningLabel(snapshot.Health.Watchdog.Running))
	if snapshot.Health.Watchdog.PID > 0 {
		fmt.Printf(" (pid %d)", snapshot.Health.Watchdog.PID)
	}
	fmt.Println()
	if refreshed {
		if snapshot.Health.Endpoint.Healthy {
			fmt.Printf("Endpoint           healthy · %.0fms\n", float64(snapshot.Health.Endpoint.Latency)/float64(time.Millisecond))
		} else {
			fmt.Printf("Endpoint           unavailable%s\n", telemetryErrorSuffix(snapshot.Health.Endpoint.Meta.Error))
		}
		if snapshot.Health.Runtime.SSH {
			fmt.Printf("SSH                healthy\n")
		} else {
			fmt.Printf("SSH                unavailable%s\n", telemetryErrorSuffix(snapshot.Health.Runtime.Meta.Error))
		}
		fmt.Printf("Runtime process    %s", runningLabel(snapshot.Health.Runtime.Running))
		if snapshot.Health.Runtime.Meta.Error != "" && snapshot.Health.Runtime.SSH {
			fmt.Printf(" · %s", snapshot.Health.Runtime.Meta.Error)
		}
		fmt.Println()
	} else {
		fmt.Println("Endpoint           not refreshed (use --refresh)")
		fmt.Println("Remote runtime     not refreshed (use --refresh)")
	}

	fmt.Println("\nGPU TELEMETRY")
	if !refreshed {
		fmt.Println("GPU metrics        not refreshed (use --refresh)")
	} else if !snapshot.GPU.Available {
		fmt.Printf("GPU metrics        unavailable%s\n", telemetryErrorSuffix(snapshot.GPU.Meta.Error))
	} else {
		if snapshot.GPU.UtilizationPercent != nil {
			fmt.Printf("Utilization        %.0f%%\n", *snapshot.GPU.UtilizationPercent)
		}
		if snapshot.GPU.MemoryUsedMiB != nil && snapshot.GPU.MemoryTotalMiB != nil {
			fmt.Printf("VRAM               %.1f / %.1f GB\n", *snapshot.GPU.MemoryUsedMiB/1024, *snapshot.GPU.MemoryTotalMiB/1024)
		}
		if snapshot.GPU.TemperatureC != nil {
			fmt.Printf("Temperature        %.0f C\n", *snapshot.GPU.TemperatureC)
		}
		if snapshot.GPU.PowerDrawW != nil {
			if snapshot.GPU.PowerLimitW != nil {
				fmt.Printf("Power              %.0f / %.0f W\n", *snapshot.GPU.PowerDrawW, *snapshot.GPU.PowerLimitW)
			} else {
				fmt.Printf("Power              %.0f W\n", *snapshot.GPU.PowerDrawW)
			}
		}
	}

	fmt.Println("\nPERFORMANCE")
	if snapshot.Performance.Available {
		fmt.Printf("TTFT               %.2fs\n", snapshot.Performance.TTFT.Seconds())
		fmt.Printf("Decode             %.1f tok/s\n", snapshot.Performance.DecodeTokensSec)
		fmt.Printf("Sample             %s ago\n", formatSessionDuration(snapshot.Performance.Age))
	} else {
		fmt.Printf("Sample             unavailable · %s\n", snapshot.Performance.UnavailableReason)
	}
	if snapshot.Session.Checkpoint != "" {
		fmt.Printf("Checkpoint         %s\n", snapshot.Session.Checkpoint)
	}
	if snapshot.Session.LastError != "" {
		lastError := strings.ReplaceAll(snapshot.Session.LastError, "\n", " ")
		if len(lastError) > 140 {
			lastError = lastError[:137] + "..."
		}
		fmt.Printf("Last error         %s\n", lastError)
	}
	switch snapshot.Session.Status {
	case sessionstate.StatusRecoverable:
		fmt.Println("Next action        stint resume")
	case sessionstate.StatusReady:
		fmt.Println("Next action        use Cline; stint down when finished")
	default:
		fmt.Println("Next action        wait for stint start")
	}
}

func runningLabel(running bool) string {
	if running {
		return "running"
	}
	return "not running"
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
