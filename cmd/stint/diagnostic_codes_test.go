package main

import "testing"

func TestDiagnosticCodesAreStable(t *testing.T) {
	if diagnosticOK != "OK" || diagnosticDestroyUnconfirmed != "DESTROY_UNCONFIRMED" || diagnosticModelDownloading != "MODEL_DOWNLOADING" {
		t.Fatal("diagnostic code values changed")
	}
}
