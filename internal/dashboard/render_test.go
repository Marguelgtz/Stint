package dashboard

import (
	"strings"
	"testing"
	"time"
)

func TestRenderHomeAcrossWidths(t *testing.T) {
	for _, width := range []int{60, 80, 100, 140} {
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
	started := time.Date(2026, 8, 31, 8, 0, 0, 0, time.UTC)
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
			Rate:       0.392,
			Spent:      0.21,
			Exposure:   0.39,
			Started:    started,
			Deadline:   started.Add(time.Hour),
			Elapsed:    20 * time.Minute,
			Remaining:  40 * time.Minute,
			Scheduled:  time.Hour,
		},
		Health: Health{Endpoint: "healthy · 38ms", Tunnel: "running", Runtime: "running", Watchdog: "running", SSH: "healthy", Refreshed: true},
		GPU: GPU{Available: true, Utilization: "87% load", VRAM: "21.4 / 24.0 GB VRAM", Power: "366 / 450 W", Temperature: "69 C"},
		Perf: Perf{Available: true, TTFT: "1.80s", Total: "3.20s", Decode: "142.0 tok/s", Age: "3m"},
	}
}
