package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuilderWebhookToolCreatesAndTestsSignedPing(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"owner","username":"owner","can_create":true},{"id":"reader","username":"reader","can_create":false}]}`)
	manifest := NewWorkflowManifest("API test")
	manifest.CreatedBy = "owner"
	manifest.Access = &WorkflowAccess{Owners: []string{"owner"}, Readers: []string{"reader"}}
	raw, _ := json.Marshal(manifest)
	mock := &mockWorkspaceAPI{files: map[string]string{
		manifestPath("Workflow/test"):            string(raw),
		"Workflow/test/planning/plan.json":       `{"steps":[{"type":"routing","id":"route","title":"Choose","routing_question":"Which?","routes":[{"route_id":"issue","route_name":"Issue","next_step_id":"work"},{"route_id":"other","route_name":"Other","next_step_id":"work"}]},{"type":"regular","id":"work","title":"Work","description":"Work"}]}`,
		"Workflow/test/variables/variables.json": `{"groups":[{"name":"default"}]}`,
	}}
	ws := httptest.NewServer(mock)
	defer ws.Close()
	t.Setenv("WORKSPACE_API_URL", ws.URL)
	svc := NewSchedulerService(&StreamingAPI{})

	api := &StreamingAPI{scheduler: svc}
	policy := workflowChatPolicy{Mode: "builder", Origin: "interactive", Capabilities: map[string]bool{"plan_authoring": true}}
	reg := &recordingRegistrar{}
	if err := api.registerWebhookTools(reg, "owner", "Workflow/test", policy); err != nil {
		t.Fatal(err)
	}
	tool := reg.tools["manage_workflow_webhook"]
	args := map[string]interface{}{"action": "create", "name": "PR smoke", "enabled": true, "auth_mode": "github", "route_selections": map[string]string{"route": "issue"}, "group_names": []string{"default"}}
	output, err := tool.exec(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	var created workflowWebhookResponse
	if err := json.Unmarshal([]byte(output), &created); err != nil || len(created.Secret) != 64 {
		t.Fatal("missing generated secret")
	}
	output, err = tool.exec(context.Background(), map[string]interface{}{"action": "list"})
	if err != nil || strings.Contains(output, created.Secret) {
		t.Fatal("list exposed secret or failed")
	}
	output, err = tool.exec(context.Background(), map[string]interface{}{"action": "test", "id": created.ID, "event": "ping", "payload": map[string]interface{}{}})
	if err != nil || !strings.Contains(output, "pong") || strings.Contains(output, created.Secret) {
		t.Fatalf("signed receiver ping failed: %v", err)
	}
	denied := &recordingRegistrar{}
	api.registerWebhookTools(denied, "reader", "Workflow/test", policy)
	if _, err := denied.tools["manage_workflow_webhook"].exec(context.Background(), args); err == nil {
		t.Fatal("reader created webhook")
	}
	if _, err := denied.tools["manage_workflow_webhook"].exec(context.Background(), map[string]interface{}{"action": "test", "id": created.ID, "payload": map[string]interface{}{}}); err == nil {
		t.Fatal("reader tested webhook")
	}
	run := &recordingRegistrar{}
	policy.Mode = "run"
	api.registerWebhookTools(run, "owner", "Workflow/test", policy)
	if len(run.tools) != 0 {
		t.Fatal("Run received management tool")
	}
}
