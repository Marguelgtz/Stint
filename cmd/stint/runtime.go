package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const (
	runtimeAuto     = "auto"
	runtimeNInfer   = "ninfer"
	runtimeLlamaCpp = "llama.cpp"

	interactiveRuntimeContext = 126976

	ninferSourceRepository = "https://github.com/sergiuszm/ninfer-4090.git"
	ninferSourceCommit     = "981b685ea2124fdaed023123d2e63fd29d529ab8"
	ninferModelURL         = "https://huggingface.co/neroued/Qwen3.8-27B-NInfer/resolve/main/qwen3_8_27b.ninfer"
	ninferModelSHA256      = "eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e"
)

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

func bootstrapSelectedRuntime(ctx context.Context, paths config.Paths, state sessionstate.State) (string, error) {
	switch runtimeForState(state) {
	case runtimeNInfer:
		if err := bootstrapNInfer(ctx, paths, state); err == nil {
			return runtimeNInfer, nil
		} else if state.RuntimeRequest != runtimeAuto {
			return "", err
		} else {
			fmt.Printf("NInfer bootstrap unavailable on this host (%v). Falling back to llama.cpp.\n", err)
			if fallbackErr := bootstrapRemoteRuntime(ctx, paths, state); fallbackErr != nil {
				return "", fmt.Errorf("ninfer bootstrap failed (%v); llama.cpp fallback also failed: %w", err, fallbackErr)
			}
			return runtimeLlamaCpp, nil
		}
	default:
		if err := bootstrapRemoteRuntime(ctx, paths, state); err != nil {
			return "", err
		}
		return runtimeLlamaCpp, nil
	}
}

func bootstrapNInfer(ctx context.Context, paths config.Paths, state sessionstate.State) error {
	fmt.Printf("Preparing NInfer for RTX 4090 at pinned commit %.12s...\n", ninferSourceCommit)
	fmt.Println("Stint overlaps the Qwen model transfer with the native NInfer build so cold-start time is bounded by the slower stage instead of their sum.")
	if err := runSSHStreaming(ctx, paths, state, ninferBootstrapCommand()); err != nil {
		return fmt.Errorf("bootstrap remote ninfer runtime: %w", err)
	}
	fmt.Println("NInfer runtime ready.")
	return nil
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
model_sha="%s"
model_url="%s"
started_prefetch_pid=""
mkdir -p "$root" "$model_dir"

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
model_sha="%s"
model_url="%s"
curl -L -C - --fail --retry 10 --retry-all-errors --retry-delay 2 --connect-timeout 20 --output "$model" "$model_url"
echo "$model_sha  $model" | sha256sum -c -
rm -f "$model_pid"
' > "$model_log" 2>&1 < /dev/null &
  started_prefetch_pid=$!
  printf '%%s\n' "$started_prefetch_pid" > "$model_pid"
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
  "$bin" --help >/dev/null
fi

prefetch_pid="$(cat "$model_pid" 2>/dev/null || true)"
if [ -n "$prefetch_pid" ] && kill -0 "$prefetch_pid" 2>/dev/null; then
  echo "NInfer build is ready; waiting for the parallel Qwen model transfer..."
  last_pct=-1
  while kill -0 "$prefetch_pid" 2>/dev/null; do
    bytes="$(stat -c %%s "$model" 2>/dev/null || echo 0)"
    pct=$((bytes * 100 / 18210531328))
    if [ "$pct" -ne "$last_pct" ]; then
      mib=$((bytes / 1048576))
      echo "  model: ${pct}%% (${mib} MiB / 17367 MiB)"
      last_pct="$pct"
    fi
    sleep 5
  done
fi
rm -f "$model_pid"

if [ -f "$model" ] && echo "$model_sha  $model" | sha256sum -c - >/dev/null 2>&1; then
  echo "Qwen3.8-27B model artifact ready."
elif [ -s "$model_log" ]; then
  echo "Parallel model prefetch did not finish cleanly; model launch will resume the partial transfer."
  tail -n 5 "$model_log" || true
else
  echo "Model prefetch was unavailable; model launch will download the artifact."
fi
`, ninferModelSHA256, ninferModelURL, ninferModelSHA256, ninferModelURL, ninferSourceCommit, ninferSourceCommit, ninferSourceRepository, ninferSourceCommit, ninferSourceCommit)
}

func selectedRuntimeReadyCommand(state sessionstate.State) string {
	if runtimeForState(state) == runtimeNInfer {
		return "if [ -x /workspace/stint/ninfer/build/apps/ninfer-serve ]; then echo ready; else echo missing; fi"
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
		return ninferModelLaunchCommand(contextForState(state))
	}
	return llamaModelLaunchCommand(contextForState(state))
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
  --flash-attn on
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

func ninferModelLaunchCommand(contextTokens int) string {
	config := ninferConfigForContext(contextTokens)
	return fmt.Sprintf(`set -eu
mkdir -p /workspace/stint/models
pid_file=/workspace/stint/llama.pid
log_file=/workspace/stint/llama.log
bin=/workspace/stint/ninfer/build/apps/ninfer-serve
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

nohup bash -c '
set -eu
model=/workspace/stint/models/qwen3_8_27b.ninfer
if [ ! -f "$model" ] || ! echo "%s  $model" | sha256sum -c - >/dev/null 2>&1; then
  echo "Downloading Qwen3.8-27B NInfer artifact..."
  curl -L -C - --fail --retry 3 --retry-delay 2 --output "$model" %s
fi
echo "%s  $model" | sha256sum -c -
exec /workspace/stint/ninfer/build/apps/ninfer-serve "$model" \
  --host 127.0.0.1 \
  --port %d \
  --model-id %s \
  --max-context %d \
  --kv-capacity %d \
  --max-concurrency 1 \
  --max-pending-requests 16 \
  --pending-timeout-ms 600000 \
  --prefill-chunk 1024 \
  --kv-dtype %s \
  --spec mtp \
  --draft-tokens 3 \
  --lm-head-draft \
  --preserve-thinking
' > "$log_file" 2>&1 < /dev/null &
new_pid=$!
printf '%%s\n' "$new_pid" > "$pid_file"
sleep 1
if ! kill -0 "$new_pid" 2>/dev/null; then
  tail -n 20 "$log_file" >&2 || true
  exit 1
fi
`, ninferModelSHA256, ninferModelURL, ninferModelSHA256, clineRemotePort, interactiveModelAlias, contextTokens, contextTokens, config.KVDType)
}
