package main

import (
	"github.com/Marguelgtz/Stint/internal/core"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
)

const (
	// Keep llama.cpp on Vast's SSH-capable CUDA base. The upstream llama.cpp
	// server-cuda image is intentionally minimal and does not provide the SSH
	// lifecycle Stint relies on for start/resume.
	llamaVastImage       = vast.NInferCUDA128Image
	llamaVastMinCUDA     = 12.8
	llamaModelFileName   = "Qwen3.8-27B-Q4_K_M.gguf"
	llamaModelSHA256     = "31629f53165ab6a7dad8c9847dcfd1fdf55829dac1e6e748f4a68581b0033d34"
	llamaModelDownloadURL = "https://huggingface.co/ggml-org/Qwen3.8-27B-GGUF/resolve/main/Qwen3.8-27B-Q4_K_M.gguf"
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
	// Runtime preparation happens after Stint has proved SSH responsiveness.
	// Do not replace Vast's SSH-capable container lifecycle with a minimal
	// inference-only entrypoint.
	return ""
}
