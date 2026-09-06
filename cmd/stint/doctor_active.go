package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const doctorRefreshBudget = 8 * time.Second

const (
	doctorOK                    = "OK"
	doctorProviderUnreachable   = "PROVIDER_UNREACHABLE"
	doctorInstanceMissing       = "INSTANCE_MISSING"
	doctorInstanceNotRunning    = "INSTANCE_NOT_RUNNING"
	doctorProviderBooting       = "PROVIDER_BOOTING"
	doctorStartupInProgress     = "STARTUP_IN_PROGRESS"
	doctorStartupStalled        = "STARTUP_STALLED"
	doctorLifecycleOwnerUnknown = "LIFECYCLE_OWNER_UNKNOWN"
	doctorSSHUnavailable        = "SSH_UNAVAILABLE"
	doctorRuntimeProcessDead    = "RUNTIME_PROCESS_DEAD"
	doctorTunnelMissing         = "TUNNEL_PROCESS_MISSING"
	doctorEndpointUnavailable   = "LOCAL_ENDPOINT_UNAVAILABLE"
	doctorWatchdogMissing       = "WATCHDOG_MISSING"
	doctorArchivedSession       = "ARCHIVED_SESSION"
)

type doctorObservation struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type activeDoctorReport struct {
	Mode         string              `json:"mode"`
	InstanceID   int64               `json:"instanceId,omitempty"`
	Recorded     string              `json:"recordedStatus,omitempty"`
	Checkpoint   string              `json:"checkpoint,omitempty"`
	Diagnosis    string              `json:"diagnosis"`
	Severity     string              `json:"severity"`
	Recovery     string              `json:"recovery,omitempty"`
	Observations []doctorObservation `json:"observations,omitempty"`
}

type lifecycleDoctorState struct {
	Busy      bool
	Verified  bool
	PID       int
	Operation string
}

type doctorInputs struct {
	ProviderReachable bool
	InstancePresent   bool
	ProviderStatus    string
	ProviderSSHReady  bool
	Lifecycle         lifecycleDoctorState
	RecordedStatus    string
	Checkpoint        string
	SSHMetadata       bool
	SSHHealthy        bool
	RuntimeRunning    bool
	TunnelRunning     bool
	EndpointHealthy   bool
	WatchdogRunning   bool
}

func init() {
	if len(os.Args) < 2 || os.Args[1] != "doctor" || wantsHelp(os.Args[2:]) {
		return
	}
	if err := runDoctorCommand(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "stint:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func runDoctorCommand(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOutput := fs.Bool("json", false, "print machine-readable diagnostic JSON")
	last := fs.Bool("last", false, "inspect the most recently archived session")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("doctor does not accept positional arguments")
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if *last {
		report, err := diagnoseLastArchivedSession(paths)
		if err != nil {
			return err
		}
		return printActiveDoctorReport(report, *jsonOutput)
	}
	state, err := sessionstate.Load(paths)
	if errors.Is(err, os.ErrNotExist) {
		if *jsonOutput {
			report := activeDoctorReport{Mode: "preflight", Diagnosis: doctorOK, Severity: "PREFLIGHT", Recovery: "run `stint doctor` without --json for prerequisite details"}
			return printActiveDoctorReport(report, true)
		}
		return runDoctor()
	}
	if err != nil {
		return err
	}
	report := diagnoseActiveSession(paths, state)
	return printActiveDoctorReport(report, *jsonOutput)
}

func diagnoseActiveSession(paths config.Paths, state sessionstate.State) activeDoctorReport {
	report := activeDoctorReport{
		Mode:       "active",
		InstanceID: state.InstanceID,
		Recorded:   state.Status,
		Checkpoint: state.Checkpoint,
		Diagnosis:  doctorOK,
		Severity:   "HEALTHY",
		Recovery:   "none",
	}
	inputs := doctorInputs{
		RecordedStatus: state.Status,
		Checkpoint:     state.Checkpoint,
		SSHMetadata:    strings.TrimSpace(state.SSHHost) != "" && state.SSHPort > 0,
		Lifecycle:      inspectLifecycleLock(paths),
	}

	credentials, credentialErr := config.LoadCredentials(paths)
	if credentialErr != nil {
		report.Observations = append(report.Observations, doctorObservation{Name: "Vast provider", OK: false, Detail: credentialErr.Error()})
		inputs.ProviderReachable = false
		return applyDoctorClassification(report, classifyDoctorInputs(inputs))
	}
	client := vast.NewClient(credentials.Vast.APIKey)
	providerCtx, cancelProvider := context.WithTimeout(context.Background(), 5*time.Second)
	instance, providerErr := client.ShowInstance(providerCtx, state.InstanceID)
	cancelProvider()
	if providerErr != nil {
		if isDoctorInstanceMissing(providerErr) {
			inputs.ProviderReachable = true
			inputs.InstancePresent = false
			report.Observations = append(report.Observations, doctorObservation{Name: "Vast instance", OK: false, Detail: "not present in provider inventory"})
		} else {
			inputs.ProviderReachable = false
			report.Observations = append(report.Observations, doctorObservation{Name: "Vast provider", OK: false, Detail: compactTelemetryError(providerErr)})
		}
		return applyDoctorClassification(report, classifyDoctorInputs(inputs))
	}
	inputs.ProviderReachable = true
	inputs.InstancePresent = true
	inputs.ProviderStatus = strings.TrimSpace(instance.ActualStatus)
	inputs.ProviderSSHReady = strings.TrimSpace(instance.SSHHost) != "" && instance.SSHPort > 0
	report.Observations = append(report.Observations,
		doctorObservation{Name: "Vast instance", OK: strings.EqualFold(inputs.ProviderStatus, "running"), Detail: valueOr(inputs.ProviderStatus, "unknown")},
		doctorObservation{Name: "Provider SSH metadata", OK: inputs.ProviderSSHReady, Detail: doctorSSHDetail(instance.SSHHost, instance.SSHPort)},
	)

	refreshCtx, cancelRefresh := context.WithTimeout(context.Background(), doctorRefreshBudget)
	snapshot := collectSessionSnapshot(refreshCtx, paths, state, time.Now().UTC(), true, defaultSnapshotProbeDeps())
	cancelRefresh()
	inputs.SSHHealthy = snapshot.Health.Runtime.SSH
	inputs.RuntimeRunning = snapshot.Health.Runtime.Running
	inputs.TunnelRunning = snapshot.Health.Tunnel.Running
	inputs.EndpointHealthy = snapshot.Health.Endpoint.Healthy
	inputs.WatchdogRunning = snapshot.Health.Watchdog.Running

	report.Observations = append(report.Observations,
		doctorObservation{Name: "Lifecycle owner", OK: !inputs.Lifecycle.Busy || inputs.Lifecycle.Verified, Detail: lifecycleDoctorDetail(inputs.Lifecycle)},
		doctorObservation{Name: "SSH", OK: inputs.SSHHealthy, Detail: doctorTelemetryDetail(snapshot.Health.Runtime.Meta.Error, inputs.SSHHealthy)},
		doctorObservation{Name: "Runtime process", OK: inputs.RuntimeRunning, Detail: doctorTelemetryDetail(snapshot.Health.Runtime.Meta.Error, inputs.RuntimeRunning)},
		doctorObservation{Name: "Tunnel", OK: inputs.TunnelRunning, Detail: doctorProcessDetail(snapshot.Health.Tunnel)},
		doctorObservation{Name: "Local /v1/models", OK: inputs.EndpointHealthy, Detail: doctorTelemetryDetail(snapshot.Health.Endpoint.Meta.Error, inputs.EndpointHealthy)},
		doctorObservation{Name: "Watchdog", OK: inputs.WatchdogRunning, Detail: doctorProcessDetail(snapshot.Health.Watchdog)},
	)
	return applyDoctorClassification(report, classifyDoctorInputs(inputs))
}

func classifyDoctorInputs(in doctorInputs) activeDoctorReport {
	report := activeDoctorReport{Diagnosis: doctorOK, Severity: "HEALTHY", Recovery: "none"}
	if !in.ProviderReachable {
		report.Diagnosis, report.Severity, report.Recovery = doctorProviderUnreachable, "SAFETY", "provider state is unknown; do not rent another instance"
		return report
	}
	if !in.InstancePresent {
		report.Diagnosis, report.Severity, report.Recovery = doctorInstanceMissing, "SAFETY", "run `stint down --yes` to reconcile the tracked session"
		return report
	}
	if !strings.EqualFold(strings.TrimSpace(in.ProviderStatus), "running") {
		if startupRecordedState(in.RecordedStatus) {
			report.Diagnosis, report.Severity, report.Recovery = doctorProviderBooting, "PROGRESS", "provider startup is still converging; `stint down` remains available"
		} else {
			report.Diagnosis, report.Severity, report.Recovery = doctorInstanceNotRunning, "RECOVERABLE", "run `stint down` or retry after provider state settles"
		}
		return report
	}
	if !in.WatchdogRunning {
		report.Diagnosis, report.Severity, report.Recovery = doctorWatchdogMissing, "SAFETY", "deadline protection is missing; recover with `stint resume` or tear down with `stint down`"
		return report
	}
	if startupRecordedState(in.RecordedStatus) {
		if in.Lifecycle.Busy && !in.Lifecycle.Verified {
			report.Diagnosis, report.Severity, report.Recovery = doctorLifecycleOwnerUnknown, "SAFETY", "a lifecycle lock is held by an unverifiable owner; do not start another session; use `stint down` after identifying the legacy owner"
			return report
		}
		if in.Lifecycle.Busy && in.Lifecycle.Verified && lifecycleOperationInterruptible(in.Lifecycle.Operation) {
			report.Diagnosis, report.Severity, report.Recovery = doctorStartupInProgress, "PROGRESS", "startup is active; wait, or use `stint down` to preempt it safely"
			return report
		}
		if !in.SSHMetadata || !in.SSHHealthy || !in.RuntimeRunning || !in.TunnelRunning || !in.EndpointHealthy {
			report.Diagnosis, report.Severity, report.Recovery = doctorStartupStalled, "RECOVERABLE", "startup has no verified lifecycle owner; run `stint resume` or `stint down`"
			return report
		}
	}
	if !in.SSHMetadata || !in.SSHHealthy {
		report.Diagnosis, report.Severity, report.Recovery = doctorSSHUnavailable, "RECOVERABLE", "run `stint resume` to re-establish SSH state"
		return report
	}
	if !in.RuntimeRunning {
		report.Diagnosis, report.Severity, report.Recovery = doctorRuntimeProcessDead, "RECOVERABLE", "run `stint resume` to restart the runtime"
		return report
	}
	if !in.TunnelRunning {
		report.Diagnosis, report.Severity, report.Recovery = doctorTunnelMissing, "RECOVERABLE", "run `stint resume` to recreate the tunnel"
		return report
	}
	if !in.EndpointHealthy {
		report.Diagnosis, report.Severity, report.Recovery = doctorEndpointUnavailable, "RECOVERABLE", "remote runtime exists but the local endpoint is unhealthy; run `stint resume`"
		return report
	}
	return report
}

func applyDoctorClassification(report activeDoctorReport, classification activeDoctorReport) activeDoctorReport {
	report.Diagnosis = classification.Diagnosis
	report.Severity = classification.Severity
	report.Recovery = classification.Recovery
	return report
}

func inspectLifecycleLock(paths config.Paths) lifecycleDoctorState {
	lockPath := filepath.Join(paths.StateDir, lifecycleLockFile)
	file, err := os.OpenFile(lockPath, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return lifecycleDoctorState{}
	}
	if err != nil {
		return lifecycleDoctorState{Busy: true}
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		return lifecycleDoctorState{}
	} else if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
		return lifecycleDoctorState{Busy: true}
	}
	owner, _ := readLifecycleLockOwner(lockPath)
	return lifecycleDoctorState{
		Busy:      true,
		Verified:  lifecycleOwnerStillHoldsLock(owner, lockPath),
		PID:       owner.PID,
		Operation: owner.Operation,
	}
}

func startupRecordedState(status string) bool {
	switch status {
	case sessionstate.StatusRenting,
		sessionstate.StatusBooting,
		sessionstate.StatusSSHConnecting,
		sessionstate.StatusSSHReady,
		sessionstate.StatusRuntimeBootstrap,
		sessionstate.StatusRuntimeReady,
		sessionstate.StatusModelStarting,
		sessionstate.StatusModelStarted,
		sessionstate.StatusModelLoading:
		return true
	default:
		return false
	}
}

func isDoctorInstanceMissing(err error) bool {
	var apiErr *vast.APIError
	return errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusGone)
}

func diagnoseLastArchivedSession(paths config.Paths) (activeDoctorReport, error) {
	entries, err := os.ReadDir(sessionArchiveDir(paths))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return activeDoctorReport{}, errors.New("no archived Stint session is available")
		}
		return activeDoctorReport{}, err
	}
	var newest os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if newest == nil || entry.Name() > newest.Name() {
			newest = entry
		}
	}
	if newest == nil {
		return activeDoctorReport{}, errors.New("no archived Stint session is available")
	}
	data, err := os.ReadFile(filepath.Join(sessionArchiveDir(paths), newest.Name()))
	if err != nil {
		return activeDoctorReport{}, err
	}
	var state sessionstate.State
	if err := json.Unmarshal(data, &state); err != nil {
		return activeDoctorReport{}, fmt.Errorf("parse archived session: %w", err)
	}
	report := activeDoctorReport{
		Mode:       "last",
		InstanceID: state.InstanceID,
		Recorded:   state.Status,
		Checkpoint: state.Checkpoint,
		Diagnosis:  doctorArchivedSession,
		Severity:   "POSTMORTEM",
		Recovery:   "none",
		Observations: []doctorObservation{
			{Name: "Archive", OK: true, Detail: newest.Name()},
			{Name: "Last error", OK: state.LastError == "", Detail: valueOr(state.LastError, "none")},
		},
	}
	return report, nil
}

func printActiveDoctorReport(report activeDoctorReport, jsonOutput bool) error {
	if jsonOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	fmt.Println("Stint doctor")
	fmt.Println()
	if report.InstanceID > 0 {
		fmt.Printf("Instance           %d\n", report.InstanceID)
	}
	if report.Recorded != "" {
		fmt.Printf("Recorded status    %s\n", report.Recorded)
	}
	if report.Checkpoint != "" {
		fmt.Printf("Checkpoint         %s\n", report.Checkpoint)
	}
	if len(report.Observations) > 0 {
		fmt.Println()
		for _, observation := range report.Observations {
			printCheck(observation.Name, observation.OK, observation.Detail)
		}
	}
	fmt.Println()
	fmt.Printf("Diagnosis          %s\n", report.Diagnosis)
	fmt.Printf("Severity           %s\n", report.Severity)
	if report.Recovery != "" && report.Recovery != "none" {
		fmt.Printf("Next action        %s\n", report.Recovery)
	}
	return nil
}

func lifecycleDoctorDetail(state lifecycleDoctorState) string {
	if !state.Busy {
		return "idle"
	}
	if state.Verified {
		return fmt.Sprintf("%s pid %d holds lifecycle lock", valueOr(state.Operation, "lifecycle"), state.PID)
	}
	return "lock busy; owner metadata is absent or unverifiable"
}

func doctorSSHDetail(host string, port int) string {
	if strings.TrimSpace(host) == "" || port <= 0 {
		return "not published yet"
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func doctorTelemetryDetail(errText string, healthy bool) string {
	if healthy {
		return "healthy"
	}
	return valueOr(compactTelemetryError(errors.New(valueOr(errText, "unavailable"))), "unavailable")
}

func doctorProcessDetail(process processHealth) string {
	if process.PID <= 0 {
		return "not recorded"
	}
	if process.Running {
		return fmt.Sprintf("pid %d running", process.PID)
	}
	return fmt.Sprintf("pid %d not running", process.PID)
}
