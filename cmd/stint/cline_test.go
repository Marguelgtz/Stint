package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigureClineStateRepairsProviderModelAndContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "globalState.json")
	original := map[string]any{
		"actModeApiProvider":     "openrouter",
		"planModeApiProvider":    "openrouter",
		"actModeOpenAiModelId":   "gpt-4o",
		"planModeOpenAiModelId":  "gpt-4o",
		"openAiBaseUrl":          "https://api.openai.com/v1",
		"unrelatedUserPreference": true,
	}
	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFile, data, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := configureClineState(clineConfigureOptions{
		StateFile:     stateFile,
		BaseURL:       "http://127.0.0.1:8409/v1",
		ModelID:       "qwen3.8-27b",
		ContextTokens: 172032,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected Cline state to change")
	}
	if result.BackupFile == "" {
		t.Fatal("expected backup path")
	}
	if _, err := os.Stat(result.BackupFile); err != nil {
		t.Fatalf("backup missing: %v", err)
	}

	updatedData, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	var updated map[string]any
	if err := json.Unmarshal(updatedData, &updated); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"actModeApiProvider", "planModeApiProvider"} {
		if got := updated[key]; got != "openai" {
			t.Fatalf("%s = %v, want openai", key, got)
		}
	}
	for _, key := range []string{"actModeOpenAiModelId", "planModeOpenAiModelId"} {
		if got := updated[key]; got != "qwen3.8-27b" {
			t.Fatalf("%s = %v, want qwen3.8-27b", key, got)
		}
	}
	if got := updated["openAiBaseUrl"]; got != "http://127.0.0.1:8409/v1" {
		t.Fatalf("openAiBaseUrl = %v", got)
	}
	if got := updated["unrelatedUserPreference"]; got != true {
		t.Fatalf("unrelated state changed: %v", got)
	}
	for _, key := range []string{"actModeOpenAiModelInfo", "planModeOpenAiModelInfo"} {
		info, ok := updated[key].(map[string]any)
		if !ok {
			t.Fatalf("%s is not an object: %#v", key, updated[key])
		}
		if got := int(info["contextWindow"].(float64)); got != 172032 {
			t.Fatalf("%s contextWindow = %d", key, got)
		}
		if got := info["supportsTools"]; got != true {
			t.Fatalf("%s supportsTools = %v", key, got)
		}
	}

	backupData, err := os.ReadFile(result.BackupFile)
	if err != nil {
		t.Fatal(err)
	}
	var backup map[string]any
	if err := json.Unmarshal(backupData, &backup); err != nil {
		t.Fatal(err)
	}
	if got := backup["actModeOpenAiModelId"]; got != "gpt-4o" {
		t.Fatalf("backup model = %v, want gpt-4o", got)
	}
}

func TestConfigureClineStateIsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "globalState.json")
	if err := os.WriteFile(stateFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	options := clineConfigureOptions{
		StateFile:     stateFile,
		BaseURL:       "http://127.0.0.1:8409/v1",
		ModelID:       "qwen3.8-27b",
		ContextTokens: 126976,
	}
	first, err := configureClineState(options)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed {
		t.Fatal("first configure should change state")
	}
	second, err := configureClineState(options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Fatal("second configure should be idempotent")
	}
	if second.BackupFile != "" {
		t.Fatalf("idempotent configure created backup %q", second.BackupFile)
	}
}
