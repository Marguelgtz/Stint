package vast

const (
	NInferMinCUDAVersion = 12.8
	NInferCUDA128Image   = "vastai/base-image:cuda-12.8.1-cudnn-devel-ubuntu22.04-py310"
)

func applyMinCUDARequirement(minVersion float64, payload map[string]any) map[string]any {
	if minVersion > 0 {
		payload["cuda_max_good"] = map[string]any{"gte": minVersion}
	}
	return payload
}
