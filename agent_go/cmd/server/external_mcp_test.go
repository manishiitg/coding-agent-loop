package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/accesstokens"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentworksclient"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// serveExternalMCP exposes handleExternalMCP over a real HTTP server with the
// given claims, so tests can drive it with an actual Streamable HTTP client.
func serveExternalMCP(t *testing.T, api *StreamingAPI, claims *UserClaims) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.handleExternalMCP(w, r.WithContext(context.WithValue(r.Context(), UserContextKey, claims)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func dialExternalMCP(t *testing.T, ctx context.Context, url string) *client.Client {
	t.Helper()
	httpTransport, err := transport.NewStreamableHTTP(url)
	if err != nil {
		t.Fatal(err)
	}
	cli := client.NewClient(httpTransport)
	t.Cleanup(func() { _ = cli.Close() })
	if err := cli.Start(ctx); err != nil {
		t.Fatalf("start MCP client: %v", err)
	}
	return cli
}

func initializeExternalMCP(t *testing.T, ctx context.Context, cli *client.Client) *mcp.InitializeResult {
	t.Helper()
	result, err := cli.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2024-11-05",
			Capabilities:    mcp.ClientCapabilities{},
			ClientInfo:      mcp.Implementation{Name: "external-mcp-test", Version: "0.0.0"},
		},
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return result
}

func listExternalMCPTools(t *testing.T, ctx context.Context, cli *client.Client) map[string]mcp.Tool {
	t.Helper()
	result, err := cli.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	tools := make(map[string]mcp.Tool, len(result.Tools))
	for _, tool := range result.Tools {
		tools[tool.Name] = tool
	}
	return tools
}

func marshalStructured(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func marshalContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	data, err := json.Marshal(result.Content)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestExternalMCPRequiresIdentity(t *testing.T) {
	api := &StreamingAPI{}
	w := httptest.NewRecorder()
	api.handleExternalMCP(w, httptest.NewRequest(http.MethodPost, externalMCPPath, nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401: %s", w.Code, w.Body.String())
	}
}

func TestExternalMCPStreamableListAndCall(t *testing.T) {
	f := newExternalToolsFixture(t)
	srv := serveExternalMCP(t, f.api, &UserClaims{UserID: "owner", Username: "owner"})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cli := dialExternalMCP(t, ctx, srv.URL+externalMCPPath)
	initResult := initializeExternalMCP(t, ctx, cli)
	if initResult.Instructions != agentworksclient.MCPInstructions {
		t.Fatalf("run-capable connection got read-only instructions: %q", initResult.Instructions)
	}

	tools := listExternalMCPTools(t, ctx, cli)
	for _, name := range []string{"list_workflows", "get_plan", "read_file", "execute_step", "get_agent_context"} {
		if _, ok := tools[name]; !ok {
			t.Fatalf("MCP catalog missing %q (got %d tools)", name, len(tools))
		}
	}

	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		result, err := cli.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{Name: name, Arguments: args},
		})
		if err != nil {
			t.Fatalf("call %s: %v", name, err)
		}
		if result.IsError {
			t.Fatalf("call %s returned tool error: %+v", name, result.Content)
		}
		return result
	}

	workflows := call("list_workflows", map[string]any{})
	if got := marshalStructured(t, workflows); !strings.Contains(got, "invoices") {
		t.Fatalf("list_workflows result missing invoices workflow: %s", got)
	}
	process := call("read_file", map[string]any{"workflow_id": "invoices", "path": "docs/process.md"})
	if got := marshalStructured(t, process); !strings.Contains(got, "reviewed weekly") {
		t.Fatalf("read_file result missing expected content: %s", got)
	}
	// A workflow the caller cannot see surfaces as a tool error carrying the
	// REST error code, not a protocol failure.
	denied, err := cli.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "read_file", Arguments: map[string]any{"workflow_id": "secret", "path": "docs/process.md"}},
	})
	if err != nil {
		t.Fatalf("denied call: %v", err)
	}
	if !denied.IsError {
		t.Fatal("read of an invisible workflow unexpectedly succeeded")
	}
	if got := marshalContent(t, denied); !strings.Contains(got, "workflow_not_found") {
		t.Fatalf("denied call missing REST error code: %s", got)
	}

	if _, err := cli.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "update_workflow_config", Arguments: map[string]any{}},
	}); err == nil {
		t.Fatal("unexposed tool call unexpectedly succeeded")
	}
}

func TestExternalMCPRespectsTokenScopes(t *testing.T) {
	f := newExternalToolsFixture(t)
	claims := &UserClaims{UserID: "owner", Username: "owner", AccessToken: &accesstokens.Token{
		ID: "read-only-token", Scopes: []string{"workflows:read", "files:read"}, AllWorkflows: true,
	}}
	srv := serveExternalMCP(t, f.api, claims)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cli := dialExternalMCP(t, ctx, srv.URL+externalMCPPath)
	initResult := initializeExternalMCP(t, ctx, cli)
	if initResult.Instructions != agentworksclient.MCPReadOnlyInstructions {
		t.Fatalf("read-only connection got run instructions: %q", initResult.Instructions)
	}
	tools := listExternalMCPTools(t, ctx, cli)
	for _, name := range []string{"list_workflows", "read_file", "get_agent_context"} {
		if _, ok := tools[name]; !ok {
			t.Fatalf("read-only catalog missing %q", name)
		}
	}
	for _, name := range []string{"execute_step", "chat", "trigger_schedule", "run_reply_input"} {
		if _, ok := tools[name]; ok {
			t.Fatalf("read-only catalog unexpectedly exposes %q", name)
		}
	}
}
