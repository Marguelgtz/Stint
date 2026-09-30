package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/Marguelgtz/Stint/internal/config"
	"github.com/Marguelgtz/Stint/internal/deep"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// deepRunStatusToolName is the single tool the local stdio MCP provider
// exposes. The server advertises exactly this one tool and no MCP resources
// or prompts.
const deepRunStatusToolName = "deep_run_status"

// statusServerName identifies this provider to MCP clients.
const statusServerName = "stint-deep-status"

// buildStatusMCPServer wires the validated, read-only Deep run status provider
// as an MCP stdio server exposing exactly one tool (deep_run_status) and no
// resources or prompts. The session is selected once via --session at process
// start and is validated before any state path is opened. The handler is the
// only state access: it reads the selected run through deep.BuildStatusReply,
// which uses Stint's existing validated Deep Work state and journal recovery
// path and never writes a competing status store.
func buildStatusMCPServer(stateDir, sessionID string) (*mcp.Server, error) {
	if err := deep.ValidateStatusSessionID(sessionID); err != nil {
		return nil, err
	}
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    statusServerName,
		Version: version,
	}, &mcp.ServerOptions{
		// Tool-only server: no prompt or resource capability is advertised, so
		// a client sees exactly one tool and no resources or prompts.
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
		// Diagnostics go to stderr only; stdout is reserved for protocol frames.
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	// No arguments: the session is bound at process start via --session, so the
	// tool takes nothing from the client and the projection is fully bounded by
	// the server.
	tool := &mcp.Tool{
		Name:        deepRunStatusToolName,
		Description: "Return the selected Deep Work run's validated, durable status as a bounded JSON projection (schemaVersion 1).",
		InputSchema: map[string]any{"type": "object", "additionalProperties": false},
	}
	handler := func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		_ = req
		data, err := deep.BuildStatusReply(stateDir, sessionID)
		if err != nil {
			// Fail closed: report a fixed, path-free tool error and never an
			// unvalidated success projection. Missing or corrupt state, an
			// over-cap projection, or an invalid selection all surface here.
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "deep run status unavailable"}},
			}, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil
	}
	srv.AddTool(tool, handler)
	return srv, nil
}

// mcpStatusArgs is the resolved, validated argument set for `stint mcp serve`.
type mcpStatusArgs struct {
	stateDir  string
	sessionID string
}

// parseMCPArgs validates the `mcp serve --session <ID>` arguments. The session
// identity is validated before any state path is resolved or opened, so an
// invalid or missing selection fails without touching disk.
func parseMCPArgs(args []string) (mcpStatusArgs, error) {
	fs := flag.NewFlagSet("mcp serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	sessionID := fs.String("session", "", "required: Deep Work session id to expose")
	if err := fs.Parse(args); err != nil {
		return mcpStatusArgs{}, err
	}
	if *sessionID == "" {
		return mcpStatusArgs{}, errors.New("mcp serve requires --session <ID>")
	}
	if err := deep.ValidateStatusSessionID(*sessionID); err != nil {
		return mcpStatusArgs{}, err
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return mcpStatusArgs{}, err
	}
	return mcpStatusArgs{stateDir: paths.StateDir, sessionID: *sessionID}, nil
}

// runMCPCommand implements `stint mcp serve --session <ID>`: a local, read-only
// stdio MCP server exposing the single deep_run_status tool. It makes no
// provider calls, no SSH, no Git mutation, no publication, and no remote
// transport. Stdout carries only MCP protocol frames; diagnostics go to
// stderr. The server runs until the stdio client closes the session.
func runMCPCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("mcp requires a subcommand: serve")
	}
	if args[0] != "serve" {
		return fmt.Errorf("unknown mcp subcommand %q (stint mcp serve --session <ID>)", args[0])
	}
	// --help is handled by the caller before dispatch; keep behavior explicit.
	if wantsHelp(args[1:]) {
		printCommandHelp("mcp")
		return nil
	}
	// Validate the session before opening any state path.
	serveArgs, err := parseMCPArgs(args[1:])
	if err != nil {
		return err
	}
	srv, err := buildStatusMCPServer(serveArgs.stateDir, serveArgs.sessionID)
	if err != nil {
		return err
	}
	ctx := context.Background()
	// The stdio transport reads protocol frames from stdin and writes them to
	// stdout; the server exits when the client closes the session. A client
	// disconnect (EOF on stdin) is a normal session end, not a failure: the
	// process exits cleanly so MCP client launchers see exit 0.
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil &&
		!isCleanStdioShutdown(err) {
		return err
	}
	return nil
}

// isCleanStdioShutdown reports whether a server Run failure is just the
// client going away. The SDK surfaces a clean stdin EOF as a wrapped
// "server is closing" sentinel (its internal connection wait reports the
// shutdown cause), so in addition to the standard EOF/connection-closed
// errors that sentinel message is treated as a normal session end.
func isCleanStdioShutdown(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, mcp.ErrConnectionClosed) {
		return true
	}
	return strings.Contains(err.Error(), "server is closing")
}
