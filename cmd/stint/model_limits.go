package main

import (
	"fmt"

	"github.com/Marguelgtz/Stint/internal/runtime/llama"
)

const (
	minInteractiveContext    = 16384
	legacyInteractiveContext = 16384
)

var interactiveModelConfig = llama.InteractiveQwen()
var interactiveModelAlias = interactiveModelConfig.Model
var interactiveContext = interactiveModelConfig.Context
var interactiveMaxOutput = interactiveModelConfig.MaxOutput

func validateInteractiveContext(tokens int) error {
	if tokens < minInteractiveContext || tokens > interactiveContext {
		return fmt.Errorf("invalid --context %d: interactive RTX 4090 profile supports %d-%d tokens", tokens, minInteractiveContext, interactiveContext)
	}
	return nil
}

func effectiveInteractiveContext(tokens int) int {
	if tokens > 0 {
		return tokens
	}
	return legacyInteractiveContext
}
