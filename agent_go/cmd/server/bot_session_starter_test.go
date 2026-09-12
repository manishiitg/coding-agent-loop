package server

import (
	"context"
	"testing"
	"time"

	internalevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"
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

// P0 integration: the bot's follow-up crosses handleQuery and reaches retained
// Muse delivery while the conversation is running. The terminal delivery
// boundary is stubbed; no real user chat is messaged.
func TestBotFollowUpReachesRetainedMuseThroughQueryP0(t *testing.T) {
	t.Setenv("TRACING_PROVIDER", "noop")
	const sessionID = "bot-steer-query-p0"
	const userID = "bot-steer-owner"
	store := internalevents.NewEventStore(20)
	defer store.Stop()
	terminalStore := terminals.NewStore()
	terminalStore.HandleEvent(sessionID, codingAgentTmuxReaperChunkEvent(time.Now(), sessionID, "main:"+sessionID, "mlp-muse-cli-int-bot-steer"))
	deliveries := 0
	api := &StreamingAPI{
		eventStore: store, terminalStore: terminalStore,
		activeSessions: map[string]*ActiveSessionInfo{sessionID: {SessionID: sessionID, UserID: userID, Status: "running"}},
		internalRetainedTerminalInputHandler: func(_ context.Context, provider llmproviders.Provider, _, owner, message string) error {
			if provider != llmproviders.ProviderMuseCLI || owner != sessionID || message != "use Notion instead" {
				t.Fatalf("wrong steer: provider=%s owner=%s message=%q", provider, owner, message)
			}
			deliveries++
			return nil
		},
	}
	t.Cleanup(func() {
		api.retainedMainTurnsMu.Lock()
		cancelWatch := api.retainedMainTurnWatchCancels[sessionID]
		api.retainedMainTurnsMu.Unlock()
		if cancelWatch != nil {
			cancelWatch()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := api.sendFollowUpInternal(ctx, map[string]interface{}{"query": "use Notion instead", "agent_mode": "workflow_phase", "phase_id": "workflow-builder"}, sessionID, userID); err != nil {
		t.Fatal(err)
	}
	if deliveries != 1 {
		t.Fatalf("retained tmux deliveries=%d, want 1", deliveries)
	}
}
