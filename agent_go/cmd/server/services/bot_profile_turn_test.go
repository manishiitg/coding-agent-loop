package services

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/manishiitg/mcpagent/events"
)

// An unrouted WhatsApp message on an account with a default product profile
// runs in that profile's own conversation: the session id and request come
// from the profile-turn builder, not the generic bot request.
func TestUnroutedMessageRunsInTheDefaultProfileConversation(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	manager.RegisterConnector(&testBotConnector{})

	manager.SetProfileTurnFunc(func(_ context.Context, userID string, msg BotIncomingMessage, _ ThreadID) (map[string]interface{}, string, bool, error) {
		if userID != "user-1" {
			t.Fatalf("profile turn userID = %q, want user-1", userID)
		}
		return map[string]interface{}{"agent_profile_id": "sparkquill", "query": msg.Text}, "conv-sparkquill", true, nil
	})

	started := make(chan struct {
		sessionID string
		req       map[string]interface{}
	}, 1)
	manager.SetStartSessionFunc(func(_ context.Context, req map[string]interface{}, sessionID, _ string, _ func(*events.AgentEvent)) error {
		started <- struct {
			sessionID string
			req       map[string]interface{}
		}{sessionID, req}
		return nil
	})

	manager.HandleIncomingMessage(BotIncomingMessage{
		Platform:        "whatsapp",
		UserID:          "phone",
		WorkspaceUserID: "user-1",
		ChannelID:       "dm",
		Text:            "how is she doing?",
		IsMention:       true,
	})

	select {
	case got := <-started:
		if got.sessionID != "conv-sparkquill" {
			t.Fatalf("session = %q, want the profile conversation conv-sparkquill", got.sessionID)
		}
		if got.req["agent_profile_id"] != "sparkquill" || got.req["query"] != "how is she doing?" {
			t.Fatalf("request = %#v, want the profile turn", got.req)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session never started")
	}

	manager.mu.RLock()
	active := manager.sessions[ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}.Key()]
	manager.mu.RUnlock()
	if active == nil || !active.profileTurn || active.SessionID != "conv-sparkquill" {
		t.Fatalf("active session = %+v, want a profile-turn session on conv-sparkquill", active)
	}
	// Later turns in that session are built the same way, never the generic
	// bot request with its workflow-runtime preamble.
	req := manager.turnRequestForActive(active, "and maths?", "user-1", "whatsapp", active.ThreadID)
	if req["agent_profile_id"] != "sparkquill" || req["query"] != "and maths?" {
		t.Fatalf("follow-up request = %#v, want the profile turn", req)
	}
	if text := manager.withBotRuntimeState(active, "and maths?"); text != "and maths?" {
		t.Fatalf("profile turn text = %q, want it untouched", text)
	}
}

// A message that names a workflow route never consults the default profile:
// @<slug> routing keeps working exactly as before.
func TestRoutedMessageBypassesTheDefaultProfile(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	manager.RegisterConnector(&testBotConnector{})
	manager.SetProfileTurnFunc(func(context.Context, string, BotIncomingMessage, ThreadID) (map[string]interface{}, string, bool, error) {
		t.Fatal("profile turn consulted for a routed message")
		return nil, "", false, nil
	})
	started := make(chan string, 1)
	manager.SetStartSessionFunc(func(_ context.Context, req map[string]interface{}, sessionID, _ string, _ func(*events.AgentEvent)) error {
		if req["preset_query_id"] != "wf-report" {
			t.Errorf("request preset = %v, want wf-report", req["preset_query_id"])
		}
		started <- sessionID
		return nil
	})

	manager.HandleIncomingMessage(BotIncomingMessage{
		Platform:        "whatsapp",
		UserID:          "phone",
		WorkspaceUserID: "user-1",
		ChannelID:       "dm",
		Text:            "run it",
		IsMention:       true,
		PresetWorkflow:  &ChannelRoute{WorkflowID: "wf-report", WorkspacePath: "Workflow/report"},
	})
	select {
	case sessionID := <-started:
		if sessionID == "" {
			t.Fatal("no session id")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session never started")
	}
}

// Without a default profile the builder says "not handled" and the message
// runs as the generic chat — AgentWorks' own behaviour, unchanged.
func TestNoDefaultProfileFallsBackToTheGenericChat(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	manager.RegisterConnector(&testBotConnector{})
	manager.SetProfileTurnFunc(func(context.Context, string, BotIncomingMessage, ThreadID) (map[string]interface{}, string, bool, error) {
		return nil, "", false, nil
	})
	started := make(chan map[string]interface{}, 1)
	manager.SetStartSessionFunc(func(_ context.Context, req map[string]interface{}, _ string, _ string, _ func(*events.AgentEvent)) error {
		started <- req
		return nil
	})
	manager.HandleIncomingMessage(BotIncomingMessage{Platform: "whatsapp", UserID: "phone", WorkspaceUserID: "user-1", ChannelID: "dm", Text: "hi", IsMention: true})
	select {
	case req := <-started:
		if _, isProfile := req["agent_profile_id"]; isProfile {
			t.Fatalf("generic request = %#v, want no agent profile", req)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session never started")
	}
}

// The default profile lives on the owner row: it survives the pairing UI's
// repeated re-claims and a service restart, and only the owner can set it.
func TestDefaultProfilePersistsWithTheOwner(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "whatsapp.db")
	svc := NewWhatsAppService(dbPath)
	if err := svc.openMetaStore(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetDefaultProfile("user-1", "sparkquill", "Chats/SparkQuill/inbox"); err == nil {
		t.Fatal("SetDefaultProfile before pairing succeeded, want an error")
	}
	if err := svc.ClaimOwnership("user-1", "user@example.com", "user"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetDefaultProfile("user-2", "sparkquill", ""); err == nil {
		t.Fatal("another user set the default profile, want an error")
	}
	if err := svc.SetDefaultProfile("user-1", "sparkquill", "/Chats/SparkQuill/inbox/"); err != nil {
		t.Fatal(err)
	}
	// The pairing screen polls the QR, which re-claims ownership every time.
	if err := svc.ClaimOwnership("user-1", "user@example.com", "user"); err != nil {
		t.Fatal(err)
	}
	if profileID, folder := svc.DefaultProfile(); profileID != "sparkquill" || folder != "Chats/SparkQuill/inbox" {
		t.Fatalf("default profile after re-claim = (%q, %q), want (sparkquill, Chats/SparkQuill/inbox)", profileID, folder)
	}

	reopened := NewWhatsAppService(dbPath)
	if err := reopened.openMetaStore(ctx); err != nil {
		t.Fatal(err)
	}
	reopened.loadOwner(ctx)
	if profileID, folder := reopened.DefaultProfile(); profileID != "sparkquill" || folder != "Chats/SparkQuill/inbox" {
		t.Fatalf("default profile after restart = (%q, %q), want it persisted", profileID, folder)
	}
	if err := reopened.SetDefaultProfile("user-1", "", "ignored"); err != nil {
		t.Fatal(err)
	}
	if profileID, folder := reopened.DefaultProfile(); profileID != "" || folder != "" {
		t.Fatalf("cleared default profile = (%q, %q), want empty", profileID, folder)
	}
}
