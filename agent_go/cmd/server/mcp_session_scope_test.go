package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	events "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	workshop "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
)

type scopeWorkshop struct{ config *workshop.WorkshopConfig }

func (s *scopeWorkshop) GetConfig() *workshop.WorkshopConfig { return s.config }

func TestWorkshopMCPScopeReadsUpdatedSelectionOnEveryCall(t *testing.T) {
	const workspacePath = "Workflow/scope-test"
	ws := &mockWorkspaceAPI{files: map[string]string{}}
	server := httptest.NewServer(ws)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	setSelection := func(names []string) {
		raw, _ := json.Marshal(map[string]interface{}{"schema_version": 1, "id": "scope-test", "label": "scope-test", "capabilities": map[string]interface{}{"selected_servers": names}})
		ws.mu.Lock()
		ws.files[workspacePath+"/workflow.json"] = string(raw)
		ws.mu.Unlock()
	}
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(configPath, []byte(`{"mcpServers":{"Jam":{"url":"https://jam.example/mcp"},"Notion":{"url":"https://notion.example/mcp","oauth":{"auth_url":"https://notion.example/auth","token_url":"https://notion.example/token"}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	api := &StreamingAPI{mcpConfigPath: configPath, logger: loggerv2.NewNoop(), eventStore: events.NewEventStore(10)}
	api.eventStore.SetSessionOwner("same-chat", "alice")
	api.workshopChatSessions.Store("same-chat", &scopeWorkshop{&workshop.WorkshopConfig{WorkspacePath: workspacePath}})
	setSelection([]string{"Jam"})
	if _, err := api.resolveWorkshopMCPServer(context.Background(), "same-chat", "Notion", "search"); err == nil {
		t.Fatal("Notion allowed before selection")
	}
	setSelection([]string{"Jam", "Notion"})
	got, err := api.resolveWorkshopMCPServer(context.Background(), "same-chat", "Notion", "search")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Notion" || got.Config.OAuth.TokenFile != getUserTokenFilePath("alice", "Notion") {
		t.Fatalf("wrong identity/config: %+v", got)
	}
	setSelection([]string{"Jam"})
	if _, err := api.resolveWorkshopMCPServer(context.Background(), "same-chat", "Notion", "search"); err == nil {
		t.Fatal("removed Notion remained in scope")
	}
	ws.mu.Lock()
	ws.files[workspacePath+"/workflow.json"] = "invalid"
	ws.mu.Unlock()
	if _, err := api.resolveWorkshopMCPServer(context.Background(), "same-chat", "Jam", "search"); err == nil {
		t.Fatal("failed open on invalid manifest")
	}
}

func TestSelectedMCPScopeAliasesToolRestrictionsAndIdentity(t *testing.T) {
	cfg := &mcpclient.MCPConfig{MCPServers: map[string]mcpclient.MCPServerConfig{"test-provider": {URL: "https://example.test/mcp"}, "Jam": {URL: "https://jam.example/mcp"}}}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, err := resolveSelectedMCPServer(cfg, []string{"test-provider"}, []string{"test-provider:search"}, "alice", "test_provider", "search")
	if err != nil || a.Name != "test-provider" {
		t.Fatalf("alias failed: %+v %v", a, err)
	}
	b, err := resolveSelectedMCPServer(cfg, []string{"test-provider"}, nil, "bob", "test-provider", "search")
	if err != nil || a.ConnectionSessionID == b.ConnectionSessionID {
		t.Fatal("accounts share a connection")
	}
	for _, test := range []struct {
		server, tool    string
		selected, tools []string
	}{
		{"Jam", "search", []string{"test-provider"}, nil},
		{"test-provider", "fetch", []string{"test-provider"}, []string{"test-provider:search"}},
		{"test-provider", "search", nil, nil},
	} {
		if _, err := resolveSelectedMCPServer(cfg, test.selected, test.tools, "alice", test.server, test.tool); err == nil {
			t.Fatalf("scope failed open: %+v", test)
		}
	}
	if _, err := resolveSelectedMCPServer(cfg, []string{"test-provider"}, []string{"test_provider:*"}, "alice", "test-provider", "fetch"); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveSelectedMCPServer(cfg, []string{"test-provider"}, nil, "alice", "missing", "search"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatal(err)
	}
}
