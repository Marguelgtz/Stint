package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/Marguelgtz/Stint/internal/core"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
	"github.com/Marguelgtz/Stint/internal/router"
	"github.com/Marguelgtz/Stint/internal/spark"
)

const version = "0.0.1"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "stint:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "plan":
		return runPlan(args[1:])
	case "onboard":
		return runOnboard(args[1:])
	case "help", "--help", "-h":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runPlan(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("plan requires a profile: interactive or deep")
	}
	profileName := args[0]
	profile, err := router.ResolveProfile(profileName)
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	hours := fs.String("hours", "5", "session duration in hours")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	parsedHours, err := strconv.ParseFloat(*hours, 64)
	if err != nil {
		return fmt.Errorf("invalid --hours value %q", *hours)
	}

	plan, err := core.CreateSessionPlan(profile, parsedHours, vast.FixtureOffers(profileName))
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func runOnboard(args []string) error {
	if len(args) == 0 || args[0] != "spark" {
		return fmt.Errorf("onboard currently supports: spark")
	}
	fs := flag.NewFlagSet("onboard spark", flag.ContinueOnError)
	dashboard := fs.String("dashboard", spark.DefaultDashboardURL, "Spark dashboard URL")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	plan := spark.CreateOnboardingPlan(*dashboard)
	fmt.Println("Spark onboarding")
	fmt.Println()
	fmt.Println("Profile:", plan.ProfilePath)
	fmt.Println("Dashboard:", plan.DashboardURL)
	fmt.Printf("Expected GitHub evidence: %v\n", plan.ExpectedEvidence)
	fmt.Println("\nSteps:")
	for i, step := range plan.Steps {
		fmt.Printf("%d. %s\n", i+1, step)
	}
	return nil
}

func printUsage() {
	fmt.Print(`Stint — elastic compute for coding agents

Usage:
  stint plan interactive --hours 5
  stint plan deep --hours 8
  stint onboard spark
  stint version
`)
}
