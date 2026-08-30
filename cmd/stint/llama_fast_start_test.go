package main

import (
	"strings"
	"testing"

	"github.com/Marguelgtz/Stint/internal/core"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
)

func TestLlamaUsesOfficialVastPrebuiltImage(t *testing.T) {
	got := vastImageForRuntime(runtimeLlamaCpp)
	if got != llamaVastImage {
		t.Fatalf("llama.cpp image = %q, want %q", got, llamaVastImage)
	}
	if !strings.HasPrefix(got, "vastai/llama-cpp:") {
		t.Fatalf("llama.cpp image = %q, want official Vast llama.cpp image", got)
	}
	for _, forbidden := range []string{
		"ggml-org/llama.cpp:server-cuda",
		vast.NInferCUDA128Image,
		":latest",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("llama.cpp image unexpectedly contains %q: %s", forbidden, got)
		}
	}
}

func TestLlamaRequiresCUDA129ForPinnedImage(t *testing.T) {
	_, options := prepareVastSearchForRuntime(core.Profile{}, vast.SearchOptions{}, runtimeLlamaCpp)
	if options.MinCUDAMaxGood != 12.9 {
		t.Fatalf("llama.cpp CUDA minimum = %.1f, want 12.9", options.MinCUDAMaxGood)
	}

	_, ninferOptions := prepareVastSearchForRuntime(core.Profile{}, vast.SearchOptions{}, runtimeNInfer)
	if ninferOptions.MinCUDAMaxGood != vast.NInferMinCUDAVersion {
		t.Fatalf("NInfer CUDA minimum = %.1f, want %.1f", ninferOptions.MinCUDAMaxGood, vast.NInferMinCUDAVersion)
	}
}

func TestLlamaOnStartBridgesPrebuiltBinaryWithoutLaunchingRuntime(t *testing.T) {
	command := vastOnStartForRuntime(runtimeLlamaCpp)
	for _, required := range []string{
		llamaPrebuiltBinary,
		llamaRuntimeBridgePath,
		"chmod 600 /root/.ssh/authorized_keys",
		"#!/bin/sh",
		`exec /opt/llama.cpp/llama-server "$@"`,
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("llama.cpp onstart missing %q: %s", required, command)
		}
	}
	for _, forbidden := range []string{"hf download", "apt-get", "git clone", "cmake", `-m "$model"`, "--host", "--port"} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("llama.cpp onstart unexpectedly performs runtime/model bootstrap %q: %s", forbidden, command)
		}
	}
}

func TestLlamaLaunchDownloadsModelAfterSSH(t *testing.T) {
	command := llamaModelLaunchCommand(60000)
	for _, required := range []string{
		"/workspace/stint/models",
		llamaRuntimeBridgePath,
		llamaModelFileName,
		llamaModelSHA256,
		"hf.co/cli/install.sh",
		"HF_XET_HIGH_PERFORMANCE",
		"hf download ggml-org/Qwen3.8-27B-GGUF",
		"curl -L -C -",
		"-m \"$model\"",
		"-c 60000",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("llama.cpp detached launch missing %q", required)
		}
	}
}

func TestNInferOnStartDoesNotReceiveLlamaPrebuiltBridge(t *testing.T) {
	command := vastOnStartForRuntime(runtimeNInfer)
	for _, forbidden := range []string{
		llamaPrebuiltBinary,
		llamaRuntimeBridgePath,
		"ninfer-serve",
		"qwen3_8_27b.ninfer",
		"apt-get",
		"git clone",
		"cmake",
	} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("NInfer onstart unexpectedly contains %q: %s", forbidden, command)
		}
	}
}
