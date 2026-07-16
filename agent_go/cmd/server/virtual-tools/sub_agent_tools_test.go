package virtualtools

import (
	"context"
	"strings"
	"testing"
)

func TestHandleCallSubAgentPropagatesMessageSequenceRestart(t *testing.T) {
	called := false
	ctx := context.WithValue(context.Background(), ExecutePredefinedSubAgentKey, ExecutePredefinedSubAgentFunc(
		func(ctx context.Context, routeID, todoID, instructions string) (string, error) {
			called = true
			if restart, _ := ctx.Value(SubAgentMessageSequenceRestartKey).(bool); !restart {
				t.Fatalf("expected message sequence restart flag to be propagated")
			}
			if routeID != "seq-route" || todoID != "todo-1" || instructions != "run again" {
				t.Fatalf("unexpected args: route=%q todo=%q instructions=%q", routeID, todoID, instructions)
			}
			return "ok", nil
		},
	))

	result, err := handleCallSubAgent(ctx, map[string]interface{}{
		"route_id":                 "seq-route",
		"todo_id":                  "todo-1",
		"instructions":             "run again",
		"preferred_tier":           float64(1),
		"message_sequence_restart": true,
	})
	if err != nil {
		t.Fatalf("handleCallSubAgent returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected execute function to be called")
	}
	if !strings.Contains(result, `"success": true`) {
		t.Fatalf("expected successful result JSON, got %s", result)
	}
}

func TestHandleCallSubAgentPassesThroughAsyncStart(t *testing.T) {
	const asyncResult = `{"async":true,"execution_id":"child-123","status":"running"}`
	ctx := context.WithValue(context.Background(), ExecutePredefinedSubAgentKey, ExecutePredefinedSubAgentFunc(
		func(context.Context, string, string, string) (string, error) {
			return asyncResult, nil
		},
	))

	result, err := handleCallSubAgent(ctx, map[string]interface{}{
		"route_id":       "review",
		"todo_id":        "todo-1",
		"instructions":   "review the output",
		"preferred_tier": float64(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != asyncResult {
		t.Fatalf("result=%s, want the authoritative async start unchanged", result)
	}
	if strings.Contains(result, `"success"`) || strings.Contains(result, `"completed_at"`) {
		t.Fatalf("async start was falsely wrapped as terminal: %s", result)
	}
}

func TestQueryAndStopSubAgentHandlersDispatchByExecutionID(t *testing.T) {
	ctx := context.WithValue(context.Background(), QuerySubAgentKey, QuerySubAgentFunc(
		func(_ context.Context, executionID string) (string, error) {
			return "query:" + executionID, nil
		},
	))
	ctx = context.WithValue(ctx, StopSubAgentKey, StopSubAgentFunc(
		func(_ context.Context, executionID string) (string, error) {
			return "stop:" + executionID, nil
		},
	))

	queryResult, err := handleQuerySubAgent(ctx, map[string]interface{}{"execution_id": "child-1"})
	if err != nil || queryResult != "query:child-1" {
		t.Fatalf("query result=(%q, %v)", queryResult, err)
	}
	stopResult, err := handleStopSubAgent(ctx, map[string]interface{}{"execution_id": "child-1"})
	if err != nil || stopResult != "stop:child-1" {
		t.Fatalf("stop result=(%q, %v)", stopResult, err)
	}
	if _, err := handleQuerySubAgent(context.Background(), map[string]interface{}{"execution_id": "child-1"}); err == nil {
		t.Fatal("query handler succeeded without an orchestrator-owned function")
	}
}
