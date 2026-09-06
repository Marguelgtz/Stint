package main

import sessionstate "github.com/Marguelgtz/Stint/internal/session"

type runtimeClassification struct {
	Code     string
	Severity string
	Recovery string
	Detail   string
	Progress bool
}

func classifyRemoteRuntimeState(state sessionstate.State, remote remoteDoctorState) runtimeClassification {
	if !remote.RuntimeReady {
		return runtimeClassification{
			Code: diagnosticRuntimeBinaryMissing, Severity: "RECOVERABLE", Recovery: "run: stint resume",
			Detail: "selected runtime binary is missing",
		}
	}
	if !remote.PIDAlive {
		detail := "launcher/runtime process is not alive"
		if remote.LogTail != "" {
			detail += "; log: " + remote.LogTail
		}
		return runtimeClassification{
			Code: diagnosticRuntimeProcessDead, Severity: "RECOVERABLE", Recovery: "run: stint resume", Detail: detail,
		}
	}
	if !remote.Listener {
		if runtimeForState(state) == runtimeNInfer && remote.ModelBytes > 0 {
			return runtimeClassification{
				Code: diagnosticModelDownloading, Severity: "PROGRESS",
				Recovery: "wait; do not destroy or rent another instance",
				Detail: formatBytes(remote.ModelBytes) + " present/downloading", Progress: true,
			}
		}
		return runtimeClassification{
			Code: diagnosticRuntimeProcessStarting, Severity: "PROGRESS",
			Recovery: "wait; rerun stint doctor if startup stops making progress",
			Detail: "runtime process is alive but remote port 8080 is not listening yet", Progress: true,
		}
	}
	if !remote.APIHealthy {
		return runtimeClassification{
			Code: diagnosticModelLoadInProgress, Severity: "PROGRESS",
			Recovery: "wait; rerun stint doctor if readiness does not converge",
			Detail: "remote server is listening but /v1/models is not ready", Progress: true,
		}
	}
	return runtimeClassification{Code: diagnosticOK, Severity: "HEALTHY", Recovery: "none"}
}
