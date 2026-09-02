package dashboard

import (
	"fmt"
	"math"
	"strings"
)

var residentContextColors = []string{"31;1", "36;1", "35;1", "33;1", "32;1", "34;1"}

func sessionProgressView(m Model, p palette) string {
	var b strings.Builder
	b.WriteString("\n\n" + p.bold("SESSION") + "\n")

	timeProgress := 0.0
	if m.Session.Scheduled > 0 {
		timeProgress = float64(m.Session.Elapsed) / float64(m.Session.Scheduled)
	}
	duration := formatDuration(m.Session.Elapsed) + " / " + formatDuration(m.Session.Scheduled)
	contextSummary := contextUsageSummary(m)
	labelWidth := max(visibleLen(duration), visibleLen(contextSummary))
	barWidth := clamp(m.Width-labelWidth-3, 16, 52)

	fmt.Fprintf(&b, "%s  %s\n", progressBar(barWidth, timeProgress), duration)
	fmt.Fprintf(&b, "%s  %s", contextBar(barWidth, m, p), contextSummary)
	if len(m.Inference.Clients) > 0 && m.Inference.Available {
		legend := contextLegend(m, p)
		if legend != "" {
			b.WriteString("\n" + legend)
		}
	}

	started, deadline := "", ""
	if !m.Session.Started.IsZero() {
		started = "Started       " + m.Session.Started.Local().Format("15:04:05")
	}
	if !m.Session.Deadline.IsZero() {
		deadline = "Auto-destroy  " + m.Session.Deadline.Local().Format("15:04:05")
	}
	if started != "" || deadline != "" {
		b.WriteByte('\n')
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
	return b.String()
}

func contextUsageSummary(m Model) string {
	if !m.Inference.Refreshed {
		return "context awaiting refresh"
	}
	if !m.Inference.Available {
		return "context unavailable"
	}
	if m.Session.Context <= 0 {
		return formatTokenCount(m.Inference.ContextUsed) + " resident ctx"
	}
	percent := int(math.Round(100 * float64(m.Inference.ContextUsed) / float64(m.Session.Context)))
	if percent < 0 {
		percent = 0
	}
	return fmt.Sprintf("%s / %s ctx · %d%% resident", formatTokenCount(m.Inference.ContextUsed), formatTokenCount(m.Session.Context), percent)
}

func contextBar(width int, m Model, p palette) string {
	if width <= 0 {
		return ""
	}
	if !m.Inference.Refreshed || !m.Inference.Available || m.Session.Context <= 0 {
		return p.muted(strings.Repeat("░", width))
	}

	capacity := m.Session.Context
	consumedTokens := 0
	consumedCells := 0
	var b strings.Builder
	for _, resident := range m.Inference.Clients {
		if resident.Tokens <= 0 || consumedTokens >= capacity {
			continue
		}
		nextTokens := consumedTokens + resident.Tokens
		if nextTokens > capacity {
			nextTokens = capacity
		}
		nextCells := int(math.Round(float64(width) * float64(nextTokens) / float64(capacity)))
		cells := max(0, nextCells-consumedCells)
		if cells > 0 {
			b.WriteString(p.wrap(residentContextColor(resident.Key), strings.Repeat("█", cells)))
		}
		consumedTokens = nextTokens
		consumedCells = nextCells
	}
	if consumedCells < width {
		b.WriteString(p.muted(strings.Repeat("░", width-consumedCells)))
	}
	return b.String()
}

func contextLegend(m Model, p palette) string {
	items := make([]string, 0, len(m.Inference.Clients))
	for _, resident := range m.Inference.Clients {
		if resident.Tokens <= 0 {
			continue
		}
		share := ""
		if m.Session.Context > 0 {
			share = fmt.Sprintf(" · %.0f%%", 100*float64(resident.Tokens)/float64(m.Session.Context))
		}
		item := fmt.Sprintf("● %s  %s%s", resident.Label, formatTokenCount(resident.Tokens), share)
		items = append(items, p.wrap(residentContextColor(resident.Key), item))
	}
	return strings.Join(wrap(items, m.Width), "\n")
}

func residentContextColor(key string) string {
	if len(residentContextColors) == 0 {
		return ""
	}
	var hash uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= 16777619
	}
	return residentContextColors[int(hash%uint32(len(residentContextColors)))]
}

func formatTokenCount(tokens int) string {
	if tokens < 0 {
		tokens = 0
	}
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}
