package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeClineScript installs a fake `cline` binary that emits a fixed JSONL
// stream. The real Cline CLI is never invoked and no network is touched.
func writeClineScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-cline")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write fake cline: %v", err)
	}
	return path
}

func runCline(t *testing.T, binary string, in execInput) (execResult, error) {
	t.Helper()
	e := newClineExecutor(binary)
	return e.run(context.Background(), in)
}

func TestClineExecutorSuccess(t *testing.T) {
	bin := writeClineScript(t, `
echo '{"ts":"2026-02-09T16:00:00Z","type":"progress","text":"warming up"}'
echo '{"ts":"2026-02-09T16:00:01Z","type":"progress","text":"writing code"}'
echo '{"ts":"2026-02-09T16:00:02Z","type":"run_result","finishReason":"completed","iterations":2,"text":"added the helper and a test","usage":{"inputTokens":120,"outputTokens":45}}'
echo '{"ts":"2026-02-09T16:00:02Z","type":"done"}'
exit 0
`)
	res, err := runCline(t, bin, execInput{prompt: "do the thing", timeout: 10 * time.Second, provider: "openai-compatible", model: "m"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.completed {
		t.Errorf("completed = false, want true (finish=%q)", res.finishReason)
	}
	if res.finishReason != "completed" {
		t.Errorf("finishReason = %q, want completed", res.finishReason)
	}
	if res.iterations != 2 || res.inputTokens != 120 || res.outputTokens != 45 {
		t.Errorf("usage = iters %d in %d out %d, want 2/120/45", res.iterations, res.inputTokens, res.outputTokens)
	}
	if res.outputText != "added the helper and a test" {
		t.Errorf("outputText = %q", res.outputText)
	}
	if res.eventCount != 4 {
		t.Errorf("eventCount = %d, want 4", res.eventCount)
	}
	if res.exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", res.exitCode)
	}
}

func TestClineExecutorMissingRunResult(t *testing.T) {
	// Progress events only: the worker produced output but never reported
	// completion. The coordinator must treat this as NOT completed —
	// absence of evidence is not evidence.
	bin := writeClineScript(t, `
echo '{"ts":"t","type":"progress","text":"started"}'
echo "not json at all"
exit 0
`)
	res, err := runCline(t, bin, execInput{prompt: "p", timeout: 10 * time.Second, provider: "p", model: "m"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.completed {
		t.Errorf("completed = true with no run_result/done event")
	}
	if res.eventCount != 1 {
		t.Errorf("eventCount = %d, want 1 (malformed line skipped)", res.eventCount)
	}
}

func TestClineExecutorDoneOnlyFallback(t *testing.T) {
	bin := writeClineScript(t, `
echo '{"ts":"t","type":"done"}'
exit 0
`)
	res, err := runCline(t, bin, execInput{prompt: "p", timeout: 10 * time.Second, provider: "p", model: "m"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.completed || res.finishReason != "done" {
		t.Errorf("done+exit0 should count as completed (got %v %q)", res.completed, res.finishReason)
	}
}

func TestClineExecutorNonzeroExit(t *testing.T) {
	bin := writeClineScript(t, `
echo '{"ts":"t","type":"run_result","finishReason":"error","iterations":1,"text":"model exploded"}'
echo "fatal: provider unreachable" >&2
exit 1
`)
	res, err := runCline(t, bin, execInput{prompt: "p", timeout: 10 * time.Second, provider: "p", model: "m"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.completed {
		t.Errorf("completed = true despite finishReason error")
	}
	if res.exitCode != 1 {
		t.Errorf("exitCode = %d, want 1", res.exitCode)
	}
	if res.stderrTail == "" {
		t.Errorf("stderr tail not captured")
	}
}

func TestClineExecutorTimeout(t *testing.T) {
	bin := writeClineScript(t, `
echo '{"ts":"t","type":"progress","text":"still working"}'
sleep 5
`)
	res, err := runCline(t, bin, execInput{prompt: "p", timeout: 300 * time.Millisecond, provider: "p", model: "m"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.completed {
		t.Errorf("completed = true for a timed-out invocation")
	}
	if res.exitCode == 0 {
		t.Errorf("exitCode = 0 for a killed invocation")
	}
	if res.duration > 2*time.Second {
		t.Errorf("duration %s: context deadline did not bound the invocation", res.duration)
	}
}

func TestClineExecutorArgv(t *testing.T) {
	e := newClineExecutor("cline")
	var saw []string
	e.invoke = func(_ context.Context, dir string, argv []string) (string, string, int) {
		saw = append(saw, dir)
		saw = append(saw, argv...)
		return "", "", 0
	}
	_, _ = e.run(context.Background(), execInput{
		workdir: "/wt", prompt: "TASK PROMPT", timeout: 5 * time.Minute,
		autoApprove: true, provider: "openai-compatible", model: "qwen3.8-27b",
		apiKey: "sk-test", clineConfig: "/cfg",
	})
	want := []string{"/wt", "-c", "/wt", "--json", "--auto-approve", "true",
		"-t", "300", "--retries", "2",
		"-P", "openai-compatible", "-m", "qwen3.8-27b",
		"-k", "sk-test", "--config", "/cfg", "TASK PROMPT"}
	if len(saw) != len(want) {
		t.Fatalf("argv = %v, want %v", saw, want)
	}
	for i := range want {
		if saw[i] != want[i] {
			t.Errorf("argv[%d] = %q, want %q", i, saw[i], want[i])
		}
	}
}
