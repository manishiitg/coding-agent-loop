package server

import (
	"context"
	"encoding/json"
	"testing"

	events "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"github.com/manishiitg/mcpagent/executor"
)

func TestToolExecutionContextUsesAuthenticatedQueryForAllTransports(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "false")
	api := &StreamingAPI{eventStore: events.NewEventStore(10)}
	api.eventStore.SetSessionOwner("builder", "alice")
	claims := &UserClaims{UserID: "alice", Email: "alice@example.com", Provider: "local"}
	requestCtx := context.WithValue(context.Background(), UserContextKey, claims)
	resolve := api.bindToolExecutionContext(requestCtx, "builder", QueryRequest{}, false)
	// Mutating the original request must not alter the definition's binding.
	claims.UserID = "bob"
	for _, incoming := range []context.Context{context.Background(), executor.WithSessionID(context.Background(), "builder")} {
		ctx, err := resolve(incoming, "any_future_tool")
		if err != nil {
			t.Fatal(err)
		}
		got := GetUserFromContext(ctx)
		if got.UserID != "alice" || got.Email != "alice@example.com" || got.Provider != "local" || executor.SessionIDFromContext(ctx) != "builder" || ctx.Value(common.UserIDKey) != "alice" {
			t.Fatalf("lost bound identity: %+v", got)
		}
		got.UserID = "changed-by-handler"
	}
	for _, incoming := range []context.Context{
		executor.WithSessionID(context.Background(), "other-session"),
		context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "bob"}),
		context.WithValue(context.Background(), common.UserIDKey, "bob"),
		context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "alice", Provider: "bot_route"}),
	} {
		if _, err := resolve(incoming, "tool"); err == nil {
			t.Fatal("conflicting caller identity was accepted")
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	ctx, err := resolve(canceled, "tool")
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("context binding discarded cancellation")
	}
	api.eventStore.SetSessionOwner("builder", "bob")
	if _, err := resolve(context.Background(), "tool"); err == nil {
		t.Fatal("session ownership change was ignored")
	}
}

func TestToolExecutionContextNeverInventsLocalOwner(t *testing.T) {
	api := &StreamingAPI{}
	for _, claims := range []*UserClaims{nil, {}} {
		ctx := context.Background()
		if claims != nil {
			ctx = context.WithValue(ctx, UserContextKey, claims)
		}
		resolve := api.bindToolExecutionContext(ctx, "builder", QueryRequest{}, false)
		if _, err := resolve(context.Background(), "tool"); err == nil {
			t.Fatal("missing authentication fell back to local owner")
		}
	}
}

func TestToolExecutionContextRetainsBotIdentityAndRevalidatesRoute(t *testing.T) {
	store, err := chathistory.NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := &StreamingAPI{chatStore: store}
	route := ChannelRoute{ProfileID: "work", ConversationKey: "acme", WorkspacePath: "Chats/Work/projects/acme", WorkspaceUserID: "alice", BotGrant: "run"}
	encoded, _ := json.Marshal(map[string]ChannelRoute{"C123": route})
	save := func(routes string) {
		if _, err := store.UpsertBotConnectorConfig(context.Background(), &chathistory.CreateBotConnectorConfigRequest{ID: "slack", Enabled: true, BotMode: true, AllowedChannels: routes}); err != nil {
			t.Fatal(err)
		}
	}
	save(string(encoded))
	req := QueryRequest{AgentProfileID: "work", AgentProfileConversationKey: "acme", SelectedFolder: route.WorkspacePath, BotPlatform: "slack", BotChannelID: "C123"}
	claims := botRouteUserClaims("bot", route)
	requestCtx, err := api.revalidateExecutionPrincipal(context.WithValue(context.Background(), UserContextKey, claims), req)
	if err != nil {
		t.Fatal(err)
	}
	resolve := api.bindToolExecutionContext(requestCtx, "bot-session", req, true)
	incoming := executor.WithSessionID(context.Background(), "bot-session")
	ctx, err := resolve(incoming, "read_tool")
	if err != nil {
		t.Fatal(err)
	}
	got := GetUserFromContext(ctx)
	if got.Provider != "bot_route" || got.BotRouteGrant != "run" || got.ExecutionPrincipal == nil || got.ExecutionPrincipal.Access != WorkflowAccessRead || userAccessForClaims(got).Admin {
		t.Fatalf("bot identity widened: %+v", got)
	}
	save("{}")
	if _, err := resolve(incoming, "read_tool"); err == nil {
		t.Fatal("revoked route was ignored")
	}
}

func TestToolExecutionContextAdmitsTheWhatsAppOwnerPrincipal(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "false")
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{
		"wa-product": {SessionID: "wa-product", BotPlatform: "whatsapp", TriggeredBy: "bot:whatsapp"},
	}}
	req := QueryRequest{BotPlatform: "whatsapp", TriggeredBy: "bot:whatsapp"}
	incoming := executor.WithSessionID(context.Background(), "wa-product")

	// The paired owner's own turn executes tools in its bot-marked session.
	owner := api.bindToolExecutionContext(
		context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "owner", Provider: "bot_owner"}),
		"wa-product", req, false)
	if _, err := owner(incoming, "read_image"); err != nil {
		t.Fatalf("bot_owner tools rejected: %v", err)
	}

	// Anything else bound on a bot-marked session is still an origin change.
	plain := api.bindToolExecutionContext(
		context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "owner", Provider: "local"}),
		"wa-product", req, false)
	if _, err := plain(incoming, "read_image"); err == nil {
		t.Fatal("non-bot principal executed tools in a bot-marked session")
	}
}
