package server

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	opencodecliadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/opencodecli"
)

// TestOpenCodeSubProviderGLMCatalog is the unit-level counterpart to the live
// test below: it proves the GLM tile is wired into the builder's sub-provider
// catalog without needing a real CLI invocation.
func TestOpenCodeSubProviderGLMCatalog(t *testing.T) {
	sp, ok := opencodecliadapter.FindOpenCodeSubProvider("opencode-cli-glm")
	if !ok {
		t.Fatal("opencode-cli-glm not found in catalog; was the tile removed from opencodecli_subproviders.go?")
	}
	if sp.OpenCodeProviderID != "zhipuai" {
		t.Fatalf("OpenCodeProviderID = %q, want zhipuai", sp.OpenCodeProviderID)
	}
	if sp.APIKeyEnvVar != "ZHIPU_API_KEY" {
		t.Fatalf("APIKeyEnvVar = %q, want ZHIPU_API_KEY (opencode reads this single env var for all zai/zhipuai tiles)", sp.APIKeyEnvVar)
	}
	if sp.DefaultModelID != "glm-4.6" {
		t.Fatalf("DefaultModelID = %q, want glm-4.6", sp.DefaultModelID)
	}
	allowed := map[string]struct{}{
		"glm-5":         {},
		"glm-5.1":       {},
		"glm-4.7":       {},
		"glm-4.6":       {},
		"glm-4.5":       {},
		"glm-4.5-flash": {},
	}
	for _, m := range sp.Models {
		if _, ok := allowed[m.ID]; !ok {
			t.Errorf("model %q is not in the live zai tile's allowed set", m.ID)
		}
	}

	// Provider runtime routes it to opencode binary.
	if got := providerRuntime("opencode-cli-glm"); got != "opencode" {
		t.Fatalf("providerRuntime = %q, want opencode", got)
	}

	// validateOpenCodeSubProvider rejects empty key for this paid tile.
	t.Setenv("ZHIPU_API_KEY", "")
	resp := validateOpenCodeSubProvider("opencode-cli-glm", structValidationReq("opencode-cli-glm", ""))
	if resp.Valid {
		t.Fatalf("expected validation to fail without ZHIPU_API_KEY; got %+v", resp)
	}
	if !strings.Contains(resp.Message, "ZHIPU_API_KEY") {
		t.Errorf("error message should mention ZHIPU_API_KEY; got %q", resp.Message)
	}
}

// TestOpenCodeSubProviderGLMCodingPlanLiveDispatch is the live e2e
// for the new tile: prove that a request scoped to
// opencode-cli-glm with a real ZHIPU_API_KEY flows
// through the builder's sub-provider plumbing → adapter → opencode
// binary → real Z.AI endpoint → returns content.
//
// This exists because the inspector + cost e2e tests above also hit
// this tile, but they assert different things (inspector events,
// cost ledger). This test isolates the *routing* contract: with the
// right sub-provider id + env var, a real call actually lands.
//
// Gated on RUN_OPENCODE_CLI_REAL_E2E=1 + ZHIPU_API_KEY + opencode
// binary in PATH.
func TestOpenCodeSubProviderGLMLiveDispatch(t *testing.T) {
	if os.Getenv("RUN_OPENCODE_CLI_REAL_E2E") == "" {
		t.Skip("set RUN_OPENCODE_CLI_REAL_E2E=1 to run this live sub-provider dispatch test")
	}
	apiKey := strings.TrimSpace(os.Getenv("ZHIPU_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("ZAI_API_KEY"))
	}
	if apiKey == "" {
		t.Skip("ZHIPU_API_KEY (or ZAI_API_KEY) required")
	}
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skipf("opencode binary not found: %v", err)
	}

	// Sanity-check the builder's validator accepts the key for this tile.
	t.Setenv("ZHIPU_API_KEY", apiKey)
	resp := validateOpenCodeSubProvider("opencode-cli-glm", llm.APIKeyValidationRequest{
		Provider: "opencode-cli-glm",
		APIKey:   apiKey,
	})
	// Note: validateOpenCodeSubProvider falls through to validateOpenCodeCLI
	// which performs a deeper check (tmux availability, runtime probe). We
	// only assert it didn't reject on the synchronous no-key branch; deeper
	// failures (e.g. tmux missing) are fine — the live call below is the
	// real proof.
	if !resp.Valid && strings.Contains(resp.Message, "requires an API key") {
		t.Fatalf("validator wrongly claims key missing: %+v", resp)
	}

	// MergedOpenCodeSubProviderKeys must surface the key under the right
	// env-var name. This is the path the builder uses to inject keys into
	// per-call options.
	keys := MergedOpenCodeSubProviderKeys(context.Background())
	if v, ok := keys["ZHIPU_API_KEY"]; !ok || v == "" {
		t.Fatalf("MergedOpenCodeSubProviderKeys missing ZHIPU_API_KEY; got keys=%v", maskKeys(keys))
	}

	// Build the adapter the way the dispatcher does for a scoped call.
	sp, ok := opencodecliadapter.FindOpenCodeSubProvider("opencode-cli-glm")
	if !ok {
		t.Fatal("opencode-cli-glm not in catalog")
	}
	adapter := opencodecliadapter.NewOpenCodeCLIAdapterForSubProvider("", sp.DefaultModelID, sp, apiKey, &e2eMockLogger{})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	respLLM, err := adapter.GenerateContent(ctx, []llmtypes.MessageContent{
		{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{
			llmtypes.TextContent{Text: "Reply with exactly the word PINGPONG and nothing else."},
		}},
	})
	if err != nil {
		t.Fatalf("adapter GenerateContent: %v", err)
	}
	if len(respLLM.Choices) == 0 {
		t.Fatal("no choices returned")
	}
	content := strings.TrimSpace(respLLM.Choices[0].Content)
	if !strings.Contains(strings.ToUpper(content), "PINGPONG") {
		t.Fatalf("response %q does not contain PINGPONG; routing/tile likely wrong", content)
	}
	if respLLM.Choices[0].GenerationInfo == nil {
		t.Fatal("response missing GenerationInfo")
	}
	if got, _ := respLLM.Choices[0].GenerationInfo.Additional["provider"].(string); got != "opencode-cli" {
		t.Errorf("GenerationInfo.provider = %q, want opencode-cli", got)
	}

	t.Logf("✅ opencode-cli-glm live dispatch: model=%q content=%q", sp.DefaultModelID, content)
}

// maskKeys redacts values so a t.Logf doesn't dump real credentials.
func maskKeys(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if len(v) > 6 {
			out[k] = v[:3] + "***" + v[len(v)-3:]
		} else if v != "" {
			out[k] = "***"
		} else {
			out[k] = ""
		}
	}
	return out
}
