package server

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	unifiedevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/llm"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// liveInputDurableWatchTimeout caps the server-side durability watch.
// The adapter's own env-tuned budget (codex default 180s, max 300s)
// bounds the actual wait; this only guards against a pathological
// hang beneath it.
const liveInputDurableWatchTimeout = 6 * time.Minute

// watchLiveInputDurable starts the durability half of the two-stage
// delivery receipt after a fast pane ack. It returns at once — the HTTP
// response must not wait behind it — and emits a live_input_confirmed
// event (matched by message_id) when the CLI's own durable record
// confirms the send. Providers without SupportsDurableAck are ignored.
func (api *StreamingAPI) watchLiveInputDurable(sessionID, provider, messageID, message string) {
	provider = strings.TrimSpace(provider)
	if api == nil || api.eventStore == nil || sessionID == "" || messageID == "" || strings.TrimSpace(message) == "" {
		return
	}
	contract, ok := llmproviders.GetCodingAgentProviderContract(llmproviders.Provider(provider), "")
	if !ok || !contract.SupportsDurableAck {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), liveInputDurableWatchTimeout)
		defer cancel()
		ack, err := api.awaitLiveInputDurable(ctx, llmproviders.Provider(provider), sessionID, message)
		outcome := string(llmtypes.DurableAckConfirmed)
		proof := ""
		var latencyMs int64
		if err != nil {
			outcome = string(llmtypes.DurableAckFailed)
		} else {
			outcome = string(ack.Outcome)
			proof = ack.ProofSource
			latencyMs = ack.Latency.Milliseconds()
		}
		// Background audit: the fast ack already told the user this
		// send succeeded, so anything but confirmed is provider
		// drift worth logging loud — unflushed is held-not-failed,
		// failed-after-fast-ack is a silent drop.
		if outcome != string(llmtypes.DurableAckConfirmed) {
			log.Printf("[LIVE-INPUT-DURABLE] session=%s provider=%s message=%s outcome=%s err=%v",
				sessionID, provider, messageID, outcome, err)
		}
		api.recordLiveInputConfirmed(sessionID, messageID, outcome, proof, provider, latencyMs)
	}()
}

// awaitLiveInputDurable dispatches to the provider's durable-ack entry
// point. internalDurableAckHandler lets routing tests observe the watch
// without a real CLI; production falls through to the typed adapter
// await, which today exists only for providers with the contract flag.
func (api *StreamingAPI) awaitLiveInputDurable(ctx context.Context, provider llmproviders.Provider, sessionID, message string) (llmtypes.DurableAck, error) {
	if api != nil && api.internalDurableAckHandler != nil {
		return api.internalDurableAckHandler(ctx, provider, sessionID, message)
	}
	switch provider {
	case llmproviders.ProviderClaudeCode:
		return llm.AwaitClaudeInputDurable(ctx, sessionID, message, 0)
	case llmproviders.ProviderCursorCLI:
		return llm.AwaitCursorInputDurable(ctx, sessionID, message, 0)
	case llmproviders.ProviderCodexCLI:
		return llm.AwaitCodexInputDurable(ctx, sessionID, message, 0)
	case llmproviders.ProviderPiCLI:
		return llm.AwaitPiInputDurable(ctx, sessionID, message, 0)
	case llmproviders.ProviderMuseCLI:
		return llm.AwaitMuseInputDurable(ctx, sessionID, message, 0)
	default:
		return llmtypes.DurableAck{Outcome: llmtypes.DurableAckFailed}, nil
	}
}

// recordLiveInputConfirmed persists the durability verdict as a session
// event so it reaches chat over SSE and survives reconnect replay like
// the user_message row it upgrades. The event ID derives
// deterministically from the message so redelivery is idempotent.
func (api *StreamingAPI) recordLiveInputConfirmed(sessionID, messageID, outcome, proofSource, provider string, latencyMs int64) {
	if api == nil || api.eventStore == nil || sessionID == "" || messageID == "" {
		return
	}
	eventData := unifiedevents.NewLiveInputConfirmedEvent(messageID, outcome, proofSource, provider, latencyMs)
	agentEvent := unifiedevents.NewAgentEvent(eventData)
	agentEvent.SessionID = sessionID
	agentEvent.Component = "coding_agent_live_input"
	event := events.Event{
		ID:        messageID + ":confirmed",
		Type:      string(unifiedevents.LiveInputConfirmed),
		Timestamp: time.Now(),
		Data:      agentEvent,
		SessionID: sessionID,
	}
	api.eventStore.AddEvent(sessionID, event)
}
