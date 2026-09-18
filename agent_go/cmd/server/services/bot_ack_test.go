package services

import (
	"context"
	"testing"
)

type ackTestConnector struct {
	testBotConnector
	acks []string
}

func (c *ackTestConnector) AddReaction(_ context.Context, channel, ts, emoji string) error {
	c.acks = append(c.acks, channel+":"+ts+":"+emoji)
	return nil
}

func TestBotAcknowledgesAcceptedMessagesOnly(t *testing.T) {
	m := NewBotConversationManager(nil, "", "")
	c := &ackTestConnector{testBotConnector: testBotConnector{name: "slack", supportsThreads: true}}
	m.RegisterConnector(c)
	msg := BotIncomingMessage{Platform: "slack", ChannelID: "C1", ThreadTS: "unrelated-thread", MessageTS: "message-1", Text: "talking to a colleague", PresetWorkflow: &ChannelRoute{WorkflowID: "wf", WorkspacePath: "Workflow/wf"}}
	m.HandleIncomingMessage(msg)
	if len(c.acks) != 0 {
		t.Fatal("ordinary thread reply received an acknowledgement")
	}
	active := &activeBotSession{Platform: "slack", ThreadID: ThreadID{Platform: "slack", ChannelID: "C1", ThreadTS: "bot-thread"}}
	if m.startFollowUpTurn(active, msg, "session", "user", "threaded") || len(c.acks) != 0 {
		t.Fatal("unavailable follow-up received an acknowledgement")
	}
	done := make(chan struct{})
	m.SetFollowUpFunc(func(context.Context, map[string]interface{}, string, string) error { close(done); return nil })
	if !m.startFollowUpTurn(active, msg, "session", "user", "threaded") {
		t.Fatal("accepted follow-up failed")
	}
	<-done
	if len(c.acks) != 1 || c.acks[0] != "C1:message-1:eyes" {
		t.Fatalf("accepted message acknowledgement = %v", c.acks)
	}
}
