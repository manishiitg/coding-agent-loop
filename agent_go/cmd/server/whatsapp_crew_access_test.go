package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

func TestWhatsAppCrewRouteChecksPairedOwnerProject(t *testing.T) {
	const owner = "shubham"
	const project = "agentworks-6068cfc3"
	const projectID = "6068cfc3-039a-4b85-8ade-f8c655000701"
	const publicPath = "Chats/Work/projects/" + project
	manifestPath := "_users/" + owner + "/" + publicPath + "/product.json"

	workspaceAPI := &mockWorkspaceAPI{files: map[string]string{
		manifestPath: `{"schema_version":1,"product":"work","id":"` + projectID + `","title":"Agentworks","session_id":"work:project"}`,
	}}
	server := httptest.NewServer(workspaceAPI)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	profiles := agentprofiles.NewRegistry()
	profile := routeTestProfile("work", true, "")
	profile.Runtime.Workspace = agentprofiles.WorkspacePolicy{Mode: agentprofiles.WorkspaceModeProject, ProjectsRoot: "Chats/Work/projects"}
	profile.Runtime.Conversation = agentprofiles.ConversationPolicy{Mode: agentprofiles.ConversationModeKeyed, KeyType: agentprofiles.ConversationKeyTypeProject}
	if err := profiles.RegisterProfile(profile); err != nil {
		t.Fatal(err)
	}
	api := &StreamingAPI{agentProfiles: profiles}
	route := services.ChannelRoute{
		ProfileID: "work", ConversationKey: projectID, WorkspacePath: publicPath,
		WorkshopMode: "run", BotGrant: "run",
	}
	if allowed, err := api.checkWhatsAppWorkflowAccess(context.Background(), owner, route); err != nil || !allowed {
		t.Fatalf("owner's Crew route allowed=%v err=%v", allowed, err)
	}

	for name, changed := range map[string]services.ChannelRoute{
		"wrong workspace": func() services.ChannelRoute {
			other := route
			other.WorkspacePath = "Chats/Work/projects/other"
			return other
		}(),
		"wrong project id":    func() services.ChannelRoute { other := route; other.ConversationKey = "other"; return other }(),
		"workflow id on crew": func() services.ChannelRoute { other := route; other.WorkflowID = "wf_other"; return other }(),
	} {
		t.Run(name, func(t *testing.T) {
			if allowed, err := api.checkWhatsAppWorkflowAccess(context.Background(), owner, changed); err != nil || allowed {
				t.Fatalf("mismatched Crew route allowed=%v err=%v", allowed, err)
			}
		})
	}
	if allowed, err := api.checkWhatsAppWorkflowAccess(context.Background(), "other-user", route); err != nil || allowed {
		t.Fatalf("other user allowed=%v err=%v", allowed, err)
	}
	workspaceAPI.mu.Lock()
	delete(workspaceAPI.files, manifestPath)
	workspaceAPI.mu.Unlock()
	if allowed, err := api.checkWhatsAppWorkflowAccess(context.Background(), owner, route); err != nil || allowed {
		t.Fatalf("deleted Crew route allowed=%v err=%v", allowed, err)
	}
}
