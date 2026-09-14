package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkWorkflowReferenceToolsDiscoverAndPersistAuthorizedWorkflows(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"reader","username":"reader","products":[]},{"id":"owner","username":"owner","products":[]}]}`)
	shared, _ := json.Marshal(WorkflowManifest{
		ID: "hdfc-personal", Label: "HDFC Bank Personal Accounts",
		Access: &WorkflowAccess{Owners: []string{"owner"}, Readers: []string{"reader"}},
	})
	private, _ := json.Marshal(WorkflowManifest{
		ID: "private-bank", Label: "Private Bank",
		Access: &WorkflowAccess{Owners: []string{"owner"}},
	})
	productPath := "_users/reader/Chats/Work/projects/banking/product.json"
	workspace := &mockWorkspaceAPI{files: map[string]string{
		manifestPath("Workflow/hdfc-personal"): string(shared),
		manifestPath("Workflow/private-bank"):  string(private),
		productPath:                            `{"schema_version":1,"product":"work","id":"banking","title":"Banking","custom":"keep","capabilities":{"selected_skills":["work-dashboard"]}}`,
	}}
	host := httptest.NewServer(workspace)
	defer host.Close()
	t.Setenv("WORKSPACE_API_URL", host.URL)

	api := &StreamingAPI{}
	registrar := &recordingRegistrar{}
	if err := api.registerWorkWorkflowReferenceTools(registrar, "reader", "session-1", "_users/reader/Chats/Work/projects/banking"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"list_accessible_workflows", "attach_workflow_reference", "detach_workflow_reference"} {
		if _, ok := registrar.tools[name]; !ok {
			t.Fatalf("tool %q was not registered", name)
		}
	}

	out, err := registrar.tools["list_accessible_workflows"].exec(context.Background(), map[string]interface{}{"query": "hdfc"})
	if err != nil || !strings.Contains(out, `"workspace_path": "Workflow/hdfc-personal"`) || strings.Contains(out, "Private Bank") {
		t.Fatalf("accessible search out=%s err=%v", out, err)
	}
	if _, err := registrar.tools["attach_workflow_reference"].exec(context.Background(), map[string]interface{}{"workspace_path": "Workflow/private-bank"}); err == nil {
		t.Fatal("unauthorized workflow was attached")
	}
	out, err = registrar.tools["attach_workflow_reference"].exec(context.Background(), map[string]interface{}{"workspace_path": "Workflow/hdfc-personal"})
	if err != nil || !strings.Contains(out, "read-only project context") {
		t.Fatalf("attach out=%s err=%v", out, err)
	}
	workspace.mu.Lock()
	saved := workspace.files[productPath]
	workspace.mu.Unlock()
	if !strings.Contains(saved, `"custom": "keep"`) || !strings.Contains(saved, `"Workflow/hdfc-personal"`) {
		t.Fatalf("product manifest was not preserved and updated: %s", saved)
	}
	out, err = registrar.tools["list_accessible_workflows"].exec(context.Background(), map[string]interface{}{"query": "personal"})
	if err != nil || !strings.Contains(out, `"attached": true`) {
		t.Fatalf("attached state out=%s err=%v", out, err)
	}
	if _, err := registrar.tools["detach_workflow_reference"].exec(context.Background(), map[string]interface{}{"workspace_path": "Workflow/hdfc-personal"}); err != nil {
		t.Fatalf("detach: %v", err)
	}
	paths, err := readWorkWorkflowReferences(context.Background(), "_users/reader/Chats/Work/projects/banking")
	if err != nil || len(paths) != 0 {
		t.Fatalf("paths after detach=%v err=%v", paths, err)
	}
}
