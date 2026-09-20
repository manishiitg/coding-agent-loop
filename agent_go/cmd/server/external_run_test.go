package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/accesstokens"
)

func TestExternalRunScopeGate(t *testing.T) {
	catalog, err := externalTools()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]externalTool{}
	for _, tool := range catalog {
		byName[tool.Name] = tool
	}
	claims := func(scopes ...string) *UserClaims {
		return &UserClaims{UserID: "owner", Username: "owner", AccessToken: &accesstokens.Token{ID: "t", Scopes: scopes, AllWorkflows: true}}
	}
	readOnly := claims("workflows:read", "files:read")
	executeOnly := claims("runs:execute")
	full := claims("workflows:read", "files:read", "runs:execute")

	// Proxied run tools require runs:execute.
	for _, name := range []string{"execute_step", "run_full_workflow", "send_step_message", "stop_step", "stop_all_executions", "query_step", "trigger_schedule", "chat", "run_reply_input"} {
		if externalTokenAllows(readOnly, byName[name]) {
			t.Fatalf("read-only token allows %s", name)
		}
		if !externalTokenAllows(executeOnly, byName[name]) {
			t.Fatalf("execute-only token denies %s", name)
		}
	}
	// Execution implies workflow visibility, never file content.
	for _, name := range []string{"list_workflows", "get_workflow", "get_plan", "list_runs", "get_run", "get_logs", "run_status", "list_executions", "list_schedules", "get_schedule_runs", "get_agent_context"} {
		if !externalTokenAllows(executeOnly, byName[name]) {
			t.Fatalf("execute-only token denies workflow read %s", name)
		}
	}
	for _, name := range []string{"read_file", "list_files", "search_files", "get_file_link", "list_workflow_knowledge", "read_workflow_knowledge"} {
		if externalTokenAllows(executeOnly, byName[name]) {
			t.Fatalf("execute-only token allows file read %s", name)
		}
		if !externalTokenAllows(full, byName[name]) {
			t.Fatalf("full token denies file read %s", name)
		}
	}
}

func TestExternalRunProxyRejectsReadOnlyToken(t *testing.T) {
	f := newExternalToolsFixture(t)
	// The scope gate bites before dispatch: a read-only PAT calling a
	// proxied run tool is rejected without touching the run runtime.
	w := httptest.NewRecorder()
	data := `{"name":"execute_step","arguments":{"workflow_id":"invoices","step_id":"fetch-invoices"}}`
	f.api.handleExternalCall(w, adminRequest(http.MethodPost, "/api/external/call", data, patClaims("owner", []string{"workflows:read", "files:read"}), nil))
	scoped := externalTestBody(t, w, 403)
	if scoped["error"].(map[string]any)["code"] != "insufficient_scope" {
		t.Fatalf("wrong error: %v", scoped)
	}
}

func TestExternalChatRejectsReadOnlyTokenAndBlankMessage(t *testing.T) {
	f := newExternalToolsFixture(t)
	w := httptest.NewRecorder()
	data := `{"name":"chat","arguments":{"workflow_id":"invoices","message":"hello"}}`
	f.api.handleExternalCall(w, adminRequest(http.MethodPost, "/api/external/call", data, patClaims("owner", []string{"workflows:read", "files:read"}), nil))
	scoped := externalTestBody(t, w, 403)
	if scoped["error"].(map[string]any)["code"] != "insufficient_scope" {
		t.Fatalf("wrong error: %v", scoped)
	}
	// A whitespace message passes schema validation but is rejected before
	// any turn starts.
	externalTestBody(t, f.call(t, "owner", "chat", map[string]any{"workflow_id": "invoices", "message": "  "}), 400)
}

func TestExternalRunReplyInputRejectsUnknownSession(t *testing.T) {
	f := newExternalToolsFixture(t)
	body := externalTestBody(t, f.call(t, "owner", "run_reply_input", map[string]any{"workflow_id": "invoices", "session_id": "no-such-session", "request_id": "r", "response": "yes"}), 404)
	if body["error"].(map[string]any)["code"] != "session_not_found" {
		t.Fatalf("wrong error: %v", body)
	}
}

func TestExternalRunStatusRejectsUnknownSession(t *testing.T) {
	f := newExternalToolsFixture(t)
	body := externalTestBody(t, f.call(t, "owner", "run_status", map[string]any{"workflow_id": "invoices", "session_id": "no-such-session"}), 404)
	if body["error"].(map[string]any)["code"] != "session_not_found" {
		t.Fatalf("wrong error: %v", body)
	}
	externalTestBody(t, f.call(t, "owner", "run_status", map[string]any{"workflow_id": "invoices", "session_id": "../escape"}), 400)
}

func TestExternalListSchedulesEmptyAndUnknownRuns(t *testing.T) {
	f := newExternalToolsFixture(t)
	body := externalTestBody(t, f.call(t, "owner", "list_schedules", map[string]any{"workflow_id": "invoices"}), 200)
	schedules, _ := body["schedules"].([]any)
	if schedules == nil || len(schedules) != 0 {
		t.Fatalf("expected empty schedules, got %v", body)
	}
	body = externalTestBody(t, f.call(t, "owner", "get_schedule_runs", map[string]any{"workflow_id": "invoices", "schedule_id": "nope"}), 404)
	if body["error"].(map[string]any)["code"] != "schedule_not_found" {
		t.Fatalf("wrong error: %v", body)
	}
}

func TestExternalTriggerScheduleWithoutScheduler(t *testing.T) {
	f := newExternalToolsFixture(t)
	body := externalTestBody(t, f.call(t, "owner", "trigger_schedule", map[string]any{"workflow_id": "invoices", "schedule_id": "daily"}), 503)
	if body["error"].(map[string]any)["code"] != "scheduler_unavailable" {
		t.Fatalf("wrong error: %v", body)
	}
}

func TestExternalListExecutionsEmpty(t *testing.T) {
	f := newExternalToolsFixture(t)
	body := externalTestBody(t, f.call(t, "owner", "list_executions", map[string]any{"workflow_id": "invoices"}), 200)
	executions, _ := body["executions"].([]any)
	if executions == nil || len(executions) != 0 {
		t.Fatalf("expected empty executions, got %v", body)
	}
}

func TestReadOnlyForRequest(t *testing.T) {
	if !readOnlyForRequest(WorkflowAccessRead, QueryRequest{}) {
		t.Fatal("reader without pin is not read-only")
	}
	if readOnlyForRequest(WorkflowAccessOwner, QueryRequest{}) {
		t.Fatal("owner without pin is read-only")
	}
	if !readOnlyForRequest(WorkflowAccessOwner, QueryRequest{PinRunMode: true}) {
		t.Fatal("pinned owner turn is not read-only")
	}
	if !readOnlyForRequest(WorkflowAccessWrite, QueryRequest{PinRunMode: true}) {
		t.Fatal("pinned writer turn is not read-only")
	}
}

func TestExternalRunInstructionPassesArgumentsThrough(t *testing.T) {
	got := externalRunInstruction("execute_step", map[string]any{"workflow_id": "invoices", "session_id": "s", "step_id": "fetch", "tier": "high"})
	for _, want := range []string{`"execute_step"`, `"step_id":"fetch"`, `"tier":"high"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("instruction missing %s: %s", want, got)
		}
	}
	for _, drop := range []string{"workflow_id", "session_id"} {
		if strings.Contains(got, drop) {
			t.Fatalf("instruction leaks routing key %s: %s", drop, got)
		}
	}
	got = externalRunInstruction("stop_all_executions", map[string]any{"workflow_id": "invoices"})
	if !strings.Contains(got, "with no arguments") {
		t.Fatalf("empty instruction wrong: %s", got)
	}
}
