package server

import (
	"context"
	"testing"
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
