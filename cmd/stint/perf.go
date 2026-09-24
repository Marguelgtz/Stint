package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const perfMaxAttempts = 3

var errPerfNoVisibleContent = errors.New("stream completed without user-visible content")

// perf is handled before main's command switch so it can remain a focused,
// stackable feature without coupling benchmark logic into the lifecycle path.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "perf" {
		return
	}
	if wantsHelp(os.Args[2:]) {
		printCommandHelp("perf")
		os.Exit(0)
	}
	if err := runPerf(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "stint:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

type perfUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type perfChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *perfUsage `json:"usage,omitempty"`
}

type perfSample struct {
	TTFT                    time.Duration
	Total                   time.Duration
	PromptTokens            int
	CompletionTokens        int
	DecodeTokensSec         float64
	DecodeAvailable         bool
	DecodeUnavailableReason string
}

type perfBenchmarkFunc func(context.Context, *http.Client, string, int) (perfSample, error)

func runPerf(args []string) error {
	fs := flag.NewFlagSet("perf", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	runs := fs.Int("runs", 3, "number of benchmark requests")
	maxTokens := fs.Int("tokens", 256, "maximum completion tokens per request")
	promptTokens := fs.Int("prompt-tokens", perfDefaultPromptTokens, "target prompt depth in tokens (32-200000)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *runs < 1 || *runs > 10 {
		return errors.New("--runs must be between 1 and 10")
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	state, err := sessionstate.Load(paths)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("no active Stint session; start or resume compute first")
	}
	if err != nil {
		return err
	}
	if err := validatePerfDepth(contextForState(state), *promptTokens, *maxTokens); err != nil {
		return err
	}
	prompt := buildPerfPrompt(*promptTokens)

	fmt.Println("SESSION PERFORMANCE")
	fmt.Println()
	fmt.Printf("GPU             %s\n", state.GPUModel)
	fmt.Printf("Runtime         %s\n", runtimeForState(state))
	fmt.Printf("Context         %d\n", contextForState(state))
	fmt.Printf("Instance        %d\n", state.InstanceID)
	fmt.Printf("Prompt depth    %d tokens requested\n", *promptTokens)
	fmt.Printf("Benchmark       %d runs x %d max tokens\n", *runs, *maxTokens)
	fmt.Println()

	// llama.cpp may close an HTTP keep-alive connection between streamed POSTs.
	// A fresh local TCP connection per benchmark run avoids reusing a stale
	// connection while keeping the SSH forwarding path identical for all runtimes.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	timeout := perfBenchmarkTimeout(*promptTokens, *maxTokens)
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
	samples := make([]perfSample, 0, *runs)
	for i := 0; i < *runs; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		sample, attempts, err := benchmarkCompletionWithRetry(ctx, client, prompt, *maxTokens, benchmarkCompletion)
		cancel()
		if err != nil {
			return fmt.Errorf("benchmark run %d after %d attempts: %w", i+1, attempts, err)
		}
		if attempts > 1 {
			fmt.Printf("Run %-2d          recovered after %d attempts\n", i+1, attempts)
		}
		samples = append(samples, sample)
		decode := fmt.Sprintf("%.1f tok/s", sample.DecodeTokensSec)
		if !sample.DecodeAvailable {
			decode = "unavailable"
		}
		fmt.Printf("Run %-2d          TTFT %6.2fs   total %6.2fs   decode %s\n",
			i+1, sample.TTFT.Seconds(), sample.Total.Seconds(), decode)
	}

	avg := averagePerf(samples)
	fmt.Println()
	fmt.Println("AVERAGE")
	fmt.Printf("TTFT            %.2fs\n", avg.TTFT.Seconds())
	fmt.Printf("Total latency   %.2fs\n", avg.Total.Seconds())
	if avg.PromptTokens > 0 {
		fmt.Printf("Prompt tokens   %d\n", avg.PromptTokens)
	}
	if avg.CompletionTokens > 0 {
		fmt.Printf("Output tokens   %d\n", avg.CompletionTokens)
	}
	if avg.DecodeAvailable {
		fmt.Printf("Decode speed    %.1f tok/s\n", avg.DecodeTokensSec)
	} else {
		fmt.Printf("Decode speed    unavailable · %s\n", avg.DecodeUnavailableReason)
	}
	if gpu, err := samplePerfGPU(context.Background(), paths, state); err == nil && gpu.MemoryUsedMiB != nil && gpu.MemoryTotalMiB != nil {
		extra := ""
		if gpu.UtilizationPercent != nil {
			extra = fmt.Sprintf(" (utilization %.0f%%)", *gpu.UtilizationPercent)
		}
		fmt.Printf("VRAM at depth   %.1f / %.1f GB%s\n", *gpu.MemoryUsedMiB/1024, *gpu.MemoryTotalMiB/1024, extra)
	} else if err != nil {
		fmt.Printf("VRAM at depth   unavailable · %s\n", err)
	}
	if err := savePerformanceSample(paths, state, avg, time.Now().UTC()); err != nil {
		return fmt.Errorf("benchmark completed but cache write failed: %w", err)
	}
	fmt.Println("Sample          cached for status/dashboard telemetry")
	fmt.Println("\nUses the local OpenAI-compatible endpoint, so llama.cpp and NInfer are measured through the same path.")
	return nil
}

func benchmarkCompletionWithRetry(ctx context.Context, client *http.Client, prompt string, maxTokens int, benchmark perfBenchmarkFunc) (perfSample, int, error) {
	var lastErr error
	for attempt := 1; attempt <= perfMaxAttempts; attempt++ {
		sample, err := benchmark(ctx, client, prompt, maxTokens)
		if err == nil {
			return sample, attempt, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return perfSample{}, attempt, ctx.Err()
		}
		if errors.Is(err, errPerfNoVisibleContent) {
			return perfSample{}, attempt, fmt.Errorf("%w; increase --tokens to allow a visible response", err)
		}
		if attempt == perfMaxAttempts {
			break
		}

		delay := time.Duration(attempt) * 250 * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return perfSample{}, attempt, ctx.Err()
		case <-timer.C:
		}
	}
	return perfSample{}, perfMaxAttempts, lastErr
}

func benchmarkCompletion(ctx context.Context, client *http.Client, prompt string, maxTokens int) (perfSample, error) {
	payload := perfCompletionPayload(prompt, maxTokens)
	body, err := json.Marshal(payload)
	if err != nil {
		return perfSample{}, err
	}
	endpoint := "http://127.0.0.1:" + strconv.Itoa(clinePort) + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return perfSample{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Close = true

	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return perfSample{}, fmt.Errorf("Cline endpoint unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return perfSample{}, fmt.Errorf("endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}

	return readPerfStream(resp.Body, started)
}

func readPerfStream(body io.Reader, started time.Time) (perfSample, error) {
	return readPerfStreamAt(body, started, time.Now)
}

func readPerfStreamAt(body io.Reader, started time.Time, now func() time.Time) (perfSample, error) {
	sample := perfSample{}
	var firstVisible, firstGenerated, lastGenerated time.Time
	generationUpdates := 0
	scanner := bufio.NewScanner(body)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk perfChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		for _, choice := range chunk.Choices {
			content := choice.Delta.Content
			reasoning := choice.Delta.ReasoningContent
			var at time.Time
			if content != "" || reasoning != "" {
				at = now()
				generationUpdates++
				if firstGenerated.IsZero() {
					firstGenerated = at
				}
				lastGenerated = at
			}
			if content != "" && firstVisible.IsZero() {
				firstVisible = at
			}
		}
		if chunk.Usage != nil {
			sample.PromptTokens = chunk.Usage.PromptTokens
			sample.CompletionTokens = chunk.Usage.CompletionTokens
		}
	}
	if err := scanner.Err(); err != nil {
		return perfSample{}, fmt.Errorf("stream interrupted: %w", err)
	}
	sample.Total = now().Sub(started)
	if firstVisible.IsZero() {
		return perfSample{}, errPerfNoVisibleContent
	}
	sample.TTFT = firstVisible.Sub(started)
	switch {
	case sample.CompletionTokens <= 1:
		sample.DecodeUnavailableReason = "endpoint did not report multiple completion tokens"
	case generationUpdates != sample.CompletionTokens:
		sample.DecodeUnavailableReason = fmt.Sprintf("stream exposed %d generation updates for %d completion tokens", generationUpdates, sample.CompletionTokens)
	case lastGenerated.Sub(firstGenerated) <= 0:
		sample.DecodeUnavailableReason = "stream did not expose a measurable token interval"
	default:
		sample.DecodeTokensSec = float64(sample.CompletionTokens-1) / lastGenerated.Sub(firstGenerated).Seconds()
		sample.DecodeAvailable = true
	}
	return sample, nil
}

func averagePerf(samples []perfSample) perfSample {
	if len(samples) == 0 {
		return perfSample{}
	}
	var result perfSample
	result.DecodeAvailable = true
	for _, sample := range samples {
		result.TTFT += sample.TTFT
		result.Total += sample.Total
		result.PromptTokens += sample.PromptTokens
		result.CompletionTokens += sample.CompletionTokens
		if !sample.DecodeAvailable {
			result.DecodeAvailable = false
			if result.DecodeUnavailableReason == "" {
				result.DecodeUnavailableReason = sample.DecodeUnavailableReason
			}
		} else {
			result.DecodeTokensSec += sample.DecodeTokensSec
		}
	}
	n := len(samples)
	result.TTFT /= time.Duration(n)
	result.Total /= time.Duration(n)
	result.PromptTokens /= n
	result.CompletionTokens /= n
	if result.DecodeAvailable {
		result.DecodeTokensSec /= float64(n)
	} else {
		result.DecodeTokensSec = 0
		if result.DecodeUnavailableReason == "" {
			result.DecodeUnavailableReason = "one or more runs lacked complete token-level stream data"
		}
	}
	return result
}
