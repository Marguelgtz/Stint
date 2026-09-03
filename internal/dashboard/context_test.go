package dashboard

import (
	"strings"
	"testing"
	"time"
)

func TestRenderHomeShowsResidentLaneContext(t *testing.T) {
	model := Model{
		Width: 100, Height: 40, NoColor: true, View: Home,
		Session: Session{
			Status: "READY", Model: "Qwen3.8-27B", GPUModel: "RTX 4090", Runtime: "ninfer",
			Context: 262144, Scheduled: time.Hour, Elapsed: 30 * time.Minute,
		},
		Inference: Inference{
			Refreshed: true, Available: true, ContextUsed: 155005, Decode: "98.2 tok/s",
			Clients: []ClientContext{
				{Key: "lane:0", Label: "lane 1 · idle retained · cache 93% · depth", Tokens: 7005},
				{Key: "lane:1", Label: "lane 2 · active · cache 95% · depth", Tokens: 148000},
			},
		},
	}

	out := stripANSI(Render(model))
	for _, want := range []string{
		"30m 00s / 1h 00m",
		"155.0k / 262.1k ctx · 59% resident",
		"lane 1 · idle retained · cache 93% · depth 7.0k · 3% · decode —",
		"lane 2 · active · cache 95% · depth 148.0k · 56% · decode 98.2 tok/s (engine)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("home view missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "client") {
		t.Fatalf("runtime lanes must not be labeled as external clients:\n%s", out)
	}
}

func TestRenderHomeKeepsEmptyLaneVisible(t *testing.T) {
	model := Model{
		Width: 100, Height: 40, NoColor: true, View: Home,
		Session: Session{Status: "READY", Context: 262144, Scheduled: time.Hour},
		Inference: Inference{
			Refreshed: true, Available: true, ContextUsed: 45000, Decode: "80.0 tok/s",
			Clients: []ClientContext{
				{Key: "lane:0", Label: "lane 1 · active · cache 91% · depth", Tokens: 45000},
				{Key: "lane:1", Label: "lane 2 · idle · cache — · depth", Tokens: 0},
			},
		},
	}
	out := stripANSI(Render(model))
	if !strings.Contains(out, "lane 1 · active") || !strings.Contains(out, "lane 2 · idle · cache — · depth 0 · decode —") {
		t.Fatalf("active and empty idle lanes should both be visible:\n%s", out)
	}
}

func TestContextLegendMarksDecodeSharedAcrossActiveLanes(t *testing.T) {
	model := Model{
		Width: 120,
		Session: Session{Context: 100000},
		Inference: Inference{
			Available: true, Decode: "120.0 tok/s",
			Clients: []ClientContext{
				{Key: "lane:0", Label: "lane 1 · active · cache 80% · depth", Tokens: 50000},
				{Key: "lane:1", Label: "lane 2 · active · cache 70% · depth", Tokens: 50000},
			},
		},
	}
	legend := stripANSI(contextLegend(model, palette{noColor: true}))
	if strings.Count(legend, "decode shared (engine 120.0 tok/s)") != 2 {
		t.Fatalf("shared engine decode should not be presented as a per-lane split:\n%s", legend)
	}
}

func TestContextBarUsesStableDistinctLaneColors(t *testing.T) {
	if first, second := residentContextColor("lane:0"), residentContextColor("lane:1"); first == second {
		t.Fatalf("test lane keys unexpectedly map to same color: %q", first)
	}

	model := Model{
		Session: Session{Context: 100000},
		Inference: Inference{
			Refreshed: true, Available: true, ContextUsed: 100000,
			Clients: []ClientContext{
				{Key: "lane:0", Label: "lane 1 · idle retained · cache 100% · depth", Tokens: 50000},
				{Key: "lane:1", Label: "lane 2 · active · cache 100% · depth", Tokens: 50000},
			},
		},
	}
	bar := contextBar(20, model, palette{})
	if !strings.Contains(bar, "\x1b["+residentContextColor("lane:0")+"m") {
		t.Fatalf("bar missing first lane color: %q", bar)
	}
	if !strings.Contains(bar, "\x1b["+residentContextColor("lane:1")+"m") {
		t.Fatalf("bar missing second lane color: %q", bar)
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
				{Key: "lane:0", Label: "lane 1 · idle retained · cache 100% · depth", Tokens: 70000},
				{Key: "lane:1", Label: "lane 2 · active · cache 100% · depth", Tokens: 20000},
			},
		},
	}
	bar := stripANSI(contextBar(10, model, palette{}))
	if bar != "█████████░" {
		t.Fatalf("bar = %q, want 9 used cells and 1 free cell", bar)
	}
	if summary := contextUsageSummary(model); summary != "90.0k / 100.0k ctx · 90% resident" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestContextBarClampsVisualWhenObservedResidencyExceedsCapacity(t *testing.T) {
	model := Model{
		Session: Session{Context: 100000},
		Inference: Inference{
			Refreshed: true, Available: true, ContextUsed: 110000,
			Clients: []ClientContext{
				{Key: "lane:0", Label: "lane 1 · idle retained · cache 100% · depth", Tokens: 60000},
				{Key: "lane:1", Label: "lane 2 · active · cache 100% · depth", Tokens: 50000},
			},
		},
	}
	if bar := stripANSI(contextBar(10, model, palette{})); bar != "██████████" {
		t.Fatalf("bar = %q, want safely clamped full bar", bar)
	}
	if summary := contextUsageSummary(model); summary != "110.0k / 100.0k ctx · 110% resident" {
		t.Fatalf("summary should preserve anomalous observed value, got %q", summary)
	}
}

func TestContextUsageDoesNotPretendUnrefreshedMeansZero(t *testing.T) {
	model := Model{Session: Session{Context: 100000}}
	if got := contextUsageSummary(model); got != "context awaiting refresh" {
		t.Fatalf("summary = %q", got)
	}
}
