package main

import (
	"bufio"
	"encoding/json"
	"strings"
)

// clineEvent is one line of the Cline CLI --json stream. Only the fields the
// coordinator relies on are decoded; unknown event types are skipped.
type clineEvent struct {
	TS           string `json:"ts"`
	Type         string `json:"type"`
	FinishReason string `json:"finishReason"`
	Iterations   int    `json:"iterations"`
	Text         string `json:"text"`
	Usage        *struct {
		InputTokens  int `json:"inputTokens"`
		OutputTokens int `json:"outputTokens"`
	} `json:"usage"`
}

// parseClineEvents extracts the JSONL event lines from a Cline stdout
// capture. Malformed lines are ignored: observability must never fail a run.
func parseClineEvents(stdout string) []clineEvent {
	var events []clineEvent
	sc := bufio.NewScanner(strings.NewReader(stdout))
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev clineEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.Type == "" {
			continue
		}
		events = append(events, ev)
	}
	return events
}
