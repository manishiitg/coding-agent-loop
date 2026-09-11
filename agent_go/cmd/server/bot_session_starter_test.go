package server

import (
	"context"
	"testing"
)

func TestInternalBotRequestContextUsesThePairedAccountsLiveRole(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[
	  {"id":"owner-1","username":"owner","admin":true,"can_create":true,"products":[]},
	  {"id":"reader-1","username":"reader","can_create":false,"products":["agentworks"]}
	]}`)

	tests := []struct {
		userID   string
		username string
		access   WorkflowAccessLevel
	}{
		{userID: "owner-1", username: "owner", access: WorkflowAccessOwner},
		{userID: "reader-1", username: "reader", access: WorkflowAccessRead},
	}
	for _, test := range tests {
		ctx := internalBotRequestContext(context.Background(), test.userID)
		claims := GetUserFromContext(ctx)
		if claims == nil || claims.UserID != test.userID || claims.Username != test.username {
			t.Fatalf("bot claims for %s = %+v", test.userID, claims)
		}
		if got := workflowAccessForClaims(claims); got != test.access {
			t.Fatalf("bot access for %s = %s, want %s", test.userID, got, test.access)
		}
	}
}

func TestInternalBotRequestContextPreservesExistingAuthenticatedClaims(t *testing.T) {
	existing := &UserClaims{UserID: "existing", Username: "signed-in-user"}
	ctx := context.WithValue(context.Background(), UserContextKey, existing)
	got := GetUserFromContext(internalBotRequestContext(ctx, "different"))
	if got != existing {
		t.Fatalf("existing authenticated claims were replaced: %+v", got)
	}
}
