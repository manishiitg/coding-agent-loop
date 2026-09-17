package services

import (
	"context"
	"github.com/manishiitg/mcpagent/events"
	"testing"
)

func TestChannelCapabilitiesDriveStreamingForAnyPlatform(t *testing.T) {
	for _, platform := range []string{"slack", "discord", "telegram", "future-channel"} {
		t.Run(platform, func(t *testing.T) {
			for _, enabled := range []bool{false, true} {
				caps := ChannelCapabilities{StreamingReplies: enabled, MessageEdits: enabled, WorkflowProgress: true}
				c := &testBotConnector{capabilities: &caps}
				f := NewBotEventFilter(c, ThreadID{Platform: platform}, "session", "", "user")
				event := BotEventData{Type: "streaming_chunk", Data: &events.AgentEvent{Data: &events.StreamingChunkEvent{Content: "Hello", IsDelta: true}}}
				got := f.sendStreamingChunk(context.Background(), event)
				if got != enabled || (len(c.sent) > 0) != enabled {
					t.Fatalf("streaming enabled=%v sent=%v", enabled, got)
				}
			}
		})
	}
}

func TestProgressCleanupUsesCapabilitiesRatherThanPlatform(t *testing.T) {
	for _, platform := range []string{"discord", "telegram", "future-channel"} {
		t.Run(platform, func(t *testing.T) {
			c := &progressTestConnector{}
			f := NewBotEventFilter(c, ThreadID{Platform: platform}, "session", "", "user")
			f.sendProgressMessage(context.Background(), "Working…")
			f.completionReceived = true
			f.checkSessionDone("unified_completion")
			if len(c.sent) != 1 || len(c.deletes) != 1 {
				t.Fatal("capable channel did not clean up progress")
			}
		})
	}
	caps := ChannelCapabilities{}
	c := &progressTestConnector{testBotConnector: testBotConnector{capabilities: &caps}}
	f := NewBotEventFilter(c, ThreadID{Platform: "slack"}, "session", "", "user")
	f.sendProgressMessage(context.Background(), "Working…")
	if len(c.sent) != 0 {
		t.Fatal("disabled progress emitted a status message")
	}
}

func TestProductionChannelCapabilities(t *testing.T) {
	slack := (&SlackService{}).Capabilities()
	if !slack.Threads || !slack.Reactions || !slack.StreamingReplies || !slack.MessageEdits || !slack.MessageDeletion || !slack.ProgressUpdates || !slack.WorkflowProgress {
		t.Fatal("Slack lost a supported feature")
	}
	for _, caps := range []ChannelCapabilities{(&WhatsAppService{}).Capabilities(), (&WhatsAppServiceManager{}).Capabilities()} {
		if caps.Threads || caps.StreamingReplies || caps.ProgressUpdates || caps.MessageEdits || caps.MessageDeletion || caps.Reactions || !caps.WorkflowProgress {
			t.Fatal("WhatsApp advertised unsupported features")
		}
	}
}

func TestStreamingRequiresEditableMessages(t *testing.T) {
	caps := ChannelCapabilities{StreamingReplies: true}
	c := &testBotConnector{capabilities: &caps}
	f := NewBotEventFilter(c, ThreadID{Platform: "discord"}, "session", "", "user")
	event := BotEventData{Data: &events.AgentEvent{Data: &events.StreamingChunkEvent{Content: "Hello", IsDelta: true}}}
	if f.sendStreamingChunk(context.Background(), event) || len(c.sent) != 0 {
		t.Fatal("streaming attempted without edit capability")
	}
}

func TestEditableProgressWithoutDeletionBecomesTerminalStatus(t *testing.T) {
	caps := ChannelCapabilities{ProgressUpdates: true, MessageEdits: true}
	c := &progressTestConnector{testBotConnector: testBotConnector{capabilities: &caps}}
	f := NewBotEventFilter(c, ThreadID{Platform: "telegram"}, "session", "", "user")
	f.sendProgressMessage(context.Background(), "Working…")
	f.completionReceived = true
	f.checkSessionDone("unified_completion")
	if len(c.deletes) != 0 || len(c.updates) != 1 || c.updates[0] != "msg:Request finished." {
		t.Fatal("non-deleting channel did not finalize progress")
	}
}
