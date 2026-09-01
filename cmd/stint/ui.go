package main

import (
	"os"
	"strings"

	dash "github.com/Marguelgtz/Stint/internal/dashboard"
)

// palette is a lightweight ANSI styling layer shared by the one-shot CLI
// commands (plan, status, doctor, help, auth, setup, start, down, resume,
// perf, ...). It is deliberately separate from the dashboard's palette: the
// dashboard runs in the alternate screen and manages its own colors, while
// these commands emit a single, linear report.
//
// Colors are only emitted when the target stream is an interactive TTY and the
// user has not asked for NO_COLOR. When output is piped or redirected -- which
// includes the stdout that the test harness swaps in for an os.Pipe() -- every
// helper degrades to a byte-identical no-op, so existing output contracts and
// test assertions stay green while a real terminal gets a colored, prettier
// rendering. Detection is done lazily at call time (never cached) precisely so
// that redirecting os.Stdout mid-process, as the tests do, disables color.
type palette struct{}

var ui = palette{}

func (palette) on(file *os.File) bool {
	if file == nil {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return dash.IsTTY(file)
}

// wrap surrounds s with the given ANSI SGR code, gated on file being an
// interactive TTY. It returns s untouched when color is unavailable so callers
// can splice a wrapped value into any fmt string without disturbing alignment.
func (p palette) wrap(file *os.File, code, s string) string {
	if !p.on(file) || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p palette) out(code, s string) string { return p.wrap(os.Stdout, code, s) }
func (p palette) err(code, s string) string { return p.wrap(os.Stderr, code, s) }

// Semantic colors scoped to stdout.
func (p palette) accent(s string) string  { return p.out("34;1", s) }
func (p palette) success(s string) string { return p.out("32;1", s) }
func (p palette) warn(s string) string    { return p.out("33;1", s) }
func (p palette) danger(s string) string  { return p.out("31;1", s) }
func (p palette) muted(s string) string   { return p.out("90", s) }
func (p palette) bold(s string) string    { return p.out("1", s) }

// Semantic colors scoped to stderr (progress and error lines).
func (p palette) errAccent(s string) string  { return p.err("34;1", s) }
func (p palette) errSuccess(s string) string { return p.err("32;1", s) }
func (p palette) errDanger(s string) string  { return p.err("31;1", s) }

// pad right-justifies s to at least width visible columns, ignoring any
// embedded ANSI sequences when counting columns. For a plain string it is
// identical to a left-justified "%-*s" field, so swapping a label column from
// fmt.Printf("%-*s", width, label) to ui.pad(label, width) keeps output
// byte-stable when color is off and stays visually aligned when it is on.
func (p palette) pad(s string, width int) string {
	if width <= 0 {
		return s
	}
	if n := uiVisibleLen(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

func uiVisibleLen(s string) int { return len([]rune(uiStripANSI(s))) }

func uiStripANSI(s string) string {
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
