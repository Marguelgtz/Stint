package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

const (
	perfDefaultPromptTokens = 8192
	perfMinPromptTokens     = 32
	perfMaxPromptTokens     = 200000
	perfMinCompletionTokens = 32
	perfMaxCompletionTokens = 2048
	perfBenchmarkTimeoutCap = 20 * time.Minute
)

// perfPromptSentences is a fixed technical corpus used to build deterministic
// benchmark prompts of a requested token depth. The tokenizer ratio for this
// prose is approximately 1.3 tokens per word; the exact prompt token count is
// always reported from the endpoint's usage response, never from this
// estimate.
var perfPromptSentences = []string{
	"A continuous batching inference server schedules prompt prefill and token decode across a shared GPU memory pool.",
	"The scheduler admits new requests while keeping the active batch within the reserved key-value cache capacity.",
	"Prefill chunks split long prompts into fixed windows so that large context encodes never block the decode loop.",
	"Key-value cache blocks are allocated per sequence and evicted when the sequence finishes or the budget is exceeded.",
	"Flash attention reduces the memory traffic of attention from quadratic reads to a fused tilewise kernel.",
	"Speculative decoding verifies a small draft window against the target model to amplify effective throughput.",
	"The draft model is kept small enough that its forward pass costs far less than the target verification step.",
	"Mixture-of-experts routing activates only a subset of expert weights for each token to cut memory bandwidth.",
	"Tensor parallelism shards each layer across devices and exchanges partial results through a high-speed interconnect.",
	"Paged attention manages key-value memory in fixed pages so that fragmentation does not cap concurrent sequences.",
	"The OpenAI-compatible endpoint multiplexes many clients over one local OpenAPI surface without exposing host details.",
	"Streaming responses ship each generated token as it is sampled so clients observe low perceived latency.",
	"Sampling temperature and top-p controls shape the distribution while deterministic decoding keeps output stable.",
	"Context length sets the ceiling for prompt plus completion tokens; exceeding it queues or rejects the request.",
	"Checkpointing the serving state lets an interrupted session resume without re-downloading the model artifact.",
	"Model weights are verified by checksum before the server starts so that corrupt downloads fail fast and loudly.",
	"The local tunnel forwards a stable loopback address to the remote worker without advertising its network path.",
	"Watchdog supervision guarantees that paid compute is destroyed at the session deadline even if the CLI exits.",
	"Read-only telemetry probes sample GPU utilization, memory, power, and temperature without mutating the runtime.",
	"Prompt depth determines how much of the context window a request actually occupies at prefill time.",
	"Decode throughput depends on batch size, key-value layout, and how much GPU memory the weights and caches consume.",
	"Time to first token grows with prefill size, which is why shallow benchmarks understate long-context agent behavior.",
	"Long-context agent workloads re-send the growing conversation on every turn, so prompt depth increases over a session.",
	"The benchmark payload mirrors the chat completion contract that coding agents send through the local endpoint.",
	"Usage accounting reports prompt and completion token counts so that measured depths are comparable across runtimes.",
	"Quantized weight formats trade a small quality delta for the memory headroom that long contexts require.",
	"KV cache quantization shrinks per-token cache state and buys additional concurrent sequences on the same GPU.",
	"Prefill chunk size balances kernel occupancy against the latency of very large context encodes.",
	"Request queues bound pending work so a burst of agents cannot push in-flight decodes past their deadline.",
	"Health checks poll the runtime process and the model endpoint before the session is declared ready.",
	"Recovery state distinguishes an interrupted paid session from a cleanly torn-down one so resume can act on it.",
	"Marketplace planning ranks offers under hard budget and reliability ceilings before any resource is rented.",
	"Network qualification samples real transfer throughput over the SSH path before the host is trusted with a model.",
	"Instance ownership is recorded as soon as the provider returns an identifier so cleanup can never lose the handle.",
	"Deadline enforcement converts the promised session length into a hard stop that releases the GPU to the marketplace.",
	"Session snapshots separate authoritative lifecycle state from volatile observations that may fail independently.",
	"Dashboard rendering keeps the most recent passive sample in memory between remote refreshes to stay responsive.",
	"Concurrency limits cap how many sequences share the GPU, which trades aggregate throughput for per-request latency.",
	"Memory pressure at full prompt depth can force the runtime to reject new requests even while one decode is in flight.",
	"Every measurement in the performance cache is tagged with the prompt depth that produced it so consumers never mix depths.",
	"Comparative benchmarking requires identical prompt depths so runtime differences are not masked by prefill differences.",
	"Agent turn latency is dominated by prompt encoding once the conversation accumulates past a few thousand tokens.",
}

// perfPromptCorpus pairs each base sentence with its word count so the
// builder can target a word budget precisely instead of counting sentences.
type perfPromptCorpus struct {
	sentences []string
	words     []int // per-sentence word counts
	passWords int   // total words in one full corpus pass
}

var perfCorpus = buildPerfPromptCorpus()

func buildPerfPromptCorpus() perfPromptCorpus {
	corpus := perfPromptCorpus{
		sentences: perfPromptSentences,
		words:     make([]int, len(perfPromptSentences)),
	}
	for i, sentence := range perfPromptSentences {
		corpus.words[i] = len(strings.Fields(sentence))
		corpus.passWords += corpus.words[i]
	}
	return corpus
}

// perfWordsPerToken converts a token target into an approximate word target.
// It is deliberately conservative: the real prompt token count is reported by
// the endpoint and shown to the user, so a slight overshoot is acceptable.
const perfWordsPerToken = 10.0 / 13 // ≈ 0.77 words per token

// buildPerfPrompt constructs a deterministic prompt with approximately
// tokenTarget tokens, where the approximation converts tokens to words via
// perfWordsPerToken. Each pass over the corpus is rotated by one sentence so
// repeated passes stay varied without changing the word budget. The result is
// stable for a given target so that repeated benchmark runs compare identical
// payloads; the endpoint's usage report is the authoritative prompt depth.
func buildPerfPrompt(tokenTarget int) string {
	words := int(float64(tokenTarget) * perfWordsPerToken)
	if words < perfCorpus.passWords {
		// Smaller than one base corpus pass: still emit the full corpus so
		// even the shallowest benchmark measures a real, non-degenerate prompt.
		words = perfCorpus.passWords
	}
	var b strings.Builder
	b.WriteString("The following is a synthetic reference text used to measure inference performance. It has no operational meaning. ")
	pass := 0
	for emitted := 0; emitted < words; pass++ {
		fmt.Fprintf(&b, "Pass %d. ", pass+1)
		for j := 0; j < len(perfCorpus.sentences) && emitted < words; j++ {
			index := (j + pass) % len(perfCorpus.sentences)
			b.WriteString(perfCorpus.sentences[index] + " ")
			emitted += perfCorpus.words[index]
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// validatePerfDepth checks the requested benchmark depths against the active
// session context. The configured context is a ceiling: a request is only
// served when prompt plus completion tokens fit inside it.
func validatePerfDepth(contextTokens, promptTokens, maxTokens int) error {
	if promptTokens < perfMinPromptTokens || promptTokens > perfMaxPromptTokens {
		return fmt.Errorf("--prompt-tokens must be between %d and %d", perfMinPromptTokens, perfMaxPromptTokens)
	}
	if maxTokens < perfMinCompletionTokens || maxTokens > perfMaxCompletionTokens {
		return fmt.Errorf("--tokens must be between %d and %d", perfMinCompletionTokens, perfMaxCompletionTokens)
	}
	if contextTokens <= 0 {
		return errors.New("active session has no context size")
	}
	if promptTokens+maxTokens > contextTokens {
		return fmt.Errorf("prompt depth %d plus %d completion tokens exceeds the active context of %d; lower --prompt-tokens or start a larger NInfer config",
			promptTokens, maxTokens, contextTokens)
	}
	return nil
}

// perfBenchmarkTimeout scales the per-run bound with the requested depths:
// a pessimistic prefill rate of 2000 tokens/s, a pessimistic decode rate of
// 100 tokens/s, and a fixed two-minute margin for connection and scheduling.
func perfBenchmarkTimeout(promptTokens, maxTokens int) time.Duration {
	timeout := 2*time.Minute +
		time.Duration(promptTokens/2000)*time.Second +
		time.Duration(maxTokens/100)*time.Second
	if timeout < 3*time.Minute {
		timeout = 3 * time.Minute
	}
	if timeout > perfBenchmarkTimeoutCap {
		timeout = perfBenchmarkTimeoutCap
	}
	return timeout
}

// perfCompletionPayload builds the OpenAI-compatible chat completion request
// the benchmark sends through the local endpoint.
func perfCompletionPayload(prompt string, maxTokens int) map[string]any {
	return map[string]any{
		"model": interactiveModelAlias,
		"messages": []map[string]string{{
			"role":    "user",
			"content": prompt,
		}},
		"max_tokens":     maxTokens,
		"temperature":    0,
		"stream":         true,
		"stream_options": map[string]bool{"include_usage": true},
	}
}

const perfGPUSampleCommand = `if command -v nvidia-smi >/dev/null 2>&1; then nvidia-smi --query-gpu=utilization.gpu,memory.used,memory.total,power.draw,power.limit,temperature.gpu --format=csv,noheader,nounits | head -n 1; else echo 'STINT_GPU_ERROR=nvidia-smi unavailable'; fi`

// samplePerfGPU takes one read-only GPU sample immediately after the
// benchmark so the reported VRAM reflects memory pressure at the measured
// prompt depth. It never mutates the remote host.
func samplePerfGPU(ctx context.Context, paths config.Paths, state sessionstate.State) (gpuTelemetry, error) {
	if state.SSHHost == "" {
		return gpuTelemetry{}, errors.New("session has no SSH endpoint")
	}
	sampleCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := runSSH(sampleCtx, paths, state, perfGPUSampleCommand)
	if err != nil {
		return gpuTelemetry{}, errors.New(compactTelemetryError(err))
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "STINT_GPU_ERROR=") {
			return gpuTelemetry{}, errors.New(strings.TrimSpace(strings.TrimPrefix(line, "STINT_GPU_ERROR=")))
		}
		if line == "" {
			continue
		}
		gpu, err := parseNvidiaSMILine(line, time.Now().UTC())
		if err != nil {
			return gpuTelemetry{}, err
		}
		if !gpu.Available {
			return gpuTelemetry{}, errors.New("nvidia-smi returned no usable GPU sample")
		}
		return gpu, nil
	}
	return gpuTelemetry{}, errors.New("nvidia-smi returned no sample")
}
