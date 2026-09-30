package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// statusRunFixture builds a real, journaled Deep Work run under
// <stateHome>/stint (the exact path config.DefaultPaths resolves from
// XDG_STATE_HOME) and lands it, so the provider has durable phase, landing
// facts, and bounded task rows. The mission deliberately carries
// secret-bearing prose, a local path, and a verifier command: the protocol
// reply must never include any of them.
func statusRunFixture(t *testing.T) (string, string, string, time.Time) {
	t.Helper()
	stateHome := t.TempDir()
	stateDir := filepath.Join(stateHome, "stint")
	now := time.Date(2026, 9, 30, 17, 13, 48, 0, time.UTC)
	sessionID := "20260930-171348"
	mission := deep.Mission{
		Name:      "status protocol fixture",
		Objective: "local read-only stdio MCP provider for deep run status",
		Success: []string{
			"reply carries no secrets: api_key=sk-live-secret-77aa token=ghp_9f8e7d6c5b4a3f2e1d0c",
			"reply never quotes /var/lib/stint-onbox/state or /home/ops/repo/.stint-deep",
		},
		Constraints: []string{"no provider calls from the status provider"},
		Verify:      "go test ./internal/deep ./cmd/stint",
		Tasks: []deep.Task{
			{ID: "STINT-STATUS-MCP-001", Objective: "implement provider", Status: deep.StatusAccepted, Source: "mission",
				Attempts: 2, CheckpointCommit: "c1a1111111111111111111111111111111111111",
				CheckpointTreeSHA: "tree-11111111111111111111", AcceptanceOutcome: deep.AcceptanceAccepted},
			{ID: "T-2", Objective: "protocol tests", Status: deep.StatusBlocked, Source: "mission",
				Attempts: 1, CheckpointCommit: "c2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2",
				CheckpointTreeSHA: "tree-22222222222222222222", AcceptanceOutcome: deep.AcceptanceNotEvaluated},
		},
	}
	state := deep.NewState(sessionID, mission, "/home/ops/repo", "/home/ops/repo/.stint-deep/"+sessionID,
		now.Add(time.Hour), now.Add(50*time.Minute), 2, now)
	if err := deep.BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatalf("BeginNewRun: %v", err)
	}
	if err := deep.BeginLanding(stateDir, &state, "status protocol fixture landed", now.Add(time.Minute)); err != nil {
		t.Fatalf("BeginLanding: %v", err)
	}
	if err := deep.CompleteLanding(stateDir, &state, "landed-protocol-commit", "landed-protocol-tree", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("CompleteLanding: %v", err)
	}
	return stateHome, stateDir, sessionID, now
}

// connectStatusServer starts the provider server on one end of an in-memory
// transport and connects an MCP client to the other, returning the client
// session. This exercises the real protocol (initialize, capabilities, tool
// discovery, tool invocation) exactly as a stdio client would.
func connectStatusServer(t *testing.T, stateDir, sessionID string) *mcp.ClientSession {
	t.Helper()
	srv, err := buildStatusMCPServer(stateDir, sessionID)
	if err != nil {
		t.Fatalf("buildStatusMCPServer: %v", err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- srv.Run(ctx, serverTransport)
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "status-test-client", Version: "0.0.0"},
		&mcp.ClientOptions{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		<-serverDone
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		<-serverDone
	})
	return session
}

func callDeepRunStatus(t *testing.T, session *mcp.ClientSession) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: deepRunStatusToolName})
	if err != nil {
		t.Fatalf("CallTool(deep_run_status): %v", err)
	}
	return res
}

// decodeStatusJSON extracts the single text content item and decodes it as
// one JSON object; it returns the object and the raw text for leak checks.
func decodeStatusJSON(t *testing.T, res *mcp.CallToolResult) (map[string]json.RawMessage, string) {
	t.Helper()
	if res.IsError {
		t.Fatalf("deep_run_status reported a tool error: %s", contentText(t, res))
	}
	if len(res.Content) != 1 {
		t.Fatalf("content items = %d, want exactly one", len(res.Content))
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want text content", res.Content[0])
	}
	var reply map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text.Text), &reply); err != nil {
		t.Fatalf("reply is not one JSON object: %v\n%s", err, text.Text)
	}
	return reply, text.Text
}

func contentText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var buf bytes.Buffer
	for _, item := range res.Content {
		if text, ok := item.(*mcp.TextContent); ok {
			buf.WriteString(text.Text)
		}
	}
	return buf.String()
}

// TestMCPStatusServerProtocolSurface verifies the protocol-level contract:
// exactly one tool is discovered (deep_run_status), no resources or prompts
// are advertised, and the server implements the latest protocol version.
func TestMCPStatusServerProtocolSurface(t *testing.T) {
	_, stateDir, sessionID, _ := statusRunFixture(t)
	session := connectStatusServer(t, stateDir, sessionID)
	ctx := context.Background()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != deepRunStatusToolName {
		names := make([]string, 0, len(tools.Tools))
		for _, tool := range tools.Tools {
			names = append(names, tool.Name)
		}
		t.Fatalf("tools = %v, want exactly [%s]", names, deepRunStatusToolName)
	}
	// The tool takes no arguments: the session is bound at process start, so
	// the schema is an empty object.
	schema, ok := tools.Tools[0].InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("tool input schema is %T, want a JSON object", tools.Tools[0].InputSchema)
	}
	if schema["type"] != "object" {
		t.Fatalf("tool input schema type = %v, want object", schema["type"])
	}

	resources, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources.Resources) != 0 {
		t.Fatalf("resources advertised: %+v, want none", resources.Resources)
	}
	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	if len(prompts.Prompts) != 0 {
		t.Fatalf("prompts advertised: %+v, want none", prompts.Prompts)
	}

	init := session.InitializeResult()
	if init == nil || init.Capabilities == nil {
		t.Fatal("initialize result missing capabilities")
	}
	if init.Capabilities.Tools == nil {
		t.Fatal("server did not advertise the tools capability")
	}
	if init.Capabilities.Prompts != nil || init.Capabilities.Resources != nil {
		t.Fatalf("server advertised non-tool capabilities: %+v", init.Capabilities)
	}
}

// TestMCPStatusServerReturnsValidatedProjection verifies a protocol-level
// client can retrieve the selected run's durable phase and separate mission
// outcome, and that secret-bearing fixture prose, verifier commands, local
// paths, and raw journal events are all absent from the reply.
func TestMCPStatusServerReturnsValidatedProjection(t *testing.T) {
	_, stateDir, sessionID, _ := statusRunFixture(t)
	session := connectStatusServer(t, stateDir, sessionID)
	res := callDeepRunStatus(t, session)
	reply, raw := decodeStatusJSON(t, res)

	assertReplyKey(t, reply, "schemaVersion", `1`)
	assertReplyKey(t, reply, "sessionId", `"`+sessionID+`"`)
	assertReplyKey(t, reply, "runId", `"`+sessionID+`"`)
	assertReplyKey(t, reply, "runEventWatermark", `3`)
	assertReplyKey(t, reply, "phase", `"landed"`)
	assertReplyKey(t, reply, "missionOutcome", `"incomplete"`)
	assertReplyKey(t, reply, "landingCommit", `"landed-protocol-commit"`)
	assertReplyKey(t, reply, "landingCheckpointTreeSha", `"landed-protocol-tree"`)
	assertReplyKey(t, reply, "deadline", `"2026-09-30T18:13:48Z"`)

	for _, key := range []string{"executionEpochId", "tasks"} {
		if _, present := reply[key]; !present {
			t.Fatalf("reply missing key %q", key)
		}
	}
	// Empty evidence must be omitted, not serialized as null/empty.
	for _, key := range []string{"missionReviewOutcome"} {
		if _, present := reply[key]; present {
			t.Fatalf("empty key %q serialized in reply", key)
		}
	}
	// Allowed top-level keys only.
	allowed := map[string]bool{
		"schemaVersion": true, "sessionId": true, "runId": true, "executionEpochId": true,
		"runEventWatermark": true, "phase": true, "missionOutcome": true, "deadline": true,
		"landingCommit": true, "landingCheckpointTreeSha": true, "missionReviewOutcome": true,
		"tasks": true,
	}
	for key := range reply {
		if !allowed[key] {
			t.Fatalf("reply carries non-allow-listed key %q", key)
		}
	}
	// Bounded task rows with allow-listed keys only.
	var tasks []map[string]json.RawMessage
	if err := json.Unmarshal(reply["tasks"], &tasks); err != nil {
		t.Fatalf("parse tasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("task rows = %d, want 2", len(tasks))
	}
	rowAllowed := map[string]bool{
		"id": true, "source": true, "status": true, "attempts": true,
		"checkpointCommit": true, "checkpointTreeSha": true,
		"acceptanceOutcome": true, "reviewOutcome": true,
	}
	for _, row := range tasks {
		for key := range row {
			if !rowAllowed[key] {
				t.Fatalf("task row carries non-allow-listed key %q", key)
			}
		}
	}
	if string(tasks[0]["id"]) != `"STINT-STATUS-MCP-001"` || string(tasks[0]["status"]) != `"accepted"` {
		t.Fatalf("first task row = %v", tasks[0])
	}
	// Secret-bearing fixture prose, local paths, verifier commands, mission
	// identity, and raw journal events must never appear in the reply.
	for _, leaked := range []string{
		"sk-live-secret-77aa", "ghp_9f8e7d6c5b4a3f2e1d0c", "api_key", "token=",
		"/var/lib/stint-onbox/state", "/home/ops/repo",
		"go test ./internal/deep", "status protocol fixture", "local read-only stdio",
		"run-events", "epoch-started", "landing-started",
	} {
		if strings.Contains(raw, leaked) {
			t.Fatalf("reply leaked %q", leaked)
		}
	}
}

// TestMCPStatusServerFailsClosedOnInvalidAndMissingState verifies missing
// state and corrupt state surface as MCP tool errors, never as a success
// projection.
func TestMCPStatusServerFailsClosedOnInvalidAndMissingState(t *testing.T) {
	t.Run("missing state", func(t *testing.T) {
		stateDir := t.TempDir()
		session := connectStatusServer(t, stateDir, "never-started")
		res := callDeepRunStatus(t, session)
		if !res.IsError {
			t.Fatalf("missing state returned a success projection: %s", contentText(t, res))
		}
		if !strings.Contains(contentText(t, res), "deep run status unavailable") {
			t.Fatalf("tool error text = %q", contentText(t, res))
		}
	})
	t.Run("corrupt state", func(t *testing.T) {
		stateDir := t.TempDir()
		dir := filepath.Join(stateDir, "deep", "corrupt-run")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "deep.json"), []byte("{corrupt"), 0o600); err != nil {
			t.Fatal(err)
		}
		session := connectStatusServer(t, stateDir, "corrupt-run")
		res := callDeepRunStatus(t, session)
		if !res.IsError {
			t.Fatalf("corrupt state returned a success projection: %s", contentText(t, res))
		}
	})
}

// TestParseMCPArgsRejectsInvalidSelectionBeforeDisk proves the session
// identity is validated before any state path is resolved: an invalid or
// missing selection fails without a state directory being involved. These
// are the arguments after the `serve` subcommand.
func TestParseMCPArgsRejectsInvalidSelectionBeforeDisk(t *testing.T) {
	for _, args := range [][]string{
		{}, // missing --session
		{"--session", ""},
		{"--session", ".."},
		{"--session", "../escape"},
		{"--session", "a b"},
		{"--session", "a.b"},
		{"--session", strings.Repeat("a", 129)},
	} {
		if _, err := parseMCPArgs(args); err == nil {
			t.Fatalf("parseMCPArgs(%v) succeeded, want failure", args)
		}
	}
	got, err := parseMCPArgs([]string{"--session", "20260930-171348"})
	if err != nil {
		t.Fatalf("parseMCPArgs with a valid selection: %v", err)
	}
	if got.sessionID != "20260930-171348" || got.stateDir == "" {
		t.Fatalf("parsed args = %+v", got)
	}
}

// TestBuildStatusMCPServerRejectsInvalidSessionBeforeServing verifies the
// server builder itself validates the selection before constructing a server.
func TestBuildStatusMCPServerRejectsInvalidSessionBeforeServing(t *testing.T) {
	for _, sessionID := range []string{"", "..", "../x", "a b", "a.b"} {
		if _, err := buildStatusMCPServer(t.TempDir(), sessionID); err == nil {
			t.Fatalf("buildStatusMCPServer accepted invalid session %q", sessionID)
		}
	}
	if _, err := buildStatusMCPServer(t.TempDir(), "20260930-171348"); err != nil {
		t.Fatalf("buildStatusMCPServer rejected a valid session: %v", err)
	}
}

// assertReplyKey checks that a top-level reply key carries an exact scalar
// JSON value.
func assertReplyKey(t *testing.T, reply map[string]json.RawMessage, key, want string) {
	t.Helper()
	value, present := reply[key]
	if !present {
		t.Fatalf("reply missing key %q", key)
	}
	if string(value) != want {
		t.Fatalf("reply key %s = %s, want %s", key, value, want)
	}
}

// stdioClient is a minimal newline-delimited JSON-RPC client: it sends one
// message and, for requests, reads frames until the matching response id
// appears. Notifications carry no id and are not awaited.
type stdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

// send writes one JSON-RPC message. When id is non-nil it reads frames until
// the matching response and returns it; notifications return without reading.
func (c *stdioClient) send(id json.RawMessage, method string, params any) (map[string]json.RawMessage, error) {
	req := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id,omitempty"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{JSONRPC: "2.0", Method: method, Params: params}
	if id != nil {
		req.ID = json.RawMessage(id)
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(c.stdin, "%s\n", data); err != nil {
		return nil, err
	}
	if id == nil {
		return nil, nil
	}
	var wantID float64
	if err := json.Unmarshal(id, &wantID); err != nil {
		return nil, fmt.Errorf("request id %s is not numeric: %w", id, err)
	}
	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("read stdio frame: %w", err)
		}
		var frame struct {
			ID     any             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(line, &frame); err != nil {
			return nil, fmt.Errorf("parse frame %q: %w", line, err)
		}
		if frame.ID == nil {
			continue // notification from the server
		}
		gotID, ok := frame.ID.(float64)
		if !ok || gotID != wantID {
			continue
		}
		if frame.Error != nil {
			return nil, fmt.Errorf("protocol error: %s", frame.Error.Message)
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(frame.Result, &result); err != nil {
			return nil, fmt.Errorf("parse result: %w", err)
		}
		return result, nil
	}
}

// startStatusStdioServer builds the stint binary once and starts
// `stint mcp serve --session <ID>` as a real subprocess with stdio wired to
// pipes, isolating it in a fresh XDG state home (stateDir lives at
// <stateHome>/stint, as config.DefaultPaths resolves it).
func startStatusStdioServer(t *testing.T, stateHome, sessionID string) *stdioClient {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "stint")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build stint: %v\n%s", err, out)
	}
	cmd := exec.Command(binPath, "mcp", "serve", "--session", sessionID)
	cmd.Env = append(os.Environ(),
		"XDG_STATE_HOME="+stateHome,
		"XDG_CONFIG_HOME="+t.TempDir(),
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrBuf := new(bytes.Buffer)
	cmd.Stderr = stderrBuf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return &stdioClient{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 1024*1024)}
}

// TestMCPStatusStdioSubprocessProtocol drives the real
// `stint mcp serve` binary over stdio: protocol handshake, discovery of
// exactly one tool, and a status call whose single JSON text item is under
// the 64 KiB cap and free of secret-bearing fixture prose and local paths.
func TestMCPStatusStdioSubprocessProtocol(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test skipped in -short mode")
	}
	stateHome, _, sessionID, _ := statusRunFixture(t)
	client := startStatusStdioServer(t, stateHome, sessionID)

	initParams := map[string]any{
		"protocolVersion": "2025-06-18",
		"clientInfo":      map[string]any{"name": "stdio-probe", "version": "0.0.0"},
	}
	result, err := client.send(json.RawMessage("1"), "initialize", initParams)
	if err != nil {
		t.Fatalf("initialize over stdio: %v", err)
	}
	if _, ok := result["capabilities"]; !ok {
		t.Fatalf("initialize result missing capabilities: %v", result)
	}
	if _, err := client.send(nil, "notifications/initialized", nil); err != nil {
		t.Fatalf("initialized notification: %v", err)
	}

	tools, err := client.send(json.RawMessage("1"), "tools/list", nil)
	if err != nil {
		t.Fatalf("tools/list over stdio: %v", err)
	}
	toolsRaw, ok := tools["tools"]
	if !ok {
		t.Fatalf("tools/list result missing tools: %v", tools)
	}
	var toolList []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(toolsRaw, &toolList); err != nil {
		t.Fatalf("parse tools: %v", err)
	}
	if len(toolList) != 1 || toolList[0].Name != deepRunStatusToolName {
		names := make([]string, 0, len(toolList))
		for _, tool := range toolList {
			names = append(names, tool.Name)
		}
		t.Fatalf("stdio tools = %v, want exactly [%s]", names, deepRunStatusToolName)
	}
	// No resources or prompts are exposed by the stdio server: those
	// capabilities are unadvertised, and listing returns empty results.
	resourcesResult, err := client.send(json.RawMessage("1"), "resources/list", nil)
	if err == nil {
		var resources struct {
			Resources []json.RawMessage `json:"resources"`
		}
		if err := json.Unmarshal(resourcesResult["resources"], &resources.Resources); err == nil {
			if len(resources.Resources) != 0 {
				t.Fatalf("stdio server exposed resources: %+v", resources.Resources)
			}
		}
	}
	promptsResult, err := client.send(json.RawMessage("1"), "prompts/list", nil)
	if err == nil {
		var prompts struct {
			Prompts []json.RawMessage `json:"prompts"`
		}
		if err := json.Unmarshal(promptsResult["prompts"], &prompts.Prompts); err == nil {
			if len(prompts.Prompts) != 0 {
				t.Fatalf("stdio server exposed prompts: %+v", prompts.Prompts)
			}
		}
	}

	callResult, err := client.send(json.RawMessage("1"), "tools/call",
		map[string]any{"name": deepRunStatusToolName, "arguments": map[string]any{}})
	if err != nil {
		t.Fatalf("tools/call over stdio: %v", err)
	}
	var call struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	callData, err := json.Marshal(callResult)
	if err != nil {
		t.Fatalf("re-encode call result: %v", err)
	}
	if err := json.Unmarshal(callData, &call); err != nil {
		t.Fatalf("parse call result: %v\n%s", err, callData)
	}
	if call.IsError {
		t.Fatalf("stdio tool call reported an error: %+v", call.Content)
	}
	if len(call.Content) != 1 || call.Content[0].Type != "text" {
		t.Fatalf("stdio content = %+v, want one text item", call.Content)
	}
	text := call.Content[0].Text
	if len(text) > deep.DeepStatusReplyMaxBytes {
		t.Fatalf("stdio reply of %d bytes exceeds the %d byte cap", len(text), deep.DeepStatusReplyMaxBytes)
	}
	for _, leaked := range []string{
		"sk-live-secret-77aa", "ghp_9f8e7d6c5b4a3f2e1d0c", "api_key",
		"/var/lib/stint-onbox/state", "/home/ops/repo",
		"go test ./internal/deep", "status protocol fixture", "run-events",
	} {
		if strings.Contains(text, leaked) {
			t.Fatalf("stdio reply leaked %q", leaked)
		}
	}
	var reply map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &reply); err != nil {
		t.Fatalf("stdio reply is not one JSON object: %v\n%s", err, text)
	}
	assertReplyKey(t, reply, "phase", `"landed"`)
	assertReplyKey(t, reply, "missionOutcome", `"incomplete"`)
}
