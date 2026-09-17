package server

import (
	"context"
	"testing"

	events "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
	"github.com/manishiitg/mcpagent/executor"
)

func TestSlackCredentialToolsRequireInteractiveMutationAdmission(t *testing.T) {
	api := &StreamingAPI{}
	for _, admitted := range []bool{false, true} {
		reg := &recordingRegistrar{}
		if err := api.registerSlackBotTools(reg, "session", "Workflow/example", "", admitted); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"configure_slack_bot", "get_slack_bot_credentials"} {
			tool, found := reg.tools[name]
			if found != admitted {
				t.Fatalf("%s admission = %v, want %v", name, found, admitted)
			}
			if found {
				contexts := []context.Context{
					context.Background(),
					context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "operator", Provider: "bot_route"}),
					context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "operator", BotRouteGrant: "owner"}),
				}
				for _, ctx := range contexts {
					if _, err := tool.exec(ctx, map[string]interface{}{"enabled": true}); err == nil {
						t.Fatalf("%s accepted unauthenticated or bot-origin caller", name)
					}
				}
			}
		}
	}
}

func TestSlackRouteToolAcceptsAuthenticatedCLIBridgeOwner(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "false")
	server, workspace := newFakeWorkspaceServer(t)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)
	workspace.files["Workflow/example/workflow.json"] = `{"schema_version":1,"id":"example","label":"Example","created_by":"alice"}`
	store, err := chathistory.NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertBotConnectorConfig(context.Background(), &chathistory.CreateBotConnectorConfigRequest{ID: "slack", Enabled: true, BotMode: true, AllowedChannels: "{}"}); err != nil {
		t.Fatal(err)
	}
	api := &StreamingAPI{chatStore: store, eventStore: events.NewEventStore(10)}
	api.eventStore.SetSessionOwner("builder-alice", "alice")
	reg := &recordingRegistrar{}
	if err := api.registerSlackBotTools(reg, "builder-alice", "Workflow/example", "", true); err != nil {
		t.Fatal(err)
	}
	tool := reg.tools["create_slack_bot_route"]
	resolve := api.bindToolExecutionContext(context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "alice"}), "builder-alice", QueryRequest{SelectedFolder: "Workflow/example", PresetQueryID: "example"}, false)
	invoke := func(ctx context.Context, args map[string]interface{}) (string, error) {
		operatorCtx, err := resolve(ctx, "create_slack_bot_route")
		if err != nil {
			return "", err
		}
		return tool.exec(operatorCtx, args)
	}
	ctx := executor.WithSessionID(context.Background(), "builder-alice")
	args := map[string]interface{}{"channel_id": "C0BTUQW85L1"}
	for _, bad := range []context.Context{
		executor.WithSessionID(context.Background(), "unknown"),
		context.WithValue(ctx, UserContextKey, &UserClaims{UserID: "bob"}),
		context.WithValue(ctx, UserContextKey, &UserClaims{UserID: "alice", Provider: "bot_route"}),
	} {
		if _, err := invoke(bad, args); err == nil {
			t.Fatal("route tool accepted anonymous, conflicting, or bot caller")
		}
	}
	if _, err := invoke(ctx, args); err != nil {
		t.Fatalf("authenticated CLI owner could not create route: %v", err)
	}
	_, routes, err := api.slackRoutes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	route, found := routes["C0BTUQW85L1"]
	if !found || route.WorkflowID != "example" || route.BotGrant != "run" || len(route.BlockedEmails) != 0 {
		t.Fatalf("unexpected persisted route: %+v", routes)
	}
	api.botExecutionSessions.Store("builder-alice", botExecutionSession{})
	if _, err := resolve(ctx, "create_slack_bot_route"); err == nil {
		t.Fatal("bot-bound bridge recovered owner authority")
	}
}
