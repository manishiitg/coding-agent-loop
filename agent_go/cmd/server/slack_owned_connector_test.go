package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
)

// switchOffNoRoutes seeds the "slack" connector config with the platform
// bot switch off and no channel routes.
func switchOffNoRoutes(t *testing.T, api *StreamingAPI) *chathistory.BotConnectorConfig {
	t.Helper()
	cfg, err := api.chatStore.UpsertBotConnectorConfig(context.Background(), &chathistory.CreateBotConnectorConfigRequest{ID: "slack", Enabled: true, BotMode: false, AllowedChannels: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func createSlackConnectionForTest(t *testing.T, api *StreamingAPI, userID, body string) {
	t.Helper()
	w := httptest.NewRecorder()
	createSlackConnectionHandler(api)(w, slackConnectionRequest(t, "POST", userID, body))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", w.Code, w.Body.String())
	}
}

// Startup-equivalent: switch off, no routes, but an enabled workflow-owned
// Slack app must still get the bot manager's message handler.
func TestSlackStartupRegistersConnectorForOwnedConnection(t *testing.T) {
	api, _ := setupSlackConnectionWorld(t)
	botConfig := switchOffNoRoutes(t, api)
	// No bot manager yet: the create must not register anything itself.
	createSlackConnectionForTest(t, api, "alice", `{"display_name":"Alpha App","bot_token":"xoxb-alpha","app_token":"xapp-alpha","enabled":true,"workspace_path":"Workflow/alpha"}`)

	svc := services.GetSlackService()
	botManager := services.NewBotConversationManager(api.chatStore, "", "")
	if !slackBotConnectorWantedAtStartup(botConfig, svc) {
		t.Fatal("owned enabled connection not wanted at startup")
	}
	if !registerSlackBotConnector(botManager, svc) {
		t.Fatal("connector not registered at startup")
	}
	if botManager.GetConnector("slack") == nil {
		t.Fatal("connector missing after startup registration")
	}
	if registerSlackBotConnector(botManager, svc) {
		t.Fatal("second registration should be a no-op")
	}
}

// Creating an owned connection through the route handler registers the
// connector when the switch is off and no route exists.
func TestSlackOwnedConnectionCreateRegistersConnector(t *testing.T) {
	api, _ := setupSlackConnectionWorld(t)
	switchOffNoRoutes(t, api)
	api.botManager = services.NewBotConversationManager(api.chatStore, "", "")

	createSlackConnectionForTest(t, api, "alice", `{"display_name":"Alpha App","bot_token":"xoxb-alpha","app_token":"xapp-alpha","enabled":true,"workspace_path":"Workflow/alpha"}`)
	if api.botManager.GetConnector("slack") == nil {
		t.Fatal("connector not registered after creating an owned connection")
	}
}

// Switch off, no routes, no enabled owned connection: nothing registers,
// exactly as before.
func TestSlackNoOwnedConnectionDoesNotRegister(t *testing.T) {
	api, _ := setupSlackConnectionWorld(t)
	botConfig := switchOffNoRoutes(t, api)
	api.botManager = services.NewBotConversationManager(api.chatStore, "", "")

	// A disabled owned app and an unscoped platform app do not count.
	createSlackConnectionForTest(t, api, "alice", `{"display_name":"Alpha App","bot_token":"xoxb-alpha","app_token":"xapp-alpha","enabled":false,"workspace_path":"Workflow/alpha"}`)
	createSlackConnectionForTest(t, api, "admin-1", `{"display_name":"Platform","bot_token":"xoxb-plat","app_token":"xapp-plat","enabled":false}`)
	if api.botManager.GetConnector("slack") != nil {
		t.Fatal("connector registered without switch, routes, or an enabled owned connection")
	}
	if slackBotConnectorWantedAtStartup(botConfig, services.GetSlackService()) {
		t.Fatal("startup wants the connector without switch, routes, or an enabled owned connection")
	}
	if slackBotConnectorWantedAtStartup(nil, nil) {
		t.Fatal("startup wants the connector with no config and no service")
	}
}
