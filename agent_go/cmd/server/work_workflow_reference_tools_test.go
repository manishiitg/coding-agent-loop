package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
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
	runtimePath := "_users/reader/Chats/Work/projects/banking/workflow.json"
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
	common.SetSessionFolderGuard("session-1", []string{"_users/reader/Chats/Work/projects/banking"}, []string{"_users/reader/Chats/Work/projects/banking"})
	t.Cleanup(func() { common.ClearSessionShellConfig("session-1") })
	if err := api.registerWorkWorkflowReferenceTools(registrar, "reader", "session-1", "_users/reader/Chats/Work/projects/banking"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"list_accessible_workflows", "attach_workflow_reference", "detach_workflow_reference"} {
		if _, ok := registrar.tools[name]; !ok {
			t.Fatalf("tool %q was not registered", name)
		}
	}

	out, err := registrar.tools["list_accessible_workflows"].exec(context.Background(), map[string]interface{}{"query": "hdfc"})
	if err != nil || !strings.Contains(out, `"workspace_path": "Workflow/hdfc-personal"`) || !strings.Contains(out, `"name": "HDFC Bank Personal Accounts"`) || !strings.Contains(out, `"icon": "H"`) || strings.Contains(out, "Private Bank") {
		t.Fatalf("accessible search out=%s err=%v", out, err)
	}
	out, err = registrar.tools["list_accessible_workflows"].exec(context.Background(), map[string]interface{}{"query": "banking"})
	if err != nil || !strings.Contains(out, `"crews":`) || !strings.Contains(out, `"name": "Banking"`) || !strings.Contains(out, `"icon": "B"`) || !strings.Contains(out, `"workspace_path": "Chats/Work/projects/banking"`) {
		t.Fatalf("Crew discovery out=%s err=%v", out, err)
	}
	if _, err := registrar.tools["attach_workflow_reference"].exec(context.Background(), map[string]interface{}{"workspace_path": "Workflow/private-bank"}); err == nil {
		t.Fatal("unauthorized workflow was attached")
	}
	out, err = registrar.tools["attach_workflow_reference"].exec(context.Background(), map[string]interface{}{"workspace_path": "Workflow/hdfc-personal"})
	if err != nil || !strings.Contains(out, "read-only project context") {
		t.Fatalf("attach out=%s err=%v", out, err)
	}
	if cfg := common.GetSessionShellConfig("session-1"); cfg == nil || !containsWorkReferencePath(cfg.ReadPaths, "Workflow/hdfc-personal") || containsWorkReferencePath(cfg.WritePaths, "Workflow/hdfc-personal") {
		t.Fatalf("attached workflow was not granted immediately as read-only: %+v", cfg)
	}
	workspace.mu.Lock()
	productSaved := workspace.files[productPath]
	saved := workspace.files[runtimePath]
	workspace.mu.Unlock()
	var productMetadata map[string]interface{}
	if json.Unmarshal([]byte(productSaved), &productMetadata) != nil || productMetadata["custom"] != "keep" || strings.Contains(productSaved, `Workflow/hdfc-personal`) {
		t.Fatalf("product metadata was changed: %s", productSaved)
	}
	if !strings.Contains(saved, `"Workflow/hdfc-personal"`) || !strings.Contains(saved, `"selected_skills"`) {
		t.Fatalf("workflow runtime manifest was not migrated and updated: %s", saved)
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
	if cfg := common.GetSessionShellConfig("session-1"); cfg == nil || containsWorkReferencePath(cfg.ReadPaths, "Workflow/hdfc-personal") {
		t.Fatalf("detached workflow remained in active read guard: %+v", cfg)
	}
}

func TestBuilderAccessibleWorkflowListUsesCurrentUserAccessWithoutCrewMutationTools(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"builder","username":"builder","products":[]},{"id":"owner","username":"owner","products":[]}]}`)
	shared, _ := json.Marshal(WorkflowManifest{
		ID: "shared", Label: "Shared workflow",
		Access: &WorkflowAccess{Owners: []string{"owner"}, Readers: []string{"builder"}},
	})
	private, _ := json.Marshal(WorkflowManifest{
		ID: "private", Label: "Private workflow",
		Access: &WorkflowAccess{Owners: []string{"owner"}},
	})
	workspace := &mockWorkspaceAPI{files: map[string]string{
		manifestPath("Workflow/shared"):                           string(shared),
		manifestPath("Workflow/private"):                          string(private),
		"_users/builder/Chats/Work/projects/release/product.json": `{"schema_version":1,"product":"work","id":"release","title":"Release project","identity":{"name":"Release Crew","icon":"🚀"}}`,
	}}
	host := httptest.NewServer(workspace)
	defer host.Close()
	t.Setenv("WORKSPACE_API_URL", host.URL)

	api := &StreamingAPI{}
	registrar := &recordingRegistrar{}
	if err := api.registerAccessibleWorkflowListTool(registrar, "builder", nil); err != nil {
		t.Fatal(err)
	}
	if len(registrar.tools) != 1 {
		t.Fatalf("builder discovery registered %d tools, want only list_accessible_workflows", len(registrar.tools))
	}
	out, err := registrar.tools["list_accessible_workflows"].exec(context.Background(), map[string]interface{}{})
	if err != nil || !strings.Contains(out, `"workspace_path": "Workflow/shared"`) || !strings.Contains(out, `"name": "Shared workflow"`) || !strings.Contains(out, `"crews":`) || !strings.Contains(out, `"name": "Release project"`) || !strings.Contains(out, `"name": "Release Crew"`) || !strings.Contains(out, `"icon": "🚀"`) || strings.Contains(out, "Private workflow") || strings.Contains(out, `"attached"`) {
		t.Fatalf("builder discovery out=%s err=%v", out, err)
	}
}

func containsWorkReferencePath(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
