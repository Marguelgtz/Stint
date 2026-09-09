package main

import (
	"os/exec"
	"strings"
	"testing"
	"time"

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

func TestNetworkQualificationSamplesActualRuntimeTransfer(t *testing.T) {
	tests := []struct {
		name     string
		state    sessionstate.State
		wantURL  string
		wantMode string
	}{
		{name: "llama", state: sessionstate.State{Runtime: runtimeLlamaCpp}, wantURL: llamaModelDownloadURL, wantMode: "hf download ggml-org/Qwen3.8-27B-GGUF"},
		{name: "ninfer", state: sessionstate.State{Runtime: runtimeNInfer}, wantURL: ninferModelURL, wantMode: "curl -L -C -"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := remoteDownloadProbeCommand(tt.state)
			for _, required := range []string{
				tt.wantURL,
				tt.wantMode,
				"model-transfer-sample.log",
				"STINT_DOWNLOAD_MB_PER_SEC",
				"STINT_TRANSFER_BYTES_END",
				"kill \"$pid\"",
			} {
				if !strings.Contains(command, required) {
					t.Fatalf("model-transfer qualification missing %q: %s", required, command)
				}
			}
			for _, forbidden := range []string{
				"--range 0-99999999",
				"--max-filesize 100000000",
				"speed.cloudflare.com",
			} {
				if strings.Contains(command, forbidden) {
					t.Fatalf("model-transfer qualification retained synthetic probe %q: %s", forbidden, command)
				}
			}
			if out, err := exec.Command("bash", "-n", "-c", command).CombinedOutput(); err != nil {
				t.Fatalf("model-transfer qualification command has invalid shell syntax: %v\n%s\ncommand:\n%s", err, out, command)
			}
		})
	}
}

func TestLlamaTransferSampleUsesXetWhenAvailableAndResumableFallback(t *testing.T) {
	command := llamaTransferSampleCommand()
	for _, required := range []string{
		"HF_XET_HIGH_PERFORMANCE",
		"hf.co/cli/install.sh",
		"hf download ggml-org/Qwen3.8-27B-GGUF",
		"observed bytes sent so far",
		"*.incomplete",
		"curl -L -C -",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("llama transfer sample missing %q", required)
		}
	}
}

func TestParseRemoteDownloadProbeIgnoresSSHBanners(t *testing.T) {
	output := "Welcome to vast.ai.\nHave fun!\nSTINT_DOWNLOAD_MB_PER_SEC=127.625\nSTINT_TRANSFER_BYTES_END=2147483648\n"
	got, err := parseRemoteDownloadProbe(output)
	if err != nil {
		t.Fatal(err)
	}
	if got != 127.625 {
		t.Fatalf("speed = %f, want 127.625", got)
	}
	bytes, err := parseTransferBytesEnd(output)
	if err != nil {
		t.Fatal(err)
	}
	if bytes != 2147483648 {
		t.Fatalf("bytes = %d, want 2147483648", bytes)
	}
}

func TestParseRemoteDownloadProbeRequiresMarker(t *testing.T) {
	if _, err := parseRemoteDownloadProbe("Welcome to vast.ai.\n"); err == nil {
		t.Fatal("expected missing marker error")
	}
	if _, err := parseTransferBytesEnd("STINT_DOWNLOAD_MB_PER_SEC=50\n"); err == nil {
		t.Fatal("expected missing byte marker error")
	}
}

func TestEstimateRemainingTransfer(t *testing.T) {
	got := estimateRemainingTransfer(18_000_000_000, 3_000_000_000, 100)
	want := 150 * time.Second
	if got != want {
		t.Fatalf("remaining transfer = %s, want %s", got, want)
	}
	if got := estimateRemainingTransfer(18_000_000_000, 18_000_000_000, 100); got != 0 {
		t.Fatalf("complete transfer ETA = %s, want 0", got)
	}
	if got := estimateRemainingTransfer(18_000_000_000, 0, 0); got != -1 {
		t.Fatalf("invalid speed ETA = %s, want -1", got)
	}
}

func TestFormatProjectedDuration(t *testing.T) {
	if got := formatProjectedDuration(4*time.Minute + 31*time.Second); got != "4m31s" {
		t.Fatalf("duration = %q, want 4m31s", got)
	}
	if got := formatProjectedDuration(31 * time.Second); got != "31s" {
		t.Fatalf("duration = %q, want 31s", got)
	}
}
