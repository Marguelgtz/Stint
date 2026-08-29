package main

import (
	"strings"
	"testing"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestSelectInteractiveRuntime(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		gpu       string
		want      string
		wantErr   bool
	}{
		{name: "auto 4090", requested: runtimeAuto, gpu: "RTX_4090", want: runtimeNInfer},
		{name: "auto non 4090", requested: runtimeAuto, gpu: "RTX_3090", want: runtimeLlamaCpp},
		{name: "explicit ninfer 4090", requested: runtimeNInfer, gpu: "NVIDIA GeForce RTX 4090", want: runtimeNInfer},
		{name: "explicit ninfer wrong gpu", requested: runtimeNInfer, gpu: "RTX_3090", wantErr: true},
		{name: "explicit llama", requested: "llama", gpu: "RTX_4090", want: runtimeLlamaCpp},
		{name: "unknown", requested: "vllm", gpu: "RTX_4090", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectInteractiveRuntime(tt.requested, tt.gpu)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("selectInteractiveRuntime(%q, %q) unexpectedly succeeded with %q", tt.requested, tt.gpu, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("selectInteractiveRuntime(%q, %q) = %q, want %q", tt.requested, tt.gpu, got, tt.want)
			}
		})
	}
}

func TestRuntimeContextsAndLegacyResume(t *testing.T) {
	if got := contextForRuntime(runtimeNInfer); got != 126976 {
		t.Fatalf("NInfer context = %d, want 126976", got)
	}
	if got := contextForRuntime(runtimeLlamaCpp); got != interactiveContext {
		t.Fatalf("llama.cpp fallback context = %d, want proven legacy context %d", got, interactiveContext)
	}
	legacy := sessionstate.State{}
	if got := runtimeForState(legacy); got != runtimeLlamaCpp {
		t.Fatalf("legacy runtime = %q, want %q", got, runtimeLlamaCpp)
	}
	if got := contextForState(legacy); got != interactiveContext {
		t.Fatalf("legacy context = %d, want %d", got, interactiveContext)
	}
}

func TestNInferBootstrapIsPinned(t *testing.T) {
	command := ninferBootstrapCommand()
	for _, required := range []string{
		ninferSourceRepository,
		ninferSourceCommit,
		"CUDA toolkit 12.8 or newer",
		"gcc-13",
		"g++-13",
		"CMake 3.28",
		"-DCMAKE_CUDA_ARCHITECTURES=89",
		"--target ninfer ninfer-serve",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("NInfer bootstrap missing %q", required)
		}
	}
}

func TestNInferLaunchUsesQualified4090Profile(t *testing.T) {
	command := ninferModelLaunchCommand(126976)
	for _, required := range []string{
		ninferModelURL,
		ninferModelSHA256,
		"/workspace/stint/llama.pid",
		"/workspace/stint/llama.log",
		"--model-id qwen3.8-27b",
		"--max-context 126976",
		"--kv-capacity 126976",
		"--kv-dtype int8",
		"--spec mtp",
		"--draft-tokens 3",
		"--lm-head-draft",
		"--pending-timeout-ms 600000",
		"--preserve-thinking",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("NInfer launch missing %q", required)
		}
	}
}

func TestRuntimeAwareLaunchKeepsLlamaFallbackConservative(t *testing.T) {
	ninferState := sessionstate.State{Runtime: runtimeNInfer, ContextTokens: 126976}
	if command := remoteModelLaunchCommandForState(ninferState); !strings.Contains(command, "ninfer-serve") {
		t.Fatal("NInfer state did not select ninfer-serve")
	}

	llamaState := sessionstate.State{Runtime: runtimeLlamaCpp, ContextTokens: interactiveContext}
	command := remoteModelLaunchCommandForState(llamaState)
	if !strings.Contains(command, "llama-server") {
		t.Fatal("llama.cpp state did not select llama-server")
	}
	if !strings.Contains(command, "-c 16384") {
		t.Fatalf("llama.cpp fallback command did not preserve 16384-token context: %s", command)
	}
}
