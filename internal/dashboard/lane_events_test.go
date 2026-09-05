package dashboard

import (
	"strings"
	"testing"
	"time"
)

func TestRenderHomeShowsClientTagAndNoAttribution(t *testing.T) {
	model := Model{
		Width: 100, Height: 40, NoColor: true, View: Home, ClientTag: "hermes",
		Session:   Session{Status: "READY", Context: 262144, Scheduled: time.Hour},
		Inference: Inference{Refreshed: true, Available: true, ContextUsed: 45000},
	}
	out := stripANSI(Render(model))
	if !strings.Contains(out, "Clients      hermes") {
		t.Fatalf("home view missing the client tag line:\n%s", out)
	}
	if !strings.Contains(out, "not attributed to a caller") {
		t.Fatalf("home view missing the no-attribution note:\n%s", out)
	}
	if strings.Contains(out, "client") {
		t.Fatalf("runtime lanes must not be labeled as external clients:\n%s", out)
	}
}

func TestRenderHomeUntaggedClientShowsUntagged(t *testing.T) {
	model := Model{
		Width: 100, Height: 40, NoColor: true, View: Home,
		Session:   Session{Status: "READY", Context: 262144},
		Inference: Inference{Refreshed: true, Available: true, ContextUsed: 45000},
	}
	out := stripANSI(Render(model))
	if !strings.Contains(out, "Clients      untagged") {
		t.Fatalf("untagged session should render 'untagged', not omit the line:\n%s", out)
	}
}

func TestRenderHomeOmitsClientLineWhenNotProbed(t *testing.T) {
	model := Model{
		Width: 100, Height: 40, NoColor: true, View: Home, ClientTag: "hermes",
		Session:   Session{Status: "READY", Context: 262144},
		Inference: Inference{Refreshed: false},
	}
	out := stripANSI(Render(model))
	if strings.Contains(out, "Clients") {
		t.Fatalf("client line must not render before the first probe:\n%s", out)
	}
}

func TestRenderPerformanceShowsLaneEvents(t *testing.T) {
	model := Model{
		Width: 100, Height: 40, NoColor: true, View: Performance,
		Session: Session{Status: "READY", Context: 262144},
		Inference: Inference{
			Refreshed: true, Available: true, Agents: 1, Depth: 45000,
		},
		LaneEvents: []LaneEvent{
			// Newest first: the lane-2 transition (12:30:41) is the most recent,
			// ahead of the lane-1 first observation (12:30:10).
			{At: time.Date(2026, 9, 5, 12, 30, 41, 0, time.UTC), LaneID: 1, Kind: "active -> idle retained", Depth: 130612, CachePct: "97%"},
			{At: time.Date(2026, 9, 5, 12, 30, 10, 0, time.UTC), LaneID: 0, Kind: "observed active", Depth: 10260, CachePct: "2%"},
		},
	}
	out := stripANSI(Render(model))
	for _, want := range []string{
		"LANE EVENTS",
		"lane state transitions, newest first",
		"not clients",
		"lane 2  active -> idle retained  130612 tok  cache 97%",
		"lane 1  observed active  10260 tok  cache 2%",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("performance view missing %q:\n%s", want, out)
		}
	}
	// The newest event (lane 2's transition) must appear before the older
	// first-observation (lane 1).
	if strings.Index(out, "lane 2") > strings.Index(out, "lane 1") {
		t.Fatalf("lane events must be newest first:\n%s", out)
	}
}

func TestRenderPerformanceOmitsLaneEventsWhenEmpty(t *testing.T) {
	model := Model{
		Width: 100, Height: 40, NoColor: true, View: Performance,
		Session:   Session{Status: "READY", Context: 262144},
		Inference: Inference{Refreshed: true, Available: true, Agents: 1},
	}
	out := stripANSI(Render(model))
	if strings.Contains(out, "LANE EVENTS") {
		t.Fatalf("LANE EVENTS panel must be omitted when the log is empty:\n%s", out)
	}
}
