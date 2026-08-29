package main

import (
	"fmt"
	"strings"
)

const (
	ninferConfigCoding    = "coding"
	ninferConfigPrecision = "precision"
	ninferConfigNative    = "native"
)

type ninferConfig struct {
	Name          string
	ContextTokens int
	KVDType       string
	Description   string
}

func resolveNInferConfig(value string) (ninferConfig, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", ninferConfigCoding, "cline", "agent":
		return ninferConfig{Name: ninferConfigCoding, ContextTokens: 126976, KVDType: "int8", Description: "126,976 context, INT8 KV, MTP3"}, nil
	case ninferConfigPrecision, "int8", "max-precision":
		return ninferConfig{Name: ninferConfigPrecision, ContextTokens: 172032, KVDType: "int8", Description: "172,032 context, INT8 KV, MTP3"}, nil
	case ninferConfigNative, "full", "max", "262k":
		return ninferConfig{Name: ninferConfigNative, ContextTokens: 262144, KVDType: "rk4v4-e8", Description: "262,144 native context, E8 4-bit KV, MTP3"}, nil
	default:
		return ninferConfig{}, fmt.Errorf("unknown NInfer config %q; choose coding, precision, or native", value)
	}
}

func ninferConfigForContext(contextTokens int) ninferConfig {
	if contextTokens > 172032 {
		config, _ := resolveNInferConfig(ninferConfigNative)
		return config
	}
	if contextTokens > 126976 {
		config, _ := resolveNInferConfig(ninferConfigPrecision)
		return config
	}
	config, _ := resolveNInferConfig(ninferConfigCoding)
	return config
}
