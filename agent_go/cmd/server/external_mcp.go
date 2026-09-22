package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentworksclient"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// externalMCPPath is the MCP Streamable HTTP endpoint. It exposes the same
// external tool catalog, schemas, scopes, and PAT auth as the REST external
// API for hosted MCP clients (ChatGPT, Claude Cowork) that cannot spawn the
// local stdio bridge. The MCP layer is a transport adapter only: every tool
// call runs through handleExternalCall, so the MCP surface can never drift
// from the REST surface.
const externalMCPPath = "/api/external/v1/mcp"

// handleExternalMCP serves the scope-filtered external tool catalog over MCP
// Streamable HTTP. It runs stateless: every request is independently
// authenticated, matching the per-request PAT validation of the REST API.
func (api *StreamingAPI) handleExternalMCP(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		externalError(w, http.StatusUnauthorized, "unauthorized", "Sign in to AgentWorks.")
		return
	}
	// Header-less MCP clients pass the token in ?token=; never let responses cache.
	w.Header().Set("Cache-Control", "no-store")
	catalog, err := externalTools()
	if err != nil {
		externalError(w, http.StatusInternalServerError, "schema_error", err.Error())
		return
	}
	allowed := make([]externalTool, 0, len(catalog))
	for _, tool := range catalog {
		if externalTokenAllows(claims, tool) {
			allowed = append(allowed, tool)
		}
	}
	instructions := agentworksclient.MCPReadOnlyInstructions
	for _, tool := range allowed {
		// The catalog omits run tools from tokens lacking runs:execute, so
		// execute_step's presence proves this connection runs.
		if tool.Name == "execute_step" {
			instructions = agentworksclient.MCPInstructions
			break
		}
	}
	mcpServer := server.NewMCPServer("AgentWorks", "1.0.0",
		server.WithToolCapabilities(false),
		server.WithInstructions(instructions),
	)
	for _, tool := range allowed {
		name := tool.Name
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			externalError(w, http.StatusInternalServerError, "schema_error", err.Error())
			return
		}
		mcpServer.AddTool(
			mcp.NewToolWithRawSchema(name, tool.Description, schema),
			func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return api.externalMCPCall(ctx, r, name, request), nil
			},
		)
	}
	httpServer := server.NewStreamableHTTPServer(mcpServer,
		server.WithStateLess(true),
		// This is an authenticated deployment behind Caddy, not a local
		// browser-driven server: the loopback listener plus public Host
		// header would trip the DNS-rebinding guard on every proxied request.
		server.WithDisableLocalhostProtection(true),
	)
	httpServer.ServeHTTP(w, r)
}

// externalMCPCall dispatches one MCP tool call through the REST external
// dispatcher and converts its response to an MCP result.
func (api *StreamingAPI) externalMCPCall(ctx context.Context, r *http.Request, name string, request mcp.CallToolRequest) *mcp.CallToolResult {
	args := request.GetArguments()
	if args == nil {
		args = map[string]any{}
	}
	body, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		return mcp.NewToolResultError("failed to encode tool arguments: " + err.Error())
	}
	sub := r.Clone(ctx)
	sub.Body = io.NopCloser(bytes.NewReader(body))
	sub.ContentLength = int64(len(body))
	rec := &externalMCPRecorder{header: http.Header{}}
	api.handleExternalCall(rec, sub)
	if rec.status != http.StatusOK {
		return mcp.NewToolResultError(externalMCPErrorText(rec))
	}
	var value any
	if err := json.Unmarshal(rec.body.Bytes(), &value); err != nil {
		return mcp.NewToolResultText(rec.body.String())
	}
	result, err := mcp.NewToolResultJSON(value)
	if err != nil {
		return mcp.NewToolResultText(rec.body.String())
	}
	return result
}

// externalMCPRecorder captures the REST dispatcher's response for conversion
// to an MCP tool result.
type externalMCPRecorder struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (rec *externalMCPRecorder) Header() http.Header { return rec.header }

func (rec *externalMCPRecorder) Write(b []byte) (int, error) {
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	return rec.body.Write(b)
}

func (rec *externalMCPRecorder) WriteHeader(status int) {
	if rec.status == 0 {
		rec.status = status
	}
}

// externalMCPErrorText renders a failed REST dispatch as tool error text,
// preserving the machine-readable error code.
func externalMCPErrorText(rec *externalMCPRecorder) string {
	status := rec.status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	var failure struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.body.Bytes(), &failure); err == nil && failure.Error.Code != "" {
		return fmt.Sprintf("%s: %s", failure.Error.Code, failure.Error.Message)
	}
	raw := rec.body.String()
	const maxRaw = 1024
	if len(raw) > maxRaw {
		raw = raw[:maxRaw] + "…"
	}
	return fmt.Sprintf("http_%d: %s", status, raw)
}
