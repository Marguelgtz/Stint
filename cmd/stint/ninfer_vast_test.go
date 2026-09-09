package main

import (
	"testing"

	"github.com/Marguelgtz/Stint/internal/core"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
)

func TestPrepareVastSearchForExplicitNInfer(t *testing.T) {
	profile := core.BuiltinProfiles["interactive"]
	prepared, options := prepareVastSearchForRuntime(profile, vast.SearchOptions{}, runtimeNInfer)
	if len(prepared.GPU.PreferredModels) != 1 || prepared.GPU.PreferredModels[0] != "RTX 4090" {
		t.Fatalf("preferred models = %#v, want only RTX 4090", prepared.GPU.PreferredModels)
	}
	if options.MinCUDAMaxGood != vast.NInferMinCUDAVersion {
		t.Fatalf("minimum CUDA = %v, want %v", options.MinCUDAMaxGood, vast.NInferMinCUDAVersion)
	}
}

func TestVastImageForRuntime(t *testing.T) {
	if got := vastImageForRuntime(runtimeNInfer); got != vast.NInferCUDA128Image {
		t.Fatalf("NInfer image = %q, want %q", got, vast.NInferCUDA128Image)
	}
	if got := vastImageForRuntime(runtimeAuto); got != interactiveImage {
		t.Fatalf("auto image = %q, want %q", got, interactiveImage)
	}
}
