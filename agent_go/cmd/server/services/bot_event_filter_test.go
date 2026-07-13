package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/mcpagent/events"
	orchestrator_events "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/events"
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
