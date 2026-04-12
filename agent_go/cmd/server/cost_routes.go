package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	unifiedevents "github.com/manishiitg/mcpagent/events"

	"mcp-agent-builder-go/agent_go/pkg/costledger"
)

// costObserver implements mcpagent's AgentEventListener. It listens for
// token_usage events on every agent run (main or sub-agent) and appends one
// entry per event to the global cost ledger. This is how the cost dashboard
// gets populated — there is no per-session event persistence anymore.
type costObserver struct {
	ledger    *costledger.Ledger
	sessionID string
	userID    string
	agentMode string
}

func newCostObserver(ledger *costledger.Ledger, sessionID, userID, agentMode string) *costObserver {
	return &costObserver{
		ledger:    ledger,
		sessionID: sessionID,
		userID:    userID,
		agentMode: agentMode,
	}
}

func (o *costObserver) Name() string { return "costObserver" }

func (o *costObserver) HandleEvent(_ context.Context, event *unifiedevents.AgentEvent) error {
	if o == nil || o.ledger == nil || event == nil {
		return nil
	}
	if event.Type != unifiedevents.TokenUsage {
		return nil
	}
	tu, ok := event.Data.(*unifiedevents.TokenUsageEvent)
	if !ok || tu == nil {
		return nil
	}

	cacheRead, cacheWrite := extractCacheTokens(tu.GenerationInfo)
	entry := costledger.Entry{
		Timestamp:        event.Timestamp,
		SessionID:        o.sessionID,
		UserID:           o.userID,
		AgentMode:        o.agentMode,
		Component:        event.Component,
		CorrelationID:    event.CorrelationID,
		Provider:         tu.Provider,
		ModelID:          tu.ModelID,
		PromptTokens:     tu.PromptTokens,
		CompletionTokens: tu.CompletionTokens,
		ReasoningTokens:  tu.ReasoningTokens,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: cacheWrite,
		TotalCostUSD:     tu.TotalCost,
	}
	if err := o.ledger.Append(entry); err != nil {
		log.Printf("[COST_LEDGER] Failed to append entry: %v", err)
	}
	return nil
}

// extractCacheTokens pulls cache_read_input_tokens / cache_creation_input_tokens
// out of the provider's raw generation info blob. Providers that don't report
// cache usage just return zeros.
func extractCacheTokens(info map[string]interface{}) (read, write int) {
	if info == nil {
		return 0, 0
	}
	if v, ok := info["cache_read_input_tokens"]; ok {
		read = toInt(v)
	}
	if v, ok := info["cache_creation_input_tokens"]; ok {
		write = toInt(v)
	}
	return read, write
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return int(f)
		}
	}
	return 0
}

// handleCostSummary is the HTTP handler for GET /api/cost/summary. Optional
// `from` and `to` query params (YYYY-MM-DD, UTC) bound the date range.
func (api *StreamingAPI) handleCostSummary(w http.ResponseWriter, r *http.Request) {
	if api.costLedger == nil {
		http.Error(w, `{"error":"cost ledger not initialized"}`, http.StatusServiceUnavailable)
		return
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	summary, err := api.costLedger.Summarize(from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}
