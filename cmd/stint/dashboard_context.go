package main

import (
	"fmt"
	"sort"
	"strings"

	dash "github.com/Marguelgtz/Stint/internal/dashboard"
)

// dashboardClientContexts projects runtime lanes into resident context owned by
// distinct client sessions. NInfer's session_digest is the preferred stable
// identity. When a runtime does not publish it, the lane ID is the fallback.
//
// A client may briefly appear on more than one lane while the runtime moves or
// retains state. For the same session digest we keep the largest resident
// prompt instead of summing duplicates, so a lane handoff cannot double-count
// context pressure.
func dashboardClientContexts(lanes []inferenceLane) (int, []dash.ClientContext) {
	type aggregate struct {
		key    string
		label  string
		tokens int
	}

	byKey := make(map[string]aggregate)
	for _, lane := range lanes {
		if lane.NPrompt <= 0 {
			continue
		}

		session := strings.TrimSpace(lane.Session)
		key := session
		label := fmt.Sprintf("client %d", lane.ID+1)
		if session != "" {
			key = "session:" + session
			label = "client " + shortClientDigest(session)
		} else {
			key = fmt.Sprintf("lane:%d", lane.ID)
		}

		current, ok := byKey[key]
		if !ok || lane.NPrompt > current.tokens {
			byKey[key] = aggregate{key: key, label: label, tokens: lane.NPrompt}
		}
	}

	values := make([]aggregate, 0, len(byKey))
	for _, value := range byKey {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].key < values[j].key })

	clients := make([]dash.ClientContext, 0, len(values))
	used := 0
	for _, value := range values {
		used += value.tokens
		clients = append(clients, dash.ClientContext{Key: value.key, Label: value.label, Tokens: value.tokens})
	}
	return used, clients
}

func shortClientDigest(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 6 {
		return value
	}
	return value[:6]
}
