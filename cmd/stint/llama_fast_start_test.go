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

func TestLlamaDoesNotReplaceVastContainerLifecycle(t *testing.T) {
	if got := vastOnStartForRuntime(runtimeLlamaCpp); got != "" {
		t.Fatalf("llama.cpp onstart = %q, want empty", got)
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

func TestNInferDoesNotReceiveLlamaOnStart(t *testing.T) {
	if got := vastOnStartForRuntime(runtimeNInfer); got != "" {
		t.Fatalf("NInfer onstart = %q, want empty", got)
	}
}
