package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestHandleGetExecutionWebhookPayloadReturnsOnlyWebhookRunPayload(t *testing.T) {
	const workspacePath = "Workflow/test"
	delivery := WorkflowWebhookDelivery{
		RunID:   "run-1",
		Payload: json.RawMessage(`{"action":"opened","number":76}`),
	}
	deliveryJSON, err := json.Marshal(delivery)
	if err != nil {
		t.Fatal(err)
	}
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/runs/iteration-6-hook/.webhook-run-id": "run-1",
		webhookInputPath(workspacePath, "run-1"):                 string(deliveryJSON),
	}})
	t.Cleanup(workspace.Close)
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	request := httptest.NewRequest("GET", "/api/workflow/logs/webhook-payload?workspace_path="+workspacePath+"&run_folder=iteration-6-hook/default", nil)
	response := httptest.NewRecorder()
	(&StreamingAPI{}).handleGetExecutionWebhookPayload(response, request)

	if response.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Success    bool   `json:"success"`
		RunID      string `json:"run_id"`
		RawPayload string `json:"raw_payload"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success || body.RunID != "run-1" || body.RawPayload != `{"action":"opened","number":76}` {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestHandleGetExecutionWebhookPayloadRejectsManualRun(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/workflow/logs/webhook-payload?workspace_path=Workflow/test&run_folder=iteration-6/default", nil)
	response := httptest.NewRecorder()
	(&StreamingAPI{}).handleGetExecutionWebhookPayload(response, request)
	if response.Code != 404 {
		t.Fatalf("expected 404 for non-webhook run, got %d: %s", response.Code, response.Body.String())
	}
}
