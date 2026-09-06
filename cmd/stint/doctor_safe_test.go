package main

import (
	"path/filepath"
	"testing"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestDiagnoseLastSessionReportsUnconfirmedDestroy(t *testing.T) {
	paths := config.Paths{StateDir: filepath.Join(t.TempDir(), "state")}
	state := sessionstate.State{InstanceID: 77, Status: sessionstate.StatusDestroyUnconfirmed, Checkpoint: sessionstate.CheckpointReady}
	if err := sessionstate.ArchiveState(paths, state, sessionstate.DispositionDestroyUnconfirmed, false, 4, "provider timeout"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	report, err := diagnoseLastSession(paths)
	if err != nil {
		t.Fatalf("diagnoseLastSession: %v", err)
	}
	if report.Diagnosis != diagnosticDestroyUnconfirmed || report.Severity != "SAFETY" {
		t.Fatalf("report = %+v", report)
	}
}

func TestClassifyLocalEndpointFailure(t *testing.T) {
	if got := classifyLocalEndpointFailure(assertError("dial tcp 127.0.0.1:8409: connect: connection refused")); got != diagnosticLocalEndpointRefused {
		t.Fatalf("refused = %s", got)
	}
	if got := classifyLocalEndpointFailure(assertError("context deadline exceeded (Client.Timeout exceeded)")); got != diagnosticLocalEndpointTimeout {
		t.Fatalf("timeout = %s", got)
	}
	if got := classifyLocalEndpointFailure(assertError("HTTP 503")); got != diagnosticLocalEndpointHTTPError {
		t.Fatalf("HTTP = %s", got)
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
