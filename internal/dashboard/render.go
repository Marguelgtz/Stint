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
	Endpoint, Tunnel, Runtime, Watchdog, SSH string
	Refreshed, Refreshing                    bool
}

type GPU struct {
	Available                                    bool
	Utilization, VRAM, Power, Temperature, Error string
}

type Perf struct {
	Available, Benchmarking         bool
	TTFT, Total, Decode, Age, Error string
	PromptTokens, CompletionTokens  int
}

type Session struct {
	InstanceID                                int64
	Status, Model, GPUModel, Runtime, Profile string
	Context                                   int
	Rate, Spent, Exposure                     float64
	Started, Deadline                         time.Time
	Elapsed, Remaining, Scheduled             time.Duration
}

type Modal struct {
	Title string
	Lines []string
	Hint  string
}

type Model struct {
	Width, Height      int
	NoColor, NoSession bool
	View               View
	Session            Session
	Health             Health
	GPU                GPU
	Perf               Perf
	Logs               []string
	Notice, Error      string
	Modal              *Modal
}

type palette struct{ noColor bool }

func (p palette) wrap(code, s string) string {
	if p.noColor || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}
func (p palette) accent(s string) string  { return p.wrap("34;1", s) }
func (p palette) success(s string) string { return p.wrap("32;1", s) }
func (p palette) warn(s string) string    { return p.wrap("33;1", s) }
func (p palette) danger(s string) string  { return p.wrap("31;1", s) }
func (p palette) muted(s string) string   { return p.wrap("90", s) }
func (p palette) bold(s string) string    { return p.wrap("1", s) }

const gutter = 2

func Render(m Model) string {
	if m.Width <= 0 {
		m.Width = 100
	}
	if m.Height <= 0 {
		m.Height = 30
	}
	viewportWidth, viewportHeight := m.Width, m.Height
	m.Width = max(36, viewportWidth-gutter*2)
	p := palette{m.NoColor}

	var b strings.Builder
	b.WriteString(header(m, p))
	b.WriteString("\n\n")
	if m.NoSession {
		b.WriteString(p.bold("NO ACTIVE SESSION") + "\n\nNo paid compute is recorded in local Stint state.\n")
		b.WriteString(p.muted("Start a session from another shell, then press r to refresh."))
	} else {
		switch m.View {
		case Performance:
			b.WriteString(performanceView(m, p))
		case Config:
			b.WriteString(configView(m, p))
		case Logs:
			b.WriteString(logsView(m, p))
		default:
			b.WriteString(homeView(m, p))
		}
	}
	if m.Error != "" {
		b.WriteString("\n\n" + p.danger("ERROR  "+compact(m.Error, m.Width-8)))
	} else if m.Notice != "" {
		b.WriteString("\n\n" + p.accent(compact(m.Notice, m.Width-2)))
	}
	b.WriteString("\n\n" + footer(m, p))
	if m.Modal != nil {
		b.WriteString("\n\n" + modalView(*m.Modal, m.Width, p))
	}
	return trimHeight(addGutterAndClamp(b.String(), m.Width), viewportHeight)
}

func header(m Model, p palette) string {
	status := m.Session.Status
	if m.NoSession {
		status = "OFFLINE"
	}
	left := statusLabel(status, p)
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
	inner := max(16, m.Width-4)
	line := left
	if right != "" {
		gap := inner - visibleLen(left) - visibleLen(right)
		if gap >= 2 {
			line += strings.Repeat(" ", gap) + right
		} else {
			line = compact(left+"  "+right, inner)
		}
	}
	label := "┌─ STINT "
	top := p.accent(label) + strings.Repeat("─", max(0, m.Width-visibleLen(label)-1)) + p.accent("┐")
	mid := "│ " + padVisible(line, inner) + " │"
	bottom := p.accent("└") + strings.Repeat("─", max(0, m.Width-2)) + p.accent("┘")
	return top + "\n" + mid + "\n" + bottom
}

func homeView(m Model, p palette) string {
	var b strings.Builder
	if m.Width >= 84 {
		left := []string{p.bold(or(m.Session.Model, "unknown model")), or(m.Session.Runtime, "unknown runtime"), fmt.Sprintf("%d ctx", m.Session.Context)}
		mid := []string{p.bold(or(m.Session.GPUModel, "unknown GPU")), fmt.Sprintf("$%.3f/hr", m.Session.Rate), fmt.Sprintf("$%.2f spent est.", m.Session.Spent)}
		right := []string{p.bold("HEALTH"), "Endpoint  " + or(m.Health.Endpoint, "not refreshed"), "Runtime   " + or(m.Health.Runtime, "not refreshed")}
		b.WriteString(grid(m.Width, 3, left, mid, right))
	} else {
		fmt.Fprintf(&b, "%s  ·  %s\n%s  ·  %d ctx  ·  $%.3f/hr\nEndpoint %s  ·  Runtime %s",
			p.bold(or(m.Session.Model, "unknown")), or(m.Session.GPUModel, "unknown GPU"),
			or(m.Session.Runtime, "unknown runtime"), m.Session.Context, m.Session.Rate,
			or(m.Health.Endpoint, "not refreshed"), or(m.Health.Runtime, "not refreshed"))
	}

	b.WriteString("\n\n" + p.bold("SESSION") + "\n")
	progress := 0.0
	if m.Session.Scheduled > 0 {
		progress = float64(m.Session.Elapsed) / float64(m.Session.Scheduled)
	}
	dur := formatDuration(m.Session.Elapsed) + " / " + formatDuration(m.Session.Scheduled)
	barWidth := clamp(m.Width-visibleLen(dur)-3, 16, 52)
	fmt.Fprintf(&b, "%s  %s\n", progressBar(barWidth, progress), dur)
	started, deadline := "", ""
	if !m.Session.Started.IsZero() {
		started = "Started       " + m.Session.Started.Local().Format("15:04:05")
	}
	if !m.Session.Deadline.IsZero() {
		deadline = "Auto-destroy  " + m.Session.Deadline.Local().Format("15:04:05")
	}
	if started != "" && deadline != "" && m.Width >= 70 {
		b.WriteString(started + strings.Repeat(" ", max(3, m.Width-visibleLen(started)-visibleLen(deadline))) + deadline)
	} else {
		if started != "" {
			b.WriteString(started)
		}
		if started != "" && deadline != "" {
			b.WriteByte('\n')
		}
		if deadline != "" {
			b.WriteString(deadline)
		}
	}

	gpu := []string{p.bold("GPU")}
	if m.GPU.Available {
		gpu = append(gpu,
			row("Load", strings.TrimSpace(strings.TrimSuffix(m.GPU.Utilization, " load"))),
			row("VRAM", strings.TrimSpace(strings.TrimSuffix(m.GPU.VRAM, " VRAM"))),
			row("Power", m.GPU.Power), row("Temp", m.GPU.Temperature))
	} else if m.Health.Refreshed {
		gpu = append(gpu, p.muted("Unavailable"))
	} else {
		gpu = append(gpu, p.muted("Not refreshed yet"))
	}
	perf := []string{p.bold("PERFORMANCE")}
	if m.Perf.Available {
		perf = append(perf, row("Decode", m.Perf.Decode), row("TTFT", m.Perf.TTFT), row("Measured", m.Perf.Age+" ago"))
	} else if m.Perf.Benchmarking {
		perf = append(perf, p.accent("Benchmarking…"))
	} else {
		perf = append(perf, p.muted("No matching sample · press b to benchmark"))
	}
	b.WriteString("\n\n")
	if m.Width >= 76 {
		b.WriteString(twoCol(m.Width, gpu, perf))
	} else {
		b.WriteString(strings.Join(gpu, "\n") + "\n\n" + strings.Join(perf, "\n"))
	}
	return b.String()
}

func performanceView(m Model, p palette) string {
	var b strings.Builder
	b.WriteString(p.bold("PERFORMANCE") + "\n\n")
	if !m.Perf.Available {
		if m.Perf.Benchmarking {
			return b.String() + p.accent("Benchmarking active model…")
		}
		return b.String() + "No benchmark sample matches this instance/runtime/context.\n" + p.muted("Press b to run one explicit 1 × 128-token sample.")
	}
	rows := [][2]string{
		{"Decode", m.Perf.Decode}, {"TTFT", m.Perf.TTFT}, {"Total latency", m.Perf.Total},
		{"Prompt tokens", fmt.Sprint(m.Perf.PromptTokens)}, {"Output tokens", fmt.Sprint(m.Perf.CompletionTokens)},
		{"Sample age", m.Perf.Age}, {"Runtime", m.Session.Runtime}, {"Context", fmt.Sprint(m.Session.Context)},
	}
	for _, x := range rows {
		fmt.Fprintf(&b, "%-18s %s\n", x[0], x[1])
	}
	b.WriteString("\n" + p.muted("Benchmarks are never automatic. Press b to replace this sample."))
	return b.String()
}

func configView(m Model, p palette) string {
	var b strings.Builder
	b.WriteString(p.bold("CONFIG") + "\n\n")
	rows := [][2]string{
		{"Model", or(m.Session.Model, "unknown")}, {"Runtime", or(m.Session.Runtime, "unknown")},
		{"Profile", or(m.Session.Profile, "unknown")}, {"Context", fmt.Sprintf("%d tokens", m.Session.Context)},
		{"GPU", or(m.Session.GPUModel, "unknown")}, {"Instance", fmt.Sprint(m.Session.InstanceID)},
		{"Rate", fmt.Sprintf("$%.3f/hr", m.Session.Rate)}, {"Scheduled exposure", fmt.Sprintf("$%.2f", m.Session.Exposure)},
		{"Endpoint", "http://127.0.0.1:8409/v1"},
	}
	for _, x := range rows {
		fmt.Fprintf(&b, "%-22s %s\n", x[0], x[1])
	}
	b.WriteString("\n" + p.muted("Only authoritative session metadata is shown; runtime internals are not inferred."))
	return b.String()
}

func logsView(m Model, p palette) string {
	if len(m.Logs) == 0 {
		return p.bold("LOCAL LOGS") + "\n\n" + p.muted("No local log lines loaded. Press r to refresh.")
	}
	maxLines := max(4, m.Height-12)
	start := max(0, len(m.Logs)-maxLines)
	lines := []string{p.bold("LOCAL LOGS"), ""}
	for _, line := range m.Logs[start:] {
		lines = append(lines, compact(line, m.Width))
	}
	return strings.Join(lines, "\n")
}

func footer(m Model, p palette) string {
	nav := []string{"1 Home", "2 Performance", "3 Config", "4 Logs"}
	for i, v := range []View{Home, Performance, Config, Logs} {
		if m.View == v {
			nav[i] = p.accent("[" + nav[i] + "]")
		}
	}
	actions := []string{"r Refresh", "b Benchmark", "+ Extend", "- Shorten", "d Down", "q Exit"}
	return strings.Join(append(wrap(nav, m.Width), wrap(actions, m.Width)...), "\n")
}

func modalView(modal Modal, width int, p palette) string {
	w := clamp(width-8, 36, 72)
	lines := []string{p.accent("┌") + strings.Repeat("─", w-2) + p.accent("┐")}
	lines = append(lines, "│"+padVisible(p.bold(compact(" "+modal.Title+" ", w-2)), w-2)+"│")
	lines = append(lines, "├"+strings.Repeat("─", w-2)+"┤")
	for _, line := range modal.Lines {
		lines = append(lines, "│"+padVisible(" "+compact(line, w-4), w-2)+"│")
	}
	if modal.Hint != "" {
		lines = append(lines, "│"+padVisible(" "+p.muted(compact(modal.Hint, w-4)), w-2)+"│")
	}
	lines = append(lines, p.accent("└")+strings.Repeat("─", w-2)+p.accent("┘"))
	return strings.Join(lines, "\n")
}

func row(label, value string) string {
	if strings.TrimSpace(value) == "" {
		value = "—"
	}
	return fmt.Sprintf("%-10s %s", label, value)
}

func grid(width, gap int, cols ...[]string) string {
	usable := width - gap*(len(cols)-1)
	cw := usable / len(cols)
	widths := make([]int, len(cols))
	for i := range widths {
		widths[i] = cw
	}
	widths[len(widths)-1] += usable - cw*len(cols)
	return columns(widths, gap, cols...)
}

func twoCol(width int, a, b []string) string {
	first := (width - 3) / 2
	return columns([]int{first, width - 3 - first}, 3, a, b)
}

func columns(widths []int, gap int, cols ...[]string) string {
	rows := 0
	for _, c := range cols {
		if len(c) > rows {
			rows = len(c)
		}
	}
	var b strings.Builder
	for r := 0; r < rows; r++ {
		for i, c := range cols {
			s := ""
			if r < len(c) {
				s = c[r]
			}
			b.WriteString(padVisible(compact(s, widths[i]), widths[i]))
			if i < len(cols)-1 {
				b.WriteString(strings.Repeat(" ", gap))
			}
		}
		if r < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func wrap(items []string, width int) []string {
	var out []string
	line := ""
	for _, item := range items {
		candidate := item
		if line != "" {
			candidate = line + "   " + item
		}
		if line != "" && visibleLen(candidate) > width {
			out = append(out, line)
			line = item
		} else {
			line = candidate
		}
	}
	if line != "" {
		out = append(out, compact(line, width))
	}
	return out
}

func addGutterAndClamp(s string, contentWidth int) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.TrimSpace(stripANSI(line)) == "" {
			lines[i] = ""
			continue
		}
		lines[i] = strings.Repeat(" ", gutter) + compact(line, contentWidth)
	}
	return strings.Join(lines, "\n")
}

func statusLabel(status string, p palette) string {
	switch s := strings.ToUpper(strings.TrimSpace(status)); s {
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
	lines := strings.Split(s, "\n")
	if height <= 0 || len(lines) <= height {
		return s
	}
	return strings.Join(lines[:height], "\n")
}

func compact(s string, width int) string {
	if width <= 0 || visibleLen(s) <= width {
		return s
	}
	if width <= 3 {
		return compactPlain(stripANSI(s), width)
	}
	return compactPlain(stripANSI(s), width-1) + "…"
}
func compactPlain(s string, width int) string {
	r := []rune(s)
	if width <= 0 {
		return ""
	}
	if len(r) <= width {
		return s
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
	esc := false
	for _, r := range s {
		if r == '\x1b' {
			esc = true
			continue
		}
		if esc {
			if r == 'm' {
				esc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
func or(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
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
