package main

import (
	"fmt"
	"sort"

	dash "github.com/Marguelgtz/Stint/internal/dashboard"
)

// dashboardClientContexts projects /slots into resident runtime context. The
// historical function/result name is kept for this stacked PR's narrow API
// surface, but these values are deliberately NOT external client identities.
// NInfer may schedule different callers onto the same lane over time, and a
// lane may retain context after a request has finished.
//
// Each lane with a positive resident prompt depth is therefore represented
// independently. session_digest is runtime metadata only and is not used for
// grouping, naming, coloring, or ownership inference.
func dashboardClientContexts(lanes []inferenceLane) (int, []dash.ClientContext) {
	resident := make([]inferenceLane, 0, len(lanes))
	for _, lane := range lanes {
		if lane.NPrompt > 0 {
			resident = append(resident, lane)
		}
	}
	sort.Slice(resident, func(i, j int) bool { return resident[i].ID < resident[j].ID })

	contexts := make([]dash.ClientContext, 0, len(resident))
	used := 0
	for _, lane := range resident {
		used += lane.NPrompt
		contexts = append(contexts, dash.ClientContext{
			Key:    fmt.Sprintf("lane:%d", lane.ID),
			Label:  fmt.Sprintf("lane %d · %s", lane.ID+1, dashboardLaneContextState(lane)),
			Tokens: lane.NPrompt,
		})
	}
	return used, contexts
}

func dashboardLaneContextState(lane inferenceLane) string {
	switch {
	case lane.Processing:
		return "processing"
	case lane.Retained:
		return "retained"
	default:
		return "resident"
	}
}
