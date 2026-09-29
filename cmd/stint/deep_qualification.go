package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func runDeepQualification(args []string) error {
	if len(args) == 0 {
		return errors.New("deep qualification requires export or verify")
	}
	switch args[0] {
	case "export":
		return runDeepQualificationExport(args[1:])
	case "verify":
		return runDeepQualificationVerify(args[1:])
	case "publication-plan":
		return runDeepQualificationPublicationPlan(args[1:])
	default:
		return fmt.Errorf("unknown deep qualification command %q", args[0])
	}
}

func runDeepQualificationPublicationPlan(args []string) error {
	fs := flag.NewFlagSet("deep qualification publication-plan", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "Stint state directory")
	runID := fs.String("run-id", "", "Deep Work run ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *stateDir == "" || *runID == "" {
		return errors.New("deep qualification publication-plan requires --state-dir and --run-id")
	}
	plan, err := deep.LoadQualificationPublicationPlan(*stateDir, *runID)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(plan)
}

func runDeepQualificationExport(args []string) error {
	fs := flag.NewFlagSet("deep qualification export", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "Stint state directory")
	runID := fs.String("run-id", "", "Deep Work run ID")
	snapshotID := fs.String("snapshot-id", "", "unique qualification snapshot ID")
	contextPath := fs.String("context", "", "allow-listed qualification provenance JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *stateDir == "" || *runID == "" || *snapshotID == "" {
		return errors.New("deep qualification export requires --state-dir, --run-id, and --snapshot-id")
	}
	var context deep.QualificationContext
	if *contextPath != "" {
		file, err := os.Open(*contextPath)
		if err != nil {
			return fmt.Errorf("open qualification context: %w", err)
		}
		decoder := json.NewDecoder(file)
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&context)
		closeErr := file.Close()
		if err != nil {
			return fmt.Errorf("decode qualification context: %w", err)
		}
		if closeErr != nil {
			return closeErr
		}
	}
	path, manifest, err := deep.ExportQualificationSnapshot(*stateDir, *runID, *snapshotID, context)
	if err != nil {
		return err
	}
	_, digest, err := deep.VerifyQualificationBundle(path)
	if err != nil {
		return fmt.Errorf("verify newly exported qualification snapshot: %w", err)
	}
	fmt.Printf("QUALIFICATION_BUNDLE_OK path=%s run=%s events=%d..%d manifest_sha256=%s\n",
		path, manifest.RunID, manifest.FirstEventSequence, manifest.LastEventSequence, digest)
	return nil
}

func runDeepQualificationVerify(args []string) error {
	fs := flag.NewFlagSet("deep qualification verify", flag.ContinueOnError)
	bundle := fs.String("bundle", "", "qualification snapshot directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *bundle == "" {
		return errors.New("deep qualification verify requires --bundle")
	}
	manifest, digest, err := deep.VerifyQualificationBundle(*bundle)
	if err != nil {
		return err
	}
	fmt.Printf("QUALIFICATION_BUNDLE_VERIFIED run=%s events=%d..%d manifest_sha256=%s\n",
		manifest.RunID, manifest.FirstEventSequence, manifest.LastEventSequence, digest)
	return nil
}
