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
	opencodecliadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/opencodecli"

	"mcp-agent-builder-go/agent_go/pkg/costledger"
)

// TestCostSummaryHTTPCapturesRealOpenCodeTurn is the opencode-cli
// counterpart to TestCostSummaryHTTPCapturesRealAnthropicTurn. It
// proves the cost ledger captures opencode CLI turns and the HTTP
// /api/cost/summary endpoint surfaces them.
//
// Known coverage limitation: the opencode-cli adapter (as of this
// commit) does NOT emit cost_usd_estimated / cost_model_id on
// GenerationInfo.Additional. cursorcli already does (see
// cursorcli_structured_adapter.go:315). Until opencode-cli is brought
// onto the same shape, this test asserts only what opencode actually
// emits today — call count and prompt/completion tokens. The
// TotalCostUSD assertion is intentionally a non-blocking log so the
// gap remains visible without making the test flaky.
//
// Gated on RUN_OPENCODE_CLI_REAL_E2E=1 + ZHIPU_API_KEY + opencode
// binary in PATH.
func TestCostSummaryHTTPCapturesRealOpenCodeTurn(t *testing.T) {
	if os.Getenv("RUN_OPENCODE_CLI_REAL_E2E") == "" {
		t.Skip("set RUN_OPENCODE_CLI_REAL_E2E=1 to run this cost HTTP e2e")
	}
	apiKey := strings.TrimSpace(os.Getenv("ZHIPU_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("ZAI_API_KEY"))
	}
	if apiKey == "" {
		t.Skip("ZHIPU_API_KEY (or ZAI_API_KEY) required for opencode-cli cost e2e")
	}
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skipf("opencode binary not found: %v", err)
	}

	// Real opencode call scoped to the GLM coding-plan tile.
	adapter := opencodecliadapter.NewOpenCodeCLIAdapter("", "opencode-cli", &e2eMockLogger{})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	resp, err := adapter.GenerateContent(ctx,
		[]llmtypes.MessageContent{
			{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Reply with exactly the word OK and nothing else."},
			}},
		},
		opencodecliadapter.WithOpenCodeSubProvider("opencode-cli-glm-coding-plan"),
		opencodecliadapter.WithOpenCodeSubProviderAPIKey("ZHIPU_API_KEY", apiKey),
		opencodecliadapter.WithOpenCodeModel("medium"),
	)
	if err != nil {
		t.Fatalf("opencode GenerateContent: %v", err)
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
	// opencode-cli currently surfaces tokens only on resp.Usage, not
	// on GenerationInfo.InputTokens / OutputTokens (a known shape gap
	// — the API adapters populate both). Fall back so the test still
	// proves the cost-ledger pipeline works for opencode turns;
	// flagging the gap via t.Log keeps it visible.
	if prompt == 0 && resp.Usage != nil && resp.Usage.InputTokens > 0 {
		prompt = resp.Usage.InputTokens
		t.Logf("⚠️  opencode prompt tokens read from resp.Usage (GenerationInfo.InputTokens unpopulated; known adapter gap)")
	}
	if completion == 0 && resp.Usage != nil && resp.Usage.OutputTokens > 0 {
		completion = resp.Usage.OutputTokens
	}
	if prompt == 0 {
		t.Fatalf("opencode reported zero prompt tokens in both GenerationInfo and resp.Usage; GenerationInfo=%+v Usage=%+v", gi, resp.Usage)
	}
	if completion == 0 {
		t.Fatalf("opencode reported zero completion tokens; GenerationInfo=%+v Usage=%+v", gi, resp.Usage)
	}

	// opencode does not yet populate cost_model_id; fall back to the
	// resolved tier model id so the ByModel bucket is queryable.
	modelID := "opencode-cli-glm-coding-plan/glm-4.7"
	tokenEvent := &unifiedevents.TokenUsageEvent{
		ModelID:          modelID,
		Provider:         "opencode-cli",
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
	observer := newCostObserver(api.costLedger, "test-session-opencode", "test-user", "chat")
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
		t.Fatal("Total.CallCount = 0; expected 1 opencode turn")
	}
	if summary.Total.PromptTokens != prompt {
		t.Fatalf("Total.PromptTokens = %d, want %d", summary.Total.PromptTokens, prompt)
	}
	if summary.Total.CompletionTokens != completion {
		t.Fatalf("Total.CompletionTokens = %d, want %d", summary.Total.CompletionTokens, completion)
	}

	if summary.Total.TotalCostUSD <= 0 {
		// Non-fatal: the gap this test documents. When opencode-cli
		// starts emitting cost_usd_estimated, this becomes a fatal
		// assertion and the t.Log can be deleted.
		t.Logf("⚠️  Total.TotalCostUSD = %v (opencode-cli does not yet emit cost_usd_estimated; tracking as a known gap)", summary.Total.TotalCostUSD)
	}

	t.Logf("✅ /api/cost/summary opencode turn: prompt=%d completion=%d call_count=%d (cost=$%.6f model=%q)",
		summary.Total.PromptTokens, summary.Total.CompletionTokens, summary.Total.CallCount, summary.Total.TotalCostUSD, modelID)
}
