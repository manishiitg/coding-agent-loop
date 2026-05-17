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
