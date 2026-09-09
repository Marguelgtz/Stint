package main

import (
	"github.com/Marguelgtz/Stint/internal/core"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
)

const (
	// Vast's official llama.cpp image is derived from the Vast base image, so it
	// keeps the SSH/container lifecycle Stint relies on while shipping a CUDA
	// llama-server prebuilt at /opt/llama.cpp/llama-server.
	llamaVastImage         = "vastai/llama-cpp:b10472-mix-4b653db-cuda-12.9"
	llamaVastMinCUDA       = 12.9
	llamaPrebuiltBinary    = "/opt/llama.cpp/llama-server"
	llamaRuntimeBridgePath = "/workspace/stint/llama.cpp/build/bin/llama-server"
	llamaModelFileName     = "Qwen3.8-27B-Q4_K_M.gguf"
	llamaModelSizeBytes    = int64(18962876416)
	llamaModelSHA256       = "31629f53165ab6a7dad8c9847dcfd1fdf55829dac1e6e748f4a68581b0033d34"
	llamaModelDownloadURL  = "https://huggingface.co/ggml-org/Qwen3.8-27B-GGUF/resolve/main/Qwen3.8-27B-Q4_K_M.gguf"

	// Stint's NInfer image is built from the same Vast CUDA 12.8.1 base used by
	// the previous cold path, but NInfer itself is compiled into the image at the
	// exact pinned source commit. This preserves Vast's SSH lifecycle while
	// removing GCC/CMake/CUDA compilation from paid startup.
	ninferVastImage         = "ghcr.io/marguelgtz/stint-ninfer:981b685e-cuda12.8"
	ninferPrebuiltBinary    = "/opt/ninfer/bin/ninfer-serve"
	ninferRuntimeBridgePath = "/workspace/stint/ninfer/build/apps/ninfer-serve"

	// Some Vast hosts have been observed creating /root/.ssh/authorized_keys
	// with ownership or modes that OpenSSH StrictModes rejects. Keep a tiny
	// background repair loop alive through the provider startup window so late
	// key injection cannot leave an otherwise healthy paid instance unreachable.
	vastSSHPermissionsOnStart = `install -d -m 700 -o root -g root /root/.ssh; chmod 700 /root /root/.ssh 2>/dev/null || true; nohup sh -c 'i=0; while [ "$i" -lt 450 ]; do if [ -e /root/.ssh/authorized_keys ]; then chown root:root /root /root/.ssh /root/.ssh/authorized_keys 2>/dev/null || true; chmod 700 /root /root/.ssh 2>/dev/null || true; chmod 600 /root/.ssh/authorized_keys 2>/dev/null || true; fi; i=$((i+1)); sleep 2; done' >/tmp/stint-ssh-permissions.log 2>&1 &`

	// Keep the rest of the llama lifecycle unchanged while replacing the old
	// per-instance source build with Vast's prebuilt binary. The wrapper is
	// always executable, so a missing/broken /opt binary fails validation rather
	// than silently falling back to a source compile and hiding the experiment.
	vastLlamaPrebuiltBridgeOnStart = `install -d -m 755 /workspace/stint/llama.cpp/build/bin; printf '%s\n' '#!/bin/sh' 'exec /opt/llama.cpp/llama-server "$@"' > /workspace/stint/llama.cpp/build/bin/llama-server; chmod 755 /workspace/stint/llama.cpp/build/bin/llama-server`

	// NInfer's existing bootstrap fast path recognizes an executable server plus
	// a matching .stint-commit marker. Bridge the image binary into those exact
	// paths so bootstrap validates the prebuilt server and proceeds directly to
	// the model transfer instead of cloning/building NInfer on paid compute.
	vastNInferPrebuiltBridgeOnStart = `install -d -m 755 /workspace/stint/ninfer/build/apps; printf '%s\n' '#!/bin/sh' 'exec /opt/ninfer/bin/ninfer-serve "$@"' > /workspace/stint/ninfer/build/apps/ninfer-serve; chmod 755 /workspace/stint/ninfer/build/apps/ninfer-serve; printf '%s\n' 981b685ea2124fdaed023123d2e63fd29d529ab8 > /workspace/stint/ninfer/.stint-commit`
)

func prepareVastSearchForRuntime(profile core.Profile, options vast.SearchOptions, runtimeRequest string) (core.Profile, vast.SearchOptions) {
	// The official llama.cpp image is CUDA 12.9. The pinned NInfer image remains
	// on its qualified CUDA 12.8.1 base. Reject incompatible hosts before rental.
	options.MinCUDAMaxGood = llamaVastMinCUDA
	if runtimeRequest == runtimeNInfer {
		profile.GPU.PreferredModels = []string{"RTX 4090"}
		options.MinCUDAMaxGood = vast.NInferMinCUDAVersion
	}
	return profile, options
}

func vastImageForRuntime(runtime string) string {
	if runtime == runtimeNInfer {
		return ninferVastImage
	}
	return llamaVastImage
}

func vastOnStartForRuntime(runtime string) string {
	// Runtime/model preparation still happens only after Stint has proved SSH
	// responsiveness. These hooks install compatibility bridges only; they do
	// not launch a server or download model artifacts.
	switch runtime {
	case runtimeLlamaCpp:
		return vastSSHPermissionsOnStart + " " + vastLlamaPrebuiltBridgeOnStart
	case runtimeNInfer:
		return vastSSHPermissionsOnStart + " " + vastNInferPrebuiltBridgeOnStart
	default:
		return vastSSHPermissionsOnStart
	}
}
