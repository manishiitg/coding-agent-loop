package step_based_workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/workspace"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

func TestWriteTodoTaskStepDoneMarkerWritesMarkerInStepExecutionFolder(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody struct {
		Content string `json:"content"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"message":"ok"}`))
	}))
	t.Cleanup(server.Close)

	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}
	base.SetWorkspacePath("Workflow/instagram")
	base.WorkspaceClient = workspace.NewClient(server.URL)

	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator:  base,
		selectedRunFolder: "iteration-0/test-run",
	}
	step := &TodoTaskPlanStep{
		CommonStepFields: CommonStepFields{
			ID:    "step-1-sub-route-pick-topic-todo-pick-topic-001",
			Title: "Route pick topic",
		},
		NextStepID: "route-critique-topic",
	}

	hcpo.writeTodoTaskStepDoneMarker(context.Background(), step, 0, "route-pick-topic", step.NextStepID, "Pre-validation passed")

	wantPath := "/api/documents/Workflow/instagram/runs/iteration-0/test-run/execution/step-1-sub-route-pick-topic-todo-pick-topic-001/step_done.json"
	if gotPath != wantPath {
		t.Fatalf("request path = %q, want %q", gotPath, wantPath)
	}

	var marker map[string]interface{}
	if err := json.Unmarshal([]byte(gotBody.Content), &marker); err != nil {
		t.Fatalf("marker is not valid JSON: %v\n%s", err, gotBody.Content)
	}
	if marker["step_id"] != step.ID {
		t.Fatalf("marker step_id = %v, want %s", marker["step_id"], step.ID)
	}
	if marker["step_path"] != "route-pick-topic" {
		t.Fatalf("marker step_path = %v, want route-pick-topic", marker["step_path"])
	}
	if marker["next_step_id"] != "route-critique-topic" {
		t.Fatalf("marker next_step_id = %v, want route-critique-topic", marker["next_step_id"])
	}
	if marker["type"] != string(StepTypeTodoTask) {
		t.Fatalf("marker type = %v, want %s", marker["type"], StepTypeTodoTask)
	}
	if marker["completion_reason"] != "Pre-validation passed" {
		t.Fatalf("marker completion_reason = %v, want Pre-validation passed", marker["completion_reason"])
	}
	completedAt, _ := marker["completed_at"].(string)
	if !strings.Contains(completedAt, "T") || !strings.HasSuffix(completedAt, "Z") {
		t.Fatalf("marker completed_at = %q, want RFC3339 UTC timestamp", completedAt)
	}
}
