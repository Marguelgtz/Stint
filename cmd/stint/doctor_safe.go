package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

type doctorObservation struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type doctorReport struct {
	Mode         string              `json:"mode"`
	InstanceID   int64               `json:"instanceId,omitempty"`
	Status       string              `json:"status,omitempty"`
	Checkpoint   string              `json:"checkpoint,omitempty"`
	Diagnosis    string              `json:"diagnosis"`
	Severity     string              `json:"severity"`
	Recovery     string              `json:"recovery,omitempty"`
	Observations []doctorObservation `json:"observations,omitempty"`
}

type remoteDoctorState struct {
	RuntimeReady bool
	PIDAlive     bool
	ProcessName  string
	Listener     bool
	APIHealthy   bool
	ModelBytes   int64
	LogTail      string
}

func runDoctorSafe(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	last := fs.Bool("last", false, "diagnose the most recently archived session")
	jsonOutput := fs.Bool("json", false, "print machine-readable diagnostic JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if *last {
		report, err := diagnoseLastSession(paths)
		if err != nil {
			return err
		}
		return printDoctorReport(report, *jsonOutput)
	}

	state, stateErr := sessionstate.Load(paths)
	if errors.Is(stateErr, os.ErrNotExist) {
		if *jsonOutput {
			return printDoctorReport(doctorReport{Mode: "preflight", Diagnosis: diagnosticOK, Severity: "HEALTHY", Recovery: "run stint doctor without --json for detailed setup checks"}, true)
		}
		return runDoctor()
	}
	if stateErr != nil {
		return stateErr
	}
	return printDoctorReport(diagnoseActiveSession(paths, state), *jsonOutput)
}

func diagnoseLastSession(paths config.Paths) (doctorReport, error) {
	archive, err := sessionstate.LoadLastArchive(paths)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return doctorReport{}, errors.New("no archived Stint session is available")
		}
		return doctorReport{}, err
	}
	report := doctorReport{
		Mode:       "last",
		InstanceID: archive.State.InstanceID,
		Status:     archive.State.Status,
		Checkpoint: archive.State.Checkpoint,
		Diagnosis:  diagnosticOK,
		Severity:   "HEALTHY",
		Recovery:   "none",
	}
	if !archive.DestroyConfirmed {
		report.Diagnosis = diagnosticDestroyUnconfirmed
		report.Severity = "SAFETY"
		report.Recovery = "retry: stint down"
	}
	report.Observations = append(report.Observations,
		doctorObservation{Name: "Disposition", OK: archive.DestroyConfirmed, Detail: archive.Disposition},
		doctorObservation{Name: "Destroy confirmation", OK: archive.DestroyConfirmed, Detail: fmt.Sprintf("attempts=%d lastError=%s", archive.DestroyAttempts, archive.DestroyLastError)},
	)
	for _, name := range []string{"runtime-tail.log", "tunnel.log", "watchdog.log"} {
		if tail := readSessionEvidenceTail(paths, archive.State.InstanceID, name, 600); tail != "" {
			report.Observations = append(report.Observations, doctorObservation{Name: name, OK: true, Detail: tail})
		}
	}
	return report, nil
}

func diagnoseActiveSession(paths config.Paths, state sessionstate.State) doctorReport {
	report := doctorReport{
		Mode:       "active",
		InstanceID: state.InstanceID,
		Status:     state.Status,
		Checkpoint: state.Checkpoint,
		Diagnosis:  diagnosticOK,
		Severity:   "HEALTHY",
		Recovery:   "none",
	}

	credentials, err := config.LoadCredentials(paths)
	if err != nil {
		return doctorFail(report, diagnosticProviderUnreachable, "SAFETY", "configure Vast credentials before attempting recovery", "Vast provider", err.Error())
	}
	client := vast.NewClient(credentials.Vast.APIKey)
	providerCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	instance, providerErr := client.ShowInstance(providerCtx, state.InstanceID)
	cancel()
	if providerErr != nil {
		if isInstanceMissingError(providerErr) {
			return doctorFail(report, diagnosticInstanceMissing, "RECOVERABLE", "run: stint down to reconcile and archive local state", "Vast instance", "provider reports instance missing")
		}
		return doctorFail(report, diagnosticProviderUnreachable, "SAFETY", "provider state unknown; do not rent another instance", "Vast provider", providerErr.Error())
	}
	running := strings.EqualFold(strings.TrimSpace(instance.ActualStatus), "running")
	report.Observations = append(report.Observations, doctorObservation{Name: "Vast instance", OK: running, Detail: instance.ActualStatus})
	if !running {
		return doctorFail(report, diagnosticInstanceNotRunning, "RECOVERABLE", "run: stint down or retry after provider state settles", "Instance state", instance.ActualStatus)
	}
	if state.Status == sessionstate.StatusDestroyUnconfirmed {
		return doctorFail(report, diagnosticDestroyUnconfirmed, "SAFETY", "retry: stint down", "Destroy confirmation", "provider still reports the paid instance; billing may still be active")
	}
	if state.SSHHost == "" || state.SSHPort <= 0 {
		return doctorFail(report, diagnosticSSHMetadataMissing, "RECOVERABLE", "run: stint resume", "SSH metadata", "missing host or port")
	}

	sshCtx, sshCancel := context.WithTimeout(context.Background(), 10*time.Second)
	_, sshErr := runSSH(sshCtx, paths, state, "echo stint-doctor-ssh")
	sshCancel()
	if sshErr != nil {
		code := classifySSHFailure(sshErr)
		return doctorFail(report, code, "RECOVERABLE", sshRecoveryFor(code), "SSH", sshErr.Error())
	}
	report.Observations = append(report.Observations, doctorObservation{Name: "SSH", OK: true, Detail: fmt.Sprintf("%s:%d", state.SSHHost, state.SSHPort)})

	remote, remoteErr := probeRemoteRuntime(paths, state)
	if remoteErr != nil {
		code := classifySSHFailure(remoteErr)
		return doctorFail(report, code, "RECOVERABLE", sshRecoveryFor(code), "Remote runtime probe", remoteErr.Error())
	}
	report.Observations = append(report.Observations,
		doctorObservation{Name: "Runtime binary", OK: remote.RuntimeReady, Detail: runtimeForState(state)},
		doctorObservation{Name: "Runtime process", OK: remote.PIDAlive, Detail: remote.ProcessName},
		doctorObservation{Name: "Remote :8080", OK: remote.Listener, Detail: boolDetail(remote.Listener, "listening", "not listening")},
		doctorObservation{Name: "Remote /v1/models", OK: remote.APIHealthy, Detail: boolDetail(remote.APIHealthy, "healthy", "not ready")},
	)
	runtimeClass := classifyRemoteRuntimeState(state, remote)
	if !runtimeClass.Progress && runtimeClass.Code != diagnosticOK {
		return doctorFail(report, runtimeClass.Code, runtimeClass.Severity, runtimeClass.Recovery, "Runtime", runtimeClass.Detail)
	}

	tunnelAlive := processAlive(state.TunnelPID)
	report.Observations = append(report.Observations, doctorObservation{Name: "Tunnel process", OK: tunnelAlive, Detail: pidDetail(state.TunnelPID, tunnelAlive)})
	if !tunnelAlive {
		return doctorFail(report, diagnosticTunnelProcessDead, "RECOVERABLE", "run: stint resume", "Tunnel", "recorded SSH tunnel process is not alive")
	}
	localListening := tcpListening(fmt.Sprintf("127.0.0.1:%d", clinePort), 700*time.Millisecond)
	report.Observations = append(report.Observations, doctorObservation{Name: "Local :8409", OK: localListening, Detail: boolDetail(localListening, "listening", "not listening")})
	if !localListening {
		return doctorFail(report, diagnosticTunnelForwardInvalid, "RECOVERABLE", "run: stint resume", "Tunnel forward", "tunnel process exists but local port 8409 is not accepting connections")
	}

	watchdogAlive := processAlive(state.WatchdogPID)
	report.Observations = append(report.Observations, doctorObservation{Name: "Watchdog", OK: watchdogAlive, Detail: pidDetail(state.WatchdogPID, watchdogAlive)})
	if !watchdogAlive {
		return doctorFail(report, diagnosticWatchdogMissing, "SAFETY", "session may work but deadline protection is missing; run: stint resume", "Watchdog", "recorded watchdog process is not alive")
	}

	if runtimeClass.Progress {
		report.Diagnosis = runtimeClass.Code
		report.Severity = runtimeClass.Severity
		report.Recovery = runtimeClass.Recovery
		if runtimeClass.Code == diagnosticModelDownloading {
			report.Observations = append(report.Observations, doctorObservation{Name: "NInfer model artifact", OK: true, Detail: runtimeClass.Detail})
		}
		return report
	}

	localHealthy, localErr := localEndpointHealthy()
	detail := "healthy"
	if localErr != nil {
		detail = localErr.Error()
	}
	report.Observations = append(report.Observations, doctorObservation{Name: "Local /v1/models", OK: localHealthy, Detail: detail})
	if !localHealthy {
		code := classifyLocalEndpointFailure(localErr)
		return doctorFail(report, code, "RECOVERABLE", "run: stint resume", "Local endpoint", detail)
	}
	return report
}

func probeRemoteRuntime(paths config.Paths, state sessionstate.State) (remoteDoctorState, error) {
	binary := "/workspace/stint/llama.cpp/build/bin/llama-server"
	model := ""
	if runtimeForState(state) == runtimeNInfer {
		binary = "/workspace/stint/ninfer/build/apps/ninfer-serve"
		model = "/workspace/stint/models/qwen3_8_27b.ninfer"
	}
	command := fmt.Sprintf(`set +e
binary=%q
model=%q
pid=""
[ -r /workspace/stint/llama.pid ] && pid="$(cat /workspace/stint/llama.pid 2>/dev/null)"
runtime_ready=0
[ -x "$binary" ] && runtime_ready=1
pid_alive=0
proc=""
if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
  pid_alive=1
  proc="$(ps -p "$pid" -o comm= 2>/dev/null | tr -d ' ' | head -n 1)"
fi
listener=0
if command -v ss >/dev/null 2>&1 && ss -ltn 2>/dev/null | grep -q ':8080 '; then listener=1; fi
api=0
if command -v curl >/dev/null 2>&1 && curl -fsS --max-time 3 http://127.0.0.1:8080/v1/models >/dev/null 2>&1; then api=1; fi
model_bytes=0
if [ -n "$model" ] && [ -f "$model" ]; then model_bytes="$(stat -c %%s "$model" 2>/dev/null || echo 0)"; fi
log_tail="$(tail -n 4 /workspace/stint/llama.log 2>/dev/null | tr '\n' ' ' | tail -c 500)"
printf 'runtime_ready=%%s\npid_alive=%%s\nproc=%%s\nlistener=%%s\napi=%%s\nmodel_bytes=%%s\nlog_tail=%%s\n' "$runtime_ready" "$pid_alive" "$proc" "$listener" "$api" "$model_bytes" "$log_tail"
`, binary, model)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	out, err := runSSH(ctx, paths, state, command)
	if err != nil {
		return remoteDoctorState{}, err
	}
	values := parseKVLines(out)
	modelBytes, _ := strconv.ParseInt(values["model_bytes"], 10, 64)
	return remoteDoctorState{
		RuntimeReady: values["runtime_ready"] == "1",
		PIDAlive:     values["pid_alive"] == "1",
		ProcessName:  values["proc"],
		Listener:     values["listener"] == "1",
		APIHealthy:   values["api"] == "1",
		ModelBytes:   modelBytes,
		LogTail:      strings.TrimSpace(values["log_tail"]),
	}, nil
}

func parseKVLines(out string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}
	return values
}

func doctorFail(report doctorReport, code, severity, recovery, name, detail string) doctorReport {
	report.Diagnosis = code
	report.Severity = severity
	report.Recovery = recovery
	report.Observations = append(report.Observations, doctorObservation{Name: name, OK: false, Detail: detail})
	return report
}

func printDoctorReport(report doctorReport, jsonOutput bool) error {
	if jsonOutput {
		out, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	fmt.Println("Stint doctor")
	fmt.Println()
	if report.InstanceID > 0 {
		fmt.Printf("Instance           %d\n", report.InstanceID)
	}
	if report.Status != "" {
		fmt.Printf("Recorded status    %s\n", report.Status)
	}
	if report.Checkpoint != "" {
		fmt.Printf("Checkpoint         %s\n", report.Checkpoint)
	}
	fmt.Println()
	for _, observation := range report.Observations {
		printCheck(observation.Name, observation.OK, observation.Detail)
	}
	fmt.Println()
	fmt.Printf("Diagnosis          %s\n", report.Diagnosis)
	fmt.Printf("Severity           %s\n", report.Severity)
	if report.Recovery != "" && report.Recovery != "none" {
		fmt.Printf("Next action        %s\n", report.Recovery)
	}
	return nil
}

func classifyLocalEndpointFailure(err error) string {
	if err == nil {
		return diagnosticLocalEndpointHTTPError
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "connection refused"):
		return diagnosticLocalEndpointRefused
	case strings.Contains(text, "timeout"), strings.Contains(text, "deadline exceeded"):
		return diagnosticLocalEndpointTimeout
	default:
		return diagnosticLocalEndpointHTTPError
	}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func tcpListening(address string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func localEndpointHealthy() (bool, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/v1/models", clinePort))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return true, nil
}

func readSessionEvidenceTail(paths config.Paths, instanceID int64, name string, limit int) string {
	data, err := os.ReadFile(sessionstate.SessionLogPath(paths, instanceID, name))
	if err != nil || len(data) == 0 {
		return ""
	}
	text := strings.TrimSpace(string(data))
	if len(text) > limit {
		text = text[len(text)-limit:]
	}
	text = strings.ReplaceAll(text, "\n", " | ")
	return text
}

func boolDetail(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func pidDetail(pid int, alive bool) string {
	if pid <= 0 {
		return "not recorded"
	}
	if alive {
		return fmt.Sprintf("pid %d alive", pid)
	}
	return fmt.Sprintf("pid %d not alive", pid)
}

func formatBytes(value int64) string {
	if value <= 0 {
		return "0 B"
	}
	const gib = 1024 * 1024 * 1024
	if value >= gib {
		return fmt.Sprintf("%.2f GiB", float64(value)/gib)
	}
	const mib = 1024 * 1024
	return fmt.Sprintf("%.1f MiB", float64(value)/mib)
}
