package server

import (
	"testing"
	"time"
)

func TestClaimChiefOfStaffChatAllowsOneChatPlusOneSchedule(t *testing.T) {
	api := &StreamingAPI{activeSessions: make(map[string]*ActiveSessionInfo)}
	userID := "user-1"

	if blocking := api.claimChiefOfStaffChatSession("chat-1", userID, "hello", "manual"); blocking != nil {
		t.Fatalf("first chat unexpectedly blocked by %s", blocking.SessionID)
	}
	api.activeSessions["schedule-cron--org-pulse_1"] = &ActiveSessionInfo{
		SessionID:    "schedule-cron--org-pulse_1",
		AgentMode:    "multi-agent",
		Status:       "running",
		UserID:       userID,
		TriggeredBy:  "cron",
		LastActivity: time.Now(),
	}

	if blocking := api.claimChiefOfStaffChatSession("chat-1", userID, "follow up", "manual"); blocking != nil {
		t.Fatalf("same chat follow-up unexpectedly blocked by %s", blocking.SessionID)
	}
	if blocking := api.claimChiefOfStaffChatSession("chat-2", userID, "second chat", "manual"); blocking == nil || blocking.SessionID != "chat-1" {
		t.Fatalf("second chat blocker = %#v, want chat-1", blocking)
	}
}

func TestChiefOfStaffChatClaimIgnoresCompletedUnretainedChat(t *testing.T) {
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{
		"old-chat": {
			SessionID: "old-chat",
			AgentMode: "multi-agent",
			Status:    "completed",
			UserID:    "user-1",
		},
	}}

	if blocking := api.claimChiefOfStaffChatSession("new-chat", "user-1", "hello", "manual"); blocking != nil {
		t.Fatalf("completed chat unexpectedly blocked new chat: %#v", blocking)
	}
}
