package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	storeevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
)

func TestExternalBuilderRejectsOtherUsersAndWorkflowsForEveryOperation(t *testing.T) {
	for _, operation := range []string{"builder_chat", "builder_status", "builder_cancel", "builder_reply_input"} {
		for _, variant := range []string{"owner", "workspace", "phase"} {
			t.Run(operation+"/"+variant, func(t *testing.T) {
				session := &ActiveSessionInfo{SessionID: "s", UserID: "user-a", WorkspacePath: "Workflow/a", AgentMode: "workflow_phase", PhaseID: "workflow-builder"}
				switch variant {
				case "owner":
					session.UserID = "user-b"
				case "workspace":
					session.WorkspacePath = "Workflow/b"
				case "phase":
					session.PhaseID = "execution"
				}
				api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{"s": session}}
				w := httptest.NewRecorder()
				api.externalBuilderCall(w, requestWithUserForSessionAccess("user-a"), operation, map[string]interface{}{"session_id": "s", "message": "hello"}, DiscoveredWorkflow{WorkspacePath: "Workflow/a"})
				if w.Code != http.StatusNotFound {
					t.Fatalf("status = %d: %s", w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestExternalPersistedBuilderBindingIgnoresConversationMentions(t *testing.T) {
	valid := `{"session_id":"s","user_id":"user-a","phase_id":"workflow-builder","runtime":{"workspace_path":"Workflow/a"},"conversation_history":[{"text":"Workflow/b"}]}`
	if !externalPersistedBuilderMatches([]byte(valid), "user-a", "Workflow/a", "s") {
		t.Fatal("valid binding rejected")
	}
	for _, target := range []struct{ user, workspace, session string }{
		{"user-b", "Workflow/a", "s"}, {"user-a", "Workflow/b", "s"}, {"user-a", "Workflow/a", "other"},
	} {
		if externalPersistedBuilderMatches([]byte(valid), target.user, target.workspace, target.session) {
			t.Fatalf("cross-boundary transcript accepted: %+v", target)
		}
	}
	legacy := `{"session_id":"s","user_id":"user-a","agent_mode":"workflow_phase","conversation_history":[{"text":"Workflow/a"}]}`
	if externalPersistedBuilderMatches([]byte(legacy), "user-a", "Workflow/a", "s") {
		t.Fatal("unbound legacy transcript accepted")
	}
	apiModel := `{"session_id":"s","user_id":"user-a","phase_id":"workflow-builder","conversation_history":[]}`
	if !externalPersistedBuilderMatches([]byte(apiModel), "user-a", "Workflow/a", "s") {
		t.Fatal("scoped API-model builder transcript without native runtime was rejected")
	}
}

func TestExternalBuilderRestoresPersistedOwnerAndWorkflowBinding(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	dir := filepath.Join(root, "Workflow", "a", "builder", "conversation", "2026-09-09")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data := `{"session_id":"saved","user_id":"user-a","phase_id":"workflow-builder","runtime":{"workspace_path":"Workflow/a"},"conversation_history":[]}`
	if err := os.WriteFile(filepath.Join(dir, "session-saved-conversation.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	api := &StreamingAPI{}
	active, persisted, err := api.externalBuilderSession(requestWithUserForSessionAccess("user-a"), "Workflow/a", "saved")
	if err != nil || !persisted || active != nil {
		t.Fatalf("restore: active=%v persisted=%v err=%v", active, persisted, err)
	}
	if _, _, err := api.externalBuilderSession(requestWithUserForSessionAccess("user-b"), "Workflow/a", "saved"); err == nil {
		t.Fatal("another user could resume persisted session")
	}
}

func TestExternalBuilderStatusUsesBoundedForwardEvents(t *testing.T) {
	store := storeevents.NewEventStore(100)
	defer store.Stop()
	for i := 0; i < 5; i++ {
		store.AddEvent("s", storeevents.Event{ID: fmt.Sprint(i), Type: "user_message", SessionID: "s", Timestamp: time.Now()})
	}
	api := &StreamingAPI{eventStore: store, activeSessions: map[string]*ActiveSessionInfo{
		"s": {SessionID: "s", UserID: "user-a", WorkspacePath: "Workflow/a", AgentMode: "workflow_phase", PhaseID: "workflow-builder", Status: "running"},
	}}
	w := httptest.NewRecorder()
	api.externalBuilderCall(w, requestWithUserForSessionAccess("user-a"), "builder_status", map[string]interface{}{"session_id": "s", "limit": float64(2)}, DiscoveredWorkflow{WorkspacePath: "Workflow/a"})
	var body struct {
		Events  []storeevents.Event `json:"events"`
		HasMore bool                `json:"has_more"`
		Cursor  int                 `json:"last_processed_index"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("status %d %s: %v", w.Code, w.Body.String(), err)
	}
	if len(body.Events) != 2 || !body.HasMore || body.Cursor != 1 {
		t.Fatalf("unbounded/incorrect status: %s", w.Body.String())
	}
}

func TestExternalBuilderCancelReusesCurrentTurnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{
		"s": {SessionID: "s", UserID: "user-a", WorkspacePath: "Workflow/a", AgentMode: "workflow_phase", PhaseID: "workflow-builder"},
	}, agentCancelFuncs: map[string]context.CancelFunc{"s": cancel}}
	w := httptest.NewRecorder()
	api.externalBuilderCall(w, requestWithUserForSessionAccess("user-a"), "builder_cancel", map[string]interface{}{"session_id": "s"}, DiscoveredWorkflow{WorkspacePath: "Workflow/a"})
	if w.Code != http.StatusNoContent || ctx.Err() == nil {
		t.Fatalf("cancel failed: %d %s", w.Code, w.Body.String())
	}
}

func TestExternalBuilderPaginationValidation(t *testing.T) {
	for _, value := range []interface{}{0, -1, 201, 2.5, "2", nil, json.Number("9007199254740993")} {
		if _, err := externalBuilderInt(map[string]interface{}{"limit": value}, "limit", 50, 1, 200); err == nil {
			t.Fatalf("invalid limit accepted: %v", value)
		}
	}
}

func TestExternalBuilderSelectsLatestOwnChatInWorkflow(t *testing.T) {
	now := time.Now()
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{}}
	for _, row := range []struct {
		id, owner, folder, phase string
		age                      time.Duration
	}{
		{"old", "user-a", "Workflow/a", "workflow-builder", time.Hour},
		{"latest", "user-a", "Workflow/a", "workflow-builder", time.Minute},
		{"other-user", "user-b", "Workflow/a", "workflow-builder", 0},
		{"other-workflow", "user-a", "Workflow/b", "workflow-builder", 0},
		{"execution", "user-a", "Workflow/a", "execution", 0},
	} {
		api.activeSessions[row.id] = &ActiveSessionInfo{SessionID: row.id, UserID: row.owner, WorkspacePath: row.folder, AgentMode: "workflow_phase", PhaseID: row.phase, LastActivity: now.Add(-row.age)}
	}
	id, active, persisted, err := api.externalLatestBuilderSession(requestWithUserForSessionAccess("user-a"), "Workflow/a")
	if err != nil || id != "latest" || active == nil || persisted {
		t.Fatalf("wrong restore selection: id=%q active=%v persisted=%v err=%v", id, active, persisted, err)
	}
}

func TestExternalBuilderForwardNormalizesActualErrors(t *testing.T) {
	for _, tc := range []struct {
		name                string
		status              int
		body, code, message string
	}{
		{"plain", 400, "Invalid request body\n", "invalid_arguments", "Invalid request body"},
		{"json", 409, `{"error":"workflow_busy","message":"Builder is running"}`, "workflow_busy", "Builder is running"},
		{"nested", 403, `{"error":{"code":"forbidden","message":"No access"}}`, "forbidden", "No access"},
		{"empty", 503, "", "builder_unavailable", "Service Unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			externalBuilderForward(w, httptest.NewRequest("POST", "/", nil), func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			var result struct {
				Error struct{ Code, Message string }
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || result.Error.Code != tc.code || result.Error.Message != tc.message || w.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("wrong error: %d %s", w.Code, w.Body.String())
			}
		})
	}
	response := &externalBuilderResponse{target: httptest.NewRecorder(), header: make(http.Header)}
	response.WriteHeader(500)
	large := strings.Repeat("x", externalBuilderErrorLimit*2)
	if n, err := response.Write([]byte(large)); n != len(large) || err != nil || response.body.Len() != externalBuilderErrorLimit || !response.truncated {
		t.Fatalf("error buffer not bounded: n=%d bytes=%d err=%v", n, response.body.Len(), err)
	}
	for _, status := range []int{200, 202, 204} {
		w := httptest.NewRecorder()
		externalBuilderForward(w, httptest.NewRequest("POST", "/", nil), func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		if w.Code != status {
			t.Fatalf("success status changed: %d -> %d", status, w.Code)
		}
	}
}

func TestExternalBuilderRepliesToBoundInputWithoutChatTurn(t *testing.T) {
	store := virtualtools.GetHumanFeedbackStore()
	requestID := "external-builder-reply-" + fmt.Sprint(time.Now().UnixNano())
	foreignID := requestID + "-foreign"
	for _, entry := range []struct{ id, session string }{{requestID, "s"}, {foreignID, "other"}} {
		if err := store.CreatePendingRequest(entry.id, "Choose", "", entry.session, []string{"yes", "no"}, false, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{"s": {SessionID: "s", UserID: "user-a", WorkspacePath: "Workflow/a", AgentMode: "workflow_phase", PhaseID: "workflow-builder"}}}
	workflow := DiscoveredWorkflow{WorkspacePath: "Workflow/a"}
	w := httptest.NewRecorder()
	api.externalBuilderCall(w, requestWithUserForSessionAccess("user-a"), "builder_reply_input", map[string]interface{}{"session_id": "s", "request_id": foreignID, "response": "yes"}, workflow)
	if w.Code != 409 {
		t.Fatalf("foreign feedback accepted: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	api.externalBuilderCall(w, requestWithUserForSessionAccess("user-a"), "builder_status", map[string]interface{}{"session_id": "s"}, workflow)
	if strings.Contains(w.Body.String(), foreignID) || !strings.Contains(w.Body.String(), requestID) {
		t.Fatalf("pending input scope incorrect: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	api.externalBuilderCall(w, requestWithUserForSessionAccess("user-a"), "builder_reply_input", map[string]interface{}{"session_id": "s", "request_id": requestID, "response": "yes"}, workflow)
	if w.Code != 200 {
		t.Fatalf("reply failed: %d %s", w.Code, w.Body.String())
	}
	if response, done := store.GetResponse(requestID); !done || response != "yes" {
		t.Fatalf("reply missing from existing store: %q %v", response, done)
	}
}
