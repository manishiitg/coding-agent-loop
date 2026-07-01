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
	picliadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/picli"

	"mcp-agent-builder-go/agent_go/pkg/costledger"
)

// TestCostSummaryHTTPCapturesRealPiTurn is the pi-cli
// counterpart to TestCostSummaryHTTPCapturesRealAnthropicTurn. It
// proves the cost ledger captures Pi CLI turns and the HTTP
// /api/cost/summary endpoint surfaces them.
func TestCostSummaryHTTPCapturesRealPiTurn(t *testing.T) {
	if os.Getenv("RUN_PI_CLI_REAL_E2E") == "" {
		t.Skip("set RUN_PI_CLI_REAL_E2E=1 to run this cost HTTP e2e")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi binary not found: %v", err)
	}

	modelID := strings.TrimSpace(os.Getenv("PI_CLI_REAL_CONTRACT_MODEL"))
	if modelID == "" {
		modelID = picliadapter.DefaultModelID
	}
	apiKey := firstNonEmptyCostPiEnv("GEMINI_API_KEY", "GOOGLE_API_KEY", "PI_API_KEY")
	adapter := picliadapter.NewPiCLIAdapter(apiKey, modelID, &e2eMockLogger{})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	resp, err := adapter.GenerateContent(ctx,
		[]llmtypes.MessageContent{
			{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Reply with exactly the word OK and nothing else."},
			}},
		},
	)
	if err != nil {
		t.Fatalf("pi GenerateContent: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].GenerationInfo == nil {
		t.Fatal("response missing GenerationInfo")
	}
	gi := resp.Choices[0].GenerationInfo

	// Build the TokenUsageEvent the way the server's hot path does.
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
	if prompt == 0 {
		t.Fatalf("pi reported zero prompt tokens in both GenerationInfo and resp.Usage; GenerationInfo=%+v Usage=%+v", gi, resp.Usage)
	}
	if completion == 0 {
		t.Fatalf("pi reported zero completion tokens; GenerationInfo=%+v Usage=%+v", gi, resp.Usage)
	}

	if effective, ok := additional["pi_model"].(string); ok && strings.TrimSpace(effective) != "" {
		modelID = strings.TrimSpace(effective)
	}
	tokenEvent := &unifiedevents.TokenUsageEvent{
		ModelID:          modelID,
		Provider:         "pi-cli",
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      prompt + completion,
		GenerationInfo:   additional,
	}

	// Wire the ledger, observer, and HTTP handler.
	wsServer := costledger.NewTestServer(t)
	defer wsServer.Close()
	api := &StreamingAPI{
		costLedger: costledger.NewLedger(wsServer.URL),
	}
	observer := newCostObserver(api.costLedger, "test-session-pi", "test-user", "chat")
	if err := observer.HandleEvent(context.Background(), &unifiedevents.AgentEvent{
		Type:      unifiedevents.TokenUsage,
		Timestamp: time.Now(),
		Component: "test",
		Data:      tokenEvent,
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	// HTTP roundtrip.
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
		t.Fatal("Total.CallCount = 0; expected 1 pi turn")
	}
	if summary.Total.PromptTokens != prompt {
		t.Fatalf("Total.PromptTokens = %d, want %d", summary.Total.PromptTokens, prompt)
	}
	if summary.Total.CompletionTokens != completion {
		t.Fatalf("Total.CompletionTokens = %d, want %d", summary.Total.CompletionTokens, completion)
	}

	if summary.Total.TotalCostUSD <= 0 {
		t.Logf("Total.TotalCostUSD = %v (Pi metadata may omit cost estimates for local/provider-qualified models)", summary.Total.TotalCostUSD)
	}

	t.Logf("✅ /api/cost/summary pi turn: prompt=%d completion=%d call_count=%d (cost=$%.6f model=%q)",
		summary.Total.PromptTokens, summary.Total.CompletionTokens, summary.Total.CallCount, summary.Total.TotalCostUSD, modelID)
}

func firstNonEmptyCostPiEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}
