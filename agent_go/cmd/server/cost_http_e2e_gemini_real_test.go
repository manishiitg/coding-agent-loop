package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	unifiedevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	geminicliadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/geminicli"

	"mcp-agent-builder-go/agent_go/pkg/costledger"
)

// TestCostSummaryHTTPCapturesRealGeminiCLITurn is the HTTP cost e2e
// for Gemini CLI, mirroring cost_http_e2e_codex_real_test.go and
// cost_http_e2e_opencode_real_test.go. Drives a real gemini turn
// through the structured (stream-json) transport, then asserts
// tokens surface on /api/cost/summary.
//
// Why this exists: gemini's TokenUsageSource is "transcript-file"
// (see coding_agent_contract.go) — the adapter parses tokens from
// ~/.gemini/tmp/gemini-cli-project-<projectDirID>/chats/session-*.jsonl
// AFTER the turn completes. The transcript reader has unit coverage
// with synthetic fixtures, but until this test no end-to-end test
// verified that:
//   - the real Gemini CLI emits that transcript in the format we
//     expect,
//   - the parser hits it before the turn's grace window closes,
//   - tokens flow through costObserver to /api/cost/summary keyed
//     by the effective model name.
//
// Cost assertion is intentionally non-fatal here (like the opencode
// cost test) because gemini's adapter doesn't currently emit
// cost_usd_estimated. When the adapter starts surfacing cost, the
// t.Logf below should become a t.Fatalf assertion.
//
// Gated on RUN_GEMINI_CLI_REAL_E2E=1 + GEMINI_API_KEY + gemini
// binary in PATH.
func TestCostSummaryHTTPCapturesRealGeminiCLITurn(t *testing.T) {
	if os.Getenv("RUN_GEMINI_CLI_REAL_E2E") == "" && os.Getenv("RUN_GEMINI_CLI_INTERACTIVE_E2E") == "" {
		t.Skip("set RUN_GEMINI_CLI_REAL_E2E=1 to run this cost HTTP e2e")
	}
	if _, err := exec.LookPath("gemini"); err != nil {
		t.Skipf("gemini binary not found: %v", err)
	}
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY required for gemini-cli real cost e2e")
	}
	model := strings.TrimSpace(os.Getenv("GEMINI_CLI_CONTRACT_MODEL"))
	if model == "" {
		model = "low" // resolves to gemini-3.5-flash via the tier resolver
	}
	t.Cleanup(func() { _ = geminicliadapter.CleanupGeminiCLIInteractiveSessions(context.Background()) })

	adapter := geminicliadapter.NewGeminiCLIAdapter(apiKey, model, &e2eMockLogger{})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	resp, err := adapter.GenerateContent(ctx,
		[]llmtypes.MessageContent{
			{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Reply with exactly the word OK and nothing else."},
			}},
		},
		geminicliadapter.WithProjectSettings(`{}`),
		geminicliadapter.WithApprovalMode("yolo"),
	)
	if err != nil {
		t.Fatalf("gemini GenerateContent: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].GenerationInfo == nil {
		t.Fatal("response missing GenerationInfo")
	}
	gi := resp.Choices[0].GenerationInfo

	prompt := derefInt(gi.PromptTokens, gi.InputTokens)
	completion := derefInt(gi.CompletionTokens, gi.OutputTokens)
	if prompt == 0 && resp.Usage != nil && resp.Usage.InputTokens > 0 {
		prompt = resp.Usage.InputTokens
	}
	if completion == 0 && resp.Usage != nil && resp.Usage.OutputTokens > 0 {
		completion = resp.Usage.OutputTokens
	}
	if prompt == 0 {
		t.Fatalf("PromptTokens = 0 — gemini transcript-file parser failed. gi.Additional=%v", gi.Additional)
	}
	if completion == 0 {
		t.Fatalf("CompletionTokens = 0 — gemini transcript-file parser regression. gi.Additional=%v", gi.Additional)
	}

	additional := map[string]interface{}{}
	for k, v := range gi.Additional {
		additional[k] = v
	}
	effectiveModel, _ := extractCostAndEffectiveModel(additional)
	if effectiveModel == "" {
		effectiveModel = model
	}

	tokenEvent := &unifiedevents.TokenUsageEvent{
		ModelID:          effectiveModel,
		Provider:         "geminicli",
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      prompt + completion,
		GenerationInfo:   additional,
	}

	wsServer := costledger.NewTestServer(t)
	defer wsServer.Close()
	api := &StreamingAPI{
		costLedger: costledger.NewLedger(wsServer.URL),
	}
	observer := newCostObserver(api.costLedger, "test-session-gemini", "test-user", "chat")
	if err := observer.HandleEvent(context.Background(), &unifiedevents.AgentEvent{
		Type:      unifiedevents.TokenUsage,
		Timestamp: time.Now(),
		Component: "test",
		Data:      tokenEvent,
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

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
		t.Fatal("Total.CallCount = 0; expected 1 gemini turn")
	}
	if summary.Total.PromptTokens == 0 {
		t.Fatalf("Total.PromptTokens = 0 — gemini transcript-file token-extraction broken end-to-end")
	}
	if summary.Total.CompletionTokens == 0 {
		t.Fatalf("Total.CompletionTokens = 0 — gemini transcript-file token-extraction broken end-to-end")
	}
	if summary.Total.TotalCostUSD <= 0 {
		// Non-fatal — gemini's adapter does not currently emit
		// cost_usd_estimated. When the adapter starts surfacing cost
		// (e.g. by computing input_tokens * model_rate via the
		// registry), this becomes a t.Fatalf and the log goes away.
		t.Logf("⚠️  Total.TotalCostUSD = %v (gemini-cli adapter does not yet emit cost_usd_estimated; tracking as a known gap)", summary.Total.TotalCostUSD)
	}
	if _, ok := summary.ByModel[effectiveModel]; !ok {
		keys := []string{}
		for k := range summary.ByModel {
			keys = append(keys, k)
		}
		t.Fatalf("summary.ByModel missing %q; keys=%v", effectiveModel, keys)
	}

	t.Logf("✅ /api/cost/summary gemini turn: prompt=%d completion=%d call_count=%d (cost=$%.6f model=%q)",
		summary.Total.PromptTokens, summary.Total.CompletionTokens, summary.Total.CallCount, summary.Total.TotalCostUSD, effectiveModel)
}
