package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
)

func TestHandleGetExecutionLogsReturnsSemanticStepLogs(t *testing.T) {
	const workspacePath = "/workspace/Workflow/test"
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/planning/plan.json": `{
			"steps": [{
				"id": "compile-report",
				"title": "Compile report",
				"context_output": "report.json"
			}]
		}`,
		workspacePath + "/runs/iteration-0/default/logs/compile-report/execution/execution-attempt-1-iteration-0.json": `{"success":true}`,
		workspacePath + "/runs/iteration-0/default/execution/compile-report/report.json":                               `{"rows":3}`,
	}})
	t.Cleanup(workspace.Close)
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	request := httptest.NewRequest(
		"GET",
		"/api/workflow/logs?workspace_path="+workspacePath+"&run_folder=iteration-0/default",
		nil,
	)
	response := httptest.NewRecorder()
	(&StreamingAPI{}).handleGetExecutionLogs(response, request)

	if response.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		Success bool `json:"success"`
		Steps   map[string]struct {
			Executions    []map[string]interface{} `json:"executions"`
			OutputContent map[string]interface{}   `json:"output_content"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	step, ok := body.Steps["compile-report"]
	if !ok {
		t.Fatalf("expected semantic step in response, got steps %v", body.Steps)
	}
	if len(step.Executions) != 1 {
		t.Fatalf("expected one execution, got %d", len(step.Executions))
	}
	if step.OutputContent == nil {
		t.Fatal("expected semantic step output content")
	}
}

func executionLogFolder(path string, children ...virtualtools.WorkspaceFolderItem) virtualtools.WorkspaceFolderItem {
	return virtualtools.WorkspaceFolderItem{
		FilePath: path,
		Type:     "folder",
		Children: children,
	}
}

func TestIsExecutionLogStepFolderSupportsSemanticStepIDs(t *testing.T) {
	metadata := map[string]map[string]string{
		"compile-report": {"title": "Compile report"},
	}

	if !isExecutionLogStepFolder(executionLogFolder("/runs/iteration-0/default/logs/compile-report"), metadata) {
		t.Fatal("expected semantic plan step ID to be recognized as a log folder")
	}
	if isExecutionLogStepFolder(executionLogFolder("/runs/iteration-0/default/logs"), metadata) {
		t.Fatal("did not expect the logs wrapper directory to be treated as a step")
	}
}

func TestIsExecutionLogStepFolderRecognizesHistoricalSemanticLogs(t *testing.T) {
	item := executionLogFolder(
		"/runs/iteration-0/default/logs/old-semantic-step",
		executionLogFolder("/runs/iteration-0/default/logs/old-semantic-step/execution"),
	)

	if !isExecutionLogStepFolder(item, nil) {
		t.Fatal("expected a historical semantic folder with execution logs to be recognized")
	}
}

func TestIsExecutionOutputStepItemSupportsSemanticStepIDs(t *testing.T) {
	metadata := map[string]map[string]string{
		"compile-report": {"title": "Compile report"},
	}
	logs := map[string]map[string]interface{}{
		"historical-step": {},
	}

	for _, path := range []string{
		"/runs/iteration-0/default/execution/compile-report",
		"/runs/iteration-0/default/execution/historical-step",
	} {
		if !isExecutionOutputStepItem(executionLogFolder(path), metadata, logs) {
			t.Fatalf("expected %s to be recognized as a step output", path)
		}
	}

	if isExecutionOutputStepItem(executionLogFolder("/runs/iteration-0/default/execution"), metadata, logs) {
		t.Fatal("did not expect the execution wrapper directory to be treated as a step")
	}
}

func TestHandleGetExecutionLogsUsesMessageSequenceSessionStatus(t *testing.T) {
	const workspacePath = "/workspace/Workflow/test"
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/planning/plan.json": `{
			"steps": [{
				"type": "message_sequence",
				"id": "deliver",
				"title": "Deliver",
				"description": "Deliver the report",
				"messages": [{"id":"send","type":"user_message","message":"send"}]
			}]
		}`,
		workspacePath + "/runs/iteration-0/default/execution/deliver/session.json": `{"status":"failed"}`,
	}})
	t.Cleanup(workspace.Close)
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	request := httptest.NewRequest("GET", "/api/workflow/logs?workspace_path="+workspacePath+"&run_folder=iteration-0/default", nil)
	response := httptest.NewRecorder()
	(&StreamingAPI{}).handleGetExecutionLogs(response, request)
	if response.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Steps map[string]struct {
			Status string `json:"message_sequence_status"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := body.Steps["deliver"].Status; got != "failed" {
		t.Fatalf("expected failed message sequence status, got %q", got)
	}
}
