package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/manishiitg/mcpagent/events"
)

type replyBoundaryConnector struct {
	testBotConnector
	updates []string
	removed []string
}

func (c *replyBoundaryConnector) SendThreadMessage(_ context.Context, _ ThreadID, text string) (string, error) {
	c.sent = append(c.sent, text)
	return fmt.Sprintf("reply-%d", len(c.sent)), nil
}
func (c *replyBoundaryConnector) UpdateMessage(_ context.Context, _ ThreadID, id, text string) error {
	c.updates = append(c.updates, id+":"+text)
	return nil
}
func (c *replyBoundaryConnector) RemoveReaction(_ context.Context, channel, ts, emoji string) error {
	c.removed = append(c.removed, channel+":"+ts+":"+emoji)
	return nil
}
func replyChunk(text string, delta bool) BotEventData {
	return BotEventData{Type: "streaming_chunk", Data: &events.AgentEvent{Data: &events.StreamingChunkEvent{Content: text, IsDelta: delta}}}
}
func TestAcceptedFollowUpGetsSeparateEditableReply(t *testing.T) {
	for _, platform := range []string{"slack", "discord", "telegram"} {
		t.Run(platform, func(t *testing.T) {
			ctx := context.Background()
			c := &replyBoundaryConnector{}
			f := NewBotEventFilter(c, ThreadID{Platform: platform}, "session", "", "user")
			f.processEvent(ctx, replyChunk("First answer.", false))
			f.pendingDelegations = 1
			f.processEvent(ctx, BotEventData{Type: "user_message"})
			if f.pendingDelegations != 1 || f.sessionDone || f.completionReceived {
				t.Fatal("reply boundary changed agent turn tracking")
			}
			f.processEvent(ctx, replyChunk("First answer. Second answer", false))
			f.lastStreamingUpdate = time.Time{}
			f.processEvent(ctx, replyChunk("First answer. Second answer continues", false))
			f.flushStreamingMessage(ctx, "Second answer final")
			if len(c.sent) != 2 || c.sent[0] != "First answer." || c.sent[1] != " Second answer" {
				t.Fatalf("separate replies = %#v", c.sent)
			}
			if len(c.updates) != 2 || c.updates[0] != "reply-2: Second answer continues" || c.updates[1] != "reply-2:Second answer final" {
				t.Fatalf("follow-up stream repeated old text: %#v", c.updates)
			}
			for _, update := range c.updates {
				if len(update) < 8 || update[:8] != "reply-2:" {
					t.Fatalf("edited older reply: %q", update)
				}
			}
		})
	}
}
func TestFollowUpReplySupportsDeltaChunksAndRepeatedMessages(t *testing.T) {
	c := &replyBoundaryConnector{}
	f := NewBotEventFilter(c, ThreadID{Platform: "slack"}, "session", "", "user")
	ctx := context.Background()
	f.processEvent(ctx, replyChunk("One", true))
	f.processEvent(ctx, BotEventData{Type: "user_message"})
	f.processEvent(ctx, replyChunk(" two", true))
	f.processEvent(ctx, BotEventData{Type: "user_message"})
	f.processEvent(ctx, replyChunk("One two three", false))
	if len(c.sent) != 3 || c.sent[2] != " three" {
		t.Fatalf("replies = %#v", c.sent)
	}
	f.ResetForNewTurn()
	if f.streamingReplyPrefix != "" {
		t.Fatal("prior turn prefix retained")
	}
}
func TestCompletionClearsEveryAcceptedMessageReaction(t *testing.T) {
	c := &replyBoundaryConnector{testBotConnector: testBotConnector{name: "slack"}}
	m := &BotConversationManager{connectors: map[string]BotConnector{"slack": c}}
	m.clearBotMessageReactions(context.Background(), []ThreadID{
		{Platform: "slack", ChannelID: "C1", ThreadTS: "original"},
		{Platform: "slack", ChannelID: "C1", ThreadTS: "follow-up"},
	})
	if len(c.removed) != 4 {
		t.Fatalf("reaction cleanup = %#v", c.removed)
	}
	if c.removed[2] != "C1:follow-up:eyes" {
		t.Fatalf("follow-up acknowledgement left behind: %#v", c.removed)
	}
}
