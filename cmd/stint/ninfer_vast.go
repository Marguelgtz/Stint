package main

import (
	"github.com/Marguelgtz/Stint/internal/core"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
)

func prepareVastSearchForRuntime(profile core.Profile, options vast.SearchOptions, runtimeRequest string) (core.Profile, vast.SearchOptions) {
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
	return interactiveImage
}

// Runtime setup starts only after the provider reports SSH metadata and Stint
// has established an SSH connection. Keep the hook explicit so the startup
// retry flow can pass it to Vast without relying on an unverified custom image.
func vastOnStartForRuntime(string) string {
	return ""
}
