package dashboard

import (
	"strings"
	"testing"
	"time"
)

func TestRenderHomeShowsStackedClientContext(t *testing.T) {
	model := Model{
		Width: 100, Height: 40, NoColor: true, View: Home,
		Session: Session{
			Status: "READY", Model: "Qwen3.8-27B", GPUModel: "RTX 4090", Runtime: "ninfer",
			Context: 100000, Scheduled: time.Hour, Elapsed: 30 * time.Minute,
		},
		Inference: Inference{
			Refreshed: true, Available: true, ContextUsed: 100000,
			Clients: []ClientContext{
				{Key: "session:aaaaaa", Label: "client aaaaaa", Tokens: 50000},
				{Key: "session:bbbbbb", Label: "client bbbbbb", Tokens: 50000},
			},
		},
	}

	out := stripANSI(Render(model))
	for _, want := range []string{
		"30m 00s / 1h 00m",
		"100.0k / 100.0k ctx · 100%",
		"client aaaaaa 50.0k · 50%",
		"client bbbbbb 50.0k · 50%",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("home view missing %q:\n%s", want, out)
		}
	}
}

func TestContextBarUsesStableDistinctClientColors(t *testing.T) {
	if first, second := clientContextColor("session-1"), clientContextColor("session-2"); first == second {
		t.Fatalf("test keys unexpectedly map to same color: %q", first)
	}

	model := Model{
		Session: Session{Context: 100000},
		Inference: Inference{
			Refreshed: true, Available: true, ContextUsed: 100000,
			Clients: []ClientContext{
				{Key: "session-1", Label: "client one", Tokens: 50000},
				{Key: "session-2", Label: "client two", Tokens: 50000},
			},
		},
	}
	bar := contextBar(20, model, palette{})
	if !strings.Contains(bar, "\x1b["+clientContextColor("session-1")+"m") {
		t.Fatalf("bar missing first client color: %q", bar)
	}
	if !strings.Contains(bar, "\x1b["+clientContextColor("session-2")+"m") {
		t.Fatalf("bar missing second client color: %q", bar)
	}
	if visibleLen(bar) != 20 {
		t.Fatalf("bar visible width = %d, want 20", visibleLen(bar))
	}
}

func TestContextBarShowsFreeCapacity(t *testing.T) {
	model := Model{
		Session: Session{Context: 100000},
		Inference: Inference{
			Refreshed: true, Available: true, ContextUsed: 90000,
			Clients: []ClientContext{
				{Key: "a", Label: "client a", Tokens: 70000},
				{Key: "b", Label: "client b", Tokens: 20000},
			},
		},
	}
	bar := stripANSI(contextBar(10, model, palette{}))
	if bar != "█████████░" {
		t.Fatalf("bar = %q, want 9 used cells and 1 free cell", bar)
	}
	if summary := contextUsageSummary(model); summary != "90.0k / 100.0k ctx · 90%" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestContextUsageDoesNotPretendUnrefreshedMeansZero(t *testing.T) {
	model := Model{Session: Session{Context: 100000}}
	if got := contextUsageSummary(model); got != "context awaiting refresh" {
		t.Fatalf("summary = %q", got)
	}
}
