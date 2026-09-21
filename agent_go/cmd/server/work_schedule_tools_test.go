package server

import "testing"

func TestRegisterWorkScheduleToolsReadsPairedUsersProjectManifest(t *testing.T) {
	workspace, docs := newFakeWorkspaceServer(t)
	t.Setenv("WORKSPACE_API_URL", workspace.URL)
	const userID = "paired-owner"
	const publicPath = "Chats/Work/projects/agentworks-6068cfc3"
	docs.files["_users/"+userID+"/"+publicPath+"/product.json"] = `{"schema_version":1,"product":"work","id":"6068cfc3-039a-4b85-8ade-f8c655000701","title":"Agentworks"}`

	api := &StreamingAPI{productSchedules: &ProductScheduleService{}}
	registrar := &recordingRegistrar{}
	if err := api.registerWorkScheduleTools(registrar, userID, publicPath); err != nil {
		t.Fatalf("register Work tools from public conversation path: %v", err)
	}
	if _, ok := registrar.tools["list_project_schedules"]; !ok {
		t.Fatal("project schedule tools were not registered")
	}

	other := &recordingRegistrar{}
	if err := api.registerWorkScheduleTools(other, "another-user", publicPath); err == nil {
		t.Fatal("another user's project manifest must not authorize schedule tools")
	}
}
