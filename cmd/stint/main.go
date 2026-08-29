package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/core"
	localenv "github.com/Marguelgtz/Stint/internal/local"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
	"github.com/Marguelgtz/Stint/internal/router"
	"github.com/Marguelgtz/Stint/internal/spark"
)

const version = "0.0.2"
const clinePort = 8409

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

func runAuth(args []string) error {
	if len(args) == 0 || args[0] != "vast" {
		return errors.New("auth currently supports: vast")
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

	fmt.Fprintln(os.Stderr, "Verifying Vast credentials...")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := vast.NewClient(apiKey).VerifyAuth(ctx); err != nil {
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
	fmt.Println("Vast authentication verified.")
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
	fmt.Println("\nAdd this key once in Vast Account → Keys → SSH Keys before Stint rents its first instance.")
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
		printCheck("Vast credentials", false, "run: stint auth vast")
		ready = false
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		verifyErr := vast.NewClient(credentials.Vast.APIKey).VerifyAuth(ctx)
		cancel()
		if verifyErr != nil {
			printCheck("Vast credentials", false, verifyErr.Error())
			ready = false
		} else {
			printCheck("Vast credentials", true, "authenticated")
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
		printCheck("Stint SSH key", true, paths.SSHPublicKey)
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
	fmt.Printf("Cline endpoint    http://127.0.0.1:%d/v1\n", clinePort)
	fmt.Println("Compute lifecycle not enabled until Phase 3.")
	if !ready {
		return errors.New("doctor found setup issues")
	}
	fmt.Println("\nReady for Phase 2 marketplace planning.")
	return nil
}

func runStatus() error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	_, credentialsErr := config.LoadCredentials(paths)
	fmt.Println("Stint local status")
	fmt.Printf("Vast credentials  %s\n", yesNo(credentialsErr == nil))
	fmt.Printf("Stint SSH key     %s\n", yesNo(localenv.SSHKeyExists(paths)))
	fmt.Printf("Cline endpoint    http://127.0.0.1:%d/v1\n", clinePort)
	fmt.Println("Active compute    none (not implemented until Phase 3)")
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

func printCheck(name string, ok bool, detail string) {
	mark := "✗"
	if ok {
		mark = "✓"
	}
	fmt.Printf("%-18s %s %s\n", name, mark, detail)
}

func yesNo(ok bool) string {
	if ok {
		return "configured"
	}
	return "not configured"
}

func printUsage() {
	fmt.Print(`Stint — elastic compute for coding agents

Phase 1 setup:
  stint auth vast
  stint auth vast --from-env
  stint setup ssh
  stint doctor
  stint status

Planning:
  stint plan interactive --hours 5
  stint plan deep --hours 8

Other:
  stint onboard spark
  stint version
`)
}
