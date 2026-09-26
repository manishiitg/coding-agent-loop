package services

import (
	"context"
	"testing"
	"time"

	"github.com/manishiitg/mcpagent/events"
)

type startedTurn struct {
	sessionID, userID string
	req               map[string]interface{}
}

func whatsappManagerForTest(t *testing.T) (*BotConversationManager, chan startedTurn) {
	t.Helper()
	manager := NewBotConversationManager(nil, "", "")
	manager.RegisterConnector(&testBotConnector{name: "whatsapp"})
	started := make(chan startedTurn, 1)
	manager.SetStartSessionFunc(func(_ context.Context, req map[string]interface{}, sessionID, userID string, _ func(*events.AgentEvent)) error {
		started <- startedTurn{sessionID, userID, req}
		return nil
	})
	return manager, started
}

func waitStarted(t *testing.T, started chan startedTurn) startedTurn {
	t.Helper()
	select {
	case got := <-started:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("session never started")
	}
	return startedTurn{}
}

// Crews are read-only for everyone but their owner. A WhatsApp message to
// someone else's crew runs as the paired user — never as the crew's owner.
func TestWhatsAppOtherOwnersCrewRunsAsThePairedUser(t *testing.T) {
	manager, started := whatsappManagerForTest(t)
	profileUser := make(chan string, 1)
	manager.SetProfileTurnFunc(func(_ context.Context, userID string, msg BotIncomingMessage, _ ThreadID) (map[string]interface{}, string, bool, error) {
		profileUser <- userID
		return map[string]interface{}{"agent_profile_id": "work", "query": msg.Text}, "reader-crew-chat", true, nil
	})
	manager.HandleIncomingMessage(BotIncomingMessage{
		Platform: "whatsapp", UserID: "phone", WorkspaceUserID: "reader", ChannelID: "dm", Text: "status?", IsMention: true,
		PresetWorkflow: &ChannelRoute{ProfileID: "work", ConversationKey: "crew-aaa", WorkspacePath: "_users/owner/Chats/Work/projects/alpha", BotGrant: "run", WorkshopMode: "run"},
	})
	if got := <-profileUser; got != "reader" {
		t.Fatalf("crew turn built for %q, want the paired user", got)
	}
	if got := waitStarted(t, started); got.userID != "reader" {
		t.Fatalf("session runs as %q, want the paired user (not the crew owner)", got.userID)
	}
}

// One user, one chat: a WhatsApp message to a workflow continues the user's
// own chat of it — the one their web Builder restores.
func TestWhatsAppWorkflowContinuesTheUsersBuilderChat(t *testing.T) {
	manager, started := whatsappManagerForTest(t)
	manager.SetWorkflowAccessFunc(func(_ context.Context, userID, _ string, _ ChannelRoute) (string, bool, error) {
		return userID, true, nil
	})
	manager.SetUserWorkflowChatFunc(func(_ context.Context, userID string, route ChannelRoute) string {
		if userID == "owner" && route.WorkflowID == "wf-report" {
			return "owner-web-builder"
		}
		return ""
	})
	manager.HandleIncomingMessage(BotIncomingMessage{
		Platform: "whatsapp", UserID: "phone", WorkspaceUserID: "owner", ChannelID: "dm", Text: "run it", IsMention: true,
		PresetWorkflow: &ChannelRoute{WorkflowID: "wf-report", WorkspacePath: "Workflow/report", WorkshopMode: "run"},
	})
	if got := waitStarted(t, started); got.sessionID != "owner-web-builder" || got.userID != "owner" {
		t.Fatalf("WhatsApp workflow turn ran in %q as %q, want the owner's web Builder chat", got.sessionID, got.userID)
	}
}
