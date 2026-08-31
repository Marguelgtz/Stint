package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/core"
	localenv "github.com/Marguelgtz/Stint/internal/local"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
	"github.com/Marguelgtz/Stint/internal/router"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
	"github.com/Marguelgtz/Stint/internal/spark"
)

const version = "0.1.0"
const clinePort = 8409

type planDiagnostics struct {
	Candidates      int                          `json:"candidates"`
	Qualified       int                          `json:"qualified"`
	RejectedBy      map[core.RejectionReason]int `json:"rejectedBy,omitempty"`
	ClosestRejected []core.OfferEvaluation       `json:"closestRejected,omitempty"`
}

type planOutput struct {
	Live          bool             `json:"live"`
	Mutating      bool             `json:"mutating"`
	ComputeRented bool             `json:"computeRented"`
	Plan          core.SessionPlan `json:"plan"`
	Alternatives  []core.Offer     `json:"alternatives"`
	Diagnostics   planDiagnostics  `json:"diagnostics"`
}

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
		if wantsHelp(args[1:]) {
			printCommandHelp("version")
			return nil
		}
		fmt.Println(version)
		return nil
	case "help", "--help", "-h":
		return runHelp(args[1:])
	}

	// Every registered command accepts --help / -h and prints its detail page
	// instead of executing, so help is always free of side effects.
	if cmd, ok := findCommand(args[0]); ok && wantsHelp(args[1:]) {
		printCommandHelp(cmd.name)
		return nil
	}

	switch args[0] {
	case "plan":
		return runPlan(args[1:])
	case "start":
		return runStartResumable(args[1:])
	case "resume":
		return runResume(args[1:])
	case "extend":
		return runExtend(args[1:])
	case "shorten":
		return runShorten(args[1:])
	case "down":
		return runDown(args[1:])
	case "_watchdog":
		return runDynamicWatchdog(args[1:])
	case "auth":
		return runAuth(args[1:])
	case "setup":
		return runSetup(args[1:])
	case "doctor":
		return runDoctor()
	case "status":
		return runStatus()
	case "onboard":
		return runOnboard(args[1:])
	default:
		return fmt.Errorf("unknown command %q (run 'stint help')", args[0])
	}
}

func runPlan(args []string) error {
	if len(args) == 0 {
		return errors.New("plan requires a profile: interactive or deep")
	}
	profileName := args[0]
	profile, err := router.ResolveProfile(profileName)
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	defaultHours := strconv.FormatFloat(profile.Session.DefaultHours, 'f', -1, 64)
	hoursValue := fs.String("hours", defaultHours, "session duration in hours")
	fixture := fs.Bool("fixture", false, "use deterministic local fixture offers instead of Vast")
	jsonOutput := fs.Bool("json", false, "print machine-readable JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	hours, err := strconv.ParseFloat(*hoursValue, 64)
	if err != nil {
		return fmt.Errorf("invalid --hours value %q", *hoursValue)
	}

	var offers []core.Offer
	if *fixture {
		offers = vast.FixtureOffers(profileName)
	} else {
		if profileName != "interactive" {
			return errors.New("live marketplace planning currently supports the interactive profile only; use --fixture for deep")
		}
		paths, err := config.DefaultPaths()
		if err != nil {
			return err
		}
		credentials, err := config.LoadCredentials(paths)
		if err != nil {
			return errors.New("Vast credentials are not configured; run: stint auth vast")
		}
		if !*jsonOutput {
			fmt.Fprintln(os.Stderr, "Searching Vast for interactive compute...")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		offers, err = vast.NewClient(credentials.Vast.APIKey).SearchOffers(ctx, profile, vast.SearchOptions{
			Hours:     hours,
			Limit:     250,
			StorageGB: profile.Session.StorageGB,
		})
		if err != nil {
			return err
		}
	}

	evaluations := core.EvaluateOffers(profile, offers)
	diagnostics := summarizeEvaluations(evaluations)
	ranked := core.RankOffers(profile, offers)
	if len(ranked) == 0 {
		if *jsonOutput {
			out, err := json.MarshalIndent(struct {
				Live          bool            `json:"live"`
				Mutating      bool            `json:"mutating"`
				ComputeRented bool            `json:"computeRented"`
				Diagnostics   planDiagnostics `json:"diagnostics"`
			}{Live: !*fixture, Mutating: false, ComputeRented: false, Diagnostics: diagnostics}, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
		} else {
			printDiagnostics(diagnostics)
		}
		return fmt.Errorf("no qualifying %s offers found within the hard policy limits", profileName)
	}
	plan, err := core.CreateSessionPlan(profile, hours, offers)
	if err != nil {
		return err
	}
	alternatives := ranked[profile.Workers:]
	if len(alternatives) > 3 {
		alternatives = alternatives[:3]
	}
	result := planOutput{
		Live:          !*fixture,
		Mutating:      false,
		ComputeRented: false,
		Plan:          plan,
		Alternatives:  alternatives,
		Diagnostics:   diagnostics,
	}
	if *jsonOutput {
		out, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	printHumanPlan(result)
	return nil
}

func summarizeEvaluations(evaluations []core.OfferEvaluation) planDiagnostics {
	d := planDiagnostics{Candidates: len(evaluations), RejectedBy: map[core.RejectionReason]int{}}
	rejected := make([]core.OfferEvaluation, 0)
	for _, evaluation := range evaluations {
		if evaluation.Qualified {
			d.Qualified++
			continue
		}
		for _, reason := range evaluation.Rejections {
			d.RejectedBy[reason]++
		}
		rejected = append(rejected, evaluation)
	}
	sort.SliceStable(rejected, func(i, j int) bool {
		if len(rejected[i].Rejections) != len(rejected[j].Rejections) {
			return len(rejected[i].Rejections) < len(rejected[j].Rejections)
		}
		if rejected[i].Offer.DLPerf != rejected[j].Offer.DLPerf {
			return rejected[i].Offer.DLPerf > rejected[j].Offer.DLPerf
		}
		return rejected[i].Offer.HourlyUSD < rejected[j].Offer.HourlyUSD
	})
	if len(rejected) > 3 {
		rejected = rejected[:3]
	}
	d.ClosestRejected = rejected
	return d
}

func printDiagnostics(d planDiagnostics) {
	fmt.Println()
	fmt.Println("MARKETPLACE DIAGNOSTICS")
	fmt.Printf("Candidates inspected  %d\n", d.Candidates)
	fmt.Printf("Hard qualified        %d\n", d.Qualified)
	if len(d.RejectedBy) > 0 {
		fmt.Println("\nRejected by hard constraint:")
		reasons := make([]string, 0, len(d.RejectedBy))
		for reason := range d.RejectedBy {
			reasons = append(reasons, string(reason))
		}
		sort.Strings(reasons)
		for _, reason := range reasons {
			fmt.Printf("  %-18s %d\n", reason, d.RejectedBy[core.RejectionReason(reason)])
		}
	}
	if len(d.ClosestRejected) > 0 {
		fmt.Println("\nClosest rejected candidates:")
		for _, evaluation := range d.ClosestRejected {
			o := evaluation.Offer
			fmt.Printf("  %s  $%.3f/hr  rel %.2f%%  DLPerf %.1f  %.0f MB/s  %.0f W  fails=%v\n",
				valueOr(o.Geolocation, "unknown"), o.HourlyUSD, o.Reliability*100, o.DLPerf, o.InetDownMBps, o.GPUMaxPowerW, evaluation.Rejections)
		}
	}
	fmt.Println("\nNO COMPUTE HAS BEEN RENTED.")
}

func runAuth(args []string) error {
	if len(args) == 0 || args[0] != "vast" {
		return errors.New("auth currently supports provider auth: vast")
	}
	fs := flag.NewFlagSet("auth vast", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fromEnv := fs.Bool("from-env", false, "read the API key from VAST_API_KEY")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	var apiKey string
	var err error
	if *fromEnv {
		apiKey = os.Getenv("VAST_API_KEY")
		if apiKey == "" {
			return errors.New("VAST_API_KEY is empty")
		}
	} else {
		apiKey, err = localenv.ReadSecret("Vast API key: ")
		if err != nil {
			return err
		}
	}

	fmt.Fprintln(os.Stderr, "Verifying Vast instance-read and marketplace-search access...")
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	client := vast.NewClient(apiKey)
	if err := client.VerifyAuth(ctx); err != nil {
		return err
	}
	if err := client.VerifySearchAccess(ctx); err != nil {
		return err
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	credentials := config.Credentials{Vast: config.VastCredentials{APIKey: apiKey}}
	if err := config.SaveCredentials(paths, credentials); err != nil {
		return err
	}
	fmt.Println("Vast provider authentication verified.")
	fmt.Println("Credentials:", paths.CredentialsFile)
	return nil
}

func runSetup(args []string) error {
	if len(args) == 0 || args[0] != "ssh" {
		return errors.New("setup currently supports: ssh")
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	publicKey, created, err := localenv.EnsureSSHKey(paths)
	if err != nil {
		return err
	}
	if created {
		fmt.Println("Created dedicated Stint SSH keypair.")
	} else {
		fmt.Println("Using existing Stint SSH keypair.")
	}
	fmt.Println("Private key:", paths.SSHPrivateKey)
	fmt.Println("\nPublic key (safe to add to Vast):")
	fmt.Println(publicKey)
	fmt.Println("\nLocal key is ready. `stint start` attaches it to the rented instance automatically.")
	return nil
}

func runDoctor() error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	fmt.Println("Stint pre-v0 doctor")
	fmt.Println()
	ready := true

	credentials, credentialErr := config.LoadCredentials(paths)
	if credentialErr != nil {
		printCheck("Vast instance read", false, "run: stint auth vast")
		printCheck("Vast search", false, "run: stint auth vast")
		ready = false
	} else {
		client := vast.NewClient(credentials.Vast.APIKey)
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		instanceErr := client.VerifyAuth(ctx)
		searchErr := client.VerifySearchAccess(ctx)
		cancel()
		if instanceErr != nil {
			printCheck("Vast instance read", false, instanceErr.Error())
			ready = false
		} else {
			printCheck("Vast instance read", true, "instance_read authorized")
		}
		if searchErr != nil {
			printCheck("Vast search", false, searchErr.Error())
			ready = false
		} else {
			printCheck("Vast search", true, "misc/search authorized")
		}
	}

	sshPath, sshErr := localenv.SSHExecutable()
	if sshErr != nil {
		printCheck("OpenSSH", false, sshErr.Error())
		ready = false
	} else {
		printCheck("OpenSSH", true, sshPath)
	}

	if localenv.SSHKeyExists(paths) {
		printCheck("Stint SSH key", true, "local keypair ready; start attaches it automatically")
	} else {
		printCheck("Stint SSH key", false, "run: stint setup ssh")
		ready = false
	}

	if localenv.PortAvailable(clinePort) {
		printCheck("Local port 8409", true, "available for Cline tunnel")
	} else {
		printCheck("Local port 8409", false, "already in use")
		ready = false
	}

	fmt.Println()
	fmt.Printf("Cline endpoint      http://127.0.0.1:%d/v1\n", clinePort)
	fmt.Println("Compute lifecycle   enabled; paid start requires explicit confirmation")
	if !ready {
		return errors.New("doctor found setup issues")
	}
	fmt.Println("\nReady for live marketplace planning and paid start.")
	return nil
}

func runStatus() error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	_, credentialsErr := config.LoadCredentials(paths)
	fmt.Println("Stint local status")
	fmt.Printf("Vast provider      %s\n", yesNo(credentialsErr == nil))
	fmt.Printf("Stint SSH key      %s\n", yesNo(localenv.SSHKeyExists(paths)))
	fmt.Printf("Cline endpoint     http://127.0.0.1:%d/v1\n", clinePort)
	fmt.Println("Product identity   GitHub (same model as Spark; hosted login not needed for local pre-v0)")
	state, stateErr := sessionstate.Load(paths)
	if errors.Is(stateErr, os.ErrNotExist) {
		fmt.Println("Active compute     none")
		return nil
	}
	if stateErr != nil {
		return stateErr
	}
	printActiveSessionStatus(state)
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

func printHumanPlan(result planOutput) {
	selected := result.Plan.Workers[0].Offer
	fmt.Println()
	fmt.Println("SELECTED")
	fmt.Printf("%-14s %s\n", "GPU", selected.GPUModel)
	fmt.Printf("%-14s %s\n", "Location", valueOr(selected.Geolocation, "unknown"))
	fmt.Printf("%-14s $%.3f/hr\n", "Price", selected.HourlyUSD)
	fmt.Printf("%-14s %.2f%%\n", "Reliability", selected.Reliability*100)
	fmt.Printf("%-14s %.1f\n", "DLPerf", selected.DLPerf)
	fmt.Printf("%-14s %.0f MB/s down\n", "Network", selected.InetDownMBps)
	fmt.Printf("%-14s %d\n", "Direct ports", selected.DirectPortCount)
	fmt.Printf("%-14s %.0f W\n", "GPU power", selected.GPUMaxPowerW)

	if len(result.Alternatives) > 0 {
		fmt.Println("\nALTERNATIVES")
		for i, offer := range result.Alternatives {
			fmt.Printf("%d. %-10s %-18s $%.3f/hr  DLPerf %.1f  rel %.2f%%\n", i+1, offer.GPUModel, valueOr(offer.Geolocation, "unknown"), offer.HourlyUSD, offer.DLPerf, offer.Reliability*100)
		}
	}

	fmt.Println("\nSESSION")
	fmt.Printf("%-20s %.2fh\n", "Duration", result.Plan.Hours)
	fmt.Printf("%-20s $%.2f\n", "Estimated compute", result.Plan.EstimatedTotalUSD)
	fmt.Printf("%-20s $%.2f/hr\n", "Hourly ceiling", result.Plan.Profile.GPU.MaxHourlyUSD)
	fmt.Printf("%-20s $%.2f\n", "Session ceiling", result.Plan.Profile.Session.MaxCostUSD)
	fmt.Printf("%-20s %.0f GB\n", "Planned storage", result.Plan.Profile.Session.StorageGB)
	fmt.Printf("%-20s %d / %d candidates\n", "Marketplace", result.Diagnostics.Qualified, result.Diagnostics.Candidates)
	if selected.InetDownCostPerGB > 0 {
		fmt.Printf("%-20s $%.4f/GB\n", "Download traffic", selected.InetDownCostPerGB)
	}
	fmt.Println("\nNO COMPUTE HAS BEEN RENTED.")
}

func printCheck(name string, ok bool, detail string) {
	mark := "✗"
	if ok {
		mark = "✓"
	}
	fmt.Printf("%-20s %s %s\n", name, mark, detail)
}

func yesNo(ok bool) string {
	if ok {
		return "configured"
	}
	return "not configured"
}

func valueOr(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}