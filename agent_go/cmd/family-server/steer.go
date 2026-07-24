package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
)

// activeTurn tracks the ONE currently in-flight agent turn against a given
// conversation, so a concurrent request for the SAME conversation can inject a
// message into it live (steer) instead of waiting for it to finish and
// starting a wholly separate turn afterward. Family-server already
// serializes every turn process-wide via agentTurnMu (the shared MCP bridge
// uses process-global env vars), so there is realistically at most one entry
// here at a time — this is just the bookkeeping needed to find it.
//
// Deliberately NOT mcpagent's own Agent.TurnInFlight(): that flag is only set
// by mcpagent's ContinueConversation, which agentsession.Session.Ask never
// calls (it goes straight to AskWithHistory) — so it would always read false
// on this call path. This registry is the actual source of truth.
var (
	activeTurnMu sync.Mutex
	activeTurn   *struct {
		conversationID string
		agent          *mcpagent.Agent
	}
)

// registerActiveTurn records the agent driving a turn that's about to start,
// so a concurrent steer attempt for the same conversation id can find it.
// Call right before the blocking Ask/turn call; pair with a deferred
// clearActiveTurn.
func registerActiveTurn(conversationID string, agent *mcpagent.Agent) {
	activeTurnMu.Lock()
	activeTurn = &struct {
		conversationID string
		agent          *mcpagent.Agent
	}{conversationID, agent}
	activeTurnMu.Unlock()
}

// clearActiveTurn removes the registration once a turn completes (success or
// error) — always via `defer`, right after registerActiveTurn.
func clearActiveTurn() {
	activeTurnMu.Lock()
	activeTurn = nil
	activeTurnMu.Unlock()
}

// trySteer attempts to inject message into the turn currently in flight for
// conversationID — actually redirecting what Quill is doing mid-turn, rather
// than waiting for it to finish. Returns true if delivered (the caller should
// return an immediate ack; the message will be reflected in whichever turn's
// blocking call is already running). Returns false if the caller should fall
// back to its normal behavior: no turn is in flight, it's a different
// conversation, the provider doesn't support live input (steering is only
// possible for the persistent-tmux CLI providers), or delivery itself
// failed/timed out.
//
// Bounded by its own short timeout, independent of the turn's own much longer
// turnTimeout: DeliverUserMessage blocks (doesn't error) if it lands while a
// session is still cold-starting its underlying tmux CLI, so a short deadline
// here lets a mistimed steer degrade quickly to "fall through" instead of
// tying up the request for a long time. No dedicated mutex is needed around
// the delivery call itself — multi-llm-provider-go's tmuxinput broker already
// serializes input delivery per tmux session with one worker goroutine, so
// rapid successive steers queue and run in submission order safely.
func trySteer(ctx context.Context, conversationID, message string) bool {
	message = strings.TrimSpace(message)
	if message == "" {
		return false
	}
	activeTurnMu.Lock()
	at := activeTurn
	activeTurnMu.Unlock()
	if at == nil || at.conversationID != conversationID {
		return false
	}
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := at.agent.DeliverUserMessage(sctx, mcpagent.UserMessageDeliveryRequest{
		SessionID: conversationID,
		Message:   message,
		Intent:    mcpagent.UserMessageDeliveryIntentAuto,
	})
	return err == nil
}

// handleParentSteer serves POST /api/parent/steer — the browser's fast-path
// attempt to redirect the parent conversation's turn while it's still
// running, instead of only ever queuing a follow-up for after. Deliberately a
// narrow, separate endpoint from /api/parent/message: it ONLY ever tries to
// steer and returns quickly, it never falls through to starting its own
// blocking turn. If steering isn't possible right now, the frontend's
// existing client-side queue (unchanged) is the fallback — it sends the
// message as an ordinary new turn once the current one finishes.
func handleParentSteer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ConversationID string `json:"conversation_id"`
		Message        string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]bool{"steered": false})
		return
	}
	convID := strings.TrimSpace(req.ConversationID)
	message := strings.TrimSpace(req.Message)
	if convID == "" || message == "" {
		writeJSON(w, http.StatusOK, map[string]bool{"steered": false})
		return
	}
	ok := trySteer(r.Context(), convID, message)
	if ok {
		// Durably record the message right away — it's now part of the live
		// turn's context, but that turn's own eventual completion won't know
		// about it unless it reloads the freshest history first (see
		// persistConversationReply in chat.go), which this write makes possible.
		appendUserMessageToConversation("parent", convID, message)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"steered": ok})
}
