package main

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/Marguelgtz/Stint/internal/core"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
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
		`mem_kb="$(grep -m1 ^MemTotal: /proc/meminfo | tr -cd 0-9)"`,
		"-m \"$model\"",
		"-c 60000",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("llama.cpp detached launch missing %q", required)
		}
	}
	if strings.Contains(command, `awk "/MemTotal:/`) || strings.Contains(command, `\\$2`) {
		t.Fatalf("llama.cpp launch retained unsafe nested awk quoting: %s", command)
	}
}

func TestLlamaLaunchCommandHasValidBashSyntax(t *testing.T) {
	command := llamaModelLaunchCommand(60000)
	if out, err := exec.Command("bash", "-n", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("llama.cpp launch command has invalid bash syntax: %v\n%s\ncommand:\n%s", err, out, command)
	}
}

func TestLlamaModelProgressReportsTransferAndLoadStages(t *testing.T) {
	command := remoteModelProgressCommandForState(sessionstate.State{Runtime: runtimeLlamaCpp})
	for _, required := range []string{
		"observed bytes sent so far",
		"*.incomplete",
		"model download ${pct}%",
		"verifying checksum",
		"pgrep -x llama-server",
		fmt.Sprintf("expected=%d", llamaModelSizeBytes),
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("llama progress command missing %q: %s", required, command)
		}
	}
	if out, err := exec.Command("bash", "-n", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("llama progress command has invalid bash syntax: %v\n%s\ncommand:\n%s", err, out, command)
	}
}

func TestNInferDefaultRemainsPinnedPrebuiltImage(t *testing.T) {
	t.Setenv(ninferDeploymentEnv, "")
	got := vastImageForRuntime(runtimeNInfer)
	if got != ninferVastImage {
		t.Fatalf("NInfer default image = %q, want control image %q", got, ninferVastImage)
	}
	if !strings.HasPrefix(got, "ghcr.io/marguelgtz/stint-ninfer:") {
		t.Fatalf("NInfer default image = %q, want Stint GHCR control image", got)
	}
	command := vastOnStartForRuntime(runtimeNInfer)
	for _, required := range []string{ninferPrebuiltBinary, ninferRuntimeBridgePath, ninferSourceCommit} {
		if !strings.Contains(command, required) {
			t.Fatalf("default NInfer onstart missing %q: %s", required, command)
		}
	}
	if strings.Contains(command, ninferRuntimeBundleURL) {
		t.Fatalf("default NInfer deployment unexpectedly references experimental bundle: %s", command)
	}
}

func TestNInferBundleDeploymentUsesPlainVastBase(t *testing.T) {
	t.Setenv(ninferDeploymentEnv, ninferDeploymentBundle)
	got := vastImageForRuntime(runtimeNInfer)
	if got != vast.NInferCUDA128Image {
		t.Fatalf("NInfer bundle image = %q, want plain Vast CUDA base %q", got, vast.NInferCUDA128Image)
	}
	if !strings.HasPrefix(got, "vastai/base-image:cuda-12.8.1-") {
		t.Fatalf("NInfer bundle image = %q, want pinned Vast CUDA 12.8.1 base", got)
	}
	for _, forbidden := range []string{"ghcr.io/marguelgtz/stint-ninfer", ":latest", ":edge"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("NInfer bundle image unexpectedly contains %q: %s", forbidden, got)
		}
	}
}

func TestNInferUnknownDeploymentFallsBackToControlImage(t *testing.T) {
	t.Setenv(ninferDeploymentEnv, "future-mode")
	if got := vastImageForRuntime(runtimeNInfer); got != ninferVastImage {
		t.Fatalf("unknown deployment selected %q, want safe control image %q", got, ninferVastImage)
	}
}

func TestNInferBundleOnStartWritesLazyRuntimeBridge(t *testing.T) {
	t.Setenv(ninferDeploymentEnv, ninferDeploymentBundle)
	command := vastOnStartForRuntime(runtimeNInfer)
	for _, required := range []string{
		ninferRuntimeBridgePath,
		ninferSourceCommit,
		"/workspace/stint/ninfer/.stint-commit",
		"chmod 600 /root/.ssh/authorized_keys",
		"<<'STINT_NINFER_WRAPPER'",
		ninferRuntimeBundleURL,
		ninferRuntimeBundleSHAURL,
		"hashlib.sha256",
		"tarfile.open",
		"LD_LIBRARY_PATH",
		`exec "$real" "$@"`,
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("NInfer bundle onstart bridge missing %q: %s", required, command)
		}
	}
	for _, forbidden := range []string{
		llamaPrebuiltBinary,
		llamaRuntimeBridgePath,
		"qwen3_8_27b.ninfer",
		"apt-get",
		"git clone",
		"cmake",
		"--max-context",
		"--kv-capacity",
	} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("NInfer bundle onstart unexpectedly performs runtime/model bootstrap %q: %s", forbidden, command)
		}
	}

	// The provider hook may write a self-installing wrapper, but it must not run
	// the bundle downloader while Vast is still in the opaque loading phase.
	parts := strings.SplitN(command, "<<'STINT_NINFER_WRAPPER'", 2)
	if len(parts) != 2 {
		t.Fatalf("NInfer onstart does not contain wrapper heredoc: %s", command)
	}
	beforeWrapper := parts[0]
	for _, forbidden := range []string{"python3", ninferRuntimeReleaseTag, ninferRuntimeArchive} {
		if strings.Contains(beforeWrapper, forbidden) {
			t.Fatalf("NInfer provider hook executes bundle preparation before SSH (%q): %s", forbidden, beforeWrapper)
		}
	}
	if out, err := exec.Command("bash", "-n", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("NInfer bundle onstart bridge has invalid shell syntax: %v\n%s\ncommand:\n%s", err, out, command)
	}
}

func TestNInferBootstrapOverlapsModelPrefetchWithBundleResolution(t *testing.T) {
	command := ninferBootstrapCommand()
	prefetch := strings.Index(command, "Starting Qwen3.8-27B model prefetch")
	bridgeValidation := strings.Index(command, `if [ -x "$bin" ]`)
	if prefetch < 0 || bridgeValidation < 0 {
		t.Fatalf("NInfer bootstrap markers missing: prefetch=%d bridge=%d", prefetch, bridgeValidation)
	}
	if prefetch >= bridgeValidation {
		t.Fatalf("NInfer runtime validation begins before model prefetch; bundle/model transfers would serialize")
	}
}
