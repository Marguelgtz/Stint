package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/core"
	"github.com/Marguelgtz/Stint/internal/router"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

type deadlineDirection string

const (
	deadlineExtend  deadlineDirection = "extend"
	deadlineShorten deadlineDirection = "shorten"
)

type deadlineMutationPreview struct {
	Direction           deadlineDirection
	Delta               time.Duration
	CurrentRemaining    time.Duration
	PreviousDeadline    time.Time
	NewDeadline         time.Time
	CurrentScheduledUSD float64
	ProjectedUSD        float64
	ExposureDeltaUSD    float64
	SessionCeilingUSD   float64
}

func runExtend(args []string) error {
	return runDeadlineMutation(deadlineExtend, args)
}

func runShorten(args []string) error {
	return runDeadlineMutation(deadlineShorten, args)
}

func runDeadlineMutation(direction deadlineDirection, args []string) error {
	fs := flag.NewFlagSet(string(direction), flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	yes := fs.Bool("yes", false, "apply the deadline change without prompting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: stint %s <duration> [--yes]", direction)
	}
	delta, err := parseSessionDuration(fs.Arg(0))
	if err != nil {
		return err
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	// Build the preview without holding the lifecycle lock. A confirmation
	// prompt can remain open indefinitely, and holding the lock while waiting
	// would prevent the watchdog from enforcing an expiring deadline. The
	// mutation phase uses optimistic concurrency and refuses to apply if the
	// session changed while the user was confirming.
	previewState, err := sessionstate.Load(paths)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("no active Stint session is recorded")
	}
	if err != nil {
		return err
	}
	preview, err := buildDeadlineMutationPreview(previewState, time.Now().UTC(), direction, delta)
	if err != nil {
		return err
	}
	printDeadlineMutationPreview(previewState, preview)
	if !*yes {
		confirmed, err := confirmDeadlineMutation(direction)
		if err != nil {
			return err
		}
		if !confirmed {
			return errors.New("session deadline change cancelled")
		}
	}

	releaseLifecycle, err := acquireLifecycleLock(paths)
	if err != nil {
		return err
	}
	defer releaseLifecycle()

	state, err := sessionstate.Load(paths)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("session ended while the deadline change was being confirmed")
	}
	if err != nil {
		return err
	}
	if state.InstanceID != previewState.InstanceID || !state.Deadline.Equal(previewState.Deadline) {
		return errors.New("session deadline changed while confirming; run the command again to review the current deadline")
	}

	// Recompute under the lock using the current clock. This is especially
	// important for shorten: a duration that was safe at preview time may have
	// become an immediate expiry while the user was deciding.
	lockedPreview, err := buildDeadlineMutationPreview(state, time.Now().UTC(), direction, delta)
	if err != nil {
		return err
	}
	if !lockedPreview.NewDeadline.Equal(preview.NewDeadline) || math.Abs(lockedPreview.ProjectedUSD-preview.ProjectedUSD) > 0.005 {
		return errors.New("session timing changed while confirming; run the command again to review the updated values")
	}

	// Guarantee a deadline enforcer before committing a new deadline. The
	// dynamically reloading watchdog observes the subsequent atomic state save;
	// no process replacement is required for a healthy watchdog.
	if err := ensureWatchdogAlive(paths, &state); err != nil {
		return fmt.Errorf("ensure session watchdog before deadline change: %w", err)
	}

	state = sessionstate.WithDeadline(state, lockedPreview.NewDeadline)
	state.LastError = ""
	if err := sessionstate.Save(paths, state); err != nil {
		return err
	}

	fmt.Println()
	if direction == deadlineExtend {
		fmt.Printf("Session extended by %s.\n", formatSessionDuration(delta))
	} else {
		fmt.Printf("Session shortened by %s.\n", formatSessionDuration(delta))
	}
	fmt.Printf("Remaining       %s\n", formatSessionDuration(sessionstate.Remaining(state, time.Now().UTC())))
	fmt.Printf("Auto-destroy    %s\n", state.Deadline.Local().Format(time.RFC1123))
	return nil
}

func buildDeadlineMutationPreview(
	state sessionstate.State,
	now time.Time,
	direction deadlineDirection,
	delta time.Duration,
) (deadlineMutationPreview, error) {
	if state.InstanceID <= 0 {
		return deadlineMutationPreview{}, errors.New("session state has no Vast instance id")
	}
	profile, err := router.ResolveProfile(state.Profile)
	if err != nil {
		return deadlineMutationPreview{}, fmt.Errorf("resolve session profile %q: %w", state.Profile, err)
	}
	change, err := calculateDeadlineChange(state, now, direction, delta)
	if err != nil {
		return deadlineMutationPreview{}, err
	}
	currentUSD := scheduledCostUSD(state.HourlyUSD, change.PreviousDuration)
	projectedUSD := scheduledCostUSD(state.HourlyUSD, change.NewDuration)
	if direction == deadlineExtend && projectedUSD > profile.Session.MaxCostUSD+0.005 {
		maxAdditional := maxAdditionalDuration(state, profile)
		return deadlineMutationPreview{}, fmt.Errorf(
			"extension would raise projected session exposure to $%.2f above the $%.2f session ceiling; maximum additional duration is %s",
			projectedUSD,
			profile.Session.MaxCostUSD,
			formatSessionDuration(maxAdditional),
		)
	}
	return deadlineMutationPreview{
		Direction:           direction,
		Delta:               delta,
		CurrentRemaining:    sessionstate.Remaining(state, now),
		PreviousDeadline:    change.PreviousDeadline,
		NewDeadline:         change.NewDeadline,
		CurrentScheduledUSD: currentUSD,
		ProjectedUSD:        projectedUSD,
		ExposureDeltaUSD:    projectedUSD - currentUSD,
		SessionCeilingUSD:   profile.Session.MaxCostUSD,
	}, nil
}

func calculateDeadlineChange(
	state sessionstate.State,
	now time.Time,
	direction deadlineDirection,
	delta time.Duration,
) (sessionstate.DeadlineChange, error) {
	switch direction {
	case deadlineExtend:
		return sessionstate.ExtendDeadline(state, now, delta)
	case deadlineShorten:
		return sessionstate.ShortenDeadline(state, now, delta)
	default:
		return sessionstate.DeadlineChange{}, fmt.Errorf("unknown deadline direction %q", direction)
	}
}

func maxAdditionalDuration(state sessionstate.State, profile core.Profile) time.Duration {
	if state.HourlyUSD <= 0 || profile.Session.MaxCostUSD <= 0 {
		return 0
	}
	maxHours := profile.Session.MaxCostUSD / state.HourlyUSD
	maxDuration := time.Duration(maxHours * float64(time.Hour))
	current := sessionstate.ScheduledDuration(state)
	if maxDuration <= current {
		return 0
	}
	return maxDuration - current
}

func scheduledCostUSD(hourlyUSD float64, duration time.Duration) float64 {
	if hourlyUSD <= 0 || duration <= 0 {
		return 0
	}
	return hourlyUSD * duration.Hours()
}

func parseSessionDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("duration is required (examples: 15m, 30m, 1h, 1h30m)")
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: use values such as 15m, 30m, 1h, or 1h30m", value)
	}
	if duration <= 0 {
		return 0, errors.New("duration must be greater than zero")
	}
	return duration, nil
}

func printDeadlineMutationPreview(state sessionstate.State, preview deadlineMutationPreview) {
	fmt.Println()
	fmt.Printf("%s SESSION\n\n", strings.ToUpper(string(preview.Direction)))
	fmt.Printf("Instance              %d\n", state.InstanceID)
	fmt.Printf("Current remaining     %s\n", formatSessionDuration(preview.CurrentRemaining))
	fmt.Printf("Current deadline      %s\n", preview.PreviousDeadline.Local().Format(time.RFC1123))
	if preview.Direction == deadlineExtend {
		fmt.Printf("Extension             +%s\n", formatSessionDuration(preview.Delta))
	} else {
		fmt.Printf("Reduction             -%s\n", formatSessionDuration(preview.Delta))
	}
	fmt.Printf("New deadline          %s\n", preview.NewDeadline.Local().Format(time.RFC1123))
	fmt.Println()
	fmt.Printf("Rate                  $%.3f/hr\n", state.HourlyUSD)
	if preview.Direction == deadlineExtend {
		fmt.Printf("Additional exposure   $%.2f\n", preview.ExposureDeltaUSD)
	} else {
		fmt.Printf("Exposure reduction    $%.2f\n", -preview.ExposureDeltaUSD)
	}
	fmt.Printf("Projected session     $%.2f\n", preview.ProjectedUSD)
	fmt.Printf("Session ceiling       $%.2f\n", preview.SessionCeilingUSD)
	fmt.Println()
}

func confirmDeadlineMutation(direction deadlineDirection) (bool, error) {
	fmt.Printf("%s this session? [y/N] ", strings.Title(string(direction))) //nolint:staticcheck // CLI label only; stable ASCII input.
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

func formatSessionDuration(duration time.Duration) string {
	if duration <= 0 {
		return "0s"
	}
	duration = duration.Round(time.Second)
	days := duration / (24 * time.Hour)
	duration -= days * 24 * time.Hour
	hours := duration / time.Hour
	duration -= hours * time.Hour
	minutes := duration / time.Minute
	duration -= minutes * time.Minute
	seconds := duration / time.Second

	parts := make([]string, 0, 4)
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}
	return strings.Join(parts, "")
}
