package main

import "testing"

func TestResolveLlamaContext(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "default", value: "", want: interactiveContext},
		{name: "64k", value: "65536", want: 65536},
		{name: "128k", value: "131072", want: 131072},
		{name: "minimum", value: "1024", want: 1024},
		{name: "too small", value: "1023", wantErr: true},
		{name: "too large", value: "131073", wantErr: true},
		{name: "not integer", value: "64k", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveLlamaContext(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveLlamaContext(%q) unexpectedly returned %d", tt.value, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("resolveLlamaContext(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestLlamaModelLaunchUsesSelectedContext(t *testing.T) {
	for _, contextTokens := range []int{16384, 65536, 131072} {
		command := llamaModelLaunchCommand(contextTokens)
		want := "-c " + map[int]string{16384: "16384", 65536: "65536", 131072: "131072"}[contextTokens]
		if !contains(command, want) {
			t.Fatalf("llama.cpp command for %d tokens missing %q", contextTokens, want)
		}
	}
}

func contains(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
