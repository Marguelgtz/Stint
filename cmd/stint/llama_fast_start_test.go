package main

import (
	"strings"
	"testing"

	"github.com/Marguelgtz/Stint/internal/provider/vast"
)

func TestLlamaUsesSSHCapableVastImage(t *testing.T) {
	if got := vastImageForRuntime(runtimeLlamaCpp); got != vast.NInferCUDA128Image {
		t.Fatalf("llama.cpp image = %q, want Vast SSH-capable CUDA image %q", got, vast.NInferCUDA128Image)
	}
	if strings.Contains(vastImageForRuntime(runtimeLlamaCpp), "ggml-org/llama.cpp:server-cuda") {
		t.Fatal("llama.cpp must not use the minimal upstream server image as the Vast instance image")
	}
}

func TestLlamaOnStartDoesNotReplaceVastContainerLifecycle(t *testing.T) {
	command := vastOnStartForRuntime(runtimeLlamaCpp)
	for _, forbidden := range []string{"llama-server", "hf download", "apt-get", "git clone", "cmake"} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("llama.cpp onstart unexpectedly performs runtime bootstrap %q: %s", forbidden, command)
		}
	}
}

func TestLlamaLaunchDownloadsModelAfterSSH(t *testing.T) {
	command := llamaModelLaunchCommand(60000)
	for _, required := range []string{
		"/workspace/stint/models",
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

func TestNInferOnStartDoesNotPerformRuntimeBootstrap(t *testing.T) {
	command := vastOnStartForRuntime(runtimeNInfer)
	for _, forbidden := range []string{"ninfer-serve", "qwen3_8_27b.ninfer", "apt-get", "git clone", "cmake"} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("NInfer onstart unexpectedly performs runtime bootstrap %q: %s", forbidden, command)
		}
	}
}
