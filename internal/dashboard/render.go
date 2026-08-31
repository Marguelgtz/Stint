package dashboard

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type View int

const (
	Home View = iota
	Performance
	Config
	Logs
)

type Health struct {
	Endpoint   string
	Tunnel     string
	Runtime    string
	Watchdog   string
	SSH        string
	Refreshed  bool
	Refreshing bool
}

type GPU struct {
	Available   bool
	Utilization string
	VRAM        string
	Power       string
	Temperature string
	Error       string
}

type Perf struct {
	Available        bool
	TTFT             string
	Total            string
	Decode           string
	PromptTokens     int
	CompletionTokens int
	Age              string
	Error            string
	Benchmarking     bool
}

type Session struct {
	InstanceID int64
	Status     string
	Model      string
	GPUModel   string
	Runtime    string
	Context    int
	Profile    string
	Rate       float64
	Spent      float64
	Exposure   float64
	Started    time.Time
	Deadline   time.Time
	Elapsed    time.Duration
	Remaining  time.Duration
	Scheduled  time.Duration
}

type Modal struct {
	Title string
	Lines []string
	Hint  string
}

type Model struct {
	Width      int
	Height     int
	NoColor    bool
	NoSession  bool
	View       View
	Session    Session
	Health     Health
	GPU        GPU
	Perf       Perf
	Logs       []string
	Notice     string
	Error      string
	Modal      *Modal
}

type palette struct {
	noColor bool
}

func (p palette) wrap(code, text string) string {
	if p.noColor || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (p palette) accent(s string) string  { return p.wrap("34;1", s) }
func (p palette) success(s string) string { return p.wrap("32;1", s) }
func (p palette) warn(s string) string    { return p.wrap("33;1", s) }
func (p palette) danger(s string) string  { return p.wrap("31;1", s) }
func (p palette) muted(s string) string   { return p.wrap("90", s) }
func (p palette) bold(s string) string    { return p.wrap("1", s) }

func Render(m Model) string {
	if m.Width <= 0 {
		m.Width = 100
	}
	if m.Height <= 0 {
		m.Height = 30
	}
	p := palette{noColor: m.NoColor}
	var b strings.Builder
	b.WriteString(renderHeader(m, p))
	b.WriteByte('\n')

	if m.NoSession {
		b.WriteString("\n")
		b.WriteString(p.bold("NO ACTIVE SESSION"))
		b.WriteString("\n\nNo paid compute is recorded in local Stint state.\n")
		b.WriteString(p.muted("Start a session from another shell, then press r to refresh."))
		b.WriteString("\n")
	} else {
		switch m.View {
		case Performance:
			b.WriteString(renderPerformance(m, p))
		case Config:
			b.WriteString(renderConfig(m, p))
		case Logs:
			b.WriteString(renderLogs(m, p))
		default:
			b.WriteString(renderHome(m, p))
		}
	}

	if m.Error != "" {
		b.WriteString("\n")
		b.WriteString(p.danger("ERROR  " + compact(m.Error, max(20, m.Width-8))))
		b.WriteString("\n")
	} else if m.Notice != "" {
		b.WriteString("\n")
		b.WriteString(p.accent(compact(m.Notice, max(20, m.Width-2))))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(renderFooter(m, p))
	if m.Modal != nil {
		b.WriteString("\n\n")
		b.WriteString(renderModal(*m.Modal, m.Width, p))
	}
	return trimHeight(b.String(), m.Height)
}

func renderHeader(m Model, p palette) string {
	status := m.Session.Status
	if m.NoSession {
		status = "OFFLINE"
	}
	state := statusLabel(status, p)
	right := ""
	if !m.NoSession {
		right = formatDuration(m.Session.Remaining) + " remaining"
	}
	if m.Health.Refreshing {
		if right != "" {
			right += "  ·  "
		}
		right += "refreshing"
	}
	innerWidth := max(20, m.Width-4)
	left := "STINT  " + state
	gap := innerWidth - visibleLen(left) - visibleLen(right)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	if visibleLen(line) > innerWidth {
		line = compactPlain(line, innerWidth)
	}
	return p.accent("┌─ STINT ") + strings.Repeat("─", max(0, m.Width-10)) + "┐\n" +
		"│ " + padVisible(line, max(1, m.Width-4)) + " │\n" +
		p.accent("└") + strings.Repeat("─", max(0, m.Width-2)) + p.accent("┘")
}

func renderHome(m Model, p palette) string {
	var b strings.Builder
	if m.Width >= 100 {
		left := []string{
			p.bold(valueOr(m.Session.Model, "unknown model")),
			valueOr(m.Session.Runtime, "unknown runtime"),
			fmt.Sprintf("%,d ctx", m.Session.Context),
		}
		mid := []string{
			p.bold(valueOr(m.Session.GPUModel, "unknown GPU")),
			fmt.Sprintf("$%.3f/hr", m.Session.Rate),
			fmt.Sprintf("$%.2f spent est.", m.Session.Spent),
		}
		right := []string{
			p.bold("HEALTH"),
			"Endpoint " + m.Health.Endpoint,
			"Runtime  " + m.Health.Runtime,
		}
		b.WriteString(columns(m.Width, left, mid, right))
		b.WriteString("\n\n")
	} else {
		fmt.Fprintf(&b, "%s  ·  %s  ·  %s\n", p.bold(valueOr(m.Session.Model, "unknown")), valueOr(m.Session.GPUModel, "unknown GPU"), valueOr(m.Session.Runtime, "unknown runtime"))
		fmt.Fprintf(&b, "Context %,d  ·  $%.3f/hr  ·  $%.2f spent est.\n\n", m.Session.Context, m.Session.Rate, m.Session.Spent)
	}

	b.WriteString(p.bold("SESSION"))
	b.WriteByte('\n')
	barWidth := clamp(m.Width-26, 16, 54)
	progress := 0.0
	if m.Session.Scheduled > 0 {
		progress = float64(m.Session.Elapsed) / float64(m.Session.Scheduled)
	}
	fmt.Fprintf(&b, "%s  %s / %s\n", progressBar(barWidth, progress), formatDuration(m.Session.Elapsed), formatDuration(m.Session.Scheduled))
	if !m.Session.Started.IsZero() {
		fmt.Fprintf(&b, "Started       %s\n", m.Session.Started.Local().Format("15:04:05"))
	}
	if !m.Session.Deadline.IsZero() {
		fmt.Fprintf(&b, "Auto-destroy  %s\n", m.Session.Deadline.Local().Format("15:04:05"))
	}

	b.WriteString("\n")
	b.WriteString(p.bold("GPU"))
	b.WriteByte('\n')
	if m.GPU.Available {
		parts := nonEmpty(m.GPU.Utilization, m.GPU.VRAM, m.GPU.Power, m.GPU.Temperature)
		b.WriteString(strings.Join(parts, "  ·  "))
		b.WriteByte('\n')
	} else if m.Health.Refreshed {
		b.WriteString(p.muted("Unavailable"))
		if m.GPU.Error != "" {
			b.WriteString(" · " + compact(m.GPU.Error, max(20, m.Width-18)))
		}
		b.WriteByte('\n')
	} else {
		b.WriteString(p.muted("Not refreshed yet"))
		b.WriteByte('\n')
	}

	b.WriteString("\n")
	b.WriteString(p.bold("PERFORMANCE"))
	b.WriteByte('\n')
	if m.Perf.Available {
		fmt.Fprintf(&b, "Decode %s  ·  TTFT %s  ·  measured %s ago\n", m.Perf.Decode, m.Perf.TTFT, m.Perf.Age)
	} else if m.Perf.Benchmarking {
		b.WriteString(p.accent("Benchmarking…"))
		b.WriteByte('\n')
	} else {
		b.WriteString(p.muted("No matching benchmark sample · press b to benchmark"))
		b.WriteByte('\n')
	}
	return b.String()
}

func renderPerformance(m Model, p palette) string {
	var b strings.Builder
	b.WriteString(p.bold("PERFORMANCE"))
	b.WriteString("\n\n")
	if !m.Perf.Available {
		if m.Perf.Benchmarking {
			b.WriteString(p.accent("Benchmarking active model…\n"))
		} else {
			b.WriteString("No benchmark sample matches this instance/runtime/context.\n")
			b.WriteString(p.muted("Press b to run one explicit 1 × 128-token sample."))
			b.WriteByte('\n')
		}
		return b.String()
	}
	rows := [][2]string{
		{"Decode", m.Perf.Decode},
		{"TTFT", m.Perf.TTFT},
		{"Total latency", m.Perf.Total},
		{"Prompt tokens", fmt.Sprintf("%d", m.Perf.PromptTokens)},
		{"Output tokens", fmt.Sprintf("%d", m.Perf.CompletionTokens)},
		{"Sample age", m.Perf.Age},
		{"Runtime", m.Session.Runtime},
		{"Context", fmt.Sprintf("%,d", m.Session.Context)},
	}
	for _, row := range rows {
		fmt.Fprintf(&b, "%-18s %s\n", row[0], row[1])
	}
	b.WriteString("\n")
	b.WriteString(p.muted("Benchmarks are never automatic. Press b to replace this sample."))
	b.WriteByte('\n')
	return b.String()
}

func renderConfig(m Model, p palette) string {
	var b strings.Builder
	b.WriteString(p.bold("CONFIG"))
	b.WriteString("\n\n")
	rows := [][2]string{
		{"Model", valueOr(m.Session.Model, "unknown")},
		{"Runtime", valueOr(m.Session.Runtime, "unknown")},
		{"Profile", valueOr(m.Session.Profile, "unknown")},
		{"Context", fmt.Sprintf("%,d tokens", m.Session.Context)},
		{"GPU", valueOr(m.Session.GPUModel, "unknown")},
		{"Instance", fmt.Sprintf("%d", m.Session.InstanceID)},
		{"Rate", fmt.Sprintf("$%.3f/hr", m.Session.Rate)},
		{"Scheduled exposure", fmt.Sprintf("$%.2f", m.Session.Exposure)},
		{"Endpoint", "http://127.0.0.1:8409/v1"},
	}
	for _, row := range rows {
		fmt.Fprintf(&b, "%-22s %s\n", row[0], row[1])
	}
	b.WriteString("\n")
	b.WriteString(p.muted("Only authoritative session metadata is shown; runtime internals are not inferred."))
	b.WriteByte('\n')
	return b.String()
}

func renderLogs(m Model, p palette) string {
	var b strings.Builder
	b.WriteString(p.bold("LOCAL LOGS"))
	b.WriteString("\n\n")
	if len(m.Logs) == 0 {
		b.WriteString(p.muted("No local log lines loaded. Press r to refresh."))
		b.WriteByte('\n')
		return b.String()
	}
	maxLines := max(4, m.Height-12)
	start := 0
	if len(m.Logs) > maxLines {
		start = len(m.Logs) - maxLines
	}
	for _, line := range m.Logs[start:] {
		b.WriteString(compact(line, max(20, m.Width-2)))
		b.WriteByte('\n')
	}
	return b.String()
}

func renderFooter(m Model, p palette) string {
	views := []struct {
		key  string
		name string
		view View
	}{{"1", "Home", Home}, {"2", "Performance", Performance}, {"3", "Config", Config}, {"4", "Logs", Logs}}
	parts := make([]string, 0, len(views))
	for _, item := range views {
		label := item.key + " " + item.name
		if m.View == item.view {
			label = p.accent("[" + label + "]")
		}
		parts = append(parts, label)
	}
	first := strings.Join(parts, "   ")
	second := "r Refresh   b Benchmark   + Extend   - Shorten   d Down   q Exit"
	if m.Width < 92 {
		return first + "\n" + second
	}
	return first + strings.Repeat(" ", max(2, m.Width-visibleLen(first)-visibleLen(second))) + second
}

func renderModal(modal Modal, width int, p palette) string {
	boxWidth := clamp(width-8, 36, 72)
	var b strings.Builder
	b.WriteString(p.accent("┌") + strings.Repeat("─", boxWidth-2) + p.accent("┐") + "\n")
	title := " " + modal.Title + " "
	b.WriteString("│" + padVisible(p.bold(compact(title, boxWidth-2)), boxWidth-2) + "│\n")
	b.WriteString("├" + strings.Repeat("─", boxWidth-2) + "┤\n")
	for _, line := range modal.Lines {
		b.WriteString("│" + padVisible(" "+compact(line, boxWidth-4), boxWidth-2) + "│\n")
	}
	if modal.Hint != "" {
		b.WriteString("│" + padVisible(" "+p.muted(compact(modal.Hint, boxWidth-4)), boxWidth-2) + "│\n")
	}
	b.WriteString(p.accent("└") + strings.Repeat("─", boxWidth-2) + p.accent("┘"))
	return b.String()
}

func statusLabel(status string, p palette) string {
	s := strings.ToUpper(strings.TrimSpace(status))
	switch s {
	case "READY":
		return p.success("● READY")
	case "RECOVERABLE":
		return p.warn("● RECOVERABLE")
	case "EXPIRED", "OFFLINE":
		return p.danger("● " + s)
	case "":
		return p.muted("● UNKNOWN")
	default:
		return p.accent("● " + s)
	}
}

func progressBar(width int, progress float64) string {
	progress = math.Max(0, math.Min(1, progress))
	filled := int(math.Round(float64(width) * progress))
	return strings.Repeat("█", filled) + strings.Repeat("░", max(0, width-filled))
}

func columns(width int, cols ...[]string) string {
	if len(cols) == 0 {
		return ""
	}
	gap := 3
	colWidth := max(12, (width-gap*(len(cols)-1))/len(cols))
	maxRows := 0
	for _, c := range cols {
		if len(c) > maxRows {
			maxRows = len(c)
		}
	}
	var b strings.Builder
	for row := 0; row < maxRows; row++ {
		for i, c := range cols {
			text := ""
			if row < len(c) {
				text = c[row]
			}
			b.WriteString(padVisible(compact(text, colWidth), colWidth))
			if i < len(cols)-1 {
				b.WriteString(strings.Repeat(" ", gap))
			}
		}
		if row < maxRows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd %dh", d/(24*time.Hour), (d%(24*time.Hour))/time.Hour)
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh %02dm", d/time.Hour, (d%time.Hour)/time.Minute)
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm %02ds", d/time.Minute, (d%time.Minute)/time.Second)
	}
	return fmt.Sprintf("%ds", d/time.Second)
}

func trimHeight(s string, height int) string {
	if height <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= height {
		return s
	}
	return strings.Join(lines[:height], "\n")
}

func compact(s string, width int) string {
	if width <= 0 || visibleLen(s) <= width {
		return s
	}
	if width <= 3 {
		return compactPlain(s, width)
	}
	plain := stripANSI(s)
	return compactPlain(plain, width-1) + "…"
}

func compactPlain(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 0 {
		return ""
	}
	return string(r[:width])
}

func padVisible(s string, width int) string {
	if n := visibleLen(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

func visibleLen(s string) int { return len([]rune(stripANSI(s))) }

func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
