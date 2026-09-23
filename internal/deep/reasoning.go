package deep

import (
	"fmt"
	"strings"
)

// The Qwen/NInfer serving contract used by the first Deep Work run exposes
// these four request levels. An empty value means "inherit the session
// default" and is intentionally accepted by callers that resolve overrides.
const (
	ReasoningNone   = "none"
	ReasoningLow    = "low"
	ReasoningMedium = "medium"
	ReasoningXHigh  = "xhigh"
)

// NormalizeReasoning validates and canonicalizes a request-level reasoning
// setting. Hermes supports more provider-specific names, but Stint persists
// the portable subset understood by the Qwen/NInfer endpoint used by CP1.
func NormalizeReasoning(value string) (string, error) {
	level := strings.ToLower(strings.TrimSpace(value))
	if level == "" {
		return "", nil
	}
	switch level {
	case ReasoningNone, ReasoningLow, ReasoningMedium, ReasoningXHigh:
		return level, nil
	default:
		return "", fmt.Errorf("invalid reasoning level %q (want none, low, medium, or xhigh)", value)
	}
}
