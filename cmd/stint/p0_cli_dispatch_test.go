package main

import "testing"

func TestStatusRefreshDispatchParsesStatusFlags(t *testing.T) {
	if err := run([]string{"status", "--definitely-not-a-real-flag"}); err == nil {
		t.Fatal("status flags were not routed through runStatusSafe")
	}
}

func TestDoctorDispatchParsesDoctorFlags(t *testing.T) {
	if err := run([]string{"doctor", "--definitely-not-a-real-flag"}); err == nil {
		t.Fatal("doctor flags were not routed through runDoctorSafe")
	}
}
