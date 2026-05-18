package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// TestCostSummaryHTTPCapturesRealAnthropicTurn is the full-stack HTTP
// e2e: real Anthropic call → costObserver → ledger → HTTP
// /api/cost/summary. Asserts the handler returns valid JSON with
// non-zero PromptTokens, CompletionTokens, TotalCostUSD, and a
// ByModel entry for the real model.
//
// Gated on RUN_ANTHROPIC_REAL_E2E=1 + ANTHROPIC_API_KEY.
//
// This complements the lower-level
// TestCostLedgerCapturesRealAnthropicTurn (which exercises the
// observer/ledger pipeline directly) by adding the HTTP layer the
// frontend actually hits.
func TestCostSummaryHTTPCapturesRealAnthropicTurn(t *testing.T) {
	if os.Getenv("RUN_ANTHROPIC_REAL_E2E") == "" {
		t.Skip("set RUN_ANTHROPIC_REAL_E2E=1 to run this HTTP e2e")
	}
	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY required")
	}
	model := strings.TrimSpace(os.Getenv("ANTHROPIC_REAL_E2E_MODEL"))
	if model == "" {
		model = "claude-haiku-4-5"
	}

	// Real provider call ----------------------------------------------------
	client := anthropic.NewClient(anthropicoption.WithAPIKey(apiKey))
	adapter := anthropicadapter.NewAnthropicAdapter(client, model, &e2eMockLogger{})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := adapter.GenerateContent(ctx,
		[]llmtypes.MessageContent{
			{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Reply with OK."}}},
		},
		llmtypes.WithMaxTokens(16),
	)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	gi := resp.Choices[0].GenerationInfo

	// Build a TokenUsageEvent the same way mcpagent does. ------------------
	additional := map[string]interface{}{}
	for k, v := range gi.Additional {
		additional[k] = v
	}
	prompt := derefInt(gi.PromptTokens, gi.InputTokens)
	completion := derefInt(gi.CompletionTokens, gi.OutputTokens)
	tokenEvent := &unifiedevents.TokenUsageEvent{
		ModelID:          model,
		Provider:         "anthropic",
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      prompt + completion,
		GenerationInfo:   additional,
	}
	// Anthropic's adapter writes cost_usd_estimated (no provider-blessed
	// cost on the Messages API). The cost-routes extractor promotes it
	// into the ledger entry's TotalCostUSD with CostUSDSource=estimated.

	// Build the API with a ledger pointed at an httptest workspace -------
	wsServer := costledger.NewTestServer(t)
	defer wsServer.Close()
	api := &StreamingAPI{
		costLedger: costledger.NewLedger(wsServer.URL),
	}

	// Wire the costObserver and feed the event through it ------------------
	observer := newCostObserver(api.costLedger, "test-session", "test-user", "chat")
	if err := observer.HandleEvent(context.Background(), &unifiedevents.AgentEvent{
		Type:      unifiedevents.TokenUsage,
		Timestamp: time.Now(),
		Component: "test",
		Data:      tokenEvent,
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	// HTTP roundtrip through handleCostSummary -----------------------------
	req := httptest.NewRequest(http.MethodGet, "/api/cost/summary", nil)
	rec := httptest.NewRecorder()
	api.handleCostSummary(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/cost/summary status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var summary costledger.Summary
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v\nbody=%s", err, rec.Body.String())
	}
	if summary.Total.CallCount == 0 {
		t.Fatalf("Total.CallCount = 0; expected 1 entry")
	}
	if summary.Total.PromptTokens == 0 {
		t.Fatalf("Total.PromptTokens = 0; expected > 0 from real Anthropic call")
	}
	if summary.Total.CompletionTokens == 0 {
		t.Fatalf("Total.CompletionTokens = 0")
	}
	if summary.Total.TotalCostUSD <= 0 {
		t.Fatalf("Total.TotalCostUSD = %v; expected > 0", summary.Total.TotalCostUSD)
	}
	if _, ok := summary.ByModel[model]; !ok {
		keys := []string{}
		for k := range summary.ByModel {
			keys = append(keys, k)
		}
		t.Fatalf("summary.ByModel missing %q; keys=%v", model, keys)
	}
	t.Logf("✅ /api/cost/summary: prompt=%d completion=%d cost=$%.6f model=%q",
		summary.Total.PromptTokens, summary.Total.CompletionTokens, summary.Total.TotalCostUSD, model)
}
