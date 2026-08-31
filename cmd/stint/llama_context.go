package main

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	minLlamaContextTokens = 1024
	maxLlamaContextTokens = 131072
)

func resolveLlamaContext(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return interactiveContext, nil
	}
	contextTokens, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid --context value %q; expected an integer between %d and %d", value, minLlamaContextTokens, maxLlamaContextTokens)
	}
	if contextTokens < minLlamaContextTokens || contextTokens > maxLlamaContextTokens {
		return 0, fmt.Errorf("invalid --context value %d; llama.cpp context must be between %d and %d tokens", contextTokens, minLlamaContextTokens, maxLlamaContextTokens)
	}
	return contextTokens, nil
}
