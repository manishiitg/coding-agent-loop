package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCommandPathForRequestUsesUserAndWorkspaceScope(t *testing.T) {
	request := func(target string) *httpRequestFixture {
		req := httptest.NewRequest("GET", target, nil)
		req = req.WithContext(context.WithValue(req.Context(), UserContextKey, &UserClaims{UserID: "alice"}))
		return &httpRequestFixture{req: req, recorder: httptest.NewRecorder()}
	}

	personal := request("/api/commands")
	if got, ok := commandPathForRequest(personal.recorder, personal.req, false); !ok || got != "_users/alice/commands/custom" {
		t.Fatalf("personal path = %q, %v", got, ok)
	}

	project := request("/api/commands?workspace_path=Chats%2FWork%2Fprojects%2Fdemo")
	if got, ok := commandPathForRequest(project.recorder, project.req, true); !ok || got != "_users/alice/Chats/Work/projects/demo/commands/custom" {
		t.Fatalf("project path = %q, %v", got, ok)
	}

	foreign := request("/api/commands?workspace_path=_users%2Fbob%2FChats%2FWork%2Fprojects%2Fdemo")
	if got, ok := commandPathForRequest(foreign.recorder, foreign.req, false); ok || got != "" || foreign.recorder.Code != 403 {
		t.Fatalf("foreign path accepted: path=%q ok=%v status=%d", got, ok, foreign.recorder.Code)
	}
}

type httpRequestFixture struct {
	req      *http.Request
	recorder *httptest.ResponseRecorder
}
