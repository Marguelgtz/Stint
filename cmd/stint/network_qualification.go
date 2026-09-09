package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/core"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const (
	defaultMinAdvertisedNetworkMbps = 500.0
	defaultMinMeasuredDownloadMBps  = 40.0
	downloadProbeBytes              = 100_000_000
)

func validateNetworkMinimums(advertisedMbps, measuredMBps float64) error {
	if advertisedMbps < 0 || math.IsNaN(advertisedMbps) || math.IsInf(advertisedMbps, 0) {
		return errors.New("--min-network-mbps must be zero or greater")
	}
	if measuredMBps < 0 || math.IsNaN(measuredMBps) || math.IsInf(measuredMBps, 0) {
		return errors.New("--min-measured-download-mbps must be zero or greater")
	}
	return nil
}

func filterOffersByMinimumNetwork(offers []core.Offer, minimumMbps float64) []core.Offer {
	if minimumMbps <= 0 {
		return offers
	}
	qualified := make([]core.Offer, 0, len(offers))
	for _, offer := range offers {
		// Vast's inet_down marketplace value is treated as Mbps here. The legacy
		// Offer field name predates that unit clarification and is left unchanged
		// in this cold-start PR to avoid an unrelated serialized-data migration.
		if offer.InetDownMBps >= minimumMbps {
			qualified = append(qualified, offer)
		}
	}
	return qualified
}

func networkProbeURLForState(state sessionstate.State) string {
	if runtimeForState(state) == runtimeNInfer {
		return ninferModelURL
	}
	return llamaModelDownloadURL
}

func remoteDownloadProbeCommand(state sessionstate.State) string {
	// Probe the exact artifact origin Stint will use next instead of a generic
	// speed-test CDN. The byte range keeps qualification bounded to 100 MB while
	// exercising redirects/CDN policy on the real model delivery path.
	probeURL := networkProbeURLForState(state)
	return fmt.Sprintf(`set -eu
if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required for the Stint network qualification probe" >&2
  exit 127
fi
speed="$(curl -L --fail --silent --show-error \
  --retry 2 --retry-all-errors --retry-delay 1 \
  --connect-timeout 10 --max-time 35 \
  --range 0-%d --max-filesize %d \
  --header 'Accept-Encoding: identity' \
  --user-agent 'Stint network qualification' \
  -o /dev/null -w '%%{speed_download}' \
  '%s')"
awk -v s="$speed" 'BEGIN { printf "STINT_DOWNLOAD_MB_PER_SEC=%%.3f\n", s/1000000 }'
`, downloadProbeBytes-1, downloadProbeBytes, probeURL)
}

func measureRemoteDownloadMBps(ctx context.Context, paths config.Paths, state sessionstate.State) (float64, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	out, err := runSSH(probeCtx, paths, state, remoteDownloadProbeCommand(state))
	if err != nil {
		return 0, err
	}
	return parseRemoteDownloadProbe(out)
}

func parseRemoteDownloadProbe(output string) (float64, error) {
	const marker = "STINT_DOWNLOAD_MB_PER_SEC="
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, marker) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, marker))
		speed, err := strconv.ParseFloat(value, 64)
		if err != nil || speed < 0 || math.IsNaN(speed) || math.IsInf(speed, 0) {
			return 0, fmt.Errorf("invalid remote download probe result %q", value)
		}
		return speed, nil
	}
	return 0, fmt.Errorf("remote download probe did not return a speed marker: %s", strings.TrimSpace(output))
}

func destroyNetworkRejectedInstance(client *vast.Client, paths config.Paths, state sessionstate.State) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := client.DestroyInstance(cleanupCtx, state.InstanceID); err != nil {
		return err
	}
	killPID(state.TunnelPID)
	killPID(state.WatchdogPID)
	return sessionstate.Clear(paths)
}
