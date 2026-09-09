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
	transferWarmupTimeout           = 20 * time.Second
	transferSampleDuration          = 15 * time.Second
	projectedModelLoadAllowance     = 45 * time.Second
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
		// to avoid an unrelated serialized-data migration.
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

func modelSizeBytesForState(state sessionstate.State) int64 {
	if runtimeForState(state) == runtimeNInfer {
		return 18_210_531_328
	}
	return llamaModelSizeBytes
}

// remoteDownloadProbeCommand no longer downloads a synthetic 100 MB range.
// It starts the exact transfer Stint will need next, waits until bytes are
// flowing, samples that live transfer, then interrupts it while preserving the
// partial artifact/cache so the normal model launch can resume instead of
// throwing the sample work away.
func remoteDownloadProbeCommand(state sessionstate.State) string {
	if runtimeForState(state) == runtimeNInfer {
		return ninferTransferSampleCommand()
	}
	return llamaTransferSampleCommand()
}

func llamaTransferSampleCommand() string {
	return fmt.Sprintf(`set -eu
model_dir=/workspace/stint/models
model="$model_dir/%s"
model_url="%s"
sample_log=/workspace/stint/model-transfer-sample.log
expected=%d
warmup=%d
sample=%d
mkdir -p "$model_dir"

if [ -f "$model" ] && [ "$(stat -c %%s "$model" 2>/dev/null || echo 0)" -ge "$expected" ]; then
  echo "STINT_DOWNLOAD_MB_PER_SEC=9999.000"
  echo "STINT_TRANSFER_BYTES_END=$expected"
  echo "STINT_TRANSFER_SAMPLE_SECONDS=0"
  exit 0
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required for Stint model-transfer qualification" >&2
  exit 127
fi

export HF_HUB_DOWNLOAD_TIMEOUT=60
export HF_HUB_DISABLE_UPDATE_CHECK=1
mem_kb="$(grep -m1 ^MemTotal: /proc/meminfo | tr -cd 0-9)"
if [ "${mem_kb:-0}" -ge 67108864 ]; then
  export HF_XET_HIGH_PERFORMANCE=1
fi
if ! command -v hf >/dev/null 2>&1; then
  curl -LsSf https://hf.co/cli/install.sh | bash -s -- --exclude-skill >/dev/null 2>&1 || true
  export PATH="$HOME/.local/bin:$PATH"
fi

read_bytes() {
  if [ -f "$model" ]; then
    stat -c %%s "$model" 2>/dev/null || echo 0
    return
  fi
  bytes="$(grep -h 'observed bytes sent so far' /root/.cache/huggingface/xet/logs/*.log 2>/dev/null | tail -n 1 | sed -n 's/.*observed bytes sent so far = \([0-9][0-9]*\).*/\1/p')"
  if [ -z "$bytes" ]; then
    bytes="$(find "$model_dir/.cache/huggingface/download" -name '*.incomplete' -printf '%%s\n' 2>/dev/null | sort -nr | head -n 1)"
  fi
  echo "${bytes:-0}"
}

: > "$sample_log"
if command -v hf >/dev/null 2>&1; then
  hf download ggml-org/Qwen3.8-27B-GGUF %s --local-dir "$model_dir" > "$sample_log" 2>&1 &
else
  curl -L -C - --fail --retry 10 --retry-all-errors --retry-delay 2 --connect-timeout 20 --output "$model" "$model_url" > "$sample_log" 2>&1 &
fi
pid=$!
cleanup() {
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
  fi
  wait "$pid" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

deadline=$(( $(date +%%s) + warmup ))
start_bytes=0
while [ "$(date +%%s)" -lt "$deadline" ]; do
  start_bytes="$(read_bytes)"
  if [ "${start_bytes:-0}" -gt 0 ]; then
    break
  fi
  if ! kill -0 "$pid" 2>/dev/null; then
    break
  fi
  sleep 1
done
if [ "${start_bytes:-0}" -le 0 ]; then
  echo "model transfer did not begin within ${warmup}s" >&2
  tail -n 8 "$sample_log" >&2 || true
  exit 2
fi

sample_started="$(date +%%s)"
i=0
while [ "$i" -lt "$sample" ]; do
  sleep 1
  i=$((i + 1))
  if ! kill -0 "$pid" 2>/dev/null; then
    break
  fi
done
sample_finished="$(date +%%s)"
end_bytes="$(read_bytes)"
seconds=$((sample_finished - sample_started))
[ "$seconds" -lt 1 ] && seconds=1
delta=$((end_bytes - start_bytes))
if [ "$delta" -le 0 ]; then
  echo "model transfer made no measurable progress during ${seconds}s sample" >&2
  tail -n 8 "$sample_log" >&2 || true
  exit 3
fi
awk -v b="$delta" -v s="$seconds" 'BEGIN { printf "STINT_DOWNLOAD_MB_PER_SEC=%%.3f\n", b/s/1000000 }'
echo "STINT_TRANSFER_BYTES_END=$end_bytes"
echo "STINT_TRANSFER_SAMPLE_SECONDS=$seconds"
`, llamaModelFileName, llamaModelDownloadURL, llamaModelSizeBytes, int(transferWarmupTimeout/time.Second), int(transferSampleDuration/time.Second), llamaModelFileName)
}

func ninferTransferSampleCommand() string {
	return fmt.Sprintf(`set -eu
model=/workspace/stint/models/qwen3_8_27b.ninfer
model_url="%s"
sample_log=/workspace/stint/model-transfer-sample.log
expected=%d
warmup=%d
sample=%d
mkdir -p /workspace/stint/models

if [ -f "$model" ] && [ "$(stat -c %%s "$model" 2>/dev/null || echo 0)" -ge "$expected" ]; then
  echo "STINT_DOWNLOAD_MB_PER_SEC=9999.000"
  echo "STINT_TRANSFER_BYTES_END=$expected"
  echo "STINT_TRANSFER_SAMPLE_SECONDS=0"
  exit 0
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required for Stint model-transfer qualification" >&2
  exit 127
fi

: > "$sample_log"
curl -L -C - --fail --retry 10 --retry-all-errors --retry-delay 2 --connect-timeout 20 --output "$model" "$model_url" > "$sample_log" 2>&1 &
pid=$!
cleanup() {
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
  fi
  wait "$pid" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

deadline=$(( $(date +%%s) + warmup ))
start_bytes=0
while [ "$(date +%%s)" -lt "$deadline" ]; do
  start_bytes="$(stat -c %%s "$model" 2>/dev/null || echo 0)"
  if [ "${start_bytes:-0}" -gt 0 ]; then
    break
  fi
  if ! kill -0 "$pid" 2>/dev/null; then
    break
  fi
  sleep 1
done
if [ "${start_bytes:-0}" -le 0 ]; then
  echo "model transfer did not begin within ${warmup}s" >&2
  tail -n 8 "$sample_log" >&2 || true
  exit 2
fi

sample_started="$(date +%%s)"
i=0
while [ "$i" -lt "$sample" ]; do
  sleep 1
  i=$((i + 1))
  if ! kill -0 "$pid" 2>/dev/null; then
    break
  fi
done
sample_finished="$(date +%%s)"
end_bytes="$(stat -c %%s "$model" 2>/dev/null || echo 0)"
seconds=$((sample_finished - sample_started))
[ "$seconds" -lt 1 ] && seconds=1
delta=$((end_bytes - start_bytes))
if [ "$delta" -le 0 ]; then
  echo "model transfer made no measurable progress during ${seconds}s sample" >&2
  tail -n 8 "$sample_log" >&2 || true
  exit 3
fi
awk -v b="$delta" -v s="$seconds" 'BEGIN { printf "STINT_DOWNLOAD_MB_PER_SEC=%%.3f\n", b/s/1000000 }'
echo "STINT_TRANSFER_BYTES_END=$end_bytes"
echo "STINT_TRANSFER_SAMPLE_SECONDS=$seconds"
`, ninferModelURL, int64(18_210_531_328), int(transferWarmupTimeout/time.Second), int(transferSampleDuration/time.Second))
}

func measureRemoteDownloadMBps(ctx context.Context, paths config.Paths, state sessionstate.State) (float64, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	out, err := runSSH(probeCtx, paths, state, remoteDownloadProbeCommand(state))
	if err != nil {
		return 0, err
	}
	speed, err := parseRemoteDownloadProbe(out)
	if err != nil {
		return 0, err
	}
	bytesEnd, bytesErr := parseTransferBytesEnd(out)
	if bytesErr == nil && speed > 0 && speed < 9000 {
		remaining := estimateRemainingTransfer(modelSizeBytesForState(state), bytesEnd, speed)
		if remaining >= 0 {
			projected := remaining + projectedModelLoadAllowance
			if !state.StartedAt.IsZero() {
				elapsed := time.Since(state.StartedAt)
				if elapsed > 0 {
					projected += elapsed
				}
			}
			fmt.Printf("Model ETA       %s remaining at sampled throughput\n", formatProjectedDuration(remaining))
			fmt.Printf("Projected READY ~%s from rental (includes %s load allowance)\n", formatProjectedDuration(projected), formatProjectedDuration(projectedModelLoadAllowance))
		}
	}
	return speed, nil
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
	return 0, fmt.Errorf("remote model-transfer sample did not return a speed marker: %s", strings.TrimSpace(output))
}

func parseTransferBytesEnd(output string) (int64, error) {
	const marker = "STINT_TRANSFER_BYTES_END="
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, marker) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, marker))
		bytes, err := strconv.ParseInt(value, 10, 64)
		if err != nil || bytes < 0 {
			return 0, fmt.Errorf("invalid transfer byte marker %q", value)
		}
		return bytes, nil
	}
	return 0, errors.New("model-transfer sample did not return a byte marker")
}

func estimateRemainingTransfer(totalBytes, downloadedBytes int64, speedMBps float64) time.Duration {
	if speedMBps <= 0 || math.IsNaN(speedMBps) || math.IsInf(speedMBps, 0) {
		return -1
	}
	remaining := totalBytes - downloadedBytes
	if remaining <= 0 {
		return 0
	}
	seconds := float64(remaining) / (speedMBps * 1_000_000)
	return time.Duration(seconds * float64(time.Second))
}

func formatProjectedDuration(value time.Duration) string {
	if value < 0 {
		return "unknown"
	}
	value = value.Round(time.Second)
	minutes := int(value / time.Minute)
	seconds := int((value % time.Minute) / time.Second)
	if minutes == 0 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm%02ds", minutes, seconds)
}

func destroyRejectedInstance(client *vast.Client, paths config.Paths, state sessionstate.State) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := client.DestroyInstance(cleanupCtx, state.InstanceID); err != nil {
		return err
	}
	killPID(state.TunnelPID)
	killPID(state.WatchdogPID)
	return sessionstate.Clear(paths)
}
