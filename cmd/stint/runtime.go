package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const (
	runtimeAuto     = "auto"
	runtimeNInfer   = "ninfer"
	runtimeLlamaCpp = "llama.cpp"

	interactiveRuntimeContext = 126976

	ninferSourceRepository        = "https://github.com/sergiuszm/ninfer-4090.git"
	ninferSourceCommit            = "81b68a20a9a0d9ab47d7e5838887c6d636ab76e0"
	ninferCUDAFloor               = "12.8"
	ninferGPUArchitecture         = "89"
	ninferArtifactFormat          = 2
	ninferModelRevision           = "18dfc887423fa5aabf3cb56fac41490e462b3fab"
	ninferModelURL                = "https://huggingface.co/neroued/Qwen3.8-27B-NInfer/resolve/" + ninferModelRevision + "/qwen3_8_27b.ninfer"
	ninferModelSHA256             = "eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e"
	ninferModelSizeBytes          = int64(18210531328)
	ninferDeploymentSourceBuild   = "source-build"
	ninferDeploymentReleaseBundle = "release-bundle"
	ninferRuntimeReleaseTag       = "ninfer-runtime-81b68a20-sm89"
	ninferRuntimeBundleName       = "stint-ninfer-81b68a20-sm89-linux-amd64.tar.gz"
	ninferRuntimeBundleSHA256     = "6725e60c8e3edb2982ad828898210868dd98ea1d4fe4d35f97bfa1e625414416"
	ninferRuntimeReleaseURL       = "https://github.com/Marguelgtz/Stint/releases/download/" + ninferRuntimeReleaseTag
)

func normalizeNInferDeployment(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", ninferDeploymentSourceBuild:
		return ninferDeploymentSourceBuild, nil
	case ninferDeploymentReleaseBundle:
		return ninferDeploymentReleaseBundle, nil
	default:
		return "", fmt.Errorf("unknown NInfer deployment %q; choose source-build or release-bundle", value)
	}
}

func ninferDeploymentForState(state sessionstate.State) string {
	if state.RuntimeDeployment == ninferDeploymentReleaseBundle {
		return ninferDeploymentReleaseBundle
	}
	// Existing NInfer sessions predate deployment metadata and used source builds.
	return ninferDeploymentSourceBuild
}

func runtimeDeploymentForStatus(state sessionstate.State) string {
	if runtimeForState(state) == runtimeNInfer {
		return ninferDeploymentForState(state)
	}
	return state.RuntimeDeployment
}

func allowNInferLlamaFallback(state sessionstate.State) bool {
	return ninferDeploymentForState(state) != ninferDeploymentReleaseBundle && allowLlamaFallbackForState(state)
}

func normalizeRuntime(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", runtimeAuto:
		return runtimeAuto, nil
	case runtimeNInfer:
		return runtimeNInfer, nil
	case "llama", "llamacpp", "llama-cpp", runtimeLlamaCpp:
		return runtimeLlamaCpp, nil
	default:
		return "", fmt.Errorf("unknown inference runtime %q; choose auto, ninfer, or llama.cpp", value)
	}
}

func selectInteractiveRuntime(requested, gpuModel string) (string, error) {
	runtime, err := normalizeRuntime(requested)
	if err != nil {
		return "", err
	}
	is4090 := strings.Contains(strings.ReplaceAll(strings.ToLower(gpuModel), " ", ""), "4090")
	switch runtime {
	case runtimeAuto:
		if is4090 {
			return runtimeNInfer, nil
		}
		return runtimeLlamaCpp, nil
	case runtimeNInfer:
		if !is4090 {
			return "", fmt.Errorf("ninfer is currently qualified only for RTX 4090, got %q", gpuModel)
		}
		return runtimeNInfer, nil
	default:
		return runtimeLlamaCpp, nil
	}
}

// runtimeForState deliberately treats sessions created before runtime metadata
// was introduced as llama.cpp sessions. That preserves resume compatibility.
func runtimeForState(state sessionstate.State) string {
	if state.Runtime == runtimeNInfer || state.Runtime == runtimeLlamaCpp {
		return state.Runtime
	}
	return runtimeLlamaCpp
}

func contextForState(state sessionstate.State) int {
	if state.ContextTokens > 0 {
		return state.ContextTokens
	}
	// Legacy v0.1.0 sessions were started with this context size.
	return interactiveContext
}

func bootstrapSelectedRuntime(ctx context.Context, paths config.Paths, state *sessionstate.State) (string, error) {
	switch runtimeForState(*state) {
	case runtimeNInfer:
		deployment := ninferDeploymentForState(*state)
		state.RuntimeDeployment = deployment
		state.RuntimeSourceCommit = ninferSourceCommit
		state.ModelArtifactRevision = ninferModelRevision
		state.ModelArtifactSHA256 = ninferModelSHA256
		state.ModelArtifactSizeBytes = ninferModelSizeBytes
		state.ModelArtifactFormat = fmt.Sprintf("NInfer v%d", ninferArtifactFormat)
		state.RuntimeAcquisitionStartedAt = time.Now().UTC()
		if deployment == ninferDeploymentReleaseBundle {
			state.RuntimeBundleTag = ninferRuntimeReleaseTag
			state.RuntimeBundleSHA256 = ninferRuntimeBundleSHA256
		} else {
			state.RuntimeBundleTag = ""
			state.RuntimeBundleSHA256 = ""
		}
		if err := sessionstate.Save(paths, *state); err != nil {
			return "", fmt.Errorf("persist NInfer runtime deployment provenance: %w", err)
		}
		if err := bootstrapNInfer(ctx, paths, *state); err == nil {
			return runtimeNInfer, nil
		} else if !allowNInferLlamaFallback(*state) {
			if state.RuntimeRequest == runtimeAuto && clientsForState(*state) > defaultNInferClients {
				return "", fmt.Errorf("ninfer bootstrap failed and --clients %d requires NInfer; refusing llama.cpp fallback: %w", clientsForState(*state), err)
			}
			return "", err
		} else {
			fmt.Printf("NInfer bootstrap unavailable on this host (%v). Falling back to llama.cpp.\n", err)
			if fallbackErr := bootstrapRemoteRuntime(ctx, paths, *state); fallbackErr != nil {
				return "", fmt.Errorf("ninfer bootstrap failed (%v); llama.cpp fallback also failed: %w", err, fallbackErr)
			}
			state.RuntimeDeployment = "llama.cpp-fallback"
			state.RuntimeSourceCommit = ""
			state.ModelArtifactRevision = ""
			state.ModelArtifactSHA256 = ""
			state.ModelArtifactSizeBytes = 0
			state.ModelArtifactFormat = ""
			state.RuntimeBundleTag = ""
			state.RuntimeBundleSHA256 = ""
			return runtimeLlamaCpp, nil
		}
	default:
		if err := bootstrapRemoteRuntime(ctx, paths, *state); err != nil {
			return "", err
		}
		return runtimeLlamaCpp, nil
	}
}

func bootstrapNInfer(ctx context.Context, paths config.Paths, state sessionstate.State) error {
	deployment := ninferDeploymentForState(state)
	command := ninferBootstrapCommand()
	if deployment == ninferDeploymentReleaseBundle {
		fmt.Printf("Preparing NInfer from immutable release %s...\n", ninferRuntimeReleaseTag)
		command = ninferReleaseBootstrapCommand()
	} else {
		fmt.Printf("Building NInfer from pinned commit %.12s...\n", ninferSourceCommit)
		fmt.Println("Stint overlaps the Qwen model transfer with the source build and records both sides of the startup critical path.")
	}
	if err := runSSHStreaming(ctx, paths, state, command); err != nil {
		return fmt.Errorf("bootstrap remote ninfer runtime: %w", err)
	}
	fmt.Println("NInfer runtime ready.")
	return nil
}

func captureRuntimeBootstrapTiming(ctx context.Context, paths config.Paths, state *sessionstate.State) {
	started := state.RuntimeAcquisitionStartedAt
	if started.IsZero() {
		started = time.Now().UTC()
	}
	state.RuntimeAcquiredAt = time.Now().UTC()
	state.RuntimeVerifiedAt = state.RuntimeAcquiredAt
	state.RuntimeAcquisitionMillis = state.RuntimeAcquiredAt.Sub(started).Milliseconds()
	state.RuntimeVerificationMillis = 0
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := runSSH(probeCtx, paths, *state, `for name in runtime-acquisition-started-ms runtime-acquired-ms runtime-verified-ms; do printf '%s=' "$name"; cat "/workspace/stint/$name" 2>/dev/null || true; done`)
	if err != nil {
		return
	}
	markers := map[string]time.Time{}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || value == "" {
			continue
		}
		millis, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr == nil && millis > 0 {
			markers[key] = time.UnixMilli(millis).UTC()
		}
	}
	if value, ok := markers["runtime-acquisition-started-ms"]; ok {
		state.RuntimeAcquisitionStartedAt = value
	}
	if value, ok := markers["runtime-acquired-ms"]; ok {
		state.RuntimeAcquiredAt = value
	}
	if value, ok := markers["runtime-verified-ms"]; ok {
		state.RuntimeVerifiedAt = value
	}
	state.RuntimeAcquisitionMillis = state.RuntimeAcquiredAt.Sub(state.RuntimeAcquisitionStartedAt).Milliseconds()
	if state.RuntimeAcquisitionMillis < 0 {
		state.RuntimeAcquisitionMillis = 0
	}
	state.RuntimeVerificationMillis = state.RuntimeVerifiedAt.Sub(state.RuntimeAcquiredAt).Milliseconds()
	if state.RuntimeVerificationMillis < 0 {
		state.RuntimeVerificationMillis = 0
	}
}

func captureModelAcquisitionTiming(ctx context.Context, paths config.Paths, state *sessionstate.State) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := runSSH(probeCtx, paths, *state, `for name in model-acquisition-started-ms model-acquired-ms; do printf '%s=' "$name"; cat "/workspace/stint/$name" 2>/dev/null || true; done`)
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || value == "" {
			continue
		}
		millis, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil || millis <= 0 {
			continue
		}
		switch key {
		case "model-acquisition-started-ms":
			state.ModelAcquisitionStartedAt = time.UnixMilli(millis).UTC()
		case "model-acquired-ms":
			state.ModelAcquiredAt = time.UnixMilli(millis).UTC()
		}
	}
	if !state.ModelAcquisitionStartedAt.IsZero() && !state.ModelAcquiredAt.IsZero() && !state.ModelAcquiredAt.Before(state.ModelAcquisitionStartedAt) {
		state.ModelAcquisitionMillis = state.ModelAcquiredAt.Sub(state.ModelAcquisitionStartedAt).Milliseconds()
	}
}

func markSessionReadyAt(state *sessionstate.State, readyAt time.Time) {
	if state.ReadyAt.IsZero() {
		state.ReadyAt = readyAt.UTC()
	}
	if !state.RentalStartedAt.IsZero() && !state.ReadyAt.Before(state.RentalStartedAt) {
		state.ReadyElapsedFromRentalMillis = state.ReadyAt.Sub(state.RentalStartedAt).Milliseconds()
	}
}

func ninferBootstrapCommand() string {
	return fmt.Sprintf(`set -eu
root=/workspace/stint
src="$root/ninfer"
build="$src/build"
bin="$build/apps/ninfer-serve"
commit_file="$src/.stint-commit"
model_dir="$root/models"
model="$model_dir/qwen3_8_27b.ninfer"
model_pid="$root/model-download.pid"
model_log="$root/model-download.log"
model_size_file="$root/model-total-bytes"
model_started_file="$root/model-acquisition-started-ms"
model_acquired_file="$root/model-acquired-ms"
model_sha="%s"
model_url="%s"
mkdir -p "$root" "$model_dir"
date +%%s%%3N > "$root/runtime-acquisition-started-ms"

if [ -f "$model" ] && echo "$model_sha  $model" | sha256sum -c - >/dev/null 2>&1; then
  echo "Qwen3.8-27B model artifact already cached."
elif [ -r "$model_pid" ] && prefetch_pid="$(cat "$model_pid" 2>/dev/null || true)" && [ -n "$prefetch_pid" ] && kill -0 "$prefetch_pid" 2>/dev/null; then
  echo "Qwen3.8-27B model prefetch already running; continuing runtime bootstrap in parallel."
elif command -v curl >/dev/null 2>&1; then
  rm -f "$model_pid"
  echo "Starting Qwen3.8-27B model prefetch in parallel with NInfer bootstrap..."
  nohup sh -c '
set -eu
model="/workspace/stint/models/qwen3_8_27b.ninfer"
model_pid="/workspace/stint/model-download.pid"
model_started_file="/workspace/stint/model-acquisition-started-ms"
model_acquired_file="/workspace/stint/model-acquired-ms"
model_sha="%s"
model_url="%s"
date +%%s%%3N > "$model_started_file"
curl -L -C - --fail --retry 10 --retry-all-errors --retry-delay 2 --connect-timeout 20 --output "$model" "$model_url"
echo "$model_sha  $model" | sha256sum -c -
date +%%s%%3N > "$model_acquired_file"
rm -f "$model_pid"
' > "$model_log" 2>&1 < /dev/null &
  printf '%%s\n' "$!" > "$model_pid"
else
  echo "No downloader is available before bootstrap; model transfer will start after curl is installed."
fi

if [ -x "$bin" ] && [ -r "$commit_file" ] && [ "$(cat "$commit_file")" = %s ]; then
  "$bin" --help >/dev/null
else
  if ! command -v nvcc >/dev/null 2>&1; then
    echo "NInfer requires CUDA toolkit 12.8 or newer (nvcc missing)." >&2
    exit 42
  fi
  cuda_version="$(nvcc --version | sed -n 's/.*release \([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\1 \2/p' | head -n 1)"
  set -- $cuda_version
  if [ "$#" -ne 2 ] || [ "$1" -lt 12 ] || { [ "$1" -eq 12 ] && [ "$2" -lt 8 ]; }; then
    echo "NInfer requires CUDA toolkit 12.8 or newer; found: $(nvcc --version | tail -n 1)" >&2
    exit 42
  fi

  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y --no-install-recommends \
    git ca-certificates curl cmake ninja-build pkg-config gcc-13 g++-13 \
    libavcodec-dev libavformat-dev libavutil-dev libcurl4-openssl-dev libswscale-dev

  if ! command -v gcc-13 >/dev/null 2>&1 || ! command -v g++-13 >/dev/null 2>&1; then
    echo "NInfer requires GCC/G++ 13." >&2
    exit 42
  fi
  if ! cmake --version | awk 'NR == 1 { split($3, v, "."); ok = (v[1] > 3 || (v[1] == 3 && v[2] >= 28)); exit(ok ? 0 : 1) }'; then
    echo "NInfer requires CMake 3.28 or newer; found: $(cmake --version | head -n 1)" >&2
    exit 42
  fi

  if [ ! -d "$src/.git" ] || [ ! -r "$commit_file" ] || [ "$(cat "$commit_file" 2>/dev/null || true)" != %s ]; then
    rm -rf "$src"
    mkdir -p "$src"
    git -C "$src" init -q
    git -C "$src" remote add origin %s
    git -C "$src" fetch -q --depth 1 origin %s
    git -C "$src" checkout -q --detach FETCH_HEAD
    printf '%%s\n' %s > "$commit_file"
  fi

  CC=/usr/bin/gcc-13 \
  CXX=/usr/bin/g++-13 \
  CUDACXX="$(command -v nvcc)" \
  CUDAHOSTCXX=/usr/bin/g++-13 \
  cmake -S "$src" -B "$build" -G Ninja \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_CUDA_ARCHITECTURES=89 \
    -DNINFER_BUILD_APPS=ON \
    -DBUILD_TESTING=OFF \
    -DNINFER_BUILD_BENCHMARKS=OFF
  cmake --build "$build" --parallel "$(nproc)" --target ninfer ninfer-serve
fi

mkdir -p "$src"
if [ -e "$src/bin" ] && [ ! -L "$src/bin" ]; then
  echo "Refusing to replace non-symlink NInfer runtime path $src/bin" >&2
  exit 1
fi
rm -f "$src/bin.next"
ln -s "$build/apps" "$src/bin.next"
mv -Tf "$src/bin.next" "$src/bin"
date +%%s%%3N > "$root/runtime-acquired-ms"
for binary in "$src/bin/ninfer" "$src/bin/ninfer-serve"; do
  test -x "$binary"
  "$binary" --help >/dev/null
  ldd_output="$(ldd "$binary")"
  if echo "$ldd_output" | grep -q "not found"; then
    echo "$ldd_output" >&2
    exit 1
  fi
done
date +%%s%%3N > "$root/runtime-verified-ms"
deployment_tmp="$(mktemp "$src/.stint-deployment.XXXXXX")"
printf '%%s %%s\n' source-build %s > "$deployment_tmp"
mv -f "$deployment_tmp" "$src/.stint-deployment"
echo "NInfer runtime verified; model acquisition does not delay runtime readiness."
`, ninferModelSHA256, ninferModelURL, ninferModelSHA256, ninferModelURL, ninferSourceCommit, ninferSourceCommit, ninferSourceRepository, ninferSourceCommit, ninferSourceCommit, ninferSourceCommit)
}

func ninferReleaseBootstrapCommand() string {
	command := `set -eu
root=/workspace/stint
model_dir="$root/models"
model="$model_dir/qwen3_8_27b.ninfer"
model_pid="$root/model-download.pid"
model_log="$root/model-download.log"
mkdir -p "$model_dir"

if [ -f "$model" ] && echo "@MODEL_SHA@  $model" | sha256sum -c - >/dev/null 2>&1; then
  echo "Qwen3.8-27B model artifact already cached."
elif [ -r "$model_pid" ] && prefetch_pid="$(cat "$model_pid" 2>/dev/null || true)" && [ -n "$prefetch_pid" ] && kill -0 "$prefetch_pid" 2>/dev/null; then
  echo "Qwen3.8-27B model prefetch already running; continuing release acquisition in parallel."
else
  rm -f "$model_pid"
  echo "Starting Qwen3.8-27B model prefetch in parallel with NInfer release acquisition..."
  nohup sh -c '
set -eu
model="/workspace/stint/models/qwen3_8_27b.ninfer"
model_sha="@MODEL_SHA@"
model_url="@MODEL_URL@"
date +%s%3N > /workspace/stint/model-acquisition-started-ms
curl -L -C - --fail --retry 10 --retry-all-errors --retry-delay 2 --connect-timeout 20 --output "$model" "$model_url"
echo "$model_sha  $model" | sha256sum -c -
date +%s%3N > /workspace/stint/model-acquired-ms
rm -f /workspace/stint/model-download.pid
' > "$model_log" 2>&1 < /dev/null &
  printf '%s\n' "$!" > "$model_pid"
fi

date +%s%3N > "$root/runtime-acquisition-started-ms"
download_dir="$(mktemp -d "$root/.ninfer-release.XXXXXX")"
trap 'rm -rf "$download_dir"' EXIT
archive="$download_dir/@ARCHIVE@"
checksum="$archive.sha256"
manifest="$download_dir/manifest.json"
release_url="@RELEASE_URL@"
curl --fail --location --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 20 -o "$archive" "$release_url/@ARCHIVE@"
curl --fail --location --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 20 -o "$checksum" "$release_url/@ARCHIVE@.sha256"
curl --fail --location --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 20 -o "$manifest" "$release_url/manifest.json"
actual_sha="$(sha256sum "$archive" | awk '{print $1}')"
test "$actual_sha" = "@BUNDLE_SHA@" || { echo "NInfer release archive SHA-256 mismatch" >&2; exit 1; }
printf '%s  %s\n' "$actual_sha" "$archive" | sha256sum -c -

python3 - "$archive" "$checksum" "$manifest" "$root/ninfer/releases" \
  "@TAG@" "@BUNDLE_SHA@" "@SOURCE_REPOSITORY@" "@SOURCE_COMMIT@" \
  "@MODEL_REVISION@" "@MODEL_SHA@" "@MODEL_SIZE@" "@MODEL_FORMAT@" \
  "@CUDA_FLOOR@" "@GPU_ARCH@" "@BASE_TAG@" "@BASE_DIGEST@" <<'PY'
import hashlib
import json
import os
from pathlib import Path
import shutil
import sys
import tarfile
import tempfile

archive_path, checksum_path, manifest_path, releases_path = map(Path, sys.argv[1:5])
(tag, archive_sha, source_repository, source_commit, artifact_revision,
 artifact_sha, artifact_size, artifact_format, cuda_floor, gpu_arch,
 base_tag, base_digest) = sys.argv[5:]
artifact_size = int(artifact_size)
archive_name = archive_path.name

def sha256_stream(stream):
    digest = hashlib.sha256()
    size = 0
    for block in iter(lambda: stream.read(1024 * 1024), b""):
        digest.update(block)
        size += len(block)
    return digest.hexdigest(), size

with archive_path.open("rb") as stream:
    digest, _ = sha256_stream(stream)
if digest != archive_sha:
    raise SystemExit("NInfer runtime archive does not match Stint's pinned SHA-256")
sidecar = checksum_path.read_text(encoding="ascii").strip().split()
if sidecar != [archive_sha, archive_name]:
    raise SystemExit("NInfer runtime checksum sidecar is invalid")

external_manifest = manifest_path.read_bytes()
with tarfile.open(archive_path, "r:gz") as archive:
    members = archive.getmembers()
    expected_names = ["ninfer", "ninfer-serve", "manifest.json"]
    if len(members) != len(expected_names) or [item.name for item in members] != expected_names:
        raise SystemExit("NInfer runtime archive members are not the exact expected files")
    for member in members:
        if not member.isfile() or member.type != tarfile.REGTYPE:
            raise SystemExit("NInfer runtime archive contains a non-regular file")
        if member.name not in expected_names or member.name.startswith("/") or ".." in Path(member.name).parts:
            raise SystemExit("NInfer runtime archive contains an unsafe member path")
    archived_manifest_stream = archive.extractfile("manifest.json")
    if archived_manifest_stream is None:
        raise SystemExit("NInfer runtime archive manifest is unreadable")
    archived_manifest = archived_manifest_stream.read()
    if archived_manifest != external_manifest:
        raise SystemExit("external NInfer manifest differs from the archived manifest")
    manifest = json.loads(archived_manifest)
    fixed = {
        "bundleFormatVersion": 1,
        "runtime": "ninfer",
        "sourceRepository": source_repository,
        "sourceCommit": source_commit,
        "platform": "linux/amd64",
        "gpuArchitecture": gpu_arch,
        "cudaFloor": cuda_floor,
        "artifact": {
            "revision": artifact_revision,
            "sha256": artifact_sha,
            "sizeBytes": artifact_size,
            "format": artifact_format,
        },
        "baseImage": {"tag": base_tag, "digest": base_digest},
        "entrypoint": "ninfer-serve",
        "buildIdentifier": f"ninfer-{source_commit}-cuda128-sm89-v1",
    }
    if any(manifest.get(key) != value for key, value in fixed.items()):
        raise SystemExit("NInfer runtime manifest does not match the pinned compatibility tuple")
    if set(manifest) != set(fixed) | {"binaries"}:
        raise SystemExit("NInfer runtime manifest contains missing or unexpected fields")
    binaries = manifest.get("binaries")
    if not isinstance(binaries, dict) or set(binaries) != {"ninfer", "ninfer-serve"}:
        raise SystemExit("NInfer runtime manifest does not enumerate both runtime binaries")

    binary_records = {}
    for name in ("ninfer", "ninfer-serve"):
        member = archive.getmember(name)
        if member.mode & 0o777 != 0o755 or member.mode & 0o7000:
            raise SystemExit(f"NInfer runtime binary {name} has unsafe executable permissions")
        stream = archive.extractfile(member)
        if stream is None:
            raise SystemExit(f"NInfer runtime binary {name} is unreadable")
        binary_digest, binary_size = sha256_stream(stream)
        binary_info = binaries[name]
        if binary_info != {"path": name, "sha256": binary_digest, "sizeBytes": binary_size}:
            raise SystemExit(f"NInfer runtime binary {name} differs from its manifest record")
        binary_records[name] = binary_info
    manifest_member = archive.getmember("manifest.json")
    if manifest_member.mode & 0o777 != 0o644:
        raise SystemExit("NInfer runtime manifest has unexpected permissions")

runtime_root = releases_path.parent
if runtime_root.is_symlink():
    raise SystemExit("NInfer runtime root must not be a symlink")
runtime_root.mkdir(parents=True, exist_ok=True)
if releases_path.is_symlink():
    raise SystemExit("NInfer runtime release directory must not be a symlink")
releases_path.mkdir(parents=True, exist_ok=True)
destination = releases_path / tag
if destination.exists():
    if destination.is_symlink() or not destination.is_dir():
        raise SystemExit("existing NInfer release install is not a regular directory")
    installed_manifest = destination / "manifest.json"
    installed_apps = destination / "apps"
    if installed_manifest.is_symlink() or not installed_manifest.is_file() or installed_apps.is_symlink() or not installed_apps.is_dir():
        raise SystemExit("existing NInfer release install has an unsafe manifest or app directory")
    if installed_manifest.read_bytes() != external_manifest:
        raise SystemExit("an existing NInfer release install has a different manifest")
    for name in ("ninfer", "ninfer-serve"):
        installed_path = installed_apps / name
        if installed_path.is_symlink() or not installed_path.is_file():
            raise SystemExit(f"existing NInfer release install is missing a regular {name}")
        with installed_path.open("rb") as stream:
            installed_sha, _ = sha256_stream(stream)
        if installed_sha != binary_records[name]["sha256"]:
            raise SystemExit(f"existing NInfer release install has a corrupt {name}")
else:
    staging = Path(tempfile.mkdtemp(prefix=f".{tag}.", dir=releases_path))
    try:
        apps = staging / "apps"
        apps.mkdir(mode=0o755)
        with tarfile.open(archive_path, "r:gz") as install_archive:
            for name in ("ninfer", "ninfer-serve"):
                source = install_archive.extractfile(name)
                if source is None:
                    raise SystemExit(f"NInfer runtime binary {name} could not be reopened")
                target = apps / name
                with target.open("xb") as stream:
                    shutil.copyfileobj(source, stream, 1024 * 1024)
                target.chmod(0o755)
        (staging / "manifest.json").write_bytes(external_manifest)
        (staging / "manifest.json").chmod(0o644)
        os.rename(staging, destination)
    except BaseException:
        shutil.rmtree(staging, ignore_errors=True)
        raise
PY

date +%s%3N > "$root/runtime-acquired-ms"
rm -f "$archive" "$checksum" "$manifest"
runtime_root="$root/ninfer"
runtime_dir="$runtime_root/releases/@TAG@"
for binary in "$runtime_dir/apps/ninfer" "$runtime_dir/apps/ninfer-serve"; do
  test -x "$binary"
  "$binary" --help >/dev/null
  ldd_output="$(ldd "$binary")"
  if echo "$ldd_output" | grep -q "not found"; then
    echo "$ldd_output" >&2
    exit 1
  fi
done
if [ -e "$runtime_root/bin" ] && [ ! -L "$runtime_root/bin" ]; then
  echo "Refusing to replace non-symlink NInfer runtime path $runtime_root/bin" >&2
  exit 1
fi
rm -f "$runtime_root/bin.next"
ln -s "releases/@TAG@/apps" "$runtime_root/bin.next"
mv -Tf "$runtime_root/bin.next" "$runtime_root/bin"
commit_tmp="$(mktemp "$runtime_root/.stint-commit.XXXXXX")"
printf '%s\n' "@SOURCE_COMMIT@" > "$commit_tmp"
mv -f "$commit_tmp" "$runtime_root/.stint-commit"
deployment_tmp="$(mktemp "$runtime_root/.stint-deployment.XXXXXX")"
printf '%s %s %s\n' "@DEPLOYMENT@" "@TAG@" "@BUNDLE_SHA@" > "$deployment_tmp"
mv -f "$deployment_tmp" "$runtime_root/.stint-deployment"
date +%s%3N > "$root/runtime-verified-ms"
echo "Immutable NInfer release verified and installed; Qwen model transfer continues in parallel."
`
	return strings.NewReplacer(
		"@MODEL_SHA@", ninferModelSHA256,
		"@MODEL_URL@", ninferModelURL,
		"@ARCHIVE@", ninferRuntimeBundleName,
		"@RELEASE_URL@", ninferRuntimeReleaseURL,
		"@BUNDLE_SHA@", ninferRuntimeBundleSHA256,
		"@TAG@", ninferRuntimeReleaseTag,
		"@DEPLOYMENT@", ninferDeploymentReleaseBundle,
		"@SOURCE_REPOSITORY@", ninferSourceRepository,
		"@SOURCE_COMMIT@", ninferSourceCommit,
		"@MODEL_REVISION@", ninferModelRevision,
		"@MODEL_SIZE@", strconv.FormatInt(ninferModelSizeBytes, 10),
		"@MODEL_FORMAT@", fmt.Sprintf("NInfer v%d", ninferArtifactFormat),
		"@CUDA_FLOOR@", ninferCUDAFloor,
		"@GPU_ARCH@", ninferGPUArchitecture,
		"@BASE_TAG@", "vastai/base-image:cuda-12.8.1-cudnn-devel-ubuntu24.04-py310",
		"@BASE_DIGEST@", "sha256:bf6bb047dbc1105c89a5ac41b9a32205a2f2e022cb24d632d055c5b14a86f7ec",
	).Replace(command)
}

func selectedRuntimeReadyCommand(state sessionstate.State) string {
	if runtimeForState(state) == runtimeNInfer {
		deployment := ninferDeploymentForState(state)
		marker := deployment
		if deployment == ninferDeploymentReleaseBundle {
			marker += " " + ninferRuntimeReleaseTag + " " + ninferRuntimeBundleSHA256
		} else {
			marker += " " + ninferSourceCommit
		}
		return fmt.Sprintf(`root=/workspace/stint/ninfer; actual="$(cat "$root/.stint-deployment" 2>/dev/null || true)"; if [ -x "$root/bin/ninfer-serve" ] && [ "$actual" = %s ]; then echo ready; elif [ %s = source-build ] && [ -z "$actual" ] && { [ -x "$root/build/apps/ninfer-serve" ] || [ -x "$root/bin/ninfer-serve" ]; } && pgrep -x ninfer-serve >/dev/null 2>&1; then echo ready; else echo missing; fi`, shellQuote(marker), shellQuote(deployment))
	}
	return "if [ -x /workspace/stint/llama.cpp/build/bin/llama-server ]; then echo ready; else echo missing; fi"
}

func selectedModelProcessName(state sessionstate.State) string {
	if runtimeForState(state) == runtimeNInfer {
		return "ninfer-serve"
	}
	return "llama-server"
}

func remoteModelLaunchCommandForState(state sessionstate.State) string {
	if runtimeForState(state) == runtimeNInfer {
		return ninferModelLaunchCommandWithClients(contextForState(state), clientsForState(state))
	}
	return llamaModelLaunchCommand(contextForState(state))
}

func remoteModelProgressCommandForState(state sessionstate.State) string {
	if runtimeForState(state) == runtimeNInfer {
		return `model=/workspace/stint/models/qwen3_8_27b.ninfer
size_file=/workspace/stint/model-total-bytes
if pgrep -f '[c]url .*qwen3_8_27b.ninfer' >/dev/null 2>&1; then
  bytes="$(stat -c %s "$model" 2>/dev/null || echo 0)"
  total="$(cat "$size_file" 2>/dev/null || true)"
  case "$total" in
    ''|*[!0-9]*) total=0 ;;
  esac
  if [ "$total" -gt 0 ]; then
    pct=$((bytes * 100 / total))
    [ "$pct" -gt 100 ] && pct=100
    echo "model download ${pct}% ($((bytes / 1048576)) MiB / $((total / 1048576)) MiB)"
  else
    echo "model download $((bytes / 1048576)) MiB transferred"
  fi
elif pgrep -x sha256sum >/dev/null 2>&1; then
  echo "model download complete; verifying checksum"
elif pgrep -x ninfer-serve >/dev/null 2>&1; then
  tail -n 1 /workspace/stint/llama.log 2>/dev/null || echo "loading model on GPU"
else
  tail -n 1 /workspace/stint/model-download.log 2>/dev/null || tail -n 1 /workspace/stint/llama.log 2>/dev/null || true
fi`
	}

	return fmt.Sprintf(`model=/workspace/stint/models/%s
expected=%d
if pgrep -f '[h]f download' >/dev/null 2>&1; then
  bytes="$(grep -h 'observed bytes sent so far' /root/.cache/huggingface/xet/logs/*.log 2>/dev/null | tail -n 1 | sed -n 's/.*observed bytes sent so far = \([0-9][0-9]*\).*/\1/p')"
  if [ -z "$bytes" ]; then
    bytes="$(find /workspace/stint/models/.cache/huggingface/download -name '*.incomplete' -printf '%%s\n' 2>/dev/null | sort -nr | head -n 1)"
  fi
  bytes="${bytes:-0}"
  pct=$((bytes * 100 / expected))
  [ "$pct" -gt 100 ] && pct=100
  echo "model download ${pct}%% ($((bytes / 1048576)) MiB / $((expected / 1048576)) MiB transferred)"
elif pgrep -x sha256sum >/dev/null 2>&1; then
  echo "model download complete; verifying checksum"
elif pgrep -x llama-server >/dev/null 2>&1; then
  tail -n 1 /workspace/stint/llama.log 2>/dev/null || echo "loading model on GPU"
elif [ -f "$model" ]; then
  echo "model artifact ready; starting llama-server"
else
  tail -n 1 /workspace/stint/llama.log 2>/dev/null || true
fi`, llamaModelFileName, llamaModelSizeBytes)
}

func llamaModelLaunchCommand(contextTokens int) string {
	return fmt.Sprintf(`set -eu
mkdir -p /workspace/stint/models
pid_file=/workspace/stint/llama.pid
log_file=/workspace/stint/llama.log

if command -v pgrep >/dev/null 2>&1; then
  for old_pid in $(pgrep -x llama-server 2>/dev/null || true); do
    kill "$old_pid" 2>/dev/null || true
  done
fi

if [ -r "$pid_file" ]; then
  old_pid="$(cat "$pid_file" 2>/dev/null || true)"
  if [ -n "$old_pid" ] && kill -0 "$old_pid" 2>/dev/null; then
    kill "$old_pid" 2>/dev/null || true
  fi
fi
rm -f "$pid_file"
: > "$log_file"

nohup bash -c '
set -eu
model_dir=/workspace/stint/models
model="$model_dir/%s"
model_sha="%s"
model_url="%s"
mkdir -p "$model_dir"

if [ ! -f "$model" ] || ! echo "$model_sha  $model" | sha256sum -c - >/dev/null 2>&1; then
  echo "Model download     Qwen3.8-27B Q4_K_M"
  echo "Model destination  $model"
  export HF_HUB_DOWNLOAD_TIMEOUT=60
  export HF_HUB_DISABLE_UPDATE_CHECK=1
  mem_kb="$(grep -m1 ^MemTotal: /proc/meminfo | tr -cd 0-9)"
  if [ "${mem_kb:-0}" -ge 67108864 ]; then
    export HF_XET_HIGH_PERFORMANCE=1
    echo "Download transport Hugging Face Xet high-performance"
  else
    echo "Download transport Hugging Face Xet/default"
  fi

  if ! command -v hf >/dev/null 2>&1; then
    echo "Installing Hugging Face CLI..."
    curl -LsSf https://hf.co/cli/install.sh | bash -s -- --exclude-skill || true
    export PATH="$HOME/.local/bin:$PATH"
  fi

  downloaded=0
  if command -v hf >/dev/null 2>&1; then
    if hf download ggml-org/Qwen3.8-27B-GGUF %s --local-dir "$model_dir"; then
      downloaded=1
    fi
  fi
  if [ "$downloaded" -ne 1 ]; then
    echo "Using resumable HTTPS model download fallback."
    curl -L -C - --fail --retry 10 --retry-all-errors --retry-delay 2 --connect-timeout 20 --output "$model" "$model_url"
  fi
fi

echo "$model_sha  $model" | sha256sum -c -
echo "Model cache        verified"
exec /workspace/stint/llama.cpp/build/bin/llama-server \
  -m "$model" \
  --no-mmproj \
  --alias %s \
  --host 127.0.0.1 \
  --port %d \
  -ngl all \
  -c %d \
  -ctk q8_0 \
  -ctv q8_0 \
  --flash-attn on \
  --metrics \
  --slots
' > "$log_file" 2>&1 < /dev/null &
new_pid=$!
printf '%%s\n' "$new_pid" > "$pid_file"
sleep 1
if ! kill -0 "$new_pid" 2>/dev/null; then
  tail -n 20 "$log_file" >&2 || true
  exit 1
fi
`, llamaModelFileName, llamaModelSHA256, llamaModelDownloadURL, llamaModelFileName, interactiveModelAlias, clineRemotePort, contextTokens)
}

// ninferModelLaunchCommand preserves the historical single-client helper used
// by tests and callers that do not have session state.
func ninferModelLaunchCommand(contextTokens int) string {
	return ninferModelLaunchCommandWithClients(contextTokens, defaultNInferClients)
}

// ninferModelArtifactCommand prepares and verifies the immutable model artifact.
// The path arguments are shell-quoted so the same command can be executed in a
// temporary fixture without reaching the network or a GPU.
func ninferModelArtifactCommand(modelPath, sizePath, modelURL, modelSHA string) string {
	command := `set -eu
model=@MODEL@
model_size_file=@SIZE_FILE@
model_url=@MODEL_URL@
model_sha=@MODEL_SHA@
model_pid_file=@MARKER_DIR@/model-download.pid
model_started_file=@MARKER_DIR@/model-acquisition-started-ms
model_acquired_file=@MARKER_DIR@/model-acquired-ms
downloaded=0
mkdir -p "$(dirname "$model")"

prefetch_pid="$(cat "$model_pid_file" 2>/dev/null || true)"
if [ -n "$prefetch_pid" ] && kill -0 "$prefetch_pid" 2>/dev/null; then
  echo "Waiting for the verified parallel Qwen3.8-27B transfer to finish..."
  while kill -0 "$prefetch_pid" 2>/dev/null; do sleep 5; done
fi
rm -f "$model_pid_file"

discover_model_size() {
  size="$(curl -fsSLI --retry 3 --retry-delay 1 -o /dev/null -w "%header{content-length}" "$model_url" 2>/dev/null || true)"
  case "$size" in
    ""|*[!0-9]*) size=0 ;;
  esac
  printf "%s\n" "$size"
}

expected="$(cat "$model_size_file" 2>/dev/null || true)"
case "$expected" in
  ""|*[!0-9]*) expected=0 ;;
esac
if [ "$expected" -le 0 ]; then
  expected="$(discover_model_size)"
  if [ "$expected" -gt 0 ]; then
    printf "%s\n" "$expected" > "$model_size_file"
  fi
fi

if [ -f "$model" ] && ! echo "$model_sha  $model" | sha256sum -c - >/dev/null 2>&1; then
  bytes="$(stat -c %s "$model" 2>/dev/null || echo 0)"
  if [ "$expected" -gt 0 ] && [ "$bytes" -ge "$expected" ]; then
    echo "Discarding invalid completed/oversized NInfer model artifact before resumable download."
    rm -f "$model"
  fi
fi

if [ ! -f "$model" ] || ! echo "$model_sha  $model" | sha256sum -c - >/dev/null 2>&1; then
  echo "Downloading Qwen3.8-27B NInfer artifact..."
  if [ ! -s "$model_started_file" ]; then date +%s%3N > "$model_started_file"; fi
  downloaded=1
  curl -L -C - --fail --retry 10 --retry-all-errors --retry-delay 2 --connect-timeout 20 --output "$model" "$model_url"
  if ! echo "$model_sha  $model" | sha256sum -c - >/dev/null 2>&1; then
    echo "Resumed NInfer artifact failed SHA-256; discarding it and retrying from byte zero."
    rm -f "$model"
    curl -L -C - --fail --retry 10 --retry-all-errors --retry-delay 2 --connect-timeout 20 --output "$model" "$model_url"
  fi
fi
echo "$model_sha  $model" | sha256sum -c -
if [ "$downloaded" -eq 1 ] || [ ! -s "$model_acquired_file" ]; then date +%s%3N > "$model_acquired_file"; fi
`
	return strings.NewReplacer(
		"@MODEL@", shellQuote(modelPath),
		"@SIZE_FILE@", shellQuote(sizePath),
		"@MODEL_URL@", shellQuote(modelURL),
		"@MODEL_SHA@", shellQuote(modelSHA),
		"@MARKER_DIR@", shellQuote(filepath.Dir(sizePath)),
	).Replace(command)
}

func ninferModelLaunchCommandWithClients(contextTokens, clients int) string {
	config := ninferConfigForContext(contextTokens)
	artifactCommand := ninferModelArtifactCommand(
		"/workspace/stint/models/qwen3_8_27b.ninfer",
		"/workspace/stint/model-total-bytes",
		ninferModelURL,
		ninferModelSHA256,
	)
	modelCommand := fmt.Sprintf(`%s
exec /workspace/stint/ninfer/bin/ninfer-serve "$model" \
  --host 127.0.0.1 \
  --port %d \
  --model-id %s \
  --max-context %d \
  --kv-capacity %d \
  --default-max-tokens %d \
  --max-concurrency %d \
  --max-pending-requests 16 \
  --pending-timeout-ms 600000 \
  --prefill-chunk 1024 \
  --kv-dtype %s \
  --spec mtp \
  --draft-tokens 3 \
  --lm-head-draft \
  --preserve-thinking`, artifactCommand, clineRemotePort, interactiveModelAlias, contextTokens, contextTokens, contextTokens, clients, config.KVDType)
	return fmt.Sprintf(`set -eu
mkdir -p /workspace/stint/models
pid_file=/workspace/stint/llama.pid
log_file=/workspace/stint/llama.log
bin=/workspace/stint/ninfer/bin/ninfer-serve
model=/workspace/stint/models/qwen3_8_27b.ninfer

if command -v pgrep >/dev/null 2>&1; then
  for process_name in ninfer-serve llama-server; do
    for old_pid in $(pgrep -x "$process_name" 2>/dev/null || true); do
      kill "$old_pid" 2>/dev/null || true
    done
  done
fi
if [ -r "$pid_file" ]; then
  old_pid="$(cat "$pid_file" 2>/dev/null || true)"
  if [ -n "$old_pid" ] && kill -0 "$old_pid" 2>/dev/null; then
    kill "$old_pid" 2>/dev/null || true
  fi
fi
rm -f "$pid_file"
: > "$log_file"

nohup bash -c %s > "$log_file" 2>&1 < /dev/null &
new_pid=$!
printf '%%s\n' "$new_pid" > "$pid_file"
sleep 1
if ! kill -0 "$new_pid" 2>/dev/null; then
  tail -n 20 "$log_file" >&2 || true
  exit 1
fi
`, shellQuote(modelCommand))
}
