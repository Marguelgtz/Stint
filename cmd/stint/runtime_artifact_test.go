package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNInferArtifactCommandResumesAndRecoversCorruption(t *testing.T) {
	payload := []byte("abcdef")
	digest := sha256.Sum256(payload)
	modelSHA := hex.EncodeToString(digest[:])
	tests := []struct {
		name         string
		initial      string
		wantOffsets  string
		wantDiscard  bool
		wantRetryLog bool
	}{
		{name: "oversized artifact is discarded", initial: "abcdef-garbage", wantOffsets: "0", wantDiscard: true},
		{name: "same-size corruption is discarded", initial: "xxxxxx", wantOffsets: "0", wantDiscard: true},
		{name: "valid partial artifact resumes", initial: "abc", wantOffsets: "3"},
		{name: "corrupt partial artifact retries from zero", initial: "Xbc", wantOffsets: "3,0", wantRetryLog: true},
		{name: "empty artifact starts transfer", wantOffsets: "0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			binDir := filepath.Join(root, "bin")
			if err := os.MkdirAll(binDir, 0o700); err != nil {
				t.Fatal(err)
			}
			payloadPath := filepath.Join(root, "remote-payload")
			if err := os.WriteFile(payloadPath, payload, 0o600); err != nil {
				t.Fatal(err)
			}
			offsetsPath := filepath.Join(root, "curl-offsets")
			curlPath := filepath.Join(binDir, "curl")
			curl := "#!/usr/bin/env bash\n" +
				"set -euo pipefail\n" +
				"head_request=0\n" +
				"output=\"\"\n" +
				"while [ \"$#\" -gt 0 ]; do\n" +
				"  case \"$1\" in\n" +
				"    -I|-fsSLI) head_request=1 ;;\n" +
				"    --output) shift; output=\"$1\" ;;\n" +
				"  esac\n" +
				"  shift\n" +
				"done\n" +
				"if [ \"$head_request\" = 1 ]; then\n" +
				"  printf '%s' \"$REMOTE_SIZE\"\n" +
				"  exit 0\n" +
				"fi\n" +
				"offset=0\n" +
				"if [ -f \"$output\" ]; then offset=\"$(wc -c < \"$output\")\"; fi\n" +
				"printf '%s\\n' \"$offset\" >> \"$CURL_OFFSETS\"\n" +
				"if [ \"$offset\" -eq 0 ]; then\n" +
				"  cp \"$REMOTE_PAYLOAD\" \"$output\"\n" +
				"else\n" +
				"  tail -c +$((offset + 1)) \"$REMOTE_PAYLOAD\" >> \"$output\"\n" +
				"fi\n"
			if err := os.WriteFile(curlPath, []byte(curl), 0o700); err != nil {
				t.Fatal(err)
			}

			modelPath := filepath.Join(root, "model with space.ninfer")
			if test.initial != "" {
				if err := os.WriteFile(modelPath, []byte(test.initial), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			sizePath := filepath.Join(root, "model-total-bytes")
			command := ninferModelArtifactCommand(modelPath, sizePath, "https://fixture.invalid/model", modelSHA)
			cmd := exec.Command("bash", "-c", command)
			for _, env := range os.Environ() {
				if !strings.HasPrefix(env, "PATH=") {
					cmd.Env = append(cmd.Env, env)
				}
			}
			cmd.Env = append(cmd.Env,
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"REMOTE_PAYLOAD="+payloadPath,
				"REMOTE_SIZE=6",
				"CURL_OFFSETS="+offsetsPath,
			)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("artifact preparation failed: %v\n%s\ncommand:\n%s", err, output, command)
			}
			got, err := os.ReadFile(modelPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(payload) {
				t.Fatalf("recovered artifact = %q, want %q", got, payload)
			}
			gotSize, err := os.ReadFile(sizePath)
			if err != nil {
				t.Fatalf("discovered model size file: %v; command output:\n%s", err, output)
			}
			if strings.TrimSpace(string(gotSize)) != "6" {
				t.Fatalf("discovered model size = %q, want 6", gotSize)
			}
			gotOffsets, err := os.ReadFile(offsetsPath)
			if err != nil {
				t.Fatal(err)
			}
			gotOffsetList := strings.TrimSuffix(strings.ReplaceAll(strings.TrimSpace(string(gotOffsets)), "\n", ","), ",")
			if gotOffsetList != test.wantOffsets {
				t.Fatalf("download resume offsets = %q, want %q", gotOffsetList, test.wantOffsets)
			}
			log := string(output)
			if strings.Contains(log, "Discarding invalid completed/oversized") != test.wantDiscard {
				t.Fatalf("discard log present=%v, want %v; output:\n%s", strings.Contains(log, "Discarding invalid completed/oversized"), test.wantDiscard, output)
			}
			if strings.Contains(log, "retrying from byte zero") != test.wantRetryLog {
				t.Fatalf("clean-retry log present=%v, want %v; output:\n%s", strings.Contains(log, "retrying from byte zero"), test.wantRetryLog, output)
			}
		})
	}
}
