package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/accesstokens"
)

func TestExternalCallFunctionRefusesBadWorkflowInputsBeforeDispatch(t *testing.T) {
	f := newExternalToolsFixture(t)
	manifest := WorkflowManifest{ID: "invoices", Label: "Invoice processing", CreatedBy: "owner", Schedules: []WorkflowSchedule{reviewPRTrigger()}}
	manifest.Access = &WorkflowAccess{Owners: []string{"owner"}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	f.write(t, "Workflow/invoices/workflow.json", string(data))
	body := externalTestBody(t, f.call(t, "owner", "call_function", map[string]any{
		"target": "invoices", "function": "review_pr", "args": map[string]any{
			"GITHUB_OWNER": "acme", "PR_NUMBER": "oops", "group": "unknown", "typo": "x",
		}, "wait_seconds": 0,
	}), 200)
	if body["status"] != "refused" || body["code"] != "invalid_inputs" || body["call_id"] != nil {
		t.Fatalf("invalid invocation = %v", body)
	}
	problems := body["problems"].([]any)
	if len(problems) < 4 {
		t.Fatalf("expected independent input problems, got %v", problems)
	}
	retry := body["retry_with"].(map[string]any)["args"].(map[string]any)
	if len(retry) != 1 || retry["GITHUB_OWNER"] != "acme" {
		t.Fatalf("retry includes invalid values: %v", retry)
	}
}

func TestExternalGetCallIsolatedByConnection(t *testing.T) {
	f := newExternalToolsFixture(t)
	call := &crewFunctionCall{
		ID: "fn-connection-test", Function: "ask", UserID: "owner",
		CallerKind: triggerCallerConnection, CallerID: "connection-a",
		TargetKind: triggerCallerWorkflow, TargetID: "invoices", TargetLabel: "Invoice processing",
		Status: "completed", Result: map[string]any{"answer": "done"},
		CreatedAt: time.Now(), UpdatedAt: time.Now(), done: make(chan struct{}),
	}
	close(call.done)
	crewFunctionCalls.Lock()
	crewFunctionCalls.m[call.ID] = call
	crewFunctionCalls.Unlock()
	t.Cleanup(func() {
		crewFunctionCalls.Lock()
		delete(crewFunctionCalls.m, call.ID)
		crewFunctionCalls.Unlock()
	})
	get := func(tokenID string) *httptest.ResponseRecorder {
		claims := &UserClaims{UserID: "owner", Username: "owner", AccessToken: &accesstokens.Token{
			ID: tokenID, Scopes: []string{"workflows:read", "runs:execute"}, AllWorkflows: true,
		}}
		w := httptest.NewRecorder()
		request := adminRequest(http.MethodPost, "/api/external/v1/call", `{"name":"get_call","arguments":{"call_id":"fn-connection-test"}}`, claims, nil)
		f.api.handleExternalCall(w, request)
		return w
	}
	if got := externalTestBody(t, get("connection-a"), 200); got["status"] != "completed" {
		t.Fatalf("owner connection result = %v", got)
	}
	if got := externalTestBody(t, get("connection-b"), 404); got["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("other connection result = %v", got)
	}
}

func TestExternalFunctionRepairKeepsOnlyValidInputs(t *testing.T) {
	fn := crewFunction{Name: "ship", InputSchema: map[string]interface{}{
		"type": "object", "required": []interface{}{"build", "env"},
		"properties": map[string]interface{}{
			"build":  map[string]interface{}{"type": "integer"},
			"env":    map[string]interface{}{"type": "string", "enum": []interface{}{"prod", "staging"}, "default": "staging"},
			"notify": map[string]interface{}{"type": "boolean"},
		},
	}}
	valid, missing := externalFunctionRepair(fn, map[string]interface{}{"env": "qa", "notify": "false", "extra": "x"}, nil)
	if len(missing) != 1 || missing[0] != "build" || len(valid) != 1 || valid["notify"] != false {
		t.Fatalf("repair included invalid or missing values: valid=%v missing=%v", valid, missing)
	}
}
