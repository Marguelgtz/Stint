package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func runDownSafe(args []string) error {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	state, err := sessionstate.Load(paths)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("No active Stint session is recorded.")
		return nil
	}
	if err != nil {
		return err
	}
	credentials, err := config.LoadCredentials(paths)
	if err != nil {
		return err
	}

	killPID(state.TunnelPID)
	client := vast.NewClient(credentials.Vast.APIKey)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fmt.Printf("Destroying Vast instance %d and confirming teardown...\n", state.InstanceID)
	result := destroyAndConfirm(ctx, client, state.InstanceID)
	if !result.Confirmed {
		return preserveUnconfirmedDestroy(paths, state, result, sessionstate.DispositionDestroyUnconfirmed)
	}

	killPID(state.WatchdogPID)
	state.TunnelPID = 0
	state.WatchdogPID = 0
	state.LastError = ""
	if err := sessionstate.ArchiveState(paths, state, sessionstate.DispositionDestroyedManual, true, result.Attempts, ""); err != nil {
		return fmt.Errorf("archive destroyed session: %w", err)
	}
	if err := sessionstate.Clear(paths); err != nil {
		return err
	}
	fmt.Println("Compute destruction confirmed. Local endpoint is offline; session history retained.")
	return nil
}

func runWatchdogSafe(args []string) error {
	if len(args) != 0 {
		return errors.New("internal watchdog does not accept arguments")
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	state, err := sessionstate.Load(paths)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if wait := time.Until(state.Deadline); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		<-timer.C
	}
	credentials, err := config.LoadCredentials(paths)
	if err != nil {
		return err
	}

	killPID(state.TunnelPID)
	client := vast.NewClient(credentials.Vast.APIKey)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	result := destroyAndConfirm(ctx, client, state.InstanceID)
	if !result.Confirmed {
		return preserveUnconfirmedDestroy(paths, state, result, sessionstate.DispositionDestroyUnconfirmed)
	}

	state.TunnelPID = 0
	state.WatchdogPID = 0
	state.LastError = ""
	if err := sessionstate.ArchiveState(paths, state, sessionstate.DispositionDestroyedWatchdog, true, result.Attempts, ""); err != nil {
		return fmt.Errorf("archive watchdog-destroyed session: %w", err)
	}
	return sessionstate.Clear(paths)
}

func preserveUnconfirmedDestroy(paths config.Paths, state sessionstate.State, result destroyResult, disposition string) error {
	lastError := "provider destruction could not be confirmed"
	if result.LastError != nil {
		lastError = result.LastError.Error()
	}
	state.Status = sessionstate.StatusDestroyUnconfirmed
	state.LastError = lastError
	state.TunnelPID = 0
	if err := sessionstate.Save(paths, state); err != nil {
		return fmt.Errorf("destroy unconfirmed and preserve active state: %w", err)
	}
	if err := sessionstate.ArchiveState(paths, state, disposition, false, result.Attempts, lastError); err != nil {
		return fmt.Errorf("destroy unconfirmed (%s); preserve archive: %w", lastError, err)
	}
	return fmt.Errorf("%s: Vast instance %d could not be confirmed destroyed after %d attempt(s); billing may still be active; state was preserved; retry: stint down", diagnosticDestroyUnconfirmed, state.InstanceID, result.Attempts)
}
