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
