package server

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
)

// newDurableChatAPI stores one interactive chat owned by ownerID and returns
// an API with no active session, so reads take the inactive (owner) path.
func newDurableChatAPI(t *testing.T, sessionID, ownerID string) (*StreamingAPI, *events.EventStore) {
	t.Helper()
	journal, err := events.OpenSQLiteEventJournal(filepath.Join(t.TempDir(), "events.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	store := events.NewEventStore(10)
	t.Cleanup(store.Stop)
	store.SetDurableJournal(journal)
	store.SetSessionOwner(sessionID, ownerID)
	if err := store.SetSessionPersistenceClass(sessionID, events.SessionPersistenceInteractiveChat); err != nil {
		t.Fatal(err)
	}
	store.AddEvent(sessionID, events.Event{ID: "msg-1", Type: "user_message", Timestamp: time.Now()})
	owner, err := store.DurableChatOwner(sessionID)
	if err != nil || owner != ownerID {
		t.Fatalf("durable owner = %q, %v; want %q", owner, err, ownerID)
	}
	return &StreamingAPI{eventStore: store, runtimeCoordinator: NewRuntimeCoordinator()}, store
}

func durableReadStatus(api *StreamingAPI, sessionID, query string, claims *UserClaims) int {
	req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"/events?durable_chat=1&limit=10"+query, nil)
	req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
	if claims != nil {
		req = req.WithContext(context.WithValue(req.Context(), UserContextKey, claims))
	}
	w := httptest.NewRecorder()
	api.handleGetSessionEvents(w, req)
	return w.Code
}

func TestDurableChatReadUsesHistoryVisibilityRule(t *testing.T) {
	cases := []struct {
		name      string
		multiUser bool
		sessionID string
		owner     string
		query     string
		claims    *UserClaims
		want      int
	}{
		{name: "author reads own chat", multiUser: true, sessionID: "chat-a", owner: "alice", claims: &UserClaims{UserID: "alice"}, want: 200},
		{name: "other user denied", multiUser: true, sessionID: "chat-b", owner: "alice", claims: &UserClaims{UserID: "bob"}, want: 404},
		{name: "legacy default owner hidden on multi-user", multiUser: true, sessionID: "chat-c", owner: "default", claims: &UserClaims{UserID: "alice"}, want: 404},
		{name: "legacy default owner readable single-user", sessionID: "chat-d", owner: "default", want: 200},
		{name: "workflow bot chat readable by workflow reader", sessionID: "bot-slack--c1", owner: "bot-slack-8d3dd8b8e39a5d46", query: "&workspace_path=Workflow/demo", want: 200},
		{name: "bot chat outside a workflow scope denied", sessionID: "bot-slack--c2", owner: "bot-slack-8d3dd8b8e39a5d46", want: 404},
		{name: "fake bot owner denied", sessionID: "bot-slack--c3", owner: "other-human", query: "&workspace_path=Workflow/demo", want: 404},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.multiUser {
				t.Setenv("MULTI_USER_MODE", "true")
			} else {
				t.Setenv("MULTI_USER_MODE", "false")
			}
			api, _ := newDurableChatAPI(t, tc.sessionID, tc.owner)
			if got := durableReadStatus(api, tc.sessionID, tc.query, tc.claims); got != tc.want {
				t.Fatalf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDeleteChatHistorySessionRemovesDurableRowsWithoutTranscript(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("MULTI_USER_MODE", "true")
	const sessionID = "chat-json-already-gone"
	api, store := newDurableChatAPI(t, sessionID, "alice")

	del := func(userID string) int {
		req := httptest.NewRequest("DELETE", "/api/chat-history/sessions/"+sessionID, nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
		req = req.WithContext(context.WithValue(req.Context(), UserContextKey, &UserClaims{UserID: userID}))
		w := httptest.NewRecorder()
		deleteChatHistorySessionHandler(api)(w, req)
		return w.Code
	}
	if code := del("bob"); code != 404 {
		t.Fatalf("non-owner delete status = %d, want 404", code)
	}
	if owner, _ := store.DurableChatOwner(sessionID); owner != "alice" {
		t.Fatalf("non-owner delete removed rows (owner now %q)", owner)
	}
	if code := del("alice"); code != 200 {
		t.Fatalf("owner delete with missing transcript status = %d, want 200", code)
	}
	if owner, _ := store.DurableChatOwner(sessionID); owner != "" {
		t.Fatalf("durable rows survived delete (owner %q)", owner)
	}
}

func TestBulkDeleteRemovesDurableRowsForDeletedTranscripts(t *testing.T) {
	const keep, gone = "chat-kept", "chat-cleaned"
	api, store := newDurableChatAPI(t, gone, "alice")
	store.SetSessionOwner(keep, "alice")
	if err := store.SetSessionPersistenceClass(keep, events.SessionPersistenceInteractiveChat); err != nil {
		t.Fatal(err)
	}
	store.AddEvent(keep, events.Event{ID: "keep-1", Type: "user_message", Timestamp: time.Now()})

	ids := durableSessionIDsFromConversationPaths([]string{
		"_users/alice/chat_history/2026-09-01/session-" + gone + "-conversation.json",
		"Workflow/demo/builder/conversation/users/alice/2026-09-01/session-" + gone + "-conversation.json",
		"_users/alice/chat_history/index.json",
	})
	if len(ids) != 1 || ids[0] != gone {
		t.Fatalf("ids = %v, want [%s]", ids, gone)
	}
	api.deleteDurableChatSessionsAfterBulkDelete("test cleanup", ids)
	if owner, _ := store.DurableChatOwner(gone); owner != "" {
		t.Fatalf("cleaned session kept durable rows (owner %q)", owner)
	}
	if owner, _ := store.DurableChatOwner(keep); owner != "alice" {
		t.Fatalf("untouched session lost durable rows (owner %q)", owner)
	}
}
