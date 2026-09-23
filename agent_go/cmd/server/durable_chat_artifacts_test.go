package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	pkgevents "github.com/manishiitg/mcpagent/events"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
)

func chatArtifactStatus(api *StreamingAPI, sessionID, artifactID string, claims *UserClaims) (int, string) {
	req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"/chat-artifacts/"+artifactID, nil)
	req = mux.SetURLVars(req, map[string]string{"session_id": sessionID, "artifact_id": artifactID})
	if claims != nil {
		req = req.WithContext(context.WithValue(req.Context(), UserContextKey, claims))
	}
	w := httptest.NewRecorder()
	api.handleGetChatArtifact(w, req)
	return w.Code, w.Body.String()
}

func TestChatArtifactEndpointUsesDurableReadAuthorization(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	api, store := newDurableChatAPI(t, "chat-a", "alice")
	full := strings.Repeat("full tool output ", 6000)
	store.AddEvent("chat-a", events.Event{ID: "tool-big", Type: "tool_call_end", Timestamp: time.Now(), Data: &pkgevents.AgentEvent{
		Type: pkgevents.EventType("tool_call_end"),
		Data: events.NewGenericEventData("tool_call_end", map[string]interface{}{"tool_name": "exec", "result": full}),
	}})
	artifactID := events.ChatArtifactID("tool-big")

	if code, body := chatArtifactStatus(api, "chat-a", artifactID, &UserClaims{UserID: "alice"}); code != 200 || !strings.Contains(body, full) {
		t.Fatalf("owner read = %d (full content present: %v)", code, strings.Contains(body, full))
	}
	if code, _ := chatArtifactStatus(api, "chat-a", artifactID, &UserClaims{UserID: "bob"}); code != 404 {
		t.Fatalf("other user read = %d, want 404", code)
	}
	for _, id := range []string{"../events.sqlite", "..%2Fevents.sqlite", "not-an-id", ""} {
		if code, _ := chatArtifactStatus(api, "chat-a", id, &UserClaims{UserID: "alice"}); code != 404 && code != 400 {
			t.Fatalf("artifact id %q = %d, want 400/404", id, code)
		}
	}
	if code, _ := chatArtifactStatus(api, "chat-a", events.ChatArtifactID("missing"), &UserClaims{UserID: "alice"}); code != 404 {
		t.Fatalf("missing artifact = %d, want 404", code)
	}
}
