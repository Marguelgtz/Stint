package dashboard

import (
	"os"
	"strings"
	"testing"
)

func TestDashboardTerminalModePreservesOutputProcessing(t *testing.T) {
	// This is the regression behind the live resize screenshots: `stty raw`
	// disables OPOST/ONLCR, so newlines no longer return to column zero and the
	// whole screen appears to drift horizontally after redraw/resize.
	mode := []string{"-icanon", "-echo", "-isig", "min", "1", "time", "0"}
	joined := " " + strings.Join(mode, " ") + " "
	if strings.Contains(joined, " raw ") || strings.Contains(joined, " -opost ") || strings.Contains(joined, " -onlcr ") {
		t.Fatalf("dashboard input mode may not disable terminal output processing: %q", joined)
	}
	for _, required := range []string{"-icanon", "-echo", "-isig"} {
		if !strings.Contains(joined, " "+required+" ") {
			t.Fatalf("dashboard input mode missing %s: %q", required, joined)
		}
	}
}

func TestSizeFallbackUsesCurrentViewportEnvironment(t *testing.T) {
	oldColumns, hadColumns := os.LookupEnv("COLUMNS")
	oldLines, hadLines := os.LookupEnv("LINES")
	t.Cleanup(func() {
		if hadColumns {
			_ = os.Setenv("COLUMNS", oldColumns)
		} else {
			_ = os.Unsetenv("COLUMNS")
		}
		if hadLines {
			_ = os.Setenv("LINES", oldLines)
		} else {
			_ = os.Unsetenv("LINES")
		}
	})

	_ = os.Setenv("COLUMNS", "91")
	_ = os.Setenv("LINES", "27")
	if got := envInt("COLUMNS", 100); got != 91 {
		t.Fatalf("columns fallback = %d, want 91", got)
	}
	if got := envInt("LINES", 30); got != 27 {
		t.Fatalf("lines fallback = %d, want 27", got)
	}
}
