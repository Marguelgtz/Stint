package main

import (
	"strings"
	"testing"

	"github.com/Marguelgtz/Stint/internal/core"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestFilterOffersByMinimumNetwork(t *testing.T) {
	offers := []core.Offer{
		{ID: "slow", InetDownMBps: 120},
		{ID: "minimum", InetDownMBps: 500},
		{ID: "fast", InetDownMBps: 950},
	}
	got := filterOffersByMinimumNetwork(offers, 500)
	if len(got) != 2 || got[0].ID != "minimum" || got[1].ID != "fast" {
		t.Fatalf("filtered offers = %#v", got)
	}
}

func TestFilterOffersByMinimumNetworkCanBeDisabled(t *testing.T) {
	offers := []core.Offer{{ID: "slow", InetDownMBps: 10}}
	got := filterOffersByMinimumNetwork(offers, 0)
	if len(got) != 1 || got[0].ID != "slow" {
		t.Fatalf("filtered offers = %#v", got)
	}
}

func TestNetworkProbeUsesActualRuntimeArtifactAndBoundedRange(t *testing.T) {
	tests := []struct {
		name    string
		state   sessionstate.State
		wantURL string
	}{
		{name: "llama", state: sessionstate.State{Runtime: runtimeLlamaCpp}, wantURL: llamaModelDownloadURL},
		{name: "ninfer", state: sessionstate.State{Runtime: runtimeNInfer}, wantURL: ninferModelURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := remoteDownloadProbeCommand(tt.state)
			for _, required := range []string{
				tt.wantURL,
				"--range 0-99999999",
				"--max-filesize 100000000",
				"Accept-Encoding: identity",
				"Stint network qualification",
			} {
				if !strings.Contains(command, required) {
					t.Fatalf("network probe missing %q: %s", required, command)
				}
			}
			if strings.Contains(command, "speed.cloudflare.com") {
				t.Fatalf("network probe must not depend on generic Cloudflare speed endpoint: %s", command)
			}
		})
	}
}

func TestParseRemoteDownloadProbeIgnoresSSHBanners(t *testing.T) {
	output := "Welcome to vast.ai.\nHave fun!\nSTINT_DOWNLOAD_MB_PER_SEC=47.625\n"
	got, err := parseRemoteDownloadProbe(output)
	if err != nil {
		t.Fatal(err)
	}
	if got != 47.625 {
		t.Fatalf("speed = %f, want 47.625", got)
	}
}

func TestParseRemoteDownloadProbeRequiresMarker(t *testing.T) {
	if _, err := parseRemoteDownloadProbe("Welcome to vast.ai.\n"); err == nil {
		t.Fatal("expected missing marker error")
	}
}
