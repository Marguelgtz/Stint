package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

func TestSelectInteractiveRuntime(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		gpu       string
		want      string
		wantErr   bool
	}{
		{name: "auto 4090", requested: runtimeAuto, gpu: "RTX_4090", want: runtimeNInfer},
		{name: "auto non 4090", requested: runtimeAuto, gpu: "RTX_3090", want: runtimeLlamaCpp},
		{name: "explicit ninfer 4090", requested: runtimeNInfer, gpu: "NVIDIA GeForce RTX 4090", want: runtimeNInfer},
		{name: "explicit ninfer wrong gpu", requested: runtimeNInfer, gpu: "RTX_3090", wantErr: true},
		{name: "explicit llama", requested: "llama", gpu: "RTX_4090", want: runtimeLlamaCpp},
		{name: "unknown", requested: "vllm", gpu: "RTX_4090", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectInteractiveRuntime(tt.requested, tt.gpu)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("selectInteractiveRuntime(%q, %q) unexpectedly succeeded with %q", tt.requested, tt.gpu, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("selectInteractiveRuntime(%q, %q) = %q, want %q", tt.requested, tt.gpu, got, tt.want)
			}
		})
	}
}

func TestRuntimeContextsAndLegacyResume(t *testing.T) {
	if got := contextForRuntime(runtimeNInfer); got != 126976 {
		t.Fatalf("NInfer context = %d, want 126976", got)
	}
	if got := contextForRuntime(runtimeLlamaCpp); got != interactiveContext {
		t.Fatalf("llama.cpp fallback context = %d, want proven legacy context %d", got, interactiveContext)
	}
	legacy := sessionstate.State{}
	if got := runtimeForState(legacy); got != runtimeLlamaCpp {
		t.Fatalf("legacy runtime = %q, want %q", got, runtimeLlamaCpp)
	}
	if got := contextForState(legacy); got != interactiveContext {
		t.Fatalf("legacy context = %d, want %d", got, interactiveContext)
	}
}

func TestNInferModelArtifactIsRevisionPinned(t *testing.T) {
	if ninferModelRevision == "" {
		t.Fatal("NInfer model revision must be pinned")
	}
	if strings.Contains(ninferModelURL, "/resolve/main/") {
		t.Fatalf("NInfer model URL is mutable: %s", ninferModelURL)
	}
	if !strings.Contains(ninferModelURL, "/resolve/"+ninferModelRevision+"/") {
		t.Fatalf("NInfer model URL %q does not contain pinned revision %q", ninferModelURL, ninferModelRevision)
	}
}

func TestNInferProductionTupleUsesValidated4090V2Profile(t *testing.T) {
	if ninferSourceRepository != "https://github.com/sergiuszm/ninfer-4090.git" {
		t.Fatalf("NInfer source repository = %q", ninferSourceRepository)
	}
	if ninferSourceCommit != "81b68a20a9a0d9ab47d7e5838887c6d636ab76e0" {
		t.Fatalf("NInfer source commit = %q", ninferSourceCommit)
	}
	if ninferCUDAFloor != "12.8" || ninferGPUArchitecture != "89" {
		t.Fatalf("NInfer target = CUDA >= %s, SM%s; want CUDA >= 12.8, SM89", ninferCUDAFloor, ninferGPUArchitecture)
	}
	if ninferArtifactFormat != 2 || ninferModelRevision != "18dfc887423fa5aabf3cb56fac41490e462b3fab" || ninferModelSHA256 != "eec39564993d6e9c7d5e383382a760f093465c9d163ec9a1bd6b80199514bf3e" || ninferModelSizeBytes != 18210531328 {
		t.Fatalf("NInfer model artifact tuple changed unexpectedly: format=v%d revision=%s size=%d sha256=%s", ninferArtifactFormat, ninferModelRevision, ninferModelSizeBytes, ninferModelSHA256)
	}
	if native, err := resolveNInferConfig(ninferConfigNative); err != nil || native.ContextTokens != 262144 {
		t.Fatalf("native NInfer context = %+v, %v; want 262144 tokens", native, err)
	}
}

func TestNInferBootstrapIsPinnedAndPrefetchesInParallel(t *testing.T) {
	command := ninferBootstrapCommand()
	for _, required := range []string{
		ninferSourceRepository,
		ninferSourceCommit,
		ninferModelURL,
		ninferModelSHA256,
		"CUDA toolkit 12.8 or newer",
		"gcc-13",
		"g++-13",
		"CMake 3.28",
		"-DCMAKE_CUDA_ARCHITECTURES=89",
		"--target ninfer ninfer-serve",
		"model-download.pid",
		"model-download.log",
		"model-total-bytes",
		"Starting Qwen3.8-27B model prefetch in parallel",
		"--retry-all-errors",
		"-C -",
		"runtime-acquisition-started-ms",
		"runtime-verified-ms",
		"model acquisition does not delay runtime readiness",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("NInfer bootstrap missing %q", required)
		}
	}
	if strings.Contains(command, "waiting for the parallel Qwen model transfer") {
		t.Fatal("NInfer runtime bootstrap must return before the model transfer finishes")
	}
	for _, forbidden := range []string{"18210531328", "17367 MiB", "/resolve/main/"} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("NInfer bootstrap retained stale hardcoded artifact metadata %q", forbidden)
		}
	}
}

func TestNInferLaunchUsesQualified4090Profile(t *testing.T) {
	command := ninferModelLaunchCommand(126976)
	for _, required := range []string{
		ninferModelURL,
		ninferModelSHA256,
		"/workspace/stint/llama.pid",
		"/workspace/stint/llama.log",
		"model-total-bytes",
		"%header{content-length}",
		"Discarding invalid completed/oversized NInfer model artifact",
		"--model-id qwen3.8-27b",
		"--max-context 126976",
		"--kv-capacity 126976",
		"--default-max-tokens 126976",
		"--kv-dtype int8",
		"--spec mtp",
		"--draft-tokens 3",
		"--lm-head-draft",
		"--pending-timeout-ms 600000",
		"--preserve-thinking",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("NInfer launch missing %q", required)
		}
	}
}

func TestNInferProgressDoesNotHardcodeArtifactSize(t *testing.T) {
	command := remoteModelProgressCommandForState(sessionstate.State{Runtime: runtimeNInfer})
	if !strings.Contains(command, "model-total-bytes") {
		t.Fatal("NInfer progress does not read discovered model size")
	}
	for _, forbidden := range []string{"18210531328", "17367 MiB"} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("NInfer progress retained hardcoded artifact metadata %q", forbidden)
		}
	}
}

func TestNInferGeneratedShellIsValid(t *testing.T) {
	commands := map[string]string{
		"bootstrap":         ninferBootstrapCommand(),
		"release-bootstrap": ninferReleaseBootstrapCommand(),
		"source-ready":      selectedRuntimeReadyCommand(sessionstate.State{Runtime: runtimeNInfer}),
		"release-ready":     selectedRuntimeReadyCommand(sessionstate.State{Runtime: runtimeNInfer, RuntimeDeployment: ninferDeploymentReleaseBundle}),
		"launch":            ninferModelLaunchCommandWithClients(262144, 2),
		"progress":          remoteModelProgressCommandForState(sessionstate.State{Runtime: runtimeNInfer}),
	}
	for name, command := range commands {
		t.Run(name, func(t *testing.T) {
			if out, err := exec.Command("bash", "-n", "-c", command).CombinedOutput(); err != nil {
				t.Fatalf("generated NInfer shell is invalid: %v\n%s\ncommand:\n%s", err, out, command)
			}
		})
	}
}

func TestNInferDeploymentSelectionIsOptInAndPinned(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  string
		bad   bool
	}{
		{value: "", want: ninferDeploymentSourceBuild},
		{value: "source-build", want: ninferDeploymentSourceBuild},
		{value: "RELEASE-BUNDLE", want: ninferDeploymentReleaseBundle},
		{value: "mutable-latest", bad: true},
	} {
		got, err := normalizeNInferDeployment(tc.value)
		if tc.bad {
			if err == nil {
				t.Fatalf("normalizeNInferDeployment(%q) unexpectedly succeeded", tc.value)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("normalizeNInferDeployment(%q) = %q, %v; want %q", tc.value, got, err, tc.want)
		}
	}
	if got := ninferDeploymentForState(sessionstate.State{Runtime: runtimeNInfer}); got != ninferDeploymentSourceBuild {
		t.Fatalf("legacy NInfer session deployment = %q, want source-build", got)
	}
	if runtimeDeploymentForStatus(sessionstate.State{Runtime: runtimeNInfer}) != ninferDeploymentSourceBuild {
		t.Fatal("legacy NInfer status should explain its source-build deployment")
	}
	if allowNInferLlamaFallback(sessionstate.State{Runtime: runtimeNInfer, RuntimeDeployment: ninferDeploymentReleaseBundle, RuntimeRequest: runtimeAuto, Clients: 1}) {
		t.Fatal("release-bundle failure must not silently fall back to llama.cpp")
	}
	if !allowNInferLlamaFallback(sessionstate.State{Runtime: runtimeNInfer, RuntimeDeployment: ninferDeploymentSourceBuild, RuntimeRequest: runtimeAuto, Clients: 1}) {
		t.Fatal("source-build compatibility should preserve the existing auto fallback")
	}
	if ninferRuntimeBundleSHA256 != "6725e60c8e3edb2982ad828898210868dd98ea1d4fe4d35f97bfa1e625414416" || ninferRuntimeReleaseTag == "" || ninferRuntimeBundleName == "" {
		t.Fatal("immutable NInfer release must have a fixed tag, archive name, and SHA-256")
	}

	command := ninferReleaseBootstrapCommand()
	for _, required := range []string{
		ninferRuntimeBundleSHA256,
		ninferRuntimeReleaseURL,
		ninferRuntimeReleaseTag,
		"DOWNLOAD_PY",
		"--workers",
		"Range",
		ninferSourceCommit,
		ninferModelRevision,
		ninferModelSHA256,
		"ninfer/releases",
		"mv -Tf",
		".stint-commit",
		"ldd",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("release bootstrap is missing %q", required)
		}
	}
	for _, forbidden := range []string{"cmake --", "apt-get", "git fetch", "git clone"} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("release bootstrap unexpectedly contains source-build operation %q", forbidden)
		}
	}
	pythonStart := strings.Index(command, "<<'PY'\n")
	if pythonStart < 0 {
		t.Fatal("release bootstrap does not contain its safe installer")
	}
	pythonStart += len("<<'PY'\n")
	pythonEnd := strings.Index(command[pythonStart:], "\nPY\n")
	if pythonEnd < 0 {
		t.Fatal("release bootstrap installer heredoc is unterminated")
	}
	python := command[pythonStart : pythonStart+pythonEnd]
	check := exec.Command("python3", "-c", "import sys; compile(sys.stdin.read(), '<NInfer release installer>', 'exec')")
	check.Stdin = strings.NewReader(python)
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("NInfer release installer Python is invalid: %v\n%s", err, output)
	}
}

func TestNInferReleaseDownloaderUsesParallelRangesAndRetries(t *testing.T) {
	const chunkSize = 4096
	payload := make([]byte, 9*chunkSize+73)
	for i := range payload {
		payload[i] = byte((i * 31) % 251)
	}
	digest := sha256.Sum256(payload)
	var active atomic.Int32
	var maxActive atomic.Int32
	var failedRange atomic.Bool
	var retryCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		var start, end int64
		if count, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil || count != 2 {
			http.Error(w, "missing byte range", http.StatusBadRequest)
			return
		}
		current := active.Add(1)
		defer active.Add(-1)
		for previous := maxActive.Load(); current > previous && !maxActive.CompareAndSwap(previous, current); previous = maxActive.Load() {
		}
		time.Sleep(20 * time.Millisecond)
		if start == chunkSize && failedRange.CompareAndSwap(false, true) {
			retryCount.Add(1)
			http.Error(w, "retry fixture", http.StatusServiceUnavailable)
			return
		}
		if start < 0 || end < start || end >= int64(len(payload)) {
			http.Error(w, "invalid byte range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		body := payload[int(start) : int(end)+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "runtime.tar.gz")
	runNInferReleaseDownloader(t, server.URL, output, hex.EncodeToString(digest[:]), "--chunk-size", strconv.Itoa(chunkSize), "--workers", "4")
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("downloaded archive bytes differ from the range fixture")
	}
	if maxActive.Load() < 2 {
		t.Fatalf("range requests did not overlap: maximum active requests = %d", maxActive.Load())
	}
	if !failedRange.Load() || retryCount.Load() != 1 {
		t.Fatalf("transient range failure was not retried: failed=%v retries=%d", failedRange.Load(), retryCount.Load())
	}
}

func TestNInferReleaseDownloaderReusesCompletedRangesAfterResume(t *testing.T) {
	const chunkSize = 4096
	payload := make([]byte, 4*chunkSize)
	for i := range payload {
		payload[i] = byte((i * 17) % 253)
	}
	digest := sha256.Sum256(payload)
	var denyOneRange atomic.Bool
	denyOneRange.Store(true)
	var requestsMu sync.Mutex
	chunkRequests := map[int64]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var start, end int64
		if count, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil || count != 2 {
			http.Error(w, "missing byte range", http.StatusBadRequest)
			return
		}
		if end > start {
			requestsMu.Lock()
			chunkRequests[start]++
			requestsMu.Unlock()
		}
		if start == chunkSize && denyOneRange.Load() {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		if start < 0 || end < start || end >= int64(len(payload)) {
			http.Error(w, "invalid byte range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		body := payload[int(start) : int(end)+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "runtime.tar.gz")
	script := strings.Replace(ninferReleaseDownloaderScript, "MAX_ATTEMPTS = 5", "MAX_ATTEMPTS = 1", 1)
	first := exec.Command("python3", "-c", script, "--chunk-size", strconv.Itoa(chunkSize), "--workers", "2", server.URL, output, hex.EncodeToString(digest[:]))
	result, err := first.CombinedOutput()
	if err == nil {
		t.Fatal("expected the first download attempt to stop with one cached range missing")
	}
	if !strings.Contains(string(result), "failed after 1 attempts") {
		t.Fatalf("first download failure was not explicit: %s", result)
	}
	denyOneRange.Store(false)
	runNInferReleaseDownloader(t, server.URL, output, hex.EncodeToString(digest[:]), "--chunk-size", strconv.Itoa(chunkSize), "--workers", "2")
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("resumed archive bytes differ from the fixture")
	}
	requestsMu.Lock()
	defer requestsMu.Unlock()
	for _, start := range []int64{0, 2 * chunkSize, 3 * chunkSize} {
		if chunkRequests[start] != 1 {
			t.Fatalf("completed range %d was not reused; requests=%d", start, chunkRequests[start])
		}
	}
	if chunkRequests[chunkSize] != 2 {
		t.Fatalf("failed range was not retried after resume; requests=%d", chunkRequests[chunkSize])
	}
}

func TestNInferReleaseDownloaderFallsBackWhenRangesAreUnsupported(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is needed for the no-range compatibility fallback")
	}
	payload := []byte("small immutable release fixture")
	digest := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "runtime.tar.gz")
	runNInferReleaseDownloader(t, server.URL, output, hex.EncodeToString(digest[:]), "--chunk-size", "4", "--workers", "2")
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("curl fallback bytes differ from the fixture")
	}
}

func TestNInferReleaseDownloaderRejectsUnexpectedPinnedDigest(t *testing.T) {
	payload := []byte("content that does not match the requested digest")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var start, end int64
		if count, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil || count != 2 {
			http.Error(w, "missing byte range", http.StatusBadRequest)
			return
		}
		if start < 0 || end < start || end >= int64(len(payload)) {
			http.Error(w, "invalid byte range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		body := payload[int(start) : int(end)+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "runtime.tar.gz")
	args := []string{"-c", ninferReleaseDownloaderScript, "--chunk-size", "16", "--workers", "2", server.URL, output, strings.Repeat("0", 64)}
	command := exec.Command("python3", args...)
	result, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("release downloader accepted an unexpected pinned digest")
	}
	if !strings.Contains(string(result), "pinned SHA-256") {
		t.Fatalf("digest failure was not explicit: %s", result)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("unverified release archive was left at %s: %v", output, err)
	}
	parts, err := os.ReadDir(output + ".parts")
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 0 {
		t.Fatalf("unverified range cache was retained after digest failure: %v", parts)
	}
}

func runNInferReleaseDownloader(t *testing.T, url, output, digest string, flags ...string) {
	t.Helper()
	args := append([]string{"-c", ninferReleaseDownloaderScript}, flags...)
	args = append(args, url, output, digest)
	command := exec.Command("python3", args...)
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run NInfer release downloader: %v\n%s", err, result)
	}
}

func TestNInferReleaseInstallerSafelyInstallsAndReusesFixtureBundle(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	binaries := map[string]string{
		"ninfer":       filepath.Join(root, "ninfer"),
		"ninfer-serve": filepath.Join(root, "ninfer-serve"),
	}
	for name, path := range binaries {
		if err := os.WriteFile(path, []byte("fixture "+name+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	packageArgs := []string{
		"../../scripts/ninfer_runtime_bundle.py", "package",
		"--ninfer", binaries["ninfer"], "--ninfer-serve", binaries["ninfer-serve"],
		"--output-dir", dist,
	}
	if output, err := exec.Command("python3", packageArgs...).CombinedOutput(); err != nil {
		t.Fatalf("package fixture bundle: %v\n%s", err, output)
	}
	archive := filepath.Join(dist, ninferRuntimeBundleName)
	checksum := archive + ".sha256"
	manifest := filepath.Join(dist, "manifest.json")
	archiveBytes, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	archiveHash := sha256.Sum256(archiveBytes)
	installer := ninferReleaseInstallerSource(t)
	installRoot := filepath.Join(root, "install", "ninfer")
	releases := filepath.Join(installRoot, "releases")
	installerArgs := []string{
		archive, checksum, manifest, releases,
		ninferRuntimeReleaseTag, hex.EncodeToString(archiveHash[:]), ninferSourceRepository,
		ninferSourceCommit, ninferModelRevision, ninferModelSHA256,
		"18210531328", "NInfer v2", ninferCUDAFloor, ninferGPUArchitecture,
		"vastai/base-image:cuda-12.8.1-cudnn-devel-ubuntu24.04-py310",
		"sha256:bf6bb047dbc1105c89a5ac41b9a32205a2f2e022cb24d632d055c5b14a86f7ec",
	}
	runInstaller := func() {
		t.Helper()
		args := append([]string{"-"}, installerArgs...)
		cmd := exec.Command("python3", args...)
		cmd.Stdin = strings.NewReader(installer)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("install fixture runtime: %v\n%s", err, output)
		}
	}
	runInstaller()
	for name, source := range binaries {
		installed := filepath.Join(installRoot, "releases", ninferRuntimeReleaseTag, "apps", name)
		got, err := os.ReadFile(installed)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("installed %s bytes do not match fixture", name)
		}
		info, err := os.Stat(installed)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("installed %s mode/stat = %v, %v; want 755", name, info, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(installRoot, "releases", ninferRuntimeReleaseTag, "manifest.json")); err != nil || string(got) != mustReadFile(t, manifest) {
		t.Fatalf("installed manifest = %q, %v; want exact external manifest", got, err)
	}
	// Resume/retry sees the same verified release directory and validates it
	// without replacing either installed executable.
	runInstaller()
}

func ninferReleaseInstallerSource(t *testing.T) string {
	t.Helper()
	command := ninferReleaseBootstrapCommand()
	start := strings.Index(command, "<<'PY'\n")
	if start < 0 {
		t.Fatal("release bootstrap does not contain its installer")
	}
	start += len("<<'PY'\n")
	end := strings.Index(command[start:], "\nPY\n")
	if end < 0 {
		t.Fatal("release bootstrap installer heredoc is unterminated")
	}
	return command[start : start+end]
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestResumeCanExplicitlySwitchDeploymentForRecovery(t *testing.T) {
	state := sessionstate.State{
		Runtime: runtimeNInfer, RuntimeDeployment: ninferDeploymentReleaseBundle,
		RuntimeBundleTag: ninferRuntimeReleaseTag, RuntimeBundleSHA256: ninferRuntimeBundleSHA256,
		RuntimeSourceCommit: ninferSourceCommit, RuntimeAcquisitionMillis: 12000,
	}
	changed, err := applyResumeNInferDeploymentOverride(&state, ninferDeploymentSourceBuild)
	if err != nil || !changed {
		t.Fatalf("source-build recovery override = changed %v, error %v", changed, err)
	}
	if state.RuntimeDeployment != ninferDeploymentSourceBuild || state.RuntimeBundleTag != "" || state.RuntimeBundleSHA256 != "" || state.RuntimeSourceCommit != "" || state.RuntimeAcquisitionMillis != 0 {
		t.Fatalf("source-build recovery retained stale release provenance: %+v", state)
	}
	changed, err = applyResumeNInferDeploymentOverride(&state, ninferDeploymentReleaseBundle)
	if err != nil || !changed {
		t.Fatalf("release-bundle override = changed %v, error %v", changed, err)
	}
	if state.RuntimeDeployment != ninferDeploymentReleaseBundle || state.RuntimeBundleTag != ninferRuntimeReleaseTag || state.RuntimeBundleSHA256 != ninferRuntimeBundleSHA256 {
		t.Fatalf("release-bundle override did not restore exact release pins: %+v", state)
	}
	if _, err := applyResumeNInferDeploymentOverride(&state, "latest"); err == nil {
		t.Fatal("unknown resume deployment override was accepted")
	}
	if _, err := applyResumeNInferDeploymentOverride(&sessionstate.State{Runtime: runtimeLlamaCpp}, ninferDeploymentReleaseBundle); err == nil {
		t.Fatal("resume NInfer deployment override was accepted for a llama.cpp session")
	}
}

func TestRemoteRuntimeReadinessMatchesSavedDeployment(t *testing.T) {
	source := selectedRuntimeReadyCommand(sessionstate.State{Runtime: runtimeNInfer})
	for _, required := range []string{"root/bin/ninfer-serve", ".stint-deployment", ninferDeploymentSourceBuild, ninferSourceCommit} {
		if !strings.Contains(source, required) {
			t.Fatalf("source-build readiness check is missing %q: %s", required, source)
		}
	}
	release := selectedRuntimeReadyCommand(sessionstate.State{Runtime: runtimeNInfer, RuntimeDeployment: ninferDeploymentReleaseBundle})
	for _, required := range []string{"root/bin/ninfer-serve", ".stint-deployment", ninferDeploymentReleaseBundle, ninferRuntimeReleaseTag, ninferRuntimeBundleSHA256} {
		if !strings.Contains(release, required) {
			t.Fatalf("release-bundle readiness check is missing %q: %s", required, release)
		}
	}
	if !strings.Contains(release, "[ 'release-bundle' = source-build ] && [ -z \"$actual\" ]") {
		t.Fatalf("release-bundle readiness must reject the marker-free legacy source path: %s", release)
	}
	if !strings.Contains(source, "pgrep -x ninfer-serve") {
		t.Fatalf("legacy active NInfer servers should resume without relabeling their unknown source SHA: %s", source)
	}
}

func TestRuntimeAwareLaunchKeepsLlamaFallbackConservative(t *testing.T) {
	ninferState := sessionstate.State{Runtime: runtimeNInfer, ContextTokens: 126976}
	if command := remoteModelLaunchCommandForState(ninferState); !strings.Contains(command, "ninfer-serve") {
		t.Fatal("NInfer state did not select ninfer-serve")
	}

	llamaState := sessionstate.State{Runtime: runtimeLlamaCpp, ContextTokens: interactiveContext}
	command := remoteModelLaunchCommandForState(llamaState)
	if !strings.Contains(command, "llama-server") {
		t.Fatal("llama.cpp state did not select llama-server")
	}
	if !strings.Contains(command, "-c 16384") {
		t.Fatalf("llama.cpp fallback command did not preserve 16384-token context: %s", command)
	}
	// Live inference observation requires the engine's /metrics and /slots
	// endpoints, which llama.cpp only serves with these explicit flags.
	for _, required := range []string{"--metrics", "--slots"} {
		if !strings.Contains(command, required) {
			t.Fatalf("llama.cpp launch missing %q, live inference telemetry would be unavailable", required)
		}
	}
}
