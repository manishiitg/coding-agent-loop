package services

import (
	"context"
	"strings"
	"testing"
	"time"

	orchestrator_events "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/events"
	"github.com/manishiitg/mcpagent/events"
)

func TestSuppressWorkflowRuntimeChatterDefaultsForSlackAndWhatsApp(t *testing.T) {
	event := BotEventData{
		Type: "llm_generation_end",
		Data: &events.AgentEvent{
			Data: &events.LLMGenerationEndEvent{
				BaseEventData: events.BaseEventData{
					Metadata: map[string]interface{}{
						"current_step_id": "step-1",
					},
				},
				Content: "internal step completion",
			},
		},
	}

	for _, platform := range []string{"slack", "whatsapp"} {
		filter := NewBotEventFilter(nil, ThreadID{Platform: platform}, "session-1", "", "user-1")
		if !filter.suppressWorkflowRuntimeChatter(event) {
			t.Fatalf("expected workflow runtime chatter to be suppressed for %s", platform)
		}
	}
}

func TestSuppressWorkflowRuntimeChatterAllowsFullDetailsOptIn(t *testing.T) {
	event := BotEventData{
		Type: "llm_generation_end",
		Data: &events.AgentEvent{
			Data: &events.LLMGenerationEndEvent{
				BaseEventData: events.BaseEventData{
					Metadata: map[string]interface{}{
						"current_step_id": "step-1",
					},
				},
				Content: "internal step completion",
			},
		},
	}

	filter := NewBotEventFilter(nil, ThreadID{Platform: "slack"}, "session-1", "", "user-1")
	filter.SetSendFullDetails(true)
	if filter.suppressWorkflowRuntimeChatter(event) {
		t.Fatal("expected full-details opt-in to allow workflow runtime chatter")
	}
}

func TestShouldSendSyntheticFinalAllowsDifferentFinalText(t *testing.T) {
	filter := NewBotEventFilter(nil, ThreadID{Platform: "whatsapp"}, "session-1", "", "user-1")
	filter.MarkMainTextSent("The RCA investigation is complete. Here's a summary of what was found.")

	if !filter.ShouldSendSyntheticFinal("Run completed successfully. Here's the plain-English summary.") {
		t.Fatal("expected different synthetic final text to be allowed")
	}
}

func TestShouldSendSyntheticFinalSuppressesDuplicateText(t *testing.T) {
	filter := NewBotEventFilter(nil, ThreadID{Platform: "whatsapp"}, "session-1", "", "user-1")
	filter.MarkMainTextSent("Run completed successfully. Here's the plain-English summary.")

	if filter.ShouldSendSyntheticFinal("  Run completed successfully. Here's the plain-English summary.\n") {
		t.Fatal("expected duplicate synthetic final text to be suppressed")
	}
}

func TestShouldSendSyntheticFinalSuppressesMarkdownEquivalentText(t *testing.T) {
	filter := NewBotEventFilter(nil, ThreadID{Platform: "whatsapp"}, "session-1", "", "user-1")
	filter.MarkMainTextSent("Step update (Sentry Latency Evidence): completed - found severe /sessionhub bottleneck.")

	if filter.ShouldSendSyntheticFinal("Step update (Sentry Latency Evidence): completed - found severe `/sessionhub` bottleneck.") {
		t.Fatal("expected markdown-equivalent synthetic final text to be suppressed")
	}
}

// A normal turn's llm_generation_end and its own trailing unified_completion
// carry the same reply — unified_completion must not re-send it just because
// its own formatting wraps the text in "**Result:**\n" first. Caught live:
// a genuine duplicate, the same reply twice a second apart, the second one
// with a "*Result:*" label — formatUnifiedCompletion's wrapper made an
// identical reply look new to the dedup check.
func TestUnifiedCompletionDoesNotResendGenerationEndsReply(t *testing.T) {
	const reply = "Looks like your message got cut off. What would you like me to do?"
	connector := &testBotConnector{}
	filter := NewBotEventFilter(connector, ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}, "session-1", "", "user-1")

	if !filter.processEvent(context.Background(), BotEventData{
		Type: "llm_generation_end",
		Data: &events.AgentEvent{Data: &events.LLMGenerationEndEvent{Content: reply}},
	}) {
		t.Fatal("llm_generation_end was not sent")
	}
	if filter.processEvent(context.Background(), BotEventData{
		Type: "unified_completion",
		Data: &events.AgentEvent{Data: &events.UnifiedCompletionEvent{FinalResult: reply, Status: "completed"}},
	}) {
		t.Fatal("unified_completion re-sent the same reply llm_generation_end already sent")
	}
	if len(connector.sent) != 1 {
		t.Fatalf("sent = %v, want exactly one message", connector.sent)
	}

	// A genuinely different final result (the progressive poller having sent
	// an earlier, different reply for this same turn) still goes out.
	filter2 := NewBotEventFilter(connector, ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}, "session-2", "", "user-1")
	if !filter2.SendProgressiveText(context.Background(), "Still working on it.") {
		t.Fatal("progressive text was not sent")
	}
	if !filter2.processEvent(context.Background(), BotEventData{
		Type: "unified_completion",
		Data: &events.AgentEvent{Data: &events.UnifiedCompletionEvent{FinalResult: reply, Status: "completed"}},
	}) {
		t.Fatal("a genuinely different final result was suppressed")
	}
}

func TestSyntheticFinalSuppressedWhileMainTextSendInFlight(t *testing.T) {
	const msg = "Daily latency report is running - pulling CloudWatch data for both prod and dev."
	sendStarted := make(chan struct{})
	releaseSend := make(chan struct{})
	connector := &testBotConnector{sendStarted: sendStarted, releaseSend: releaseSend}
	filter := NewBotEventFilter(connector, ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}, "session-1", "", "user-1")

	done := make(chan bool, 1)
	go func() {
		done <- filter.processEvent(context.Background(), BotEventData{
			Type: "llm_generation_end",
			Data: &events.AgentEvent{
				HierarchyLevel: 3,
				Data: &events.LLMGenerationEndEvent{
					Content: msg,
				},
			},
		})
	}()

	select {
	case <-sendStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for connector send to start")
	}

	if filter.ShouldSendSyntheticFinal(" " + msg + "\n") {
		t.Fatal("expected duplicate synthetic final to be suppressed while main text send is in flight")
	}

	close(releaseSend)
	select {
	case sent := <-done:
		if !sent {
			t.Fatal("expected processEvent to report a sent message")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event processing to finish")
	}
}

func TestFullDetailsStepStartIncludesAgentUserMessage(t *testing.T) {
	filter := NewBotEventFilter(nil, ThreadID{Platform: "whatsapp"}, "session-1", "", "user-1")
	filter.SetSendFullDetails(true)

	msg := filter.formatOrchestratorAgentStart(BotEventData{
		Type: "orchestrator_agent_start",
		Data: &events.AgentEvent{
			Data: &orchestrator_events.OrchestratorAgentStartEvent{
				AgentType:   "todo_task_execution_step",
				AgentName:   "Step: Gather evidence",
				UserMessage: "Investigate the latest production latency RCA inputs.",
				InputData: map[string]string{
					"group_name": "production",
				},
			},
		},
	})

	if !strings.Contains(msg, "Step started (Gather evidence): running now [production].") {
		t.Fatalf("step start missing base message: %q", msg)
	}
	if !strings.Contains(msg, "User message sent to agent:") ||
		!strings.Contains(msg, "Investigate the latest production latency RCA inputs.") {
		t.Fatalf("step start missing user message: %q", msg)
	}
}

func TestConciseStepStartOmitsAgentUserMessage(t *testing.T) {
	filter := NewBotEventFilter(nil, ThreadID{Platform: "whatsapp"}, "session-1", "", "user-1")

	msg := filter.formatOrchestratorAgentStart(BotEventData{
		Type: "orchestrator_agent_start",
		Data: &events.AgentEvent{
			Data: &orchestrator_events.OrchestratorAgentStartEvent{
				AgentType:   "todo_task_execution_step",
				AgentName:   "Step: Gather evidence",
				UserMessage: "Investigate the latest production latency RCA inputs.",
			},
		},
	})

	if strings.Contains(msg, "User message sent to agent:") ||
		strings.Contains(msg, "Investigate the latest production latency RCA inputs.") {
		t.Fatalf("concise step start should omit user message: %q", msg)
	}
}
