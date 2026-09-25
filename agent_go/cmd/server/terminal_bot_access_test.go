package server

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestBotSessionTerminalNeedsWorkflowWriteAccess(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"owner","username":"owner","can_create":true,"products":["agentworks"]},{"id":"reader","username":"reader","can_create":false,"products":["agentworks"]},{"id":"outsider","username":"outsider","can_create":false,"products":["agentworks"]}]}`)
	workspace := &mockWorkspaceAPI{files: map[string]string{"Workflow/term-bot/workflow.json": `{"id":"wf-term","schema_version":1,"created_by":"owner","access":{"owners":["owner"],"readers":["reader"]}}`}}
	server := httptest.NewServer(workspace)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)
	session := &ActiveSessionInfo{SessionID: "bot-slack--term", UserID: "bot-slack-8d3dd8b8e39a5d46", BotPlatform: "slack", WorkspacePath: "Workflow/term-bot", PresetQueryID: "wf-term"}
	as := func(user string) context.Context {
		return context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: user, Username: user})
	}
	if !botSessionTerminalAllowed(as("owner"), session) {
		t.Fatal("workflow owner cannot open the bot session terminal")
	}
	if botSessionTerminalAllowed(as("reader"), session) {
		t.Fatal("workflow reader got a terminal that can type into the session")
	}
	if botSessionTerminalAllowed(as("outsider"), session) {
		t.Fatal("outsider opened the bot session terminal")
	}
	human := *session
	human.UserID, human.BotPlatform = "someone", ""
	if botSessionTerminalAllowed(as("owner"), &human) {
		t.Fatal("another user's own chat terminal exposed")
	}
}
