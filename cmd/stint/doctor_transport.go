package main

import "strings"

func classifySSHFailure(err error) string {
	if err == nil {
		return diagnosticOK
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "connection refused"):
		return diagnosticSSHConnectionRefused
	case strings.Contains(text, "network is unreachable"),
		strings.Contains(text, "no route to host"),
		strings.Contains(text, "i/o timeout"),
		strings.Contains(text, "operation timed out"),
		strings.Contains(text, "connection timed out"),
		strings.Contains(text, "server") && strings.Contains(text, "not responding"):
		return diagnosticSSHTCPUnreachable
	case strings.Contains(text, "permission denied"),
		strings.Contains(text, "authentication failed"),
		strings.Contains(text, "publickey"):
		return diagnosticSSHAuthFailed
	default:
		return diagnosticSSHCommandFailed
	}
}

func sshRecoveryFor(code string) string {
	switch code {
	case diagnosticSSHConnectionRefused, diagnosticSSHTCPUnreachable:
		return "retry shortly; if persistent run: stint resume"
	case diagnosticSSHAuthFailed:
		return "run: stint resume; if authentication still fails, verify the Stint SSH key attachment"
	default:
		return "run: stint resume"
	}
}
