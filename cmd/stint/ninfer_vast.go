package main

import (
	"github.com/Marguelgtz/Stint/internal/core"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
)

const (
	// Keep llama.cpp on Vast's SSH-capable CUDA base. The upstream llama.cpp
	// server-cuda image is intentionally minimal and does not provide the SSH
	// lifecycle Stint relies on for start/resume.
	llamaVastImage        = vast.NInferCUDA128Image
	llamaVastMinCUDA      = 12.8
	llamaModelFileName    = "Qwen3.8-27B-Q4_K_M.gguf"
	llamaModelSHA256      = "31629f53165ab6a7dad8c9847dcfd1fdf55829dac1e6e748f4a68581b0033d34"
	llamaModelDownloadURL = "https://huggingface.co/ggml-org/Qwen3.8-27B-GGUF/resolve/main/Qwen3.8-27B-Q4_K_M.gguf"

	// Some Vast hosts have been observed creating /root/.ssh/authorized_keys
	// with ownership or modes that OpenSSH StrictModes rejects. Keep a tiny
	// background repair loop alive through the provider startup window so late
	// key injection cannot leave an otherwise healthy paid instance unreachable.
	vastSSHPermissionsOnStart = `install -d -m 700 -o root -g root /root/.ssh; chmod 700 /root /root/.ssh 2>/dev/null || true; nohup sh -c 'i=0; while [ "$i" -lt 450 ]; do if [ -e /root/.ssh/authorized_keys ]; then chown root:root /root /root/.ssh /root/.ssh/authorized_keys 2>/dev/null || true; chmod 700 /root /root/.ssh 2>/dev/null || true; chmod 600 /root/.ssh/authorized_keys 2>/dev/null || true; fi; i=$((i+1)); sleep 2; done' >/tmp/stint-ssh-permissions.log 2>&1 &`
)

func prepareVastSearchForRuntime(profile core.Profile, options vast.SearchOptions, runtimeRequest string) (core.Profile, vast.SearchOptions) {
	// Both current runtime paths require CUDA 12.8. Qualify hosts before rental
	// so Stint does not discover an image/toolchain incompatibility only after
	// paid compute has started.
	options.MinCUDAMaxGood = llamaVastMinCUDA
	if runtimeRequest == runtimeNInfer {
		profile.GPU.PreferredModels = []string{"RTX 4090"}
		options.MinCUDAMaxGood = vast.NInferMinCUDAVersion
	}
	return profile, options
}

func vastImageForRuntime(runtime string) string {
	if runtime == runtimeNInfer {
		return vast.NInferCUDA128Image
	}
	return llamaVastImage
}

func vastOnStartForRuntime(runtime string) string {
	// Runtime preparation still happens only after Stint has proved SSH
	// responsiveness. This hook is intentionally limited to repairing OpenSSH's
	// required ownership/modes while Vast may still be injecting authorized_keys.
	return vastSSHPermissionsOnStart
}
