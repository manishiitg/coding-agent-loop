package virtualtools

import (
	"context"
	"strings"
	"testing"
)

func resetHumanToolTestState() {
	parentChatMu.Lock()
	parentChatRegistry = map[string]*ParentChatContext{}
	parentChatMu.Unlock()

	chatInjectMu.Lock()
	chatInject = nil
	chatInjectMu.Unlock()

	store := GetHumanFeedbackStore()
	store.mu.Lock()
	store.requests = make(map[string]*HumanFeedbackRequest)
	store.waiters = make(map[string]chan string)
	store.mu.Unlock()
}

func TestHandleHumanFeedbackRoutesToParentChat(t *testing.T) {
	resetHumanToolTestState()
	t.Cleanup(resetHumanToolTestState)

	RegisterParentChat("workflow-session", &ParentChatContext{
		SessionID:    "builder-session",
		WorkflowPath: "Workflow/upwork",
		GroupName:    "daily-bid",
	})

	var injected string
	SetChatInjector(func(ctx context.Context, sessionID, userID, message string) error {
		if sessionID != "builder-session" {
			t.Fatalf("unexpected parent session: %s", sessionID)
		}
		injected = message
		if err := GetHumanFeedbackStore().SubmitResponse("req-1", "approve"); err != nil {
			t.Fatalf("submit response: %v", err)
		}
		return nil
	})

	ctx := context.WithValue(context.Background(), BGAgentSessionIDKey, "workflow-session")
	got, err := handleHumanFeedback(ctx, map[string]interface{}{
		"unique_id":        "req-1",
		"message_for_user": "Review the drafted cover letter.",
		"options":          []interface{}{"approve", "decline"},
	})
	if err != nil {
		t.Fatalf("handleHumanFeedback returned error: %v", err)
	}
	if got != "approve" {
		t.Fatalf("unexpected response: %q", got)
	}
	if !strings.Contains(injected, "[WORKFLOW_HUMAN_FEEDBACK]") {
		t.Fatalf("expected routed workflow feedback marker, got %q", injected)
	}
	if !strings.Contains(injected, "Review the drafted cover letter.") {
		t.Fatalf("expected question in injected message, got %q", injected)
	}
	if !strings.Contains(injected, "approve") || !strings.Contains(injected, "decline") {
		t.Fatalf("expected options in injected message, got %q", injected)
	}
}
