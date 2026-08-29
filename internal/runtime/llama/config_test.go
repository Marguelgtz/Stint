package llama

import "testing"

func TestInteractiveQwenLimits(t *testing.T) {
	cfg := InteractiveQwen()

	if cfg.Context != 32768 {
		t.Fatalf("Context = %d, want 32768", cfg.Context)
	}
	if cfg.MaxOutput != 8192 {
		t.Fatalf("MaxOutput = %d, want 8192", cfg.MaxOutput)
	}
	if cfg.MaxOutput >= cfg.Context {
		t.Fatalf("MaxOutput = %d must be smaller than Context = %d", cfg.MaxOutput, cfg.Context)
	}
	if cfg.Port != DefaultInteractivePort {
		t.Fatalf("Port = %d, want %d", cfg.Port, DefaultInteractivePort)
	}
}
