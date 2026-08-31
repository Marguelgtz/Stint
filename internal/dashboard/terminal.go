package dashboard

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type Terminal struct {
	stdin     *os.File
	stdout    *os.File
	sttyState string
	active    bool
}

func IsTTY(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func OpenTerminal(stdin, stdout *os.File) (*Terminal, error) {
	if !IsTTY(stdin) || !IsTTY(stdout) {
		return nil, errors.New("dashboard requires a terminal")
	}
	stateCmd := exec.Command("stty", "-g")
	stateCmd.Stdin = stdin
	stateBytes, err := stateCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read terminal mode with stty: %w", err)
	}
	state := strings.TrimSpace(string(stateBytes))

	// The dashboard only needs byte-at-a-time input. Do not use `stty raw`
	// here: raw mode also disables output post-processing (OPOST/ONLCR), which
	// makes a rendered '\n' advance to the next row without returning to
	// column zero. The resulting horizontal drift looks like a broken viewport
	// resize/reflow even when SIGWINCH supplied the correct dimensions. Keep
	// output semantics intact while disabling canonical input, echo, and
	// signal-character handling so Ctrl+C reaches the dashboard as byte 3 and
	// can exit through the normal restore path.
	cmd := exec.Command("stty", dashboardInputModeArgs()...)
	cmd.Stdin = stdin
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("enable dashboard input mode: %w", err)
	}
	t := &Terminal{stdin: stdin, stdout: stdout, sttyState: state, active: true}
	fmt.Fprint(stdout, "\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H")
	return t, nil
}

func dashboardInputModeArgs() []string {
	return []string{"-icanon", "-echo", "-isig", "min", "1", "time", "0"}
}

func (t *Terminal) Restore() {
	if t == nil || !t.active {
		return
	}
	t.active = false
	cmd := exec.Command("stty", t.sttyState)
	cmd.Stdin = t.stdin
	_ = cmd.Run()
	fmt.Fprint(t.stdout, "\x1b[?25h\x1b[?1049l")
}

func (t *Terminal) Draw(content string) error {
	if t == nil || !t.active {
		return errors.New("terminal is not active")
	}
	_, err := fmt.Fprintf(t.stdout, "\x1b[H\x1b[2J%s", content)
	return err
}

func Size() (width, height int) {
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err == nil {
		fields := strings.Fields(string(out))
		if len(fields) == 2 {
			rows, rowErr := strconv.Atoi(fields[0])
			cols, colErr := strconv.Atoi(fields[1])
			if rowErr == nil && colErr == nil && rows > 0 && cols > 0 {
				return cols, rows
			}
		}
	}
	width = envInt("COLUMNS", 100)
	height = envInt("LINES", 30)
	return width, height
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
