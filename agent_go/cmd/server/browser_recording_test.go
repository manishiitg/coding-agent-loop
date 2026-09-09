package server

import (
	"context"
	"github.com/gorilla/mux"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/browser"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserRecordingRejectsOtherOwner(t *testing.T) {
	browser.GetSessionTracker().Touch("recording-owned", "run-owner", "run-owner")
	defer browser.GetSessionTracker().Remove("recording-owned")
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{"run-owner": {UserID: "alice", WorkspacePath: "Workflow/one"}}}
	for _, action := range []string{"status", "start", "stop"} {
		req := httptest.NewRequest("POST", "/?workspace_path=Workflow/one", strings.NewReader(`{"action":"`+action+`"}`))
		req = mux.SetURLVars(req, map[string]string{"session": "recording-owned"})
		req = req.WithContext(context.WithValue(req.Context(), UserContextKey, &UserClaims{UserID: "bob"}))
		response := httptest.NewRecorder()
		api.handleBrowserRecording(response, req)
		if response.Code != 404 {
			t.Fatalf("%s exposed another user's recording: %d", action, response.Code)
		}
	}
}
