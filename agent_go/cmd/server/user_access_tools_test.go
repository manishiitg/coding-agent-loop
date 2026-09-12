package server

import (
	"context"
	"strings"
	"testing"
)

func TestUserAccessToolsPermissionsAndRevocation(t *testing.T) {
	api := managedGlobalTestAPI(t)
	policy := workflowChatPolicy{Capabilities: map[string]bool{"user_management": true}}
	register := func(id string) recordedTool {
		t.Helper()
		r := &recordingRegistrar{}
		if err := api.registerUserAccessTools(r, id, sharedSecretsTestWorkflow, policy); err != nil {
			t.Fatal(err)
		}
		return r.tools["manage_user_access"]
	}
	owner, reader, admin := register("a1"), register("c3"), register("admin")
	ctx := context.Background()
	for _, action := range []string{"get_workflow_access", "set_workflow_access", "list_users", "create_user", "update_user"} {
		if _, err := reader.exec(ctx, map[string]interface{}{"action": action}); err == nil {
			t.Fatalf("reader allowed %s", action)
		}
	}
	if _, err := owner.exec(ctx, map[string]interface{}{"action": "create_user", "username": "unexpected"}); err == nil {
		t.Fatal("owner can create account")
	}
	if _, err := owner.exec(ctx, map[string]interface{}{"action": "list_users"}); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.exec(ctx, map[string]interface{}{"action": "get_workflow_access"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"Workflow/missing", "Workflow/renewals/../other", ""} {
		if _, err := admin.exec(ctx, map[string]interface{}{"action": "get_workflow_access", "workspace_path": path}); err == nil {
			t.Fatalf("invalid path %q accepted", path)
		}
	}
	if _, err := owner.exec(ctx, map[string]interface{}{"action": "set_workflow_access", "owners": []string{}, "readers": []string{"c3"}}); err == nil {
		t.Fatal("last owner removed")
	}
	if _, err := owner.exec(ctx, map[string]interface{}{"action": "set_workflow_access", "owners": []string{"a1"}, "readers": []string{"c3"}}); err != nil {
		t.Fatal(err)
	}
	output, err := admin.exec(ctx, map[string]interface{}{"action": "create_user", "username": "newreader", "password": "private-password-123", "can_edit": false})
	if err != nil || strings.Contains(output, "private-password-123") || strings.Contains(output, "password_hash") {
		t.Fatalf("account creation: %v", err)
	}
	output, err = admin.exec(ctx, map[string]interface{}{"action": "update_user", "user_id": userIDForUsername("newreader"), "can_create": true})
	if err != nil || !strings.Contains(output, `"can_create":true`) {
		t.Fatalf("account update: %v", err)
	}
	withMemoryUserDirectory(t, `{"users":[{"id":"admin","username":"admin","can_create":true,"products":[]},{"id":"a1","username":"owner","can_create":true,"products":[]}]}`)
	if _, err := admin.exec(ctx, map[string]interface{}{"action": "list_users"}); err == nil {
		t.Fatal("demoted non-owner retained directory authority")
	}
	if _, err := admin.exec(ctx, map[string]interface{}{"action": "create_user", "username": "late"}); err == nil {
		t.Fatal("demoted admin retained mutation authority")
	}
}

func TestUserAccessToolsAbsentOutsideBuilder(t *testing.T) {
	api := &StreamingAPI{}
	for _, mode := range []string{"workshop", "run"} {
		for _, origin := range []string{"interactive", "scheduled", "child", "pulse"} {
			req := QueryRequest{}
			switch origin {
			case "scheduled":
				req.TriggeredBy = "cron"
			case "child":
				req.ParentSessionID = "parent"
			case "pulse":
				req.PulseLifecycleTurn = true
			}
			policy := resolveWorkflowChatPolicy(mode, "session", req, nil, false)
			reg := &recordingRegistrar{}
			if err := api.registerUserAccessTools(reg, "user", "Workflow/example", policy); err != nil {
				t.Fatal(err)
			}
			_, exists := reg.tools["manage_user_access"]
			if exists != (mode == "workshop" && origin == "interactive") {
				t.Fatalf("admission %s/%s=%v", mode, origin, exists)
			}
		}
	}
}
