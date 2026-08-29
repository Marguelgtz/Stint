package main

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestWaitForPortAvailableWaitsForRelease(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		time.Sleep(75 * time.Millisecond)
		_ = listener.Close()
	}()

	if err := waitForPortAvailable(context.Background(), port, time.Second); err != nil {
		t.Fatalf("waitForPortAvailable returned %v", err)
	}
}

func TestWaitForPortAvailableTimesOut(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	err = waitForPortAvailable(context.Background(), port, 80*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "still in use") {
		t.Fatalf("error = %v, want still-in-use timeout", err)
	}
}
