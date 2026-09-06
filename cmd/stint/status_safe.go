package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func runStatusSafe(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	refresh := fs.Bool("refresh", false, "actively verify provider, SSH, runtime, tunnel, endpoint, and watchdog health")
	jsonOutput := fs.Bool("json", false, "print refreshed health as JSON; requires --refresh")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*refresh {
		if *jsonOutput {
			return errors.New("status --json currently requires --refresh")
		}
		return runStatus()
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	state, stateErr := sessionstate.Load(paths)
	if errors.Is(stateErr, os.ErrNotExist) {
		if !*jsonOutput {
			return runStatus()
		}
		return printDoctorReport(doctorReport{Mode: "status-refresh", Diagnosis: diagnosticOK, Severity: "HEALTHY", Recovery: "no active compute"}, true)
	}
	if stateErr != nil {
		return stateErr
	}

	if !*jsonOutput {
		if err := runStatus(); err != nil {
			return err
		}
		fmt.Println("\nHEALTH (refreshed)")
	}
	report := diagnoseActiveSession(paths, state)
	if *jsonOutput {
		report.Mode = "status-refresh"
		return printDoctorReport(report, true)
	}
	fmt.Printf("Diagnosis          %s\n", report.Diagnosis)
	fmt.Printf("Severity           %s\n", report.Severity)
	if report.Recovery != "" && report.Recovery != "none" {
		fmt.Printf("Recovery           %s\n", report.Recovery)
	}
	for _, observation := range report.Observations {
		if !observation.OK {
			fmt.Printf("%-18s %s\n", observation.Name, observation.Detail)
		}
	}
	return nil
}
