package main

func contextForRuntime(runtime string) int {
	if runtime == runtimeNInfer {
		return interactiveRuntimeContext
	}
	return interactiveContext
}
