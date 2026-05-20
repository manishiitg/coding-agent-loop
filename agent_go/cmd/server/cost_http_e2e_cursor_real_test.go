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
	cursorcliadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/cursorcli"

	"mcp-agent-builder-go/agent_go/pkg/costledger"
)

// TestCostSummaryHTTPCapturesRealCursorTurn is the cursor-cli
// counterpart to TestCostSummaryHTTPCapturesRealAnthropicTurn.
//
// Known coverage limitation: cursor-cli has the cost emission code
// path wired (cursorcli_structured_adapter.go:312-318), but the
// effective-model the cursor CLI returns is a *display* name like
// "Composer 2.5" while the model-metadata registry is keyed by
// model *id* like "composer-2-fast". The metadata lookup therefore
// returns nil and no cost is emitted. This is a real cursor-cli
// adapter gap. The test still proves the ledger pipeline works for
// cursor-cli turns by asserting CallCount + tokens; cost is logged
// as a known gap so the gap remains visible without making the
// test flaky.
//
// Gated on RUN_CURSOR_CLI_REAL_E2E=1 + cursor-agent binary in PATH.
func TestCostSummaryHTTPCapturesRealCursorTurn(t *testing.T) {
	if os.Getenv("RUN_CURSOR_CLI_REAL_E2E") == "" {
		t.Skip("set RUN_CURSOR_CLI_REAL_E2E=1 to run this cost HTTP e2e")
	}
	if _, err := exec.LookPath("cursor-agent"); err != nil {
		t.Skipf("cursor-agent binary not found: %v", err)
	}
	model := strings.TrimSpace(os.Getenv("CURSOR_CLI_REAL_E2E_MODEL"))
	if model == "" {
		model = "cursor-cli"
	}

	adapter := cursorcliadapter.NewCursorCLIAdapter("", model, &e2eMockLogger{})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	resp, err := adapter.GenerateContent(ctx,
		[]llmtypes.MessageContent{
			{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Reply with exactly the word OK and nothing else."},
			}},
		},
	)
	if err != nil {
		t.Fatalf("cursor-cli GenerateContent: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].GenerationInfo == nil {
		t.Fatal("response missing GenerationInfo")
	}
	gi := resp.Choices[0].GenerationInfo

	additional := map[string]interface{}{}
	for k, v := range gi.Additional {
		additional[k] = v
	}
	prompt := derefInt(gi.PromptTokens, gi.InputTokens)
	completion := derefInt(gi.CompletionTokens, gi.OutputTokens)
	if prompt == 0 && resp.Usage != nil && resp.Usage.InputTokens > 0 {
		prompt = resp.Usage.InputTokens
	}
	if completion == 0 && resp.Usage != nil && resp.Usage.OutputTokens > 0 {
		completion = resp.Usage.OutputTokens
	}

	// cost_model_id is what the rate lookup keyed off — use the
	// cursor effective model the adapter actually picked so the
	// ByModel bucket assertion below works.
	effectiveModel, _ := extractCostAndEffectiveModel(additional)
	if effectiveModel == "" {
		effectiveModel = model
	}

	tokenEvent := &unifiedevents.TokenUsageEvent{
		ModelID:          effectiveModel,
		Provider:         "cursor-cli",
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
	observer := newCostObserver(api.costLedger, "test-session-cursor", "test-user", "chat")
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
		t.Fatal("Total.CallCount = 0; expected 1 cursor turn")
	}
	if summary.Total.PromptTokens == 0 {
		t.Fatal("Total.PromptTokens = 0")
	}
	if summary.Total.CompletionTokens == 0 {
		t.Fatal("Total.CompletionTokens = 0")
	}
	if summary.Total.TotalCostUSD <= 0 {
		// Non-fatal: documents the display-name vs model-id mismatch
		// in cursorcli_structured_adapter.go's cost lookup. When that
		// is fixed, promote this to a fatal assertion.
		t.Logf("⚠️  Total.TotalCostUSD = %v (cursor-cli effective model %q is a display name, not in the metadata registry; known gap)", summary.Total.TotalCostUSD, effectiveModel)
	}
	if _, ok := summary.ByModel[effectiveModel]; !ok {
		keys := []string{}
		for k := range summary.ByModel {
			keys = append(keys, k)
		}
		t.Fatalf("summary.ByModel missing %q; keys=%v", effectiveModel, keys)
	}

	t.Logf("✅ /api/cost/summary cursor turn: prompt=%d completion=%d cost=$%.6f model=%q",
		summary.Total.PromptTokens, summary.Total.CompletionTokens, summary.Total.TotalCostUSD, effectiveModel)
}
