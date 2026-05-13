package services

import "testing"

func TestWhatsAppManagedChannelIDRoundTrip(t *testing.T) {
	encoded := encodeWhatsAppManagedChannelID("user-1", "12345@s.whatsapp.net")
	userID, chatJID, ok := decodeWhatsAppManagedChannelID(encoded)
	if !ok {
		t.Fatal("expected encoded channel ID to decode")
	}
	if userID != "user-1" {
		t.Fatalf("expected user-1, got %q", userID)
	}
	if chatJID != "12345@s.whatsapp.net" {
		t.Fatalf("expected raw chat JID, got %q", chatJID)
	}
}

func TestWhatsAppManagerNamespacesIncomingMessage(t *testing.T) {
	manager := NewWhatsAppServiceManager(t.TempDir())
	var got BotIncomingMessage
	manager.SetMessageHandler(func(msg BotIncomingMessage) {
		got = msg
	})

	svc := NewWhatsAppService("")
	manager.configureService("user-1", svc)
	svc.messageHandler(BotIncomingMessage{
		Platform:  "whatsapp",
		ChannelID: "12345@s.whatsapp.net",
		ThreadTS:  "12345@s.whatsapp.net",
		Text:      "hello",
	})

	if got.ChannelID != "user-1|12345@s.whatsapp.net" {
		t.Fatalf("expected namespaced channel ID, got %q", got.ChannelID)
	}
	if got.ThreadTS != got.ChannelID {
		t.Fatalf("expected thread TS to follow namespaced channel ID, got %q", got.ThreadTS)
	}
}
