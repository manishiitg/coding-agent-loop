package orchestrator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcp-agent-builder-go/agent_go/pkg/workspace"
)

func TestReadWorkspaceFileAcceptsExistingEmptyFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"filepath":"empty.txt","content":"","size":0}}`))
	}))
	defer server.Close()

	bo := &BaseOrchestrator{WorkspaceClient: workspace.NewClient(server.URL)}
	content, err := bo.ReadWorkspaceFile(context.Background(), "empty.txt")
	if err != nil {
		t.Fatalf("empty file should be a successful read: %v", err)
	}
	if content != "" {
		t.Fatalf("expected empty content, got %q", content)
	}
}
