// Package deepdashboard renders the terminal cockpit for one Deep Work session.
// It receives a projection of durable coordinator state plus passive observations;
// it never reads files, opens network connections, or changes lifecycle state.
package deepdashboard

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type View int

const (
	Run View = iota
	Tasks
	Activity
	WorkerView
)

type Task struct {
	ID, Objective, Status, LastResult, Blocker string
	Attempts                                   int
}

type Event struct {
	Time, Kind, Task, Detail string
}

type Compute struct {
	Available                      bool
	Status, Endpoint, Runtime, GPU string
	Utilization, VRAM, Lane, Error string
}

type Worker struct {
	Observed, Reachable                                bool
	NInferContext, NInferKV, NInferDefaultMaxTokens    int
	XHighRequests, MediumRequests                      int
	LatestPhase, LatestAt, Compression, CompressionAt  string
	CompressionCompleted, CompressionFailed, Truncated int
	Error                                              string
}

type Modal struct {
	Title string
	Lines []string
	Hint  string
}

type Model struct {
	Width, Height int
	NoColor       bool
	View          View
	SessionID     string
	Mission       string
	Phase         string
	WorkerName    string
	Coordinator   string
	StartedAt     time.Time
	Deadline      time.Time
	LandBefore    time.Time
	Now           time.Time
	Tasks         []Task
	ActiveSince   *time.Time
	Events        []Event
	Compute       Compute
	Worker        Worker
	Error, Notice string
	Modal         *Modal
}

type palette struct{ noColor bool }

func (p palette) wrap(code, value string) string {
	if p.noColor || value == "" {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}
func (p palette) accent(value string) string  { return p.wrap("34;1", value) }
func (p palette) success(value string) string { return p.wrap("32;1", value) }
func (p palette) warn(value string) string    { return p.wrap("33;1", value) }
func (p palette) danger(value string) string  { return p.wrap("31;1", value) }
func (p palette) muted(value string) string   { return p.wrap("90", value) }
func (p palette) bold(value string) string    { return p.wrap("1", value) }

func Render(m Model) string {
	if m.Width <= 0 {
		m.Width = 100
	}
	if m.Height <= 0 {
		m.Height = 30
	}
	viewportWidth, viewportHeight := m.Width, m.Height
	m.Width = max(36, viewportWidth-4)
	p := palette{noColor: m.NoColor}

	var body string
	switch m.View {
	case Tasks:
		body = tasksView(m, p)
	case Activity:
		body = activityView(m, p)
	case WorkerView:
		body = workerView(m, p)
	default:
		body = runView(m, p)
	}
	if m.Error != "" {
		body += "\n\n" + p.danger("ERROR  "+compact(m.Error, m.Width-8))
	} else if m.Notice != "" {
		body += "\n\n" + p.accent(compact(m.Notice, m.Width))
	}
	output := header(m, p) + "\n\n" + body + "\n\n" + footer(m, p)
	if m.Modal != nil {
		output += "\n\n" + modalView(*m.Modal, m.Width, p)
	}
	return trimHeight(addGutter(output, m.Width), viewportHeight)
}

func header(m Model, p palette) string {
	left := status(m.Phase, p)
	right := ""
	if !m.LandBefore.IsZero() {
		right = formatDuration(remaining(m.LandBefore, m.Now)) + " to landing"
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
	label := "┌─ DEEP WORK "
	return p.accent(label) + strings.Repeat("─", max(0, m.Width-visibleLen(label)-1)) + p.accent("┐") + "\n" +
		"│ " + pad(line, inner) + " │\n" +
		p.accent("└") + strings.Repeat("─", max(0, m.Width-2)) + p.accent("┘")
}

func runView(m Model, p palette) string {
	var b strings.Builder
	worker := or(m.WorkerName, "unknown worker")
	if m.Compute.Available {
		worker += " · compute " + or(m.Compute.Status, "unknown")
	} else {
		worker += " · compute not recorded"
	}
	fmt.Fprintf(&b, "%s  %s\n", p.bold(or(m.Mission, "unnamed mission")), p.muted("session "+m.SessionID))
	fmt.Fprintf(&b, "Worker     %s\n", worker)
	fmt.Fprintf(&b, "Coordinator %s\n", or(m.Coordinator, "unknown"))

	active := activeTask(m.Tasks)
	next := nextTask(m.Tasks)
	b.WriteString("\n" + p.bold("NOW") + "        ")
	if active == nil {
		b.WriteString(p.muted("no active task"))
	} else {
		line := fmt.Sprintf("%s  attempt %d", active.ID, active.Attempts)
		if m.ActiveSince != nil {
			line += "  ·  " + formatDuration(elapsed(*m.ActiveSince, m.Now)) + " running"
		}
		b.WriteString(line)
	}
	b.WriteString("\nNEXT       ")
	if next == nil {
		b.WriteString(p.muted("no queued task"))
	} else {
		b.WriteString(next.ID + "  " + compact(next.Objective, max(12, m.Width-22)))
	}
	verified, queued, blocked, incomplete := taskCounts(m.Tasks)
	fmt.Fprintf(&b, "\nPROGRESS   %d verified · %d active/retry · %d queued · %d blocked", verified, incomplete, queued, blocked)

	b.WriteString("\n\n" + p.bold("COMPUTE") + "    ")
	if !m.Compute.Available {
		b.WriteString(p.muted("not available"))
	} else {
		parts := []string{or(m.Compute.Status, "unknown"), or(m.Compute.Endpoint, "endpoint unknown"), or(m.Compute.Runtime, "runtime unknown")}
		if m.Compute.Utilization != "" {
			parts = append(parts, m.Compute.Utilization)
		}
		b.WriteString(strings.Join(parts, " · "))
	}
	b.WriteString("\n" + p.bold("CONTEXT") + "   " + contextLine(m.Worker, p))
	return b.String()
}

func tasksView(m Model, p palette) string {
	lines := []string{p.bold("TASKS"), ""}
	if len(m.Tasks) == 0 {
		return strings.Join(append(lines, p.muted("No tasks in durable Deep Work state.")), "\n")
	}
	for _, task := range m.Tasks {
		label := taskStatus(task.Status, p)
		line := fmt.Sprintf("%-12s %-11s %d  %s", task.ID, label, task.Attempts, compact(task.Objective, max(12, m.Width-32)))
		lines = append(lines, line)
		if task.Blocker != "" {
			lines = append(lines, "             "+p.danger("blocker: ")+compact(task.Blocker, max(12, m.Width-25)))
		} else if task.LastResult != "" && (task.Status == "incomplete" || task.Status == "verified") {
			lines = append(lines, "             "+p.muted(compact(task.LastResult, max(12, m.Width-14))))
		}
	}
	return strings.Join(lines, "\n")
}

func activityView(m Model, p palette) string {
	lines := []string{p.bold("ACTIVITY"), ""}
	if len(m.Events) == 0 {
		return strings.Join(append(lines, p.muted("No coordinator incidents recorded yet.")), "\n")
	}
	maxLines := max(4, m.Height-11)
	start := max(0, len(m.Events)-maxLines)
	for i := len(m.Events) - 1; i >= start; i-- {
		e := m.Events[i]
		prefix := e.Time
		if e.Task != "" {
			prefix += " " + e.Task
		}
		line := prefix + "  " + eventLabel(e.Kind, p)
		if e.Detail != "" {
			line += "  " + compact(e.Detail, max(12, m.Width-visibleLen(prefix)-18))
		}
		lines = append(lines, compact(line, m.Width))
	}
	return strings.Join(lines, "\n")
}

func workerView(m Model, p palette) string {
	var b strings.Builder
	b.WriteString(p.bold("GPU WORKER") + "\n\n")
	if !m.Compute.Available {
		b.WriteString(p.muted("Compute telemetry unavailable; durable Deep Work state remains readable."))
	} else {
		fmt.Fprintf(&b, "Compute          %s · %s · %s\n", or(m.Compute.Status, "unknown"), or(m.Compute.Endpoint, "endpoint unknown"), or(m.Compute.Runtime, "runtime unknown"))
		fmt.Fprintf(&b, "Live inference   %s\n", or(m.Compute.Lane, "no lane observation"))
	}
	if !m.Worker.Observed {
		b.WriteString("\n" + p.muted("Worker observer has not returned a sample yet."))
		if m.Worker.Error != "" {
			b.WriteString("\n" + p.danger(compact(m.Worker.Error, m.Width)))
		}
		return b.String()
	}
	fmt.Fprintf(&b, "\nNInfer           context %d · KV %d · completion budget %d\n", m.Worker.NInferContext, m.Worker.NInferKV, m.Worker.NInferDefaultMaxTokens)
	fmt.Fprintf(&b, "Phase routes      xhigh %d · medium %d", m.Worker.XHighRequests, m.Worker.MediumRequests)
	if m.Worker.LatestPhase != "" {
		fmt.Fprintf(&b, " · latest %s", m.Worker.LatestPhase)
	}
	b.WriteString("\nCompression       " + contextLine(m.Worker, p))
	if m.Worker.Error != "" {
		b.WriteString("\n" + p.danger(compact(m.Worker.Error, m.Width)))
	}
	return b.String()
}

func footer(m Model, p palette) string {
	nav := []string{"1 Run", "2 Tasks", "3 Activity", "4 Worker"}
	for i, view := range []View{Run, Tasks, Activity, WorkerView} {
		if m.View == view {
			nav[i] = p.accent("[" + nav[i] + "]")
		}
	}
	return strings.Join(wrap(append(nav, "r Refresh", "s Land", "q Exit"), m.Width), "\n")
}

func modalView(modal Modal, width int, p palette) string {
	w := clamp(width-8, 36, 72)
	lines := []string{p.accent("┌") + strings.Repeat("─", w-2) + p.accent("┐")}
	lines = append(lines, "│"+pad(p.bold(compact(" "+modal.Title+" ", w-2)), w-2)+"│")
	lines = append(lines, "├"+strings.Repeat("─", w-2)+"┤")
	for _, line := range modal.Lines {
		lines = append(lines, "│"+pad(" "+compact(line, w-4), w-2)+"│")
	}
	if modal.Hint != "" {
		lines = append(lines, "│"+pad(" "+p.muted(compact(modal.Hint, w-4)), w-2)+"│")
	}
	lines = append(lines, p.accent("└")+strings.Repeat("─", w-2)+p.accent("┘"))
	return strings.Join(lines, "\n")
}

func contextLine(worker Worker, p palette) string {
	switch worker.Compression {
	case "completed":
		return p.success("completed")
	case "running":
		return p.accent("running")
	case "failed", "truncated":
		return p.danger(worker.Compression)
	case "not_observed", "":
		return p.muted("not observed")
	default:
		return p.warn(worker.Compression)
	}
}

func status(value string, p palette) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "EXECUTING":
		return p.success("● EXECUTING")
	case "LANDING":
		return p.warn("● LANDING")
	case "LANDED", "STOPPED":
		return p.accent("● " + strings.ToUpper(value))
	case "":
		return p.muted("● UNKNOWN")
	default:
		return p.danger("● " + strings.ToUpper(value))
	}
}

func taskStatus(value string, p palette) string {
	switch value {
	case "verified":
		return p.success(value)
	case "active":
		return p.accent(value)
	case "blocked", "needs_human":
		return p.danger(value)
	case "incomplete":
		return p.warn(value)
	default:
		return p.muted(or(value, "queued"))
	}
}

func eventLabel(value string, p palette) string {
	if strings.Contains(value, "error") || strings.Contains(value, "fail") {
		return p.danger(value)
	}
	if strings.Contains(value, "verify") || strings.Contains(value, "land") {
		return p.accent(value)
	}
	return p.muted(value)
}

func activeTask(tasks []Task) *Task {
	for i := range tasks {
		if tasks[i].Status == "active" {
			return &tasks[i]
		}
	}
	return nil
}

func nextTask(tasks []Task) *Task {
	for i := range tasks {
		if tasks[i].Status == "queued" || tasks[i].Status == "incomplete" {
			return &tasks[i]
		}
	}
	return nil
}

func taskCounts(tasks []Task) (verified, queued, blocked, working int) {
	for _, task := range tasks {
		switch task.Status {
		case "verified":
			verified++
		case "queued":
			queued++
		case "blocked", "needs_human":
			blocked++
		case "active", "incomplete":
			working++
		}
	}
	return
}

func elapsed(then, now time.Time) time.Duration {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if now.Before(then) {
		return 0
	}
	return now.Sub(then)
}

func remaining(then, now time.Time) time.Duration {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if then.Before(now) {
		return 0
	}
	return then.Sub(now)
}

func formatDuration(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	value = value.Round(time.Second)
	if value >= time.Hour {
		return fmt.Sprintf("%dh %02dm", value/time.Hour, (value%time.Hour)/time.Minute)
	}
	if value >= time.Minute {
		return fmt.Sprintf("%dm %02ds", value/time.Minute, (value%time.Second)/time.Second)
	}
	return fmt.Sprintf("%ds", value/time.Second)
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

func addGutter(value string, width int) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		if strings.TrimSpace(stripANSI(line)) == "" {
			lines[i] = ""
			continue
		}
		lines[i] = "  " + compact(line, width)
	}
	return strings.Join(lines, "\n")
}

func trimHeight(value string, height int) string {
	lines := strings.Split(value, "\n")
	if height <= 0 || len(lines) <= height {
		return value
	}
	return strings.Join(lines[:height], "\n")
}

func compact(value string, width int) string {
	if width <= 0 || visibleLen(value) <= width {
		return value
	}
	if width <= 3 {
		return string([]rune(stripANSI(value))[:width])
	}
	runes := []rune(stripANSI(value))
	return string(runes[:width-1]) + "…"
}

func pad(value string, width int) string {
	if n := visibleLen(value); n < width {
		return value + strings.Repeat(" ", width-n)
	}
	return value
}

func visibleLen(value string) int { return len([]rune(stripANSI(value))) }

func stripANSI(value string) string {
	var b strings.Builder
	escaping := false
	for _, r := range value {
		if r == '\x1b' {
			escaping = true
			continue
		}
		if escaping {
			if r == 'm' {
				escaping = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func or(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func clamp(value, low, high int) int {
	return int(math.Max(float64(low), math.Min(float64(high), float64(value))))
}
