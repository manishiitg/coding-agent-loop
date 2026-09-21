package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

func TestConversationTargetAccessUsesCrewProjectAndWorkflowGrant(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"owner","username":"owner","can_create":false,"products":["work"]},{"id":"other","username":"other","can_create":true,"products":["work"]}]}`)
	const projectID = "project-1"
	const publicPath = "Chats/Work/projects/my-crew"
	workspace := &mockWorkspaceAPI{files: map[string]string{
		"_users/owner/" + publicPath + "/product.json": `{"schema_version":1,"product":"work","id":"project-1","title":"My Crew","session_id":"crew-session"}`,
		manifestPath("Workflow/shared"):                `{"schema_version":1,"id":"wf_shared","access":{"owners":["other"],"readers":["owner"]}}`,
	}}
	host := httptest.NewServer(workspace)
	defer host.Close()
	t.Setenv("WORKSPACE_API_URL", host.URL)

	profiles := agentprofiles.NewRegistry()
	profile := routeTestProfile("work", true, "")
	profile.Product = "work"
	profile.Runtime.Workspace = agentprofiles.WorkspacePolicy{Mode: agentprofiles.WorkspaceModeProject, ProjectsRoot: "Chats/Work/projects"}
	profile.Runtime.Conversation = agentprofiles.ConversationPolicy{Mode: agentprofiles.ConversationModeKeyed, KeyType: agentprofiles.ConversationKeyTypeProject}
	if err := profiles.RegisterProfile(profile); err != nil {
		t.Fatal(err)
	}
	api := &StreamingAPI{agentProfiles: profiles}
	ctx := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "owner", Username: "owner"})
	crew := QueryRequest{AgentMode: "multi-agent", AgentProfileID: "work", AgentProfileConversationKey: projectID, SelectedFolder: publicPath}
	if access, err := api.conversationTargetAccess(ctx, crew); err != nil || access != WorkflowAccessOwner {
		t.Fatalf("own Crew access=%q err=%v, want owner", access, err)
	}
	botClaims := &UserClaims{UserID: "owner", Provider: "bot_route", BotRouteProfileID: "work", BotRouteConversationKey: projectID, BotRouteWorkspacePath: publicPath, BotRouteGrant: "run"}
	botCtx := context.WithValue(context.Background(), UserContextKey, botClaims)
	if access, err := api.conversationTargetAccess(botCtx, crew); err != nil || access != WorkflowAccessRead {
		t.Fatalf("Slack Crew route access=%q err=%v, want read", access, err)
	}
	if access, err := api.conversationTargetAccess(ctx, QueryRequest{SelectedFolder: "Workflow/shared"}); err != nil || access != WorkflowAccessRead {
		t.Fatalf("shared workflow access=%q err=%v, want read", access, err)
	}
	for _, forged := range []QueryRequest{
		{AgentProfileID: "work", AgentProfileConversationKey: projectID, SelectedFolder: "Chats/Work/projects/other"},
		{AgentProfileID: "work", AgentProfileConversationKey: "missing", SelectedFolder: publicPath},
		{AgentProfileID: "work", AgentProfileConversationKey: projectID, SelectedFolder: "_users/other/" + publicPath},
	} {
		if access, err := api.conversationTargetAccess(ctx, forged); err == nil || access != WorkflowAccessNone {
			t.Fatalf("forged Crew request allowed: %+v => %q, %v", forged, access, err)
		}
	}
	workspace.mu.Lock()
	delete(workspace.files, "_users/owner/"+publicPath+"/product.json")
	workspace.mu.Unlock()
	if access, err := api.conversationTargetAccess(ctx, crew); err == nil || access != WorkflowAccessNone {
		t.Fatalf("deleted Crew allowed: %q, %v", access, err)
	}
}
