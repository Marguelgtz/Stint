package main

import (
	"errors"
	"testing"
)

func TestClassifySSHFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"refused", errors.New("ssh: connect to host 89.221.67.152 port 16222: Connection refused"), diagnosticSSHConnectionRefused},
		{"unreachable", errors.New("ssh: connect to host 98.191.113.12 port 11148: Network is unreachable"), diagnosticSSHTCPUnreachable},
		{"timeout", errors.New("Timeout, server 98.191.113.12 not responding."), diagnosticSSHTCPUnreachable},
		{"auth", errors.New("Permission denied (publickey)."), diagnosticSSHAuthFailed},
		{"other", errors.New("remote command exited 1"), diagnosticSSHCommandFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifySSHFailure(tt.err); got != tt.want {
				t.Fatalf("classifySSHFailure(%v) = %s, want %s", tt.err, got, tt.want)
			}
		})
	}
}
