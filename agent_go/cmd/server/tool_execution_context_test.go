package server

import (
	"context"
	"encoding/json"
	"testing"

	events "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"github.com/manishiitg/mcpagent/executor"
	"github.com/manishiitg/mcpagent/mcpclient"
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

func TestToolExecutionContextAdmitsRegisteredWorkflowChildSession(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "false")
	const parentSession = "schedule-cron--daily-tool-context-test"
	const childSession = "session-group-default-tool-context-test"
	registry := mcpclient.GetSessionRegistry()
	registry.RegisterHTTPSession(parentSession, childSession)
	t.Cleanup(func() { registry.CloseHTTPSession(parentSession) })

	api := &StreamingAPI{eventStore: events.NewEventStore(10)}
	api.eventStore.SetSessionOwner(parentSession, "alice")
	requestCtx := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "alice", Provider: "local"})
	resolve := api.bindToolExecutionContext(requestCtx, parentSession, QueryRequest{}, false)

	ctx, err := resolve(executor.WithSessionID(context.Background(), childSession), "agent_browser")
	if err != nil {
		t.Fatalf("registered workflow child was rejected: %v", err)
	}
	if got := executor.SessionIDFromContext(ctx); got != parentSession {
		t.Fatalf("bound execution session = %q, want authenticated parent %q", got, parentSession)
	}
	if got := GetUserFromContext(ctx); got == nil || got.UserID != "alice" {
		t.Fatalf("registered child lost owner identity: %+v", got)
	}

	if _, err := resolve(executor.WithSessionID(context.Background(), childSession+"-forged"), "agent_browser"); err == nil {
		t.Fatal("unregistered lookalike child session was accepted")
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

// TestToolExecutionContextTopologyMatrix is the CI contract for every place a
// tool call can originate. Keep the topology names explicit: adding a new
// execution origin, transport, or privileged tool surface must add a row here
// rather than relying on an interactive-chat test to stand in for it.
func TestToolExecutionContextTopologyMatrix(t *testing.T) {
	t.Run("interactive owner and conflicting reader/direct tool invocation", TestToolExecutionContextUsesAuthenticatedQueryForAllTransports)
	t.Run("cron manual API and internal triggers/workflow owner/action surfaces", func(t *testing.T) {
		t.Setenv("MULTI_USER_MODE", "false")
		t.Setenv("DEFAULT_USER_ID", "legacy-owner")
		origins := []struct {
			name    string
			session string
			tool    string
			owner   string
			caller  func(string) context.Context
		}{
			{"cron schedule", "schedule-cron--daily", "agent_browser", "owner", func(session string) context.Context { return executor.WithSessionID(context.Background(), session) }},
			{"manual schedule", "schedule-manual--daily", "execute_shell_command", "owner", func(session string) context.Context { return executor.WithSessionID(context.Background(), session) }},
			{"API trigger", "schedule-api--incoming", "diff_patch_workspace_file", "owner", func(session string) context.Context {
				return context.WithValue(context.Background(), common.ChatSessionIDKey, session)
			}},
			{"internal trigger", "schedule-internal--incoming", "get_workflow_config", "owner", func(session string) context.Context {
				return context.WithValue(context.Background(), common.ChatSessionIDKey, session)
			}},
			{"legacy single-user schedule", "schedule-cron--legacy", "get_workflow_config", workflowExecutionOwnerUserID(&WorkflowManifest{ID: "legacy"}), func(session string) context.Context { return executor.WithSessionID(context.Background(), session) }},
		}
		for _, origin := range origins {
			t.Run(origin.name, func(t *testing.T) {
				api := &StreamingAPI{eventStore: events.NewEventStore(10)}
				api.eventStore.SetSessionOwner(origin.session, origin.owner)
				requestCtx := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: origin.owner, Provider: "local"})
				ctx, err := api.bindToolExecutionContext(requestCtx, origin.session, QueryRequest{}, false)(origin.caller(origin.session), origin.tool)
				if err != nil {
					t.Fatalf("%s rejected: %v", origin.tool, err)
				}
				if got := GetUserFromContext(ctx); got == nil || got.UserID != origin.owner {
					t.Fatalf("bound claims = %+v, want owner %q", got, origin.owner)
				}
			})
		}
	})
	t.Run("registered and forged workflow children/MCP custom HTTP invocation", TestToolExecutionContextAdmitsRegisteredWorkflowChildSession)
	t.Run("real MCP stdio bridge and HTTP custom-tool transport", TestWorkflowStepDatabaseToolsThroughMCPBridge)
	t.Run("interactive reader remains read-only", func(t *testing.T) {
		t.Setenv("MULTI_USER_MODE", "true")
		withMemoryUserDirectory(t, `{"users":[{"id":"reader","username":"reader","can_create":false}]}`)
		requestCtx := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "reader", Username: "reader", Provider: "local"})
		api := &StreamingAPI{}
		read := api.bindToolExecutionContext(requestCtx, "reader-session", QueryRequest{}, true)
		if _, err := read(executor.WithSessionID(context.Background(), "reader-session"), "get_workflow_config"); err != nil {
			t.Fatalf("reader read tool rejected: %v", err)
		}
		write := api.bindToolExecutionContext(requestCtx, "reader-session", QueryRequest{}, false)
		if _, err := write(executor.WithSessionID(context.Background(), "reader-session"), "execute_shell_command"); err == nil {
			t.Fatal("reader was widened to builder authority")
		}
	})
	t.Run("Slack Run principal/read-only custom tool", TestToolExecutionContextRetainsBotIdentityAndRevalidatesRoute)
	t.Run("WhatsApp paired owner", TestToolExecutionContextAdmitsTheWhatsAppOwnerPrincipal)
	t.Run("missing authenticated principal fails closed", TestToolExecutionContextNeverInventsLocalOwner)
	t.Run("delegated and background agent isolated session", func(t *testing.T) {
		t.Setenv("MULTI_USER_MODE", "false")
		api := &StreamingAPI{eventStore: events.NewEventStore(10)}
		api.eventStore.SetSessionOwner("parent", "owner")
		requestCtx := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "owner", Provider: "local"})
		resolve := api.bindToolExecutionContextForSession(requestCtx, "parent", "delegated-isolated", QueryRequest{}, false)
		ctx, err := resolve(executor.WithSessionID(context.Background(), "delegated-isolated"), "execute_shell_command")
		if err != nil {
			t.Fatalf("delegated action tool rejected: %v", err)
		}
		if got := executor.SessionIDFromContext(ctx); got != "delegated-isolated" {
			t.Fatalf("delegated execution session = %q", got)
		}
		if got := GetUserFromContext(ctx); got == nil || got.UserID != "owner" {
			t.Fatalf("delegated claims = %+v", got)
		}
		api.eventStore.SetSessionOwner("parent", "different-owner")
		if _, err := resolve(executor.WithSessionID(context.Background(), "delegated-isolated"), "agent_browser"); err == nil {
			t.Fatal("background child ignored parent ownership revocation")
		}
	})
}
