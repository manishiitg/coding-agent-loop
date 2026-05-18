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
	effectiveModel, estimatedCost := extractCostAndEffectiveModel(tu.GenerationInfo)

	// Provider-blessed cost wins over the adapter-side estimate; the
	// estimate only fills in for CLIs whose JSON doesn't ship USD
	// (codex / cursor / gemini-cli) or for tmux paths.
	totalCostUSD := tu.TotalCost
	costSource := ""
	if totalCostUSD > 0 {
		costSource = "provider"
	} else if estimatedCost > 0 {
		totalCostUSD = estimatedCost
		costSource = "estimated"
	}

	entry := costledger.Entry{
		Timestamp:        event.Timestamp,
		SessionID:        o.sessionID,
		UserID:           o.userID,
		AgentMode:        o.agentMode,
		Component:        event.Component,
		CorrelationID:    event.CorrelationID,
		Provider:         tu.Provider,
		ModelID:          tu.ModelID,
		EffectiveModelID: effectiveModel,
		PromptTokens:     tu.PromptTokens,
		CompletionTokens: tu.CompletionTokens,
		ReasoningTokens:  tu.ReasoningTokens,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: cacheWrite,
		TotalCostUSD:     totalCostUSD,
		CostUSDSource:    costSource,
	}
	if err := o.ledger.Append(entry); err != nil {
		log.Printf("[COST_LEDGER] Failed to append entry: %v", err)
	}
	return nil
}

// extractCostAndEffectiveModel pulls the unified cost+model fields the
// CLI adapters now emit on GenerationInfo.Additional:
//
//	cost_usd_estimated  — float, computed from tokens × registry rates
//	cost_model_id       — string, model used for the rate lookup
//	<provider>_effective_model / claude_code_model / cursor_model /
//	  gemini_effective_model / codex_effective_model — string, the model
//	  the CLI actually served the turn with
//
// Returns ("", 0) when none of those are present.
func extractCostAndEffectiveModel(info map[string]interface{}) (effectiveModel string, estimatedCost float64) {
	if info == nil {
		return "", 0
	}
	if v, ok := info["cost_usd_estimated"]; ok {
		switch n := v.(type) {
		case float64:
			estimatedCost = n
		case float32:
			estimatedCost = float64(n)
		}
	}
	for _, key := range []string{
		"cost_model_id",
		"claude_code_model",
		"codex_effective_model",
		"gemini_effective_model",
		"cursor_model",
	} {
		if v, ok := info[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				effectiveModel = s
				break
			}
		}
	}
	return effectiveModel, estimatedCost
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
