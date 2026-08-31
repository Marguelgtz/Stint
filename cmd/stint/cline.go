package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const (
	clineDefaultModelID = "qwen3.8-27b"
	clineProviderID     = "openai"
)

type clineConfigureOptions struct {
	StateFile     string
	BaseURL       string
	ModelID       string
	ContextTokens int
}

type clineConfigureResult struct {
	Changed    bool
	StateFile  string
	BackupFile string
	BaseURL    string
	ModelID    string
	Context    int
}

func runCline(args []string) error {
	if len(args) == 0 {
		return errors.New("cline requires a command: configure")
	}
	switch args[0] {
	case "configure", "fix":
		return runClineConfigure(args[1:])
	default:
		return fmt.Errorf("unknown cline command %q; choose configure", args[0])
	}
}

func runClineConfigure(args []string) error {
	fs := flag.NewFlagSet("cline configure", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	stateFile := fs.String("state-file", "", "Cline globalState.json path")
	baseURL := fs.String("base-url", fmt.Sprintf("http://127.0.0.1:%d/v1", clinePort), "OpenAI-compatible Stint endpoint")
	modelID := fs.String("model", clineDefaultModelID, "model ID exposed by Stint")
	contextTokens := fs.Int("context", 0, "Cline model context window; defaults to the active Stint session")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	resolvedStateFile, err := resolveClineStateFile(*stateFile)
	if err != nil {
		return err
	}
	resolvedContext := *contextTokens
	if resolvedContext <= 0 {
		resolvedContext = activeClineContextTokens()
	}
	if resolvedContext <= 0 {
		return errors.New("Cline context must be greater than zero")
	}
	if strings.TrimSpace(*modelID) == "" {
		return errors.New("Cline model ID cannot be empty")
	}
	if strings.TrimSpace(*baseURL) == "" {
		return errors.New("Cline base URL cannot be empty")
	}

	result, err := configureClineState(clineConfigureOptions{
		StateFile:     resolvedStateFile,
		BaseURL:       strings.TrimRight(strings.TrimSpace(*baseURL), "/"),
		ModelID:       strings.TrimSpace(*modelID),
		ContextTokens: resolvedContext,
	})
	if err != nil {
		return err
	}

	if result.Changed {
		fmt.Println("Cline configured for Stint.")
		fmt.Printf("State file       %s\n", result.StateFile)
		fmt.Printf("Backup           %s\n", result.BackupFile)
	} else {
		fmt.Println("Cline is already configured for Stint.")
		fmt.Printf("State file       %s\n", result.StateFile)
	}
	fmt.Printf("Provider         OpenAI Compatible (%s)\n", clineProviderID)
	fmt.Printf("Base URL         %s\n", result.BaseURL)
	fmt.Printf("Model            %s\n", result.ModelID)
	fmt.Printf("Context          %d tokens\n", result.Context)
	fmt.Println("Next             Reload the VS Code window, then start a new Cline task.")
	return nil
}

func resolveClineStateFile(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		path, err := expandHomePath(strings.TrimSpace(explicit))
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("Cline state file %s: %w", path, err)
		}
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	path := filepath.Join(home, ".cline", "data", "globalState.json")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("Cline state file not found at %s; open Cline once or pass --state-file: %w", path, err)
	}
	return path, nil
}

func expandHomePath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

func activeClineContextTokens() int {
	paths, err := config.DefaultPaths()
	if err == nil {
		state, loadErr := sessionstate.Load(paths)
		if loadErr == nil && state.ContextTokens > 0 {
			return state.ContextTokens
		}
	}
	return interactiveRuntimeContext
}

func configureClineState(options clineConfigureOptions) (clineConfigureResult, error) {
	result := clineConfigureResult{StateFile: options.StateFile, BaseURL: options.BaseURL, ModelID: options.ModelID, Context: options.ContextTokens}
	original, err := os.ReadFile(options.StateFile)
	if err != nil {
		return result, fmt.Errorf("read Cline state: %w", err)
	}
	var state map[string]any
	if err := json.Unmarshal(original, &state); err != nil {
		return result, fmt.Errorf("parse Cline state: %w", err)
	}
	if state == nil {
		return result, errors.New("Cline state is not a JSON object")
	}

	changed := false
	set := func(key string, value any) {
		if reflect.DeepEqual(state[key], value) {
			return
		}
		state[key] = value
		changed = true
	}
	set("actModeApiProvider", clineProviderID)
	set("planModeApiProvider", clineProviderID)
	set("actModeOpenAiModelId", options.ModelID)
	set("planModeOpenAiModelId", options.ModelID)
	set("openAiBaseUrl", options.BaseURL)
	set("actModeOpenAiModelInfo", mergeClineModelInfo(state["actModeOpenAiModelInfo"], options.ContextTokens))
	set("planModeOpenAiModelInfo", mergeClineModelInfo(state["planModeOpenAiModelInfo"], options.ContextTokens))

	if !changed {
		return result, nil
	}

	info, err := os.Stat(options.StateFile)
	if err != nil {
		return result, fmt.Errorf("stat Cline state: %w", err)
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o600
	}
	backup := options.StateFile + ".stint-backup-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	if err := os.WriteFile(backup, original, mode); err != nil {
		return result, fmt.Errorf("back up Cline state: %w", err)
	}
	result.BackupFile = backup

	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return result, fmt.Errorf("encode Cline state: %w", err)
	}
	encoded = append(encoded, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(options.StateFile), ".globalState.stint-*")
	if err != nil {
		return result, fmt.Errorf("create Cline state temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return result, fmt.Errorf("set Cline state temp permissions: %w", err)
	}
	if _, err := tmp.Write(encoded); err != nil {
		tmp.Close()
		return result, fmt.Errorf("write Cline state temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return result, fmt.Errorf("close Cline state temp file: %w", err)
	}
	if err := os.Rename(tmpName, options.StateFile); err != nil {
		return result, fmt.Errorf("install Cline state: %w", err)
	}
	result.Changed = true
	return result, nil
}

func mergeClineModelInfo(existing any, contextTokens int) map[string]any {
	info := map[string]any{}
	if current, ok := existing.(map[string]any); ok {
		for key, value := range current {
			info[key] = value
		}
	}
	info["contextWindow"] = float64(contextTokens)
	if _, ok := info["maxTokens"]; !ok {
		info["maxTokens"] = float64(8192)
	}
	if _, ok := info["supportsImages"]; !ok {
		info["supportsImages"] = false
	}
	if _, ok := info["supportsPromptCache"]; !ok {
		info["supportsPromptCache"] = false
	}
	if _, ok := info["inputPrice"]; !ok {
		info["inputPrice"] = float64(0)
	}
	if _, ok := info["outputPrice"]; !ok {
		info["outputPrice"] = float64(0)
	}
	info["supportsTools"] = true
	info["supportsStreaming"] = true
	return info
}
