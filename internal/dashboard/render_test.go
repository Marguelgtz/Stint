package dashboard

import (
	"strings"
	"testing"
	"time"
)

func TestRenderHomeAcrossWidths(t *testing.T) {
	for _, width := range []int{60, 80, 100, 118, 140} {
		t.Run(string(rune(width)), func(t *testing.T) {
			model := fixtureModel(width)
			out := Render(model)
			for _, want := range []string{"STINT", "READY", "Qwen3.8-27B", "RTX 4090", "SESSION", "PERFORMANCE", "q Exit"} {
				if !strings.Contains(stripANSI(out), want) {
					t.Fatalf("width %d missing %q:\n%s", width, want, out)
				}
			}
		})
	}
}

func TestRenderNeverUsesTerminalLastColumn(t *testing.T) {
	for _, width := range []int{60, 80, 100, 118, 140} {
		t.Run(string(rune(width)), func(t *testing.T) {
			out := Render(fixtureModel(width))
			for i, line := range strings.Split(out, "\n") {
				got := visibleLen(line)
				if got > width-1 {
					t.Fatalf("width %d line %d is %d columns (must stay below terminal width): %q", width, i+1, got, stripANSI(line))
				}
			}
		})
	}
}

func TestRenderUsesConsistentLeftGutter(t *testing.T) {
	out := Render(fixtureModel(118))
	for i, line := range strings.Split(out, "\n") {
		plain := stripANSI(line)
		if strings.TrimSpace(plain) == "" {
			continue
		}
		if !strings.HasPrefix(plain, "  ") {
			t.Fatalf("line %d has no two-column gutter: %q", i+1, plain)
		}
	}
}

func TestScreenshotWidthRegressionKeepsBlocksAligned(t *testing.T) {
	out := stripANSI(Render(fixtureModel(118)))
	lines := strings.Split(out, "\n")

	var identityLine, timingLine, cardsLine, navLine, actionsLine string
	for _, line := range lines {
		switch {
		case strings.Contains(line, "Qwen3.8-27B") && strings.Contains(line, "RTX 4090") && strings.Contains(line, "HEALTH"):
			identityLine = line
		case strings.Contains(line, "Started") && strings.Contains(line, "Auto-destroy"):
			timingLine = line
		case strings.Contains(line, "GPU") && strings.Contains(line, "PERFORMANCE"):
			cardsLine = line
		case strings.Contains(line, "[1 Home]") && strings.Contains(line, "4 Logs"):
			navLine = line
		case strings.Contains(line, "r Refresh") && strings.Contains(line, "q Exit"):
			actionsLine = line
		}
	}
	if identityLine == "" {
		t.Fatalf("identity grid was not kept on one aligned row:\n%s", out)
	}
	if timingLine == "" {
		t.Fatalf("session timing did not share a deliberate row:\n%s", out)
	}
	if cardsLine == "" {
		t.Fatalf("GPU and performance cards are not aligned as a two-column row:\n%s", out)
	}
	if navLine == "" || actionsLine == "" {
		t.Fatalf("navigation/actions should be separate left-aligned rows:\n%s", out)
	}
}

func TestRenderNoColorEmitsNoANSI(t *testing.T) {
	model := fixtureModel(100)
	model.NoColor = true
	out := Render(model)
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("NO_COLOR render contains ANSI sequence: %q", out)
	}
}

func TestRenderNoSession(t *testing.T) {
	model := Model{Width: 80, Height: 24, NoColor: true, NoSession: true, View: Home}
	out := Render(model)
	if !strings.Contains(out, "NO ACTIVE SESSION") || !strings.Contains(out, "press r") {
		t.Fatalf("unexpected no-session view:\n%s", out)
	}
}

func TestRenderRespectsHeight(t *testing.T) {
	model := fixtureModel(80)
	model.Height = 12
	model.Logs = make([]string, 40)
	for i := range model.Logs {
		model.Logs[i] = "log line"
	}
	model.View = Logs
	out := Render(model)
	if got := len(strings.Split(out, "\n")); got > model.Height {
		t.Fatalf("rendered %d lines, height is %d", got, model.Height)
	}
}

func TestProgressBarClamps(t *testing.T) {
	if got := progressBar(10, -1); got != "░░░░░░░░░░" {
		t.Fatalf("negative progress = %q", got)
	}
	if got := progressBar(10, 2); got != "██████████" {
		t.Fatalf("overflow progress = %q", got)
	}
}

func fixtureModel(width int) Model {
	started := time.Date(2026, 8, 31, 8, 34, 41, 0, time.Local)
	return Model{
		Width:  width,
		Height: 32,
		View:   Home,
		Session: Session{
			InstanceID: 49337900,
			Status:     "READY",
			Model:      "Qwen3.8-27B",
			GPUModel:   "RTX 4090",
			Runtime:    "ninfer",
			Context:    172032,
			Profile:    "interactive",
			Rate:       0.387,
			Spent:      0.41,
			Exposure:   0.77,
			Started:    started,
			Deadline:   started.Add(2 * time.Hour),
			Elapsed:    64 * time.Minute,
			Remaining:  56 * time.Minute,
			Scheduled:  2 * time.Hour,
		},
		Health: Health{
			Endpoint:  "healthy · 519ms",
			Tunnel:    "running",
			Runtime:   "running",
			Watchdog:  "running",
			SSH:       "healthy",
			Refreshed: true,
		},
		GPU: GPU{
			Available:   true,
			Utilization: "100% load",
			VRAM:        "23.3 / 24.0 GB VRAM",
			Power:       "414 / 415 W",
			Temperature: "66 C",
		},
		Perf: Perf{
			Available: true,
			TTFT:      "2.68s",
			Total:     "5.20s",
			Decode:    "158.7 tok/s",
			Age:       "3m28s",
		},
	}
}
