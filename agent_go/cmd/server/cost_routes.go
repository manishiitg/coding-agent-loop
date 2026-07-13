package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"

	unifiedevents "github.com/manishiitg/mcpagent/events"

	"mcp-agent-builder-go/agent_go/pkg/costledger"
)

// costObserver persists immutable per-LLM-call events. The cumulative
// token_usage event remains a compatibility fallback for older providers, but
// is ignored once a per-call completion has been observed.
type costObserver struct {
	ledger      *costledger.Ledger
	sessionID   string
	userID      string
	agentMode   string
	provider    string
	modelID     string
	workflowID  string
	runID       string
	executionID string
	scope       string

	mu         sync.Mutex
	sawPerCall bool
}

type costObserverOption func(*costObserver)

func withCostModel(provider, modelID string) costObserverOption {
	return func(o *costObserver) {
		o.provider = strings.TrimSpace(provider)
		o.modelID = strings.TrimSpace(modelID)
	}
}

func withCostAttribution(scope, workflowID, runID, executionID string) costObserverOption {
	return func(o *costObserver) {
		o.scope = strings.TrimSpace(scope)
		o.workflowID = strings.TrimSpace(workflowID)
		o.runID = strings.TrimSpace(runID)
		o.executionID = strings.TrimSpace(executionID)
	}
}

func newCostObserver(ledger *costledger.Ledger, sessionID, userID, agentMode string, opts ...costObserverOption) *costObserver {
	observer := &costObserver{
		ledger:    ledger,
		sessionID: sessionID,
		userID:    userID,
		agentMode: agentMode,
		scope:     inferCostScope(agentMode, ""),
	}
	for _, opt := range opts {
		opt(observer)
	}
	return observer
}

func (o *costObserver) Name() string { return "costObserver" }

func (o *costObserver) HandleEvent(_ context.Context, event *unifiedevents.AgentEvent) error {
	if o == nil || o.ledger == nil || event == nil {
		return nil
	}
	switch event.Type {
	case unifiedevents.LLMGenerationEnd:
		generation, ok := event.Data.(*unifiedevents.LLMGenerationEndEvent)
		if !ok || generation == nil {
			return nil
		}
		o.mu.Lock()
		o.sawPerCall = true
		o.mu.Unlock()
		o.recordLLMGeneration(event, generation)
	case unifiedevents.TokenUsage:
		o.mu.Lock()
		sawPerCall := o.sawPerCall
		o.mu.Unlock()
		if sawPerCall {
			return nil
		}
		tu, ok := event.Data.(*unifiedevents.TokenUsageEvent)
		if !ok || tu == nil {
			return nil
		}
		o.recordLegacyTokenUsage(event, tu)
	}
	return nil
}

func (o *costObserver) recordLLMGeneration(event *unifiedevents.AgentEvent, generation *unifiedevents.LLMGenerationEndEvent) {
	metadata := generation.Metadata
	cacheRead, cacheWrite := extractCacheTokens(metadata)
	effectiveModel, estimatedCost := extractCostAndEffectiveModel(metadata)
	provider := costFirstNonEmpty(costStringValue(metadata["provider"]), o.provider)
	modelID := o.modelID
	if modelID == "" {
		modelID = costStringValue(metadata["requested_model_id"])
	}
	totalCostUSD := costNumberValue(metadata["cost_usd"])
	billingBasis := "provider_actual"
	pricingSource := "provider"
	if totalCostUSD <= 0 && estimatedCost > 0 {
		totalCostUSD = estimatedCost
		billingBasis = "token_estimate"
		pricingSource = "model_registry"
		if isSubscriptionCodingProvider(provider) {
			billingBasis = "subscription_shadow"
		}
	}
	if totalCostUSD <= 0 {
		billingBasis = "unpriced"
		pricingSource = ""
	}
	if effectiveModel == "" {
		effectiveModel = modelID
	}
	entry := o.baseEntry(event)
	entry.Provider = provider
	entry.ModelID = modelID
	entry.EffectiveProvider = provider
	entry.EffectiveModelID = effectiveModel
	entry.TurnCount = 1
	entry.LLMCallCount = 1
	entry.PromptTokens = generation.UsageMetrics.PromptTokens
	entry.CompletionTokens = generation.UsageMetrics.CompletionTokens
	entry.ReasoningTokens = generation.UsageMetrics.ReasoningTokens
	entry.CacheReadTokens = costFirstPositive(cacheRead, generation.UsageMetrics.CacheTokens)
	entry.CacheWriteTokens = cacheWrite
	entry.TotalCostUSD = totalCostUSD
	entry.BillingBasis = billingBasis
	entry.PricingSource = pricingSource
	o.append(entry)
}

func (o *costObserver) recordLegacyTokenUsage(event *unifiedevents.AgentEvent, tu *unifiedevents.TokenUsageEvent) {
	cacheRead, cacheWrite := extractCacheTokens(tu.GenerationInfo)
	effectiveModel, estimatedCost := extractCostAndEffectiveModel(tu.GenerationInfo)
	provider := costFirstNonEmpty(tu.Provider, o.provider)
	modelID := costFirstNonEmpty(tu.ModelID, o.modelID)
	totalCostUSD := costNumberValue(tu.GenerationInfo["cost_usd"])
	billingBasis := "provider_actual"
	pricingSource := "provider"
	if totalCostUSD <= 0 && estimatedCost > 0 {
		totalCostUSD = estimatedCost
		billingBasis = "token_estimate"
		pricingSource = "model_registry"
		if isSubscriptionCodingProvider(provider) {
			billingBasis = "subscription_shadow"
		}
	} else if totalCostUSD <= 0 && tu.TotalCost > 0 {
		// TotalCost on the cumulative event is generally computed from model
		// metadata. It must not be labeled as a provider invoice.
		totalCostUSD = tu.TotalCost
		billingBasis = "token_estimate"
		pricingSource = "agent_token_pricing"
		if isSubscriptionCodingProvider(provider) {
			billingBasis = "subscription_shadow"
		}
	}
	if totalCostUSD <= 0 {
		billingBasis = "unpriced"
		pricingSource = ""
	}
	if effectiveModel == "" {
		effectiveModel = modelID
	}
	entry := o.baseEntry(event)
	entry.Provider = provider
	entry.ModelID = modelID
	entry.EffectiveProvider = provider
	entry.EffectiveModelID = effectiveModel
	entry.TurnCount = 1
	entry.LLMCallCount = costFirstPositive(toInt(tu.GenerationInfo["llm_call_count"]), 1)
	entry.PromptTokens = tu.PromptTokens
	entry.CompletionTokens = tu.CompletionTokens
	entry.ReasoningTokens = tu.ReasoningTokens
	entry.CacheReadTokens = cacheRead
	entry.CacheWriteTokens = cacheWrite
	entry.TotalCostUSD = totalCostUSD
	entry.BillingBasis = billingBasis
	entry.PricingSource = pricingSource
	o.append(entry)
}

func (o *costObserver) baseEntry(event *unifiedevents.AgentEvent) costledger.Entry {
	eventID := strings.TrimSpace(event.SpanID)
	if eventID == "" {
		eventID = strings.TrimSpace(event.CorrelationID)
	}
	storedEventID := ""
	idempotencyKey := ""
	if eventID != "" {
		storedEventID = strings.Join([]string{o.sessionID, eventID}, ":")
		idempotencyKey = storedEventID
	}
	return costledger.Entry{
		EventID:        storedEventID,
		IdempotencyKey: idempotencyKey,
		Timestamp:      event.Timestamp,
		SessionID:      o.sessionID,
		UserID:         o.userID,
		WorkflowID:     o.workflowID,
		RunID:          o.runID,
		ExecutionID:    o.executionID,
		Scope:          o.scope,
		AgentMode:      o.agentMode,
		Component:      event.Component,
		CorrelationID:  event.CorrelationID,
	}
}

func (o *costObserver) append(entry costledger.Entry) {
	if err := o.ledger.Append(entry); err != nil {
		log.Printf("[COST_LEDGER] Failed to append entry: %v", err)
	}
}

func inferCostScope(agentMode, phaseID string) string {
	phase := strings.ToLower(strings.TrimSpace(phaseID))
	switch {
	case strings.Contains(phase, "post_run_monitor"), strings.Contains(phase, "pulse"):
		return "pulse"
	case strings.Contains(phase, "evaluation"), strings.Contains(phase, "eval"):
		return "evaluation"
	case strings.Contains(strings.ToLower(agentMode), "chief"):
		return "chief_of_staff"
	case strings.Contains(strings.ToLower(agentMode), "workflow"):
		return "builder"
	default:
		return "builder"
	}
}

func isSubscriptionCodingProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude-code", "claude_code", "codex-cli", "codex_cli", "cursor-cli", "cursor_cli":
		return true
	default:
		return false
	}
}

func costStringValue(v interface{}) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func costNumberValue(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		value, _ := n.Float64()
		return value
	default:
		return 0
	}
}

func costFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func costFirstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
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
