package main

import (
	"os/exec"
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

func TestNInferModelArtifactIsRevisionPinned(t *testing.T) {
	if ninferModelRevision == "" {
		t.Fatal("NInfer model revision must be pinned")
	}
	if strings.Contains(ninferModelURL, "/resolve/main/") {
		t.Fatalf("NInfer model URL is mutable: %s", ninferModelURL)
	}
	if !strings.Contains(ninferModelURL, "/resolve/"+ninferModelRevision+"/") {
		t.Fatalf("NInfer model URL %q does not contain pinned revision %q", ninferModelURL, ninferModelRevision)
	}
}

func TestNInferProductionTupleUsesValidated4090V2Profile(t *testing.T) {
	if ninferSourceRepository != "https://github.com/sergiuszm/ninfer-4090.git" {
		t.Fatalf("NInfer source repository = %q", ninferSourceRepository)
	}
	if ninferSourceCommit != "81b68a20a9a0d9ab47d7e5838887c6d636ab76e0" {
		t.Fatalf("NInfer source commit = %q", ninferSourceCommit)
	}
	if ninferCUDAFloor != "12.8" || ninferGPUArchitecture != "89" {
		t.Fatalf("NInfer target = CUDA >= %s, SM%s; want CUDA >= 12.8, SM89", ninferCUDAFloor, ninferGPUArchitecture)
	}
	if ninferArtifactFormat != 2 || ninferModelRevision != "18dfc887423fa5aabf3cb56fac41490e462b3fab" || ninferModelSHA256 != "eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e" {
		t.Fatalf("NInfer model artifact tuple changed unexpectedly: format=v%d revision=%s sha256=%s", ninferArtifactFormat, ninferModelRevision, ninferModelSHA256)
	}
	if native, err := resolveNInferConfig(ninferConfigNative); err != nil || native.ContextTokens != 262144 {
		t.Fatalf("native NInfer context = %+v, %v; want 262144 tokens", native, err)
	}
}

func TestNInferBootstrapIsPinnedAndPrefetchesInParallel(t *testing.T) {
	command := ninferBootstrapCommand()
	for _, required := range []string{
		ninferSourceRepository,
		ninferSourceCommit,
		ninferModelURL,
		ninferModelSHA256,
		"CUDA toolkit 12.8 or newer",
		"gcc-13",
		"g++-13",
		"CMake 3.28",
		"-DCMAKE_CUDA_ARCHITECTURES=89",
		"--target ninfer ninfer-serve",
		"model-download.pid",
		"model-download.log",
		"model-total-bytes",
		"Starting Qwen3.8-27B model prefetch in parallel",
		"--retry-all-errors",
		"-C -",
		"waiting for the parallel Qwen model transfer",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("NInfer bootstrap missing %q", required)
		}
	}
	for _, forbidden := range []string{"18210531328", "17367 MiB", "/resolve/main/"} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("NInfer bootstrap retained stale hardcoded artifact metadata %q", forbidden)
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
		"model-total-bytes",
		"%header{content-length}",
		"Discarding invalid completed/oversized NInfer model artifact",
		"--model-id qwen3.8-27b",
		"--max-context 126976",
		"--kv-capacity 126976",
		"--default-max-tokens 126976",
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

func TestNInferProgressDoesNotHardcodeArtifactSize(t *testing.T) {
	command := remoteModelProgressCommandForState(sessionstate.State{Runtime: runtimeNInfer})
	if !strings.Contains(command, "model-total-bytes") {
		t.Fatal("NInfer progress does not read discovered model size")
	}
	for _, forbidden := range []string{"18210531328", "17367 MiB"} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("NInfer progress retained hardcoded artifact metadata %q", forbidden)
		}
	}
}

func TestNInferGeneratedShellIsValid(t *testing.T) {
	commands := map[string]string{
		"bootstrap": ninferBootstrapCommand(),
		"launch":    ninferModelLaunchCommandWithClients(262144, 2),
		"progress":  remoteModelProgressCommandForState(sessionstate.State{Runtime: runtimeNInfer}),
	}
	for name, command := range commands {
		t.Run(name, func(t *testing.T) {
			if out, err := exec.Command("bash", "-n", "-c", command).CombinedOutput(); err != nil {
				t.Fatalf("generated NInfer shell is invalid: %v\n%s\ncommand:\n%s", err, out, command)
			}
		})
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
	// Live inference observation requires the engine's /metrics and /slots
	// endpoints, which llama.cpp only serves with these explicit flags.
	for _, required := range []string{"--metrics", "--slots"} {
		if !strings.Contains(command, required) {
			t.Fatalf("llama.cpp launch missing %q, live inference telemetry would be unavailable", required)
		}
	}
}
