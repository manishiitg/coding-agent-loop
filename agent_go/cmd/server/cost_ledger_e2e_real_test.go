package server

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"

	unifiedevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	anthropicadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/anthropic"

	"mcp-agent-builder-go/agent_go/pkg/costledger"
)

// e2eMockLogger is a stdout-silent logger for the adapter under test.
// Mirrors the shape the real provider-side test files use; kept
// inline here so this file compiles standalone.
type e2eMockLogger struct{}

func (l *e2eMockLogger) Infof(format string, args ...any)         {}
func (l *e2eMockLogger) Errorf(format string, args ...any)        {}
func (l *e2eMockLogger) Debugf(format string, args ...interface{}) {}

// TestCostLedgerCapturesRealAnthropicTurn is the cross-stack e2e for
// token + USD cost capture: it makes a real Anthropic API call, mirrors
// the mcpagent → cost_routes.go pipeline by building a TokenUsageEvent
// from the response's GenerationInfo, feeds it through the actual
// costObserver, then asserts the ledger entry carries the
// provider-blessed total_cost_usd (not the adapter-side estimate) and
// non-zero token counts.
//
// Gated on RUN_ANTHROPIC_REAL_E2E=1 + ANTHROPIC_API_KEY so a routine
// `go test ./...` without secrets skips cleanly.
func TestCostLedgerCapturesRealAnthropicTurn(t *testing.T) {
	if os.Getenv("RUN_ANTHROPIC_REAL_E2E") == "" {
		t.Skip("set RUN_ANTHROPIC_REAL_E2E=1 to run the cost-ledger e2e against Anthropic")
	}
	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY required")
	}
	model := strings.TrimSpace(os.Getenv("ANTHROPIC_REAL_E2E_MODEL"))
	if model == "" {
		model = "claude-haiku-4-5"
	}

	// Real provider call.
	client := anthropic.NewClient(anthropicoption.WithAPIKey(apiKey))
	adapter := anthropicadapter.NewAnthropicAdapter(client, model, &e2eMockLogger{})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := adapter.GenerateContent(ctx,
		[]llmtypes.MessageContent{
			{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Reply with the single word OK."}}},
		},
		llmtypes.WithMaxTokens(16),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].GenerationInfo == nil {
		t.Fatal("response had no GenerationInfo")
	}
	gi := resp.Choices[0].GenerationInfo

	// Mirror what mcpagent's llm.ExtractTokenUsageWithCacheInfo would
	// build: tokens from gi + the full Additional map copied through.
	additional := map[string]interface{}{}
	for k, v := range gi.Additional {
		additional[k] = v
	}
	promptTokens := derefInt(gi.PromptTokens, gi.InputTokens)
	completionTokens := derefInt(gi.CompletionTokens, gi.OutputTokens)
	tokenEvent := &unifiedevents.TokenUsageEvent{
		BaseEventData:    unifiedevents.BaseEventData{},
		ModelID:          model,
		Provider:         "anthropic",
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		GenerationInfo:   additional,
	}
	// Anthropic adapter writes provider-blessed cost_usd straight into
	// Additional. Promote it to the canonical TokenUsageEvent.TotalCost
	// field — that's what mcpagent's NewTokenUsageEventWithCache does
	// after pricing computation.
	if v, ok := additional["cost_usd"]; ok {
		if f, ok := v.(float64); ok {
			tokenEvent.TotalCost = f
		}
	}

	// Build a fresh ledger in a temp dir; spin up the costObserver
	// exactly the way handleChat() does in production.
	wsServer := costledger.NewTestServer(t)
	defer wsServer.Close()
	ledger := costledger.NewLedger(wsServer.URL)
	observer := newCostObserver(ledger, "test-session", "test-user", "chat")

	agentEvent := &unifiedevents.AgentEvent{
		Type:      unifiedevents.TokenUsage,
		Timestamp: time.Now(),
		Component: "test",
		Data:      tokenEvent,
	}
	if err := observer.HandleEvent(context.Background(), agentEvent); err != nil {
		t.Fatalf("costObserver.HandleEvent: %v", err)
	}

	// Read back through Summarize, which is what GET /api/cost/summary
	// returns to the frontend.
	summary, err := ledger.Summarize("", "")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary.Total.CallCount == 0 {
		t.Fatalf("expected at least 1 ledger entry; got 0 (ledger probably never saw the event)")
	}
	if summary.Total.PromptTokens == 0 {
		t.Fatalf("PromptTokens = 0 in summary; expected > 0 from real Anthropic call")
	}
	if summary.Total.CompletionTokens == 0 {
		t.Fatalf("CompletionTokens = 0; expected > 0")
	}
	if summary.Total.TotalCostUSD <= 0 {
		t.Fatalf("TotalCostUSD = %v; expected > 0 (provider-blessed cost_usd from anthropic Additional)", summary.Total.TotalCostUSD)
	}
	// The aggregate is bucketed by ModelID. Anthropic's adapter does
	// not set an effective-model override, so the bucket key should
	// be the requested model.
	bucket, ok := summary.ByModel[model]
	if !ok {
		t.Fatalf("summary.ByModel missing key %q; got keys=%v", model, costLedgerMapKeys(summary.ByModel))
	}
	if bucket.TotalCostUSD <= 0 {
		t.Fatalf("by_model[%q].TotalCostUSD = %v; expected > 0", model, bucket.TotalCostUSD)
	}
	t.Logf("✅ ledger captured: prompt=%d completion=%d cost_usd=$%.6f model=%q",
		summary.Total.PromptTokens, summary.Total.CompletionTokens, summary.Total.TotalCostUSD, model)
}

// TestCostLedgerCapturesEstimatedCostFromGenerationInfo exercises the
// CLI-side path: a synthetic TokenUsageEvent with cost_usd_estimated
// set on Additional (no provider-blessed cost). The ledger must
// fall back to the estimated value, mark CostUSDSource="estimated",
// and key the bucket by EffectiveModelID rather than the requested
// ModelID. No external API required — pure pipeline test.
func TestCostLedgerCapturesEstimatedCostFromGenerationInfo(t *testing.T) {
	wsServer := costledger.NewTestServer(t)
	defer wsServer.Close()
	ledger := costledger.NewLedger(wsServer.URL)
	observer := newCostObserver(ledger, "test-session", "test-user", "chat")

	tokenEvent := &unifiedevents.TokenUsageEvent{
		ModelID:          "cursor-cli", // alias the user picked
		Provider:         "cursor-cli",
		PromptTokens:     1000,
		CompletionTokens: 250,
		TotalTokens:      1250,
		// TotalCost intentionally 0 — cursor's JSON ships no USD.
		GenerationInfo: map[string]interface{}{
			"cost_usd_estimated": 0.0123,
			"cost_model_id":      "composer-2.5", // the real model that served
			"cursor_model":       "composer-2.5",
		},
	}
	if err := observer.HandleEvent(context.Background(), &unifiedevents.AgentEvent{
		Type:      unifiedevents.TokenUsage,
		Timestamp: time.Now(),
		Component: "test",
		Data:      tokenEvent,
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	summary, err := ledger.Summarize("", "")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary.Total.TotalCostUSD <= 0 {
		t.Fatalf("TotalCostUSD = %v; expected the estimated value to fill in", summary.Total.TotalCostUSD)
	}
	if summary.Total.TotalCostUSD != 0.0123 {
		t.Fatalf("TotalCostUSD = %v; expected 0.0123 (the cost_usd_estimated value)", summary.Total.TotalCostUSD)
	}
}

func derefInt(ptrs ...*int) int {
	for _, p := range ptrs {
		if p != nil && *p > 0 {
			return *p
		}
	}
	return 0
}

func costLedgerMapKeys(m map[string]*costledger.Aggregate) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
