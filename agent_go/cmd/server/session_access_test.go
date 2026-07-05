package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func requestWithUserForSessionAccess(userID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/query", nil)
	ctx := context.WithValue(req.Context(), UserContextKey, &UserClaims{UserID: userID, Username: userID})
	return req.WithContext(ctx)
}

func TestCanUseSessionIDForQueryRejectsOtherUsersActiveSession(t *testing.T) {
	api := &StreamingAPI{
		activeSessions: map[string]*ActiveSessionInfo{
			"session-owned-by-b": {SessionID: "session-owned-by-b", UserID: "user-b"},
		},
	}

	if api.canUseSessionIDForQuery(requestWithUserForSessionAccess("user-a"), "session-owned-by-b") {
		t.Fatal("canUseSessionIDForQuery allowed another user's active session")
	}
}

func TestCanUseSessionIDForQueryAllowsNewSessionID(t *testing.T) {
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{}}

	if !api.canUseSessionIDForQuery(requestWithUserForSessionAccess("user-a"), "new-session") {
		t.Fatal("canUseSessionIDForQuery rejected a new session id")
	}
}

func TestCanAccessTerminalSessionRejectsOtherUsersActiveSession(t *testing.T) {
	api := &StreamingAPI{
		activeSessions: map[string]*ActiveSessionInfo{
			"session-owned-by-b": {SessionID: "session-owned-by-b", UserID: "user-b"},
		},
	}

	if api.canAccessTerminalSession(requestWithUserForSessionAccess("user-a"), "session-owned-by-b") {
		t.Fatal("canAccessTerminalSession allowed another user's active session")
	}
}

func TestCanAccessSessionAllowsLegacyOwnerlessActiveSession(t *testing.T) {
	api := &StreamingAPI{
		activeSessions: map[string]*ActiveSessionInfo{
			"legacy-session": {SessionID: "legacy-session"},
		},
	}

	req := requestWithUserForSessionAccess("user-a")
	if !api.canUseSessionIDForQuery(req, "legacy-session") {
		t.Fatal("canUseSessionIDForQuery rejected legacy ownerless active session")
	}
	if !api.canAccessTerminalSession(req, "legacy-session") {
		t.Fatal("canAccessTerminalSession rejected legacy ownerless active session")
	}
}
