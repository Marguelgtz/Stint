package vast

import "testing"

func TestApplyMinCUDARequirement(t *testing.T) {
	payload := map[string]any{}
	applyMinCUDARequirement(NInferMinCUDAVersion, payload)
	filter, ok := payload["cuda_max_good"].(map[string]any)
	if !ok {
		t.Fatalf("cuda_max_good = %#v, want filter", payload["cuda_max_good"])
	}
	if got := filter["gte"]; got != NInferMinCUDAVersion {
		t.Fatalf("cuda_max_good.gte = %#v, want %.1f", got, NInferMinCUDAVersion)
	}
}

func TestApplyMinCUDARequirementDisabled(t *testing.T) {
	payload := map[string]any{}
	applyMinCUDARequirement(0, payload)
	if _, ok := payload["cuda_max_good"]; ok {
		t.Fatalf("disabled CUDA requirement unexpectedly changed payload: %#v", payload)
	}
}
