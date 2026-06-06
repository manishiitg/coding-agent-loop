package services

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
)

func TestWhatsAppUserNotificationSkipsWhenUnpaired(t *testing.T) {
	svc := &WhatsAppService{}

	msgID, err := svc.SendUserNotification(context.Background(), "hello", "", &NotificationDestination{UserID: "user-1"})
	if err != nil {
		t.Fatalf("SendUserNotification returned error for unpaired WhatsApp service: %v", err)
	}
	if msgID != "" {
		t.Fatalf("SendUserNotification msgID = %q, want empty", msgID)
	}
}

func TestParseWhatsAppWorkflowCommandRecognizesBotSessionControls(t *testing.T) {
	tests := []struct {
		text    string
		wantCmd string
		wantArg string
	}{
		{text: "@resume", wantCmd: "resume"},
		{text: "@resume 2", wantCmd: "resume", wantArg: "2"},
		{text: "@continue abc123", wantCmd: "continue", wantArg: "abc123"},
		{text: "@sessions", wantCmd: "sessions"},
		{text: "@runs", wantCmd: "runs"},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			cmd, arg, ok := parseWhatsAppWorkflowCommand(tt.text)
			if !ok {
				t.Fatalf("parseWhatsAppWorkflowCommand(%q) ok=false", tt.text)
			}
			if cmd != tt.wantCmd || arg != tt.wantArg {
				t.Fatalf("parseWhatsAppWorkflowCommand(%q) = (%q, %q), want (%q, %q)", tt.text, cmd, arg, tt.wantCmd, tt.wantArg)
			}
		})
	}
}

func TestWhatsAppBotSessionControlsForwardToBotManager(t *testing.T) {
	var got []BotIncomingMessage
	svc := &WhatsAppService{
		messageHandler: func(msg BotIncomingMessage) {
			got = append(got, msg)
		},
	}
	owner := &WhatsAppOwner{UserID: "user-1", Email: "user@example.com"}
	info := types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:   types.JID{User: "15551234567", Server: types.DefaultUserServer},
			Sender: types.JID{User: "15551234567", Server: types.DefaultUserServer},
		},
		ID:        "msg-1",
		Timestamp: time.Now(),
		PushName:  "User",
	}

	if !svc.handleWorkflowCommand(context.Background(), "@resume 2", "15551234567@s.whatsapp.net", owner, info) {
		t.Fatal("expected @resume to be handled")
	}
	if !svc.handleWorkflowCommand(context.Background(), "@sessions", "15551234567@s.whatsapp.net", owner, info) {
		t.Fatal("expected @sessions to be handled")
	}
	if !svc.handleWorkflowCommand(context.Background(), "@status", "15551234567@s.whatsapp.net", owner, info) {
		t.Fatal("expected @status to be handled")
	}

	if len(got) != 3 {
		t.Fatalf("forwarded messages = %d, want 3", len(got))
	}
	if got[0].Text != "@resume 2" {
		t.Fatalf("first forwarded text = %q, want @resume 2", got[0].Text)
	}
	if got[1].Text != "@status" {
		t.Fatalf("second forwarded text = %q, want @status", got[1].Text)
	}
	if got[2].Text != "@status" {
		t.Fatalf("third forwarded text = %q, want @status", got[2].Text)
	}
	if got[0].WorkspaceUserID != "user-1" || got[0].UserEmail != "user@example.com" {
		t.Fatalf("forwarded owner fields = userID %q email %q", got[0].WorkspaceUserID, got[0].UserEmail)
	}
}
