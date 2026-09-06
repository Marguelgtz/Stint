package main

import (
	"testing"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestClassifyRemoteRuntimeNInferDownloadIsProgress(t *testing.T) {
	state := sessionstate.State{Runtime: runtimeNInfer}
	remote := remoteDoctorState{
		RuntimeReady: true,
		PIDAlive:     true,
		ProcessName:  "bash",
		Listener:     false,
		APIHealthy:   false,
		ModelBytes:   12 * 1024 * 1024 * 1024,
	}
	got := classifyRemoteRuntimeState(state, remote)
	if got.Code != diagnosticModelDownloading || got.Severity != "PROGRESS" || !got.Progress {
		t.Fatalf("classification = %+v, want MODEL_DOWNLOADING/PROGRESS", got)
	}
}

func TestClassifyRemoteRuntimeDeadIsRecoverable(t *testing.T) {
	state := sessionstate.State{Runtime: runtimeNInfer}
	remote := remoteDoctorState{RuntimeReady: true, PIDAlive: false, LogTail: "fatal"}
	got := classifyRemoteRuntimeState(state, remote)
	if got.Code != diagnosticRuntimeProcessDead || got.Severity != "RECOVERABLE" || got.Progress {
		t.Fatalf("classification = %+v, want RUNTIME_PROCESS_DEAD/RECOVERABLE", got)
	}
}
