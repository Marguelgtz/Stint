package main

import "github.com/Marguelgtz/Stint/internal/runtime/llama"

var interactiveModelConfig = llama.InteractiveQwen()
var interactiveModelAlias = interactiveModelConfig.Model
var interactiveContext = interactiveModelConfig.Context
var interactiveMaxOutput = interactiveModelConfig.MaxOutput
