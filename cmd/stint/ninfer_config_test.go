package main

import (
	"strings"
	"testing"
)

func TestResolveNInferConfig(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		wantName   string
		wantCtx    int
		wantKVD    string
		wantErr    bool
	}{
		{name: "coding default", value: "coding", wantName: ninferConfigCoding, wantCtx: 126976, wantKVD: "int8"},
		{name: "coding alias", value: "cline", wantName: ninferConfigCoding, wantCtx: 126976, wantKVD: "int8"},
		{name: "precision", value: "precision", wantName: ninferConfigPrecision, wantCtx: 172032, wantKVD: "int8"},
		{name: "precision alias", value: "int8", wantName: ninferConfigPrecision, wantCtx: 172032, wantKVD: "int8"},
		{name: "native", value: "native", wantName: ninferConfigNative, wantCtx: 262144, wantKVD: "rk4v4-e8"},
		{name: "native alias", value: "262k", wantName: ninferConfigNative, wantCtx: 262144, wantKVD: "rk4v4-e8"},
		{name: "unknown", value: "turbo", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveNInferConfig(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveNInferConfig(%q) unexpectedly succeeded: %+v", tt.value, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != tt.wantName || got.ContextTokens != tt.wantCtx || got.KVDType != tt.wantKVD {
				t.Fatalf("resolveNInferConfig(%q) = %+v, want name=%q context=%d kv=%q", tt.value, got, tt.wantName, tt.wantCtx, tt.wantKVD)
			}
		})
	}
}

func TestNInferConfigSelectsLaunchProfile(t *testing.T) {
	coding := ninferModelLaunchCommand(126976)
	if !strings.Contains(coding, "--max-context 126976") || !strings.Contains(coding, "--kv-dtype int8") {
		t.Fatalf("coding config launch mismatch: %s", coding)
	}

	precision := ninferModelLaunchCommand(172032)
	if !strings.Contains(precision, "--max-context 172032") || !strings.Contains(precision, "--kv-dtype int8") {
		t.Fatalf("precision config launch mismatch: %s", precision)
	}

	native := ninferModelLaunchCommand(262144)
	if !strings.Contains(native, "--max-context 262144") || !strings.Contains(native, "--kv-capacity 262144") || !strings.Contains(native, "--kv-dtype rk4v4-e8") {
		t.Fatalf("native config launch mismatch: %s", native)
	}

	for name, command := range map[string]string{"coding": coding, "precision": precision, "native": native} {
		for _, required := range []string{"--spec mtp", "--draft-tokens 3", "--lm-head-draft", "--preserve-thinking"} {
			if !strings.Contains(command, required) {
				t.Fatalf("%s config missing %q", name, required)
			}
		}
	}
}
