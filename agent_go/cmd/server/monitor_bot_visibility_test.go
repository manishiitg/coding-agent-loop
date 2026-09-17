package server

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestMonitorBotSessionVisibilityRespectsWorkflowAccess(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"reader","username":"reader","can_create":false,"products":["agentworks"]},{"id":"outsider","username":"outsider","can_create":false,"products":["agentworks"]}]}`)
	workspace := &mockWorkspaceAPI{files: map[string]string{"Workflow/monitor-bot/workflow.json": `{"id":"wf-monitor","schema_version":1,"created_by":"owner","access":{"owners":["owner"],"readers":["reader"]}}`}}
	server := httptest.NewServer(workspace)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)
	session := &ActiveSessionInfo{SessionID: "bot-slack--session", UserID: "bot-slack-8d3dd8b8e39a5d46", BotPlatform: "slack", WorkspacePath: "Workflow/monitor-bot", PresetQueryID: "wf-monitor"}
	ctx := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "reader", Username: "reader"})
	if !monitorSessionVisibleTo(ctx, session) {
		t.Fatal("workflow reader cannot see bot activity")
	}
	outsider := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "outsider", Username: "outsider"})
	if monitorSessionVisibleTo(outsider, session) {
		t.Fatal("outsider can see bot activity")
	}
	session.PresetQueryID = "other-workflow"
	if monitorSessionVisibleTo(ctx, session) {
		t.Fatal("mismatched workflow exposed")
	}
	session.PresetQueryID = "wf-monitor"
	session.UserID = "another-human"
	if monitorSessionVisibleTo(ctx, session) {
		t.Fatal("another user's private session exposed")
	}
}
