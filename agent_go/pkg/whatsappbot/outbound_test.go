package whatsappbot

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestRememberOutboundSuppressesSelfChatEcho(t *testing.T) {
	connector := New(Config{})
	id := types.MessageID("bot-response-id")
	connector.rememberOutbound(id)
	if !connector.seen.Seen(id) {
		t.Fatal("outbound message ID was not present in the inbound dedupe window")
	}
}

func TestRememberOutboundIgnoresEmptyID(t *testing.T) {
	connector := New(Config{})
	connector.rememberOutbound("")
	if connector.seen.Seen("") {
		t.Fatal("empty outbound ID polluted the inbound dedupe window")
	}
}

func TestPendingOutboundSuppressesEchoBeforeMessageIDIsKnown(t *testing.T) {
	connector := New(Config{})
	chat := types.NewJID("15551234567", types.DefaultUserServer)
	connector.expectOutboundEcho(chat, "Choose a workflow")

	if !connector.consumeOutboundEcho(chat, "Choose a workflow") {
		t.Fatal("outbound echo was not suppressed")
	}
	if connector.consumeOutboundEcho(chat, "Choose a workflow") {
		t.Fatal("a later user message with the same text was suppressed")
	}
}

func TestPendingOutboundEchoesAreCountedPerChat(t *testing.T) {
	connector := New(Config{})
	chat := types.NewJID("15551234567", types.DefaultUserServer)
	other := types.NewJID("15557654321", types.DefaultUserServer)
	connector.expectOutboundEcho(chat, "same")
	connector.expectOutboundEcho(chat, "same")

	if connector.consumeOutboundEcho(other, "same") {
		t.Fatal("suppressed another chat")
	}
	if !connector.consumeOutboundEcho(chat, "same") || !connector.consumeOutboundEcho(chat, "same") {
		t.Fatal("did not suppress both expected echoes")
	}
	if connector.consumeOutboundEcho(chat, "same") {
		t.Fatal("suppressed more echoes than were sent")
	}
}
