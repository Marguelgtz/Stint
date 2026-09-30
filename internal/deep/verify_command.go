package deep

import (
	"errors"
	"fmt"
	"strings"
)

// ValidateVerifyCommand checks that a value persisted as executable command
// data is a non-empty raw shell command, not Markdown presentation syntax.
// It deliberately does not parse or sandbox shell syntax: mission authors are
// trusted to provide shell commands, and commands continue to run via sh -c.
func ValidateVerifyCommand(command string) error {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return errors.New("verification command is empty")
	}
	if strings.IndexByte(command, 0) >= 0 {
		return errors.New("verification command contains a NUL byte")
	}
	if isMarkdownCommandWrapper(trimmed) {
		return errors.New("verification command contains a Markdown wrapper; provide raw shell command data")
	}
	return nil
}

func isMarkdownCommandWrapper(command string) bool {
	for _, fence := range []string{"```", "~~~"} {
		if strings.HasPrefix(command, fence) && strings.HasSuffix(command, fence) {
			return true
		}
	}
	if !strings.HasPrefix(command, "`") {
		return false
	}
	opening := len(command) - len(strings.TrimLeft(command, "`"))
	closing := len(command) - len(strings.TrimRight(command, "`"))
	if opening == 0 || opening != closing || len(command) <= opening+closing {
		return false
	}
	// A Markdown code span cannot contain its own delimiter. This leaves
	// ordinary shell substitutions such as echo `date` untouched.
	return !strings.Contains(command[opening:len(command)-closing], strings.Repeat("`", opening))
}

// parseVerificationFenceLine parses an explicitly fenced Markdown command.
// A line with an opening and closing fence is a single-line code span/block;
// a line with only an opening fence starts a multiline block. The only
// accepted language labels are shell labels because the contents are passed
// to sh -c.
func parseVerificationFenceLine(line string) (command, fence string, fenced bool, err error) {
	trimmed := strings.TrimSpace(line)
	for _, marker := range []string{"```", "~~~"} {
		if !strings.HasPrefix(trimmed, marker) {
			continue
		}
		fenced = true
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
		if closeAt := strings.Index(rest, marker); closeAt >= 0 {
			if strings.TrimSpace(rest[closeAt+len(marker):]) != "" {
				return "", "", true, errors.New("text follows the closing verification fence")
			}
			command, err = commandFromFenceBody(rest[:closeAt])
			return command, "", true, err
		}
		if rest != "" && !isShellFenceLabel(rest) {
			return "", "", true, fmt.Errorf("unsupported verification fence label %q; use sh or bash", rest)
		}
		return "", marker, true, nil
	}
	return trimmed, "", false, nil
}

func commandFromFenceBody(body string) (string, error) {
	command := strings.TrimSpace(body)
	for _, label := range []string{"sh", "bash", "shell"} {
		if command == label {
			return "", nil
		}
		if strings.HasPrefix(command, label+" ") || strings.HasPrefix(command, label+"\t") || strings.HasPrefix(command, label+"\n") {
			command = strings.TrimSpace(strings.TrimPrefix(command, label))
			break
		}
	}
	return command, nil
}

func isShellFenceLabel(label string) bool {
	switch strings.TrimSpace(label) {
	case "sh", "bash", "shell":
		return true
	default:
		return false
	}
}

func parseTaskVerifyCommand(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	for _, marker := range []string{"```", "~~~"} {
		if !strings.HasPrefix(trimmed, marker) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
		closeAt := strings.Index(rest, marker)
		if closeAt < 0 || strings.TrimSpace(rest[closeAt+len(marker):]) != "" {
			return "", errors.New("task verify must use a complete single-line fence")
		}
		command, err := commandFromFenceBody(rest[:closeAt])
		if err != nil {
			return "", err
		}
		if err := ValidateVerifyCommand(command); err != nil {
			return "", err
		}
		return command, nil
	}
	if err := ValidateVerifyCommand(trimmed); err != nil {
		return "", err
	}
	return trimmed, nil
}
