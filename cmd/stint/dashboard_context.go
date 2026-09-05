package main

import (
	"fmt"
	"math"
	"sort"

	dash "github.com/Marguelgtz/Stint/internal/dashboard"
)

// dashboardClientContexts projects /slots into observed runtime-lane state.
// The historical function/result name is kept for this stacked PR's narrow API
// surface, but these values are deliberately NOT external client identities.
// NInfer may schedule different callers onto the same lane over time, and a
// lane may retain context after a request has finished.
//
// All observed lanes are represented so the dashboard can show active versus
// idle execution lanes even when a lane currently has zero resident tokens.
// session_digest is runtime metadata only and is not used for grouping,
// naming, coloring, or ownership inference.
func dashboardClientContexts(lanes []inferenceLane) (int, []dash.ClientContext) {
	observed := append([]inferenceLane(nil), lanes...)
	sort.Slice(observed, func(i, j int) bool { return observed[i].ID < observed[j].ID })

	contexts := make([]dash.ClientContext, 0, len(observed))
	used := 0
	for _, lane := range observed {
		tokens := lane.NPrompt
		if tokens < 0 {
			tokens = 0
		}
		used += tokens
		contexts = append(contexts, dash.ClientContext{
			Key:    fmt.Sprintf("lane:%d", lane.ID),
			Label:  dashboardLaneLiveLabel(lane),
			Tokens: tokens,
		})
	}
	return used, contexts
}

func dashboardLaneLiveLabel(lane inferenceLane) string {
	state := dashboardLaneContextState(lane)
	cache := "cache —"
	if lane.NPrompt > 0 {
		ratio := float64(lane.NCached) / float64(lane.NPrompt)
		ratio = math.Max(0, math.Min(1, ratio))
		cache = fmt.Sprintf("cache %.0f%%", ratio*100)
	}
	return fmt.Sprintf("lane %d · %s · %s · depth", lane.ID+1, state, cache)
}

func dashboardLaneContextState(lane inferenceLane) string {
	switch {
	case lane.Processing:
		return "active"
	case lane.Retained && lane.NPrompt > 0:
		return "idle retained"
	case lane.NPrompt > 0:
		return "idle resident"
	default:
		return "idle"
	}
}
