package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
	"github.com/manishiitg/mcpagent/events"
)

type steeringReplyConnector struct {
	testBotConnector
	replies chan string
}

func (c *steeringReplyConnector) SendThreadMessage(_ context.Context, _ ThreadID, text string) (string, error) {
	c.replies <- text
	return "reply", nil
}

func TestBotSteeringDeliveryFailureDoesNotBlockRetryP0(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &steeringReplyConnector{replies: make(chan string, 1)}
	manager.RegisterConnector(connector)
	inputs := make(chan string, 2)
	manager.SetFollowUpFunc(func(_ context.Context, req map[string]interface{}, _, _ string) error {
		text := req["query"].(string)
		inputs <- text
		if text == "first steer" {
			return errors.New("live input unavailable")
		}
		return nil
	})
	active := &activeBotSession{SessionID: "session-1", UserID: "user-1", Platform: "whatsapp",
		ThreadID: ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"},
		Status:   chathistory.BotSessionStatusRunning, LastActivity: time.Now()}
	send := func(text string) {
		manager.handleExistingSession(active, BotIncomingMessage{Platform: "whatsapp", ChannelID: "dm", Text: text}, false)
		select {
		case got := <-inputs:
			if got != text {
				t.Fatalf("input=%q", got)
			}
		case <-time.After(time.Second):
			t.Fatal("message was blocked")
		}
	}
	send("first steer")
	select {
	case reply := <-connector.replies:
		if !strings.Contains(reply, "Couldn't deliver your message") || !strings.Contains(reply, "live input unavailable") {
			t.Fatalf("delivery error hidden: %q", reply)
		}
	case <-time.After(time.Second):
		t.Fatal("delivery failed silently")
	}
	send("try again")
}

func TestBotTerminalErrorReleasesBusyAndBlockingStateP0(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	filter := NewBotEventFilter(connector, ThreadID{Platform: "whatsapp"}, "session-1", "", "user-1")
	active := &activeBotSession{Status: chathistory.BotSessionStatusRunning, eventFilter: filter,
		awaitingUserInput: true, blockingEventType: "blocking_human_feedback", blockingRequestID: "old-prompt"}
	filter.awaitingInput = true
	filter.pendingDelegations = 1
	done := 0
	filter.SetSessionDoneCallback(func() { done++; manager.markBotBuilderDone(active) })
	failure := BotEventData{Type: "unified_completion", Data: &events.AgentEvent{HierarchyLevel: 0,
		Data: &events.UnifiedCompletionEvent{Status: "error"}}}
	filter.processEvent(context.Background(), failure)
	filter.processEvent(context.Background(), failure)
	if done != 1 || !active.builderDone || active.Status != chathistory.BotSessionStatusFailed {
		t.Fatalf("failed turn not released once: done=%d state=%+v", done, active)
	}
	if active.awaitingUserInput || active.blockingEventType != "" || active.blockingRequestID != "" {
		t.Fatal("failed turn left a stale input prompt")
	}
	filter.ResetForNewTurn()
	if filter.SessionFailed() {
		t.Fatal("failure leaked into next turn")
	}
}

func TestBotNestedErrorDoesNotFinishParentP0(t *testing.T) {
	filter := NewBotEventFilter(nil, ThreadID{Platform: "whatsapp"}, "session-1", "", "user-1")
	filter.baseHierarchySet, filter.baseHierarchy = true, 0
	done := 0
	filter.SetSessionDoneCallback(func() { done++ })
	filter.processEvent(context.Background(), BotEventData{Type: "unified_completion", Data: &events.AgentEvent{HierarchyLevel: 1, Data: &events.UnifiedCompletionEvent{Status: "error"}}})
	if done != 0 || filter.SessionFailed() {
		t.Fatal("nested failure finished the main builder")
	}
}

func TestBotErrorAndTerminalCompletionNotifyOnceP0(t *testing.T) {
	for _, errorFirst := range []bool{true, false} {
		connector := &testBotConnector{}
		filter := NewBotEventFilter(connector, ThreadID{Platform: "whatsapp"}, "session-1", "", "user-1")
		done := 0
		filter.SetSessionDoneCallback(func() {
			if len(connector.sent) != 1 {
				t.Error("completion fired before reporting failure")
			}
			done++
		})
		errorEvent := BotEventData{Type: "conversation_error", Data: &events.AgentEvent{Data: &events.ConversationErrorEvent{Error: "Muse returned no assistant text"}}}
		terminalEvent := BotEventData{Type: "unified_completion", Data: &events.AgentEvent{Data: &events.UnifiedCompletionEvent{Status: "error", Error: "Muse returned no assistant text"}}}
		sequence := []BotEventData{terminalEvent, errorEvent, terminalEvent}
		if errorFirst {
			sequence = []BotEventData{errorEvent, terminalEvent, terminalEvent}
		}
		for _, event := range sequence {
			filter.processEvent(context.Background(), event)
		}
		if len(connector.sent) != 1 || done != 1 {
			t.Fatalf("errorFirst=%v messages=%v done=%d", errorFirst, connector.sent, done)
		}
	}
}
